//! tauri-app library core — the integration seam between Plan 2
//! (cdp-driver / browser-launcher) and Plan 3 (mcp-server BrowserDriver).
//!
//! The binary target (`src/main.rs`) wraps this lib for the Tauri shell.

pub mod commands;
pub mod driver;
pub mod registry;

pub use driver::TauriBrowserDriver;
pub use registry::ProfileRegistry;

use std::path::PathBuf;
use std::sync::Arc;

use mcp_server::activity::ActivityLog;
use multizen_core::AppSettings;
use settings_store::{default_settings_path, SettingsStore};
use tauri::Manager;
use tokio::sync::Mutex;

use crate::commands::{
    activity::activity_recent,
    dialog::{dialog_pick_browser_binary, dialog_pick_directory},
    fingerprint::{
        fingerprint_devices, fingerprint_generate, fingerprint_locale_for_country,
        fingerprint_locales, fingerprint_reconcile,
    },
    profiles::{
        profiles_close, profiles_create, profiles_delete, profiles_get, profiles_launch,
        profiles_list, profiles_update,
    },
    proxy::proxy_detect_geo,
    settings::{settings_get, settings_update},
    system::system_info,
};

/// Process-wide shared state injected into Tauri via `Builder::manage`.
///
/// `SettingsStore::load`/`update` need `&mut self`, so it's wrapped in a
/// `tokio::sync::Mutex`. `mcp_token` is similarly guarded — P4.4 generates
/// it; until then it stays `None`. `TauriBrowserDriver` and `ActivityLog`
/// are interior-sync and can live behind a plain `Arc`.
pub struct AppState {
    pub driver: Arc<TauriBrowserDriver>,
    pub settings: Mutex<SettingsStore>,
    pub activity: Arc<ActivityLog>,
    pub mcp_token: Mutex<Option<String>>,
}

/// Resolve the on-disk paths for `profiles.db`, `profiles/`, and
/// `settings.json` from the Tauri app handle. Uses `app_local_data_dir`
/// (e.g. `%LOCALAPPDATA%\com.multizen.browser` on Windows) and falls back
/// to `app_config_dir` then the current working directory.
fn resolve_paths(app: &tauri::AppHandle) -> (PathBuf, PathBuf, PathBuf) {
    let base = app
        .path()
        .app_local_data_dir()
        .or_else(|_| app.path().app_config_dir())
        .unwrap_or_else(|_| std::env::current_dir().unwrap_or_default());
    let db_path = base.join("profiles.db");
    let profiles_root = base.join("profiles");
    let settings_path = default_settings_path(&base);
    (db_path, profiles_root, settings_path)
}

/// Fallback browser binary path when settings has none. Looks for
/// `MULTIZEN_BROWSER_BINARY` env var first, then a platform default. The
/// launcher will surface the real error if the binary is missing.
fn default_browser_binary() -> PathBuf {
    if let Ok(path) = std::env::var("MULTIZEN_BROWSER_BINARY") {
        return PathBuf::from(path);
    }
    #[cfg(target_os = "windows")]
    {
        PathBuf::from("cloakbrowser.exe")
    }
    #[cfg(target_os = "macos")]
    {
        PathBuf::from("/Applications/CloakBrowser.app/Contents/MacOS/CloakBrowser")
    }
    #[cfg(target_os = "linux")]
    {
        PathBuf::from("cloakbrowser")
    }
    #[cfg(not(any(target_os = "windows", target_os = "macos", target_os = "linux")))]
    {
        PathBuf::from("cloakbrowser")
    }
}

/// Build the `AppState` from the Tauri app handle: load settings,
/// construct the `TauriBrowserDriver` (spawns the dedicated launcher
/// thread), and allocate the shared `ActivityLog` + mcp token slot.
fn build_app_state(app: &tauri::AppHandle) -> AppState {
    let (db_path, profiles_root, settings_path) = resolve_paths(app);
    let mut store = SettingsStore::new(&settings_path);
    let settings: AppSettings = store.load().unwrap_or_default();
    let engine = settings.browser_engine;
    let browser_binary = settings
        .browser_binary_path
        .as_ref()
        .filter(|s| !s.trim().is_empty())
        .map(PathBuf::from)
        .unwrap_or_else(default_browser_binary);
    let companion_dir = None; // P4.4 will populate from app data dir.

    let registry = Arc::new(ProfileRegistry::new());
    let driver = TauriBrowserDriver::start(
        db_path,
        profiles_root,
        registry,
        engine,
        browser_binary,
        companion_dir,
    )
    .expect("TauriBrowserDriver::start");

    AppState {
        driver: Arc::new(driver),
        settings: Mutex::new(store),
        activity: Arc::new(ActivityLog::new()),
        mcp_token: Mutex::new(None),
    }
}

/// Build the Tauri app and run it. Lives in the lib crate (not the bin)
/// because `tauri::generate_handler!` expands to references to the
/// `__cmd__<name>` macros that `#[tauri::command]` generates in THIS
/// crate — keeping the handler list in the lib crate ensures the macro
/// can see those generated names.
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .setup(|app| {
            let state = build_app_state(app.handle());
            app.manage(state);
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            // profiles
            profiles_list,
            profiles_get,
            profiles_create,
            profiles_update,
            profiles_delete,
            profiles_launch,
            profiles_close,
            // settings
            settings_get,
            settings_update,
            // dialog
            dialog_pick_browser_binary,
            dialog_pick_directory,
            // fingerprint
            fingerprint_generate,
            fingerprint_devices,
            fingerprint_locales,
            fingerprint_reconcile,
            fingerprint_locale_for_country,
            // proxy
            proxy_detect_geo,
            // system
            system_info,
            // activity
            activity_recent,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
