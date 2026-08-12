//! tauri-app library core — the integration seam between Plan 2
//! (cdp-driver / browser-launcher) and Plan 3 (mcp-server BrowserDriver).
//!
//! The binary target (`src/main.rs`) wraps this lib for the Tauri shell.

pub mod commands;
pub mod driver;
pub mod mcp_embed;
pub mod registry;
pub mod token;

pub use driver::TauriBrowserDriver;
pub use registry::ProfileRegistry;

use std::path::{Path, PathBuf};
use std::sync::Arc;

use mcp_server::activity::ActivityLog;
use multizen_core::AppSettings;
use settings_store::{default_settings_path, SettingsStore};
use tauri::{Emitter, Manager};
use tokio::sync::Mutex;

use crate::commands::{
    activity::activity_recent,
    dialog::{dialog_pick_browser_binary, dialog_pick_directory},
    extensions::{
        extensions_add_from_file, extensions_add_from_folder, extensions_add_from_web_store,
        extensions_icon, extensions_list, extensions_prepare_from_file,
        extensions_prepare_from_folder, extensions_prepare_from_web_store,
        extensions_remove, extensions_store_entries, extensions_toggle,
    },
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
fn resolve_paths(app: &tauri::AppHandle) -> (PathBuf, PathBuf, PathBuf, PathBuf) {
    let base = app
        .path()
        .app_local_data_dir()
        .or_else(|_| app.path().app_config_dir())
        .unwrap_or_else(|_| std::env::current_dir().unwrap_or_default());
    let db_path = base.join("profiles.db");
    let profiles_root = base.join("profiles");
    let extensions_root = base.join("extensions");
    let settings_path = default_settings_path(&base);
    (db_path, profiles_root, extensions_root, settings_path)
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
///
/// Returns the state plus the resolved data-dir path (used by callers
/// to locate the `mcp-token` file).
fn build_app_state(app: &tauri::AppHandle) -> (AppState, PathBuf) {
    let (db_path, profiles_root, extensions_root, settings_path) = resolve_paths(app);
    let data_dir = settings_path
        .parent()
        .map(Path::to_path_buf)
        .unwrap_or_else(|| std::env::current_dir().unwrap_or_default());
    let mut store = SettingsStore::new(&settings_path);
    let settings: AppSettings = match store.load() {
        Ok(s) => s,
        Err(e) => {
            tracing::warn!("settings load failed at {}: {e}; using defaults", settings_path.display());
            AppSettings::default()
        }
    };
    let engine = settings.browser_engine;
    let browser_binary = settings
        .browser_binary_path
        .as_ref()
        .filter(|s| !s.trim().is_empty())
        .map(PathBuf::from)
        .unwrap_or_else(default_browser_binary);
    let companion_dir = None;

    // Ensure the shared extensions directory exists so extension commands
    // can just write into `extensions_root/<ext_id>/`.
    std::fs::create_dir_all(&extensions_root).ok();

    let registry = Arc::new(ProfileRegistry::new());
    let driver = TauriBrowserDriver::start(
        db_path,
        profiles_root,
        extensions_root,
        registry,
        engine,
        browser_binary,
        companion_dir,
    )
    .expect("TauriBrowserDriver::start");

    let state = AppState {
        driver: Arc::new(driver),
        settings: Mutex::new(store),
        activity: Arc::new(ActivityLog::new()),
        mcp_token: Mutex::new(None),
    };
    (state, data_dir)
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
            let (state, data_dir) = build_app_state(app.handle());

            // Wire the driver's `AppHandle` so launch/close can emit
            // `profiles:running-changed` / `chromium:status` push events.
            state.driver.set_app(app.handle().clone());

            // Spawn a background task that bridges `ActivityLog`'s broadcast
            // stream to the Tauri frontend via `activity:event`. Every
            // `start_call` (pending) and `finish` (completed) event is emitted;
            // the frontend filters as needed. Broadcast lag is logged and
            // recovered (the next event resyncs the receiver).
            {
                let app_handle = app.handle().clone();
                let activity = state.activity.clone();
                tauri::async_runtime::spawn(async move {
                    let mut rx = activity.subscribe();
                    tracing::info!("activity:event bridge task started");
                    loop {
                        match rx.recv().await {
                            Ok(event) => {
                                if let Err(e) =
                                    app_handle.emit("activity:event", &event)
                                {
                                    tracing::warn!(
                                        error = %e,
                                        "emit activity:event failed"
                                    );
                                }
                            }
                            Err(tokio::sync::broadcast::error::RecvError::Lagged(n)) => {
                                tracing::warn!(
                                    skipped = n,
                                    "activity:event bridge lagged; resyncing"
                                );
                            }
                            Err(tokio::sync::broadcast::error::RecvError::Closed) => {
                                tracing::info!("activity:event bridge: sender closed; exiting");
                                break;
                            }
                        }
                    }
                });
            }

            // Load (or create) the MCP bearer token.
            match token::load_or_create_mcp_token(&data_dir) {
                Ok(tok) => {
                    tracing::info!("mcp token loaded from {}", data_dir.join("mcp-token").display());
                    // Decide whether to spawn the HTTP server.
                    let port = {
                        let mut store = state.settings.blocking_lock();
                        let settings = store.load().unwrap_or_default();
                        if settings.mcp_http_enabled {
                            Some(settings.mcp_http_port)
                        } else {
                            None
                        }
                    };
                    if let Some(port) = port {
                        mcp_embed::start_embedded_mcp(
                            port,
                            tok.clone(),
                            state.driver.clone(),
                            state.activity.clone(),
                        );
                        tracing::info!("embedded mcp http server requested on port {port}");
                    } else {
                        tracing::info!("mcp http disabled in settings; not spawning server");
                    }
                    *state.mcp_token.blocking_lock() = Some(tok);
                }
                Err(e) => {
                    tracing::warn!("failed to load/create mcp token at {}: {e}", data_dir.join("mcp-token").display());
                }
            }

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
            // extensions
            extensions_list,
            extensions_add_from_web_store,
            extensions_add_from_file,
            extensions_add_from_folder,
            extensions_remove,
            extensions_toggle,
            extensions_store_entries,
            extensions_prepare_from_web_store,
            extensions_prepare_from_file,
            extensions_prepare_from_folder,
            extensions_icon,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
