//! TauriBrowserDriver — `mcp_server::BrowserDriver` impl bridging Plan 2
//! (`cdp_driver::BrowserSession` + `browser_launcher::BrowserLauncher`) to
//! Plan 3 (`mcp_server::BrowserDriver` trait).
//!
//! # Design
//!
//! `BrowserLauncher` holds an `Arc<ProfileManager>` whose
//! `rusqlite::Connection` is `!Send + !Sync` (sqlite uses `RefCell`
//! internally), making `BrowserLauncher` itself `!Send + !Sync`. The
//! `BrowserDriver` trait requires `Self: Send + Sync`, so
//! `TauriBrowserDriver` CANNOT own a `BrowserLauncher` directly, wrap it in
//! `tokio::sync::Mutex` (which needs `T: Send`), or move it into
//! `tokio::task::spawn` / `std::thread::spawn` (both require `F: Send`).
//!
//! Resolution: a dedicated OS thread owns the `ProfileManager` + the
//! `BrowserLauncher` for their entire lifetime. The thread constructs both
//! from `db_path` + `profiles_root` (which are `Send`) inside its own
//! `current_thread` tokio runtime + `LocalSet`, then runs a command loop
//! over an `mpsc` channel. `TauriBrowserDriver` holds only the
//! `mpsc::Sender<LauncherCmd>` (which is `Send + Sync` regardless of the
//! inner type) plus a local sync `running` cache, so the driver itself is
//! `Send + Sync`. Each `BrowserDriver` method that needs the launcher sends
//! a command + a `oneshot::Sender` and awaits the reply.
//!
//! # Method resolutions
//!
//! | Trait method        | Resolution                                              |
//! |---------------------|---------------------------------------------------------|
//! | `launch`            | Send `LauncherCmd::Launch` to the launcher thread → receive `LaunchedProfile` → `registry.get_or_connect(endpoint, engine)`. Update `running` cache. |
//! | `close`             | `registry.remove` (drops CDP `Arc<BrowserSession>` → CDP closes) **then** `LauncherCmd::Close` (kills process). `BrowserSession::close(mut self)` is consuming and cannot be called through `Arc`, so the drop path is the intended teardown. |
//! | `is_running` (SYNC) | `BrowserLauncher::is_running_async` is async. The driver maintains a local `std::sync::Mutex<HashSet<ProfileId>>` updated by `launch`/`close`. Cache; can be stale if the process died externally. |
//! | `navigate`          | `registry.get` → `session.navigate(url, timeout_ms)` → returns `NavResult.url`. |
//! | `click`             | `session.click(selector)`. |
//! | `type_text`         | `session.type_text(selector, text)`. |
//! | `extract`           | `session.extract()`. |
//! | `screenshot`        | `session.screenshot()` (base64 PNG string). |
//! | `cdp_send`          | `BrowserSession` has no raw-CDP dispatch. Only `Runtime.evaluate` is supported — extract `expression` from `params`, delegate to `session.evaluate`. Other methods return `MultizenError::Mcp`. Full raw CDP dispatch deferred. |
//!
//! # Construction
//!
//! `TauriBrowserDriver::start(db_path, profiles_root, ...)` spawns the
//! dedicated thread and returns the driver. `browser_binary` and
//! `companion_dir` are stored on the driver (from `AppSettings`) because
//! `BrowserDriver::launch(&self, profile_id)` has no parameter for them.

use std::collections::HashSet;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex as StdMutex};

use async_trait::async_trait;
use cdp_driver::session::BrowserSession;
use mcp_server::driver::BrowserDriver;
use multizen_core::{
    BrowserEngine, CreateProfileInput, LaunchedProfile, MultizenError, Profile, ProfileSummary,
    Result, UpdateProfileInput,
};
use serde::Serialize;
use tauri::Emitter;
use tokio::sync::{mpsc, oneshot};

use crate::registry::ProfileRegistry;

const NAV_TIMEOUT_MS: u64 = 30_000;
const CMD_CHANNEL_SIZE: usize = 64;

/// Payload for the `profiles:running-changed` push event.
///
/// Discriminated union tagged on `kind`, matching the frontend
/// `RunningStateChange` type in `ui/src/types.ts`. Emitted from
/// `TauriBrowserDriver::launch` (with `kind: "launched"`) and
/// `TauriBrowserDriver::close` (with `kind: "closed"`) after the
/// operation succeeds. The frontend uses `change.kind` to drive its
/// running-indicator UI — `launched`/`closed` end any "Terminating…"
/// safety timer, `closing` would start one (we don't currently emit a
/// separate closing phase; the atomic `close()` path emits `closed`).
#[derive(Clone, Debug, Serialize)]
#[serde(tag = "kind", rename_all = "camelCase")]
pub enum RunningStateChange {
    /// Emitted after a successful launch — the profile is now running.
    Launched { profile_id: String },
    /// Reserved for a future non-atomic close flow where the process is
    /// winding down but hasn't fully exited. Not currently emitted.
    Closing { profile_id: String },
    /// Emitted after a successful close — the profile is no longer running.
    /// `reason: "user-close"` is the explicit close path; `"external-exit"`
    /// would be used if we ever detect an externally-killed process.
    Closed {
        profile_id: String,
        reason: &'static str,
    },
}

/// Payload for the `chromium:status` push event.
///
/// Emitted alongside `profiles:running-changed` to give the frontend a
/// finer-grained lifecycle signal: `started` on successful launch, `stopped`
/// after successful close, or `failed` with an error message when launch
/// fails (the process may still be left running; `close` is the recovery
/// path).
#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ChromiumStatus {
    pub profile_id: String,
    pub status: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// Commands sent to the dedicated launcher thread.
enum LauncherCmd {
    Launch {
        profile_id: String,
        binary: PathBuf,
        engine: BrowserEngine,
        companion: Option<PathBuf>,
        resp: oneshot::Sender<Result<LaunchedProfile>>,
    },
    Close {
        profile_id: String,
        resp: oneshot::Sender<Result<()>>,
    },
    /// Ask the launcher thread to shut down (runs `close_all` then exits).
    Shutdown,
    // --- Profile commands (P4.3) ---------------------------------------
    // ProfileManager lives on the launcher thread (its rusqlite Connection
    // is `!Send + !Sync`). These variants route synchronous pm calls onto
    // that thread; each carries a oneshot for the reply.
    ListProfiles {
        resp: oneshot::Sender<Result<Vec<ProfileSummary>>>,
    },
    GetProfile {
        id: String,
        resp: oneshot::Sender<Result<Option<Profile>>>,
    },
    CreateProfile {
        input: CreateProfileInput,
        resp: oneshot::Sender<Result<Profile>>,
    },
    UpdateProfile {
        id: String,
        patch: UpdateProfileInput,
        resp: oneshot::Sender<Result<Profile>>,
    },
    DeleteProfile {
        id: String,
        resp: oneshot::Sender<Result<()>>,
    },
    InsertImported {
        profile: multizen_core::Profile,
        resp: oneshot::Sender<Result<multizen_core::Profile>>,
    },
    // --- Extensions -----------------------------------------------------
    ListExtensions {
        id: String,
        resp: oneshot::Sender<Result<Vec<multizen_core::ExtensionConfig>>>,
    },
    SetExtensions {
        id: String,
        exts: Vec<multizen_core::ExtensionConfig>,
        resp: oneshot::Sender<Result<Vec<multizen_core::ExtensionConfig>>>,
    },
    StoreEntries {
        resp: oneshot::Sender<Result<Vec<multizen_core::ExtensionConfig>>>,
    },
    SetProxyCountry {
        id: String,
        country: Option<String>,
        resp: oneshot::Sender<Result<()>>,
    },
}

pub struct TauriBrowserDriver {
    /// Channel to the dedicated launcher thread. `mpsc::Sender` is
    /// `Send + Sync` regardless of the inner type, so this field keeps
    /// `TauriBrowserDriver: Send + Sync` even though `BrowserLauncher`
    /// itself is `!Send + !Sync`.
    launcher_tx: mpsc::Sender<LauncherCmd>,
    registry: Arc<ProfileRegistry>,
    engine: BrowserEngine,
    browser_binary: PathBuf,
    companion_dir: Option<PathBuf>,
    /// Shared extensions directory (`<data_dir>/extensions/`). Each
    /// extension is unpacked into `<extensions_root>/<ext_id>/` and
    /// referenced by `ExtensionConfig.dir` across profiles.
    extensions_root: PathBuf,
    /// Profiles root directory (`<data_dir>/profiles/`). Each profile's
    /// user data dir lives at `<profiles_root>/<profile_id>/`.
    profiles_root: PathBuf,
    /// Sync cache of profile ids believed to be running. Updated on
    /// `launch`/`close`. Used by the sync `is_running` trait method because
    /// `BrowserLauncher::is_running_async` cannot be awaited from a sync
    /// context.
    running: StdMutex<HashSet<String>>,
    /// Optional Tauri `AppHandle` used to emit push events
    /// (`profiles:running-changed`, `chromium:status`). Populated by
    /// `set_app` during `run()` setup. `None` in unit tests / before
    /// setup completes; emits are silently skipped in that case.
    app: StdMutex<Option<tauri::AppHandle>>,
}

impl TauriBrowserDriver {
    /// Spawn the dedicated launcher thread and return a driver wired to it.
    /// The thread constructs a `ProfileManager` from `db_path` +
    /// `profiles_root` and a `BrowserLauncher` wrapping it, then runs a
    /// `current_thread` tokio runtime + `LocalSet` command loop.
    pub fn start(
        db_path: PathBuf,
        profiles_root: PathBuf,
        extensions_root: PathBuf,
        registry: Arc<ProfileRegistry>,
        engine: BrowserEngine,
        browser_binary: PathBuf,
        companion_dir: Option<PathBuf>,
    ) -> Result<Self> {
        let (tx, rx) = mpsc::channel(CMD_CHANNEL_SIZE);
        let builder = std::thread::Builder::new().name("tauri-launcher".into());
        let profiles_root_for_thread = profiles_root.clone();
        let handle = builder
            .spawn(move || launcher_thread_main(db_path, profiles_root_for_thread, rx))
            .map_err(|e| MultizenError::Launch(format!("launcher thread spawn: {e}")))?;
        let _ = handle; // detached; exits on Shutdown or channel close
        Ok(Self {
            launcher_tx: tx,
            registry,
            engine,
            browser_binary,
            companion_dir,
            extensions_root,
            profiles_root,
            running: StdMutex::new(HashSet::new()),
            app: StdMutex::new(None),
        })
    }

    /// Get the shared extensions directory.
    pub fn extensions_root(&self) -> &Path {
        &self.extensions_root
    }

    /// Get the profile session registry (used by the companion poller to
    /// obtain the `BrowserSession` for CDP polling).
    pub fn registry(&self) -> &Arc<ProfileRegistry> {
        &self.registry
    }

    /// Profiles root directory (`<data_dir>/profiles/`). Used by archive
    /// import to compute the target data-dir path.
    pub fn profiles_root(&self) -> &Path {
        &self.profiles_root
    }

    /// Inject the Tauri `AppHandle` so `launch`/`close` can emit push events
    /// to the frontend. Called once from `run()`'s `setup` hook. Safe to
    /// call before or after the driver is `manage`d; the field is a
    /// `StdMutex<Option<_>>` and emits no-op when `None` (e.g. unit tests).
    pub fn set_app(&self, app: tauri::AppHandle) {
        *self.app.lock().unwrap() = Some(app);
    }

    /// Emit a push event. No-op when no `AppHandle` is set (unit tests,
    /// pre-setup). Errors from `emit` are logged at warn level and swallowed
    /// — push events are best-effort and must not break the launch/close
    /// path.
    fn emit<E: Serialize + Clone>(&self, event: &str, payload: &E) {
        let guard = self.app.lock().unwrap();
        if let Some(app) = guard.as_ref() {
            if let Err(e) = app.emit(event, payload) {
                tracing::warn!(event = event, error = %e, "tauri emit failed");
            }
        }
    }

    /// Best-effort graceful shutdown: tell the launcher thread to exit. The
    /// thread runs `close_all` on its `BrowserLauncher` before terminating.
    pub async fn shutdown(&self) {
        let _ = self.launcher_tx.send(LauncherCmd::Shutdown).await;
    }
}

/// The launcher thread entry point. Constructs `ProfileManager` +
/// `BrowserLauncher` on THIS thread (so the `!Send` sqlite Connection never
/// crosses threads), then runs the command loop on a `current_thread`
/// runtime inside a `LocalSet`.
fn launcher_thread_main(
    db_path: PathBuf,
    profiles_root: PathBuf,
    mut rx: mpsc::Receiver<LauncherCmd>,
) {
    let pm = match profile_manager::ProfileManager::new(&db_path, &profiles_root) {
        Ok(p) => p,
        Err(e) => {
            tracing::error!(error = ?e, "launcher thread: ProfileManager::new failed");
            return;
        }
    };
    // `BrowserLauncher::new` takes `Arc<ProfileManager>`. `ProfileManager`
    // is `!Sync` (sqlite Connection), so the `Arc` is `!Send + !Sync`, but
    // the launcher is single-threaded by construction (lives only on this
    // launcher thread). Suppress the clippy lint — same pattern as the
    // browser-launcher integration test.
    #[allow(clippy::arc_with_non_send_sync)]
    let pm_arc = Arc::new(pm);
    let launcher = browser_launcher::BrowserLauncher::new(pm_arc.clone());

    let rt = match tokio::runtime::Builder::new_current_thread().enable_all().build() {
        Ok(r) => r,
        Err(e) => {
            tracing::error!(error = ?e, "launcher thread: runtime build failed");
            return;
        }
    };
    let local = tokio::task::LocalSet::new();
    local.block_on(&rt, launcher_task(launcher, pm_arc, &mut rx));
}

/// Command loop run on the launcher thread's `LocalSet`. Owns the
/// `BrowserLauncher` and processes commands one at a time. Also owns an
/// `Arc<ProfileManager>` clone for synchronous profile CRUD (the pm lives
/// on this thread; commands are routed over the channel by
/// `TauriBrowserDriver`).
async fn launcher_task(
    launcher: browser_launcher::BrowserLauncher,
    pm: Arc<profile_manager::ProfileManager>,
    rx: &mut mpsc::Receiver<LauncherCmd>,
) {
    while let Some(cmd) = rx.recv().await {
        match cmd {
            LauncherCmd::Launch {
                profile_id,
                binary,
                engine,
                companion,
                resp,
            } => {
                let result = launcher
                    .launch(&profile_id, &binary, engine, companion.as_deref())
                    .await;
                let _ = resp.send(result);
            }
            LauncherCmd::Close { profile_id, resp } => {
                let result = launcher.close(&profile_id).await;
                let _ = resp.send(result);
            }
            LauncherCmd::Shutdown => {
                launcher.close_all().await;
                break;
            }
            LauncherCmd::ListProfiles { resp } => {
                let _ = resp.send(pm.list());
            }
            LauncherCmd::GetProfile { id, resp } => {
                let _ = resp.send(pm.get(&id));
            }
            LauncherCmd::CreateProfile { input, resp } => {
                let _ = resp.send(pm.create(input));
            }
            LauncherCmd::UpdateProfile { id, patch, resp } => {
                let _ = resp.send(pm.update(&id, patch));
            }
            LauncherCmd::DeleteProfile { id, resp } => {
                let _ = resp.send(pm.delete(&id));
            }
            LauncherCmd::InsertImported { profile, resp } => {
                let _ = resp.send(pm.insert_imported(profile));
            }
            LauncherCmd::ListExtensions { id, resp } => {
                let _ = resp.send(pm.get(&id).map(|opt| {
                    opt.and_then(|p| p.extensions).unwrap_or_default()
                }));
            }
            LauncherCmd::SetExtensions { id, exts, resp } => {
                let result = pm.update(&id, UpdateProfileInput {
                    extensions: Some(exts),
                    ..Default::default()
                }).map(|p| p.extensions.unwrap_or_default());
                let _ = resp.send(result);
            }
            LauncherCmd::StoreEntries { resp } => {
                let result = pm.all_extension_refs().map(|refs| {
                    let mut seen = std::collections::HashSet::new();
                    let mut out = Vec::new();
                    for r in refs {
                        if seen.insert(r.ext.id.clone()) {
                            out.push(r.ext);
                        }
                    }
                    out
                });
                let _ = resp.send(result);
            }
            LauncherCmd::SetProxyCountry { id, country, resp } => {
                let _ = resp.send(pm.set_proxy_country(&id, country.as_deref()));
            }
        }
    }
}

#[async_trait]
impl BrowserDriver for TauriBrowserDriver {
    async fn launch(&self, profile_id: &str) -> Result<LaunchedProfile> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::Launch {
                profile_id: profile_id.to_string(),
                binary: self.browser_binary.clone(),
                engine: self.engine,
                companion: self.companion_dir.clone(),
                resp: resp_tx,
            })
            .await
            .map_err(|e| {
                // Launch channel failure → emit chromium:status failed before
                // propagating the error.
                self.emit(
                    "chromium:status",
                    &ChromiumStatus {
                        profile_id: profile_id.to_string(),
                        status: "failed".into(),
                        error: Some(e.to_string()),
                    },
                );
                MultizenError::Mcp("launcher thread closed".into())
            })?;
        let launched = match resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))
        {
            Ok(r) => match r {
                Ok(l) => l,
                Err(e) => {
                    self.emit(
                        "chromium:status",
                        &ChromiumStatus {
                            profile_id: profile_id.to_string(),
                            status: "failed".into(),
                            error: Some(e.to_string()),
                        },
                    );
                    return Err(e);
                }
            },
            Err(e) => {
                self.emit(
                    "chromium:status",
                    &ChromiumStatus {
                        profile_id: profile_id.to_string(),
                        status: "failed".into(),
                        error: Some(e.to_string()),
                    },
                );
                return Err(e);
            }
        };

        // Connect a BrowserSession to the freshly-launched CDP endpoint and
        // register it. If the connect fails the process is left running; the
        // caller can retry `launch` (idempotent on the launcher side) or
        // `close` to clean up. We do NOT roll back the launch here because the
        // launcher may have already marked the profile opened and stored the
        // handle — propagating the connect error keeps the system state
        // inspectable.
        let session = match self
            .registry
            .get_or_connect(profile_id, &launched.cdp_endpoint, self.engine)
            .await
        {
            Ok(session) => session,
            Err(e) => {
                self.emit(
                    "chromium:status",
                    &ChromiumStatus {
                        profile_id: profile_id.to_string(),
                        status: "failed".into(),
                        error: Some(e.to_string()),
                    },
                );
                return Err(e);
            }
        };

        // Apply CDP bootstrap (fingerprint preload, UA override, locale) so
        // the persistent fingerprint actually reaches the browser runtime.
        // Without this the preload script and UA/Accept-Language overrides
        // defined in cdp-driver were never executed after launch.
        let profile = self
            .get_profile(profile_id)
            .await?
            .ok_or_else(|| MultizenError::NotFound(profile_id.to_string()))?;
        if let Err(e) =
            cdp_driver::bootstrap::bootstrap_targets(session.as_ref(), &profile.fingerprint, self.engine, None)
                .await
        {
            self.emit(
                "chromium:status",
                &ChromiumStatus {
                    profile_id: profile_id.to_string(),
                    status: "failed".into(),
                    error: Some(e.to_string()),
                },
            );
            return Err(e);
        }

        self.running.lock().unwrap().insert(profile_id.to_string());
        // Success → notify frontend. `profiles:running-changed` carries the
        // authoritative running state; `chromium:status` carries the
        // lifecycle signal.
        self.emit(
            "profiles:running-changed",
            &RunningStateChange::Launched {
                profile_id: profile_id.to_string(),
            },
        );
        self.emit(
            "chromium:status",
            &ChromiumStatus {
                profile_id: profile_id.to_string(),
                status: "started".into(),
                error: None,
            },
        );
        Ok(launched)
    }

    async fn close(&self, profile_id: &str) -> Result<()> {
        // 1. Drop the CDP session (Arc<BrowserSession>). When the last Arc
        //    goes away the chromiumoxide Browser + its CDP connection drop,
        //    closing the WebSocket. `BrowserSession::close(mut self)` is
        //    consuming and cannot be called through Arc, so the drop path is
        //    the intended teardown for shared sessions.
        self.registry.remove(profile_id).await;
        // 2. Kill the browser process (also stops the socks5 bridge) via the
        //    dedicated launcher thread.
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::Close {
                profile_id: profile_id.to_string(),
                resp: resp_tx,
            })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))??;
        self.running.lock().unwrap().remove(profile_id);
        // Success → notify frontend that the profile is no longer running
        // and the chromium process has stopped.
        self.emit(
            "profiles:running-changed",
            &RunningStateChange::Closed {
                profile_id: profile_id.to_string(),
                reason: "user-close",
            },
        );
        self.emit(
            "chromium:status",
            &ChromiumStatus {
                profile_id: profile_id.to_string(),
                status: "stopped".into(),
                error: None,
            },
        );
        Ok(())
    }

    fn is_running(&self, profile_id: &str) -> bool {
        // Sync trait method — cannot await launcher.is_running_async.
        // Use the local cache. Stale if the process died externally; callers
        // that need authoritative state should invoke a follow-up async
        // health check (deferred — see task P4.8).
        self.running.lock().unwrap().contains(profile_id)
    }

    async fn navigate(&self, profile_id: &str, url: &str) -> Result<String> {
        let session = self.require_session(profile_id).await?;
        let nav = session.navigate(url, NAV_TIMEOUT_MS).await?;
        Ok(nav.url)
    }

    async fn click(&self, profile_id: &str, selector: &str) -> Result<()> {
        let session = self.require_session(profile_id).await?;
        session.click(selector).await
    }

    async fn type_text(&self, profile_id: &str, selector: &str, text: &str) -> Result<()> {
        let session = self.require_session(profile_id).await?;
        session.type_text(selector, text).await
    }

    async fn extract(&self, profile_id: &str) -> Result<serde_json::Value> {
        let session = self.require_session(profile_id).await?;
        session.extract().await
    }

    async fn screenshot(&self, profile_id: &str) -> Result<String> {
        let session = self.require_session(profile_id).await?;
        session.screenshot().await
    }

    async fn cdp_send(
        &self,
        profile_id: &str,
        method: &str,
        params: Option<serde_json::Value>,
        session_id: Option<&str>,
        _safe: bool,
    ) -> Result<serde_json::Value> {
        let session = self.require_session(profile_id).await?;
        session.cdp_send(method, params, session_id).await

    }
}

impl TauriBrowserDriver {
    /// Fetch the active session for `profile_id` or return an error. Does NOT
    /// auto-launch — `launch` is the explicit entry point. If a caller
    /// invokes a tool method before `launch`, that's a usage error.
    async fn require_session(&self, profile_id: &str) -> Result<Arc<BrowserSession>> {
        self.registry.get(profile_id).await.ok_or_else(|| {
            MultizenError::Mcp(format!(
                "no active browser session for profile `{profile_id}`; \
                 call launch first"
            ))
        })
    }

    // --- Profile CRUD (P4.3) -------------------------------------------
    // Each method sends a `LauncherCmd` variant + oneshot to the launcher
    // thread, where `ProfileManager` lives. The pm methods are synchronous;
    // they execute on the launcher thread and reply via the oneshot.

    pub async fn list_profiles(&self) -> Result<Vec<ProfileSummary>> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::ListProfiles { resp: resp_tx })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))?
    }

    pub async fn get_profile(&self, id: &str) -> Result<Option<Profile>> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::GetProfile {
                id: id.to_string(),
                resp: resp_tx,
            })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))?
    }

    pub async fn activate_tab(&self, profile_id: &str, tab_id: &str) -> Result<()> {
        let session = self.require_session(profile_id).await?;
        session.activate_page(tab_id).await
    }

    pub async fn new_tab(&self, profile_id: &str, url: &str) -> Result<String> {
        let session = self.require_session(profile_id).await?;
        session.new_page(url).await
    }

    pub async fn close_tab(&self, profile_id: &str, tab_id: &str) -> Result<()> {
        let session = self.require_session(profile_id).await?;
        session.close_page(tab_id).await
    }

    pub async fn create_profile(&self, input: CreateProfileInput) -> Result<Profile> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::CreateProfile {
                input,
                resp: resp_tx,
            })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))?
    }

    pub async fn update_profile(
        &self,
        id: &str,
        patch: UpdateProfileInput,
    ) -> Result<Profile> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::UpdateProfile {
                id: id.to_string(),
                patch,
                resp: resp_tx,
            })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))?
    }

    pub async fn delete_profile(&self, id: &str) -> Result<()> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::DeleteProfile {
                id: id.to_string(),
                resp: resp_tx,
            })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))?
    }

    pub async fn insert_imported(&self, profile: multizen_core::Profile) -> Result<multizen_core::Profile> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::InsertImported {
                profile,
                resp: resp_tx,
            })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))?
    }

    // --- Extensions -----------------------------------------------------

    pub async fn list_extensions(&self, id: &str) -> Result<Vec<multizen_core::ExtensionConfig>> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::ListExtensions {
                id: id.to_string(),
                resp: resp_tx,
            })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))?
    }

    pub async fn set_extensions(
        &self,
        id: &str,
        exts: Vec<multizen_core::ExtensionConfig>,
    ) -> Result<Vec<multizen_core::ExtensionConfig>> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::SetExtensions {
                id: id.to_string(),
                exts,
                resp: resp_tx,
            })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))?
    }

    pub async fn store_entries(&self) -> Result<Vec<multizen_core::ExtensionConfig>> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::StoreEntries { resp: resp_tx })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))?
    }

    pub async fn set_proxy_country(
        &self,
        id: &str,
        country: Option<String>,
    ) -> Result<()> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.launcher_tx
            .send(LauncherCmd::SetProxyCountry {
                id: id.to_string(),
                country,
                resp: resp_tx,
            })
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))?
    }
}
