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
use std::path::PathBuf;
use std::sync::{Arc, Mutex as StdMutex};

use async_trait::async_trait;
use cdp_driver::session::BrowserSession;
use mcp_server::driver::BrowserDriver;
use multizen_core::{
    BrowserEngine, CreateProfileInput, LaunchedProfile, MultizenError, Profile, ProfileSummary,
    Result, UpdateProfileInput,
};
use tokio::sync::{mpsc, oneshot};

use crate::registry::ProfileRegistry;

const NAV_TIMEOUT_MS: u64 = 30_000;
const CMD_CHANNEL_SIZE: usize = 64;

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
    /// Sync cache of profile ids believed to be running. Updated on
    /// `launch`/`close`. Used by the sync `is_running` trait method because
    /// `BrowserLauncher::is_running_async` cannot be awaited from a sync
    /// context.
    running: StdMutex<HashSet<String>>,
}

impl TauriBrowserDriver {
    /// Spawn the dedicated launcher thread and return a driver wired to it.
    /// The thread constructs a `ProfileManager` from `db_path` +
    /// `profiles_root` and a `BrowserLauncher` wrapping it, then runs a
    /// `current_thread` tokio runtime + `LocalSet` command loop.
    pub fn start(
        db_path: PathBuf,
        profiles_root: PathBuf,
        registry: Arc<ProfileRegistry>,
        engine: BrowserEngine,
        browser_binary: PathBuf,
        companion_dir: Option<PathBuf>,
    ) -> Result<Self> {
        let (tx, rx) = mpsc::channel(CMD_CHANNEL_SIZE);
        let builder = std::thread::Builder::new().name("tauri-launcher".into());
        let handle = builder
            .spawn(move || launcher_thread_main(db_path, profiles_root, rx))
            .map_err(|e| MultizenError::Launch(format!("launcher thread spawn: {e}")))?;
        let _ = handle; // detached; exits on Shutdown or channel close
        Ok(Self {
            launcher_tx: tx,
            registry,
            engine,
            browser_binary,
            companion_dir,
            running: StdMutex::new(HashSet::new()),
        })
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
            .map_err(|_| MultizenError::Mcp("launcher thread closed".into()))?;
        let launched = resp_rx
            .await
            .map_err(|_| MultizenError::Mcp("launcher thread dropped response".into()))??;

        // Connect a BrowserSession to the freshly-launched CDP endpoint and
        // register it. If the connect fails the process is left running; the
        // caller can retry `launch` (idempotent on the launcher side) or
        // `close` to clean up. We do NOT roll back the launch here because the
        // launcher may have already marked the profile opened and stored the
        // handle — propagating the connect error keeps the system state
        // inspectable.
        self.registry
            .get_or_connect(profile_id, &launched.cdp_endpoint, self.engine)
            .await?;

        self.running.lock().unwrap().insert(profile_id.to_string());
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
        _session_id: Option<&str>,
        _safe: bool,
    ) -> Result<serde_json::Value> {
        // BrowserSession exposes only Runtime.evaluate (via `evaluate`); it
        // does not expose a generic raw-CDP dispatch. Support the one method
        // Plan 3's cdp_send tool uses most — Runtime.evaluate — by extracting
        // the `expression` field from params. Reject everything else with a
        // clear Mcp error so callers know to use a specific tool method
        // (navigate/click/type_text/extract/screenshot) instead.
        if method == "Runtime.evaluate" {
            let session = self.require_session(profile_id).await?;
            let expression = params
                .as_ref()
                .and_then(|p| p.get("expression"))
                .and_then(|v| v.as_str())
                .ok_or_else(|| {
                    MultizenError::Mcp(
                        "cdp_send Runtime.evaluate requires `expression` string in params".into(),
                    )
                })?;
            return session.evaluate(expression).await;
        }
        Err(MultizenError::Mcp(format!(
            "cdp_send not supported via TauriBrowserDriver for method `{method}`; \
             use a specific tool method (navigate/click/type_text/extract/screenshot) instead. \
             Only `Runtime.evaluate` is dispatched (delegated to BrowserSession::evaluate)."
        )))
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
}
