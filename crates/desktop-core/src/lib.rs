//! Headless desktop business runtime and stdio host protocol.

pub mod commands;
pub mod driver;
pub mod mcp_embed;
pub mod registry;
pub mod rpc;
pub mod runtime;
pub mod token;

pub use driver::DesktopBrowserDriver;
pub use registry::ProfileRegistry;

use std::path::{Path, PathBuf};
use std::sync::Arc;

use crate::runtime::AppHandle;
use mcp_server::activity::ActivityLog;
use multizen_core::{AppSettings, BrowserEngine};
use settings_store::{default_settings_path, SettingsStore};
use tokio::sync::Mutex;

/// Shared business state for desktop commands and embedded MCP.
pub struct AppState {
    pub driver: Arc<DesktopBrowserDriver>,
    pub settings: Mutex<SettingsStore>,
    pub activity: Arc<ActivityLog>,
    pub mcp_token: Mutex<Option<String>>,
    pub update: std::sync::Mutex<crate::commands::update::UpdateState>,
}

/// Keep the original desktop application's per-user data directory.
fn data_dir() -> PathBuf {
    if let Some(path) = std::env::var_os("CLOAKSESSION_DATA_DIR").filter(|p| !p.is_empty()) {
        return PathBuf::from(path);
    }
    #[cfg(not(target_os = "windows"))]
    let home = std::env::var_os("HOME").map(PathBuf::from);
    #[cfg(target_os = "windows")]
    let base = std::env::var_os("LOCALAPPDATA")
        .or_else(|| std::env::var_os("APPDATA"))
        .map(PathBuf::from);
    #[cfg(target_os = "macos")]
    let base = home.map(|p| p.join("Library/Application Support"));
    #[cfg(not(any(target_os = "windows", target_os = "macos")))]
    let base = std::env::var_os("XDG_DATA_HOME")
        .filter(|p| !p.is_empty())
        .map(PathBuf::from)
        .or_else(|| home.map(|p| p.join(".local/share")));
    base.unwrap_or_else(|| std::env::current_dir().unwrap_or_default())
        .join("com.cloaksession.browser")
}

fn resolve_paths() -> (PathBuf, PathBuf, PathBuf, PathBuf) {
    let base = data_dir();
    (
        base.join("profiles.db"),
        base.join("profiles"),
        base.join("extensions"),
        default_settings_path(&base),
    )
}

/// Fallback browser binary path when settings has none. Looks for
/// `MULTIZEN_BROWSER_BINARY` env var first, then a platform default. The
/// launcher will surface the real error if the binary is missing.
fn default_browser_binary(engine: BrowserEngine) -> PathBuf {
    if let Ok(path) = std::env::var("MULTIZEN_BROWSER_BINARY") {
        if !path.trim().is_empty() {
            return PathBuf::from(path);
        }
    }
    if engine == BrowserEngine::Chromix {
        return PathBuf::new();
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

fn chromix_runtime_dir() -> PathBuf {
    if let Some(path) = std::env::var_os("CLOAKSESSION_RESOURCE_DIR").filter(|p| !p.is_empty()) {
        return PathBuf::from(path).join("chromix");
    }
    if cfg!(debug_assertions) {
        return PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("resources/chromix");
    }
    let executable = std::env::current_exe().unwrap_or_default();
    let parent = executable.parent().unwrap_or(Path::new("."));
    #[cfg(target_os = "macos")]
    let resources = parent.join("../Resources");
    #[cfg(not(target_os = "macos"))]
    let resources = parent.join("resources");
    resources.join("chromix")
}

fn build_app_state() -> Result<(AppState, PathBuf), String> {
    let (db_path, profiles_root, extensions_root, settings_path) = resolve_paths();
    let data_dir = settings_path
        .parent()
        .map(Path::to_path_buf)
        .unwrap_or_else(|| std::env::current_dir().unwrap_or_default());
    let mut store = SettingsStore::new(&settings_path);
    let settings: AppSettings = match store.load() {
        Ok(s) => s,
        Err(e) => {
            tracing::warn!(
                "settings load failed at {}: {e}; using defaults",
                settings_path.display()
            );
            AppSettings::default()
        }
    };
    let engine = settings.browser_engine;
    let browser_binary = settings
        .browser_binary_path
        .as_ref()
        .filter(|s| !s.trim().is_empty())
        .map(PathBuf::from)
        .unwrap_or_else(|| default_browser_binary(engine));
    // Set up the companion extension (injects "Add to Cloaksession" button on
    // Chrome Web Store pages). Files are embedded at compile time and
    // written to the data dir on startup so they survive across launches.
    let companion_root = data_dir.join("companion");
    std::fs::create_dir_all(&companion_root).ok();
    let companion_manifest = include_str!("../resources/companion/manifest.json");
    let companion_cs = include_str!("../resources/companion/cs.js");
    let manifest_path = companion_root.join("manifest.json");
    let cs_path = companion_root.join("cs.js");
    // Only rewrite if content differs (avoid touching disk every launch).
    let needs_write = std::fs::read_to_string(&manifest_path).ok().as_deref()
        != Some(companion_manifest)
        || std::fs::read_to_string(&cs_path).ok().as_deref() != Some(companion_cs);
    if needs_write {
        std::fs::write(&manifest_path, companion_manifest).ok();
        std::fs::write(&cs_path, companion_cs).ok();
    }
    let companion_dir = Some(companion_root);

    // Ensure the shared extensions directory exists so extension commands
    // can just write into `extensions_root/<ext_id>/`.
    std::fs::create_dir_all(&extensions_root).ok();

    let registry = Arc::new(ProfileRegistry::new());
    let driver = DesktopBrowserDriver::start(
        db_path,
        profiles_root,
        extensions_root,
        registry,
        engine,
        browser_binary,
        companion_dir,
    )
    .map_err(|error| error.to_string())?
    .with_chromix(
        settings.chromix,
        chromix_runtime_dir(),
        settings.skip_browser_download,
    );

    let state = AppState {
        driver: Arc::new(driver),
        settings: Mutex::new(store),
        activity: Arc::new(ActivityLog::new()),
        mcp_token: Mutex::new(None),
        update: std::sync::Mutex::new(crate::commands::update::UpdateState::default()),
    };
    Ok((state, data_dir))
}

/// Probe geo for every profile that has a proxy but no cached country code.
/// Runs sequentially (don't hammer the upstream proxy). Emits a
/// `profiles:proxy-country-updated` event after each successful probe so the
/// renderer refetches and re-renders flag chips. Failures are non-fatal.
async fn backfill_proxy_countries(app: AppHandle, driver: Arc<DesktopBrowserDriver>) {
    let summaries = match driver.list_profiles().await {
        Ok(s) => s,
        Err(e) => {
            tracing::warn!(error = ?e, "backfill: list_profiles failed");
            return;
        }
    };
    for summary in summaries {
        // Skip profiles with no proxy or a cached country already.
        if summary.proxy.is_none() || summary.proxy_country.is_some() {
            continue;
        }
        let Some(proxy) = summary.proxy else { continue };
        let proxy_config = multizen_core::ProxyConfig {
            proxy_type: proxy.proxy_type,
            host: proxy.host,
            port: proxy.port,
            username: proxy.username,
            password: proxy.password,
        };
        match browser_launcher::proxy_geo::probe_proxy_geo(&proxy_config, 6_000).await {
            Ok(geo) => {
                let country = geo.country.to_lowercase();
                let _ = driver
                    .set_proxy_country(&summary.id, Some(country.clone()))
                    .await;
                let _ = app.emit(
                    "profiles:proxy-country-updated",
                    serde_json::json!({ "id": &summary.id, "country": country }),
                );
            }
            Err(e) => {
                tracing::debug!(error = ?e, profile = %summary.id, "backfill: probe failed");
            }
        }
    }
    tracing::info!("backfill_proxy_countries complete");
}

/// Delete extension directories in `extensions/` that no profile references.
/// Best-effort — errors are logged and swallowed.
async fn sweep_extension_orphans(driver: Arc<DesktopBrowserDriver>) {
    let extensions_root = driver.extensions_root().to_path_buf();

    // Get all referenced extension dirs from profiles.
    let referenced = match driver.store_entries().await {
        Ok(entries) => entries
            .into_iter()
            .filter_map(|e| {
                let p = PathBuf::from(&e.dir);
                p.file_name().map(|n| n.to_string_lossy().to_string())
            })
            .collect::<std::collections::HashSet<_>>(),
        Err(e) => {
            tracing::warn!(error = ?e, "orphan sweep: store_entries failed");
            return;
        }
    };

    // Scan the extensions directory and remove unreferenced dirs.
    let read_dir = match tokio::fs::read_dir(&extensions_root).await {
        Ok(d) => d,
        Err(_) => return, // dir doesn't exist yet — nothing to sweep.
    };

    let mut removed = 0u32;
    let mut entries = read_dir;
    while let Ok(Some(entry)) = entries.next_entry().await {
        let path = entry.path();
        if !path.is_dir() {
            continue;
        }
        let name = match path.file_name() {
            Some(n) => n.to_string_lossy().to_string(),
            None => continue,
        };
        // Skip temp dirs from interrupted unpacks.
        if name.ends_with(".tmp_unpacked") {
            let _ = tokio::fs::remove_dir_all(&path).await;
            removed += 1;
            continue;
        }
        if !referenced.contains(&name) {
            if let Err(e) = tokio::fs::remove_dir_all(&path).await {
                tracing::warn!(error = ?e, dir = %path.display(), "orphan sweep: remove failed");
            } else {
                removed += 1;
            }
        }
    }
    if removed > 0 {
        tracing::info!(removed, "extension orphan sweep reclaimed");
    }
}

/// Run the headless core until the host closes stdin or requests shutdown.
pub fn run() -> Result<(), String> {
    let _ = tracing_subscriber::fmt()
        .with_writer(std::io::stderr)
        .with_ansi(false)
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .try_init();
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .map_err(|error| error.to_string())?;
    let result = runtime.block_on(async {
        let app = AppHandle::new(std::io::stdout());
        let (state, data_dir) = build_app_state()?;
        let state = Arc::new(state);
        state.driver.set_app(app.clone());
        // Confirm the launcher's database is initialized before accepting commands.
        state
            .driver
            .list_profiles()
            .await
            .map_err(|error| error.to_string())?;
        start_background_tasks(app.clone(), state.clone(), &data_dir).await;
        let result = rpc::serve_stdio(app, state.clone()).await;
        let _ =
            tokio::time::timeout(std::time::Duration::from_secs(3), state.driver.shutdown()).await;
        result
    });
    runtime.shutdown_timeout(std::time::Duration::from_millis(250));
    result
}

async fn start_background_tasks(app: AppHandle, state: Arc<AppState>, data_dir: &Path) {
    let mut events = state.activity.subscribe();
    let activity_app = app.clone();
    tokio::spawn(async move {
        loop {
            match events.recv().await {
                Ok(event) => {
                    if let Err(error) = activity_app.emit("activity:event", &event) {
                        tracing::warn!(%error, "emit activity:event failed");
                    }
                }
                Err(tokio::sync::broadcast::error::RecvError::Lagged(skipped)) => {
                    tracing::warn!(skipped, "activity:event bridge lagged; resyncing");
                }
                Err(tokio::sync::broadcast::error::RecvError::Closed) => break,
            }
        }
    });

    match token::load_or_create_mcp_token(data_dir) {
        Ok(token) => {
            let settings = state.settings.lock().await.load().unwrap_or_default();
            if settings.mcp_http_enabled {
                mcp_embed::start_embedded_mcp(
                    settings.mcp_http_port,
                    token.clone(),
                    state.driver.clone(),
                    state.activity.clone(),
                );
            }
            *state.mcp_token.lock().await = Some(token);
        }
        Err(error) => tracing::warn!(%error, "failed to load/create MCP token"),
    }

    tokio::spawn(backfill_proxy_countries(app.clone(), state.driver.clone()));
    tokio::spawn(sweep_extension_orphans(state.driver.clone()));
    tokio::spawn(async move {
        tokio::time::sleep(std::time::Duration::from_secs(8)).await;
        let auto_update = state
            .settings
            .lock()
            .await
            .load()
            .unwrap_or_default()
            .auto_update;
        if auto_update {
            tracing::info!("auto-update: checking for updates");
            let _ = commands::update::update_check(app, &state).await;
        }
    });
}
