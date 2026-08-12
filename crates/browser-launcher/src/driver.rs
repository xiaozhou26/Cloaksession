use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU16, Ordering};
use std::sync::Arc;

use multizen_core::{BrowserEngine, LaunchedProfile, MultizenError, Result};
use profile_manager::ProfileManager;
use tokio::process::{Child, Command};

use crate::args::build_spawn_args;
use crate::proxy_geo::probe_proxy_geo;
use crate::registry::RunningRegistry;
use crate::session_restore::{clean_stale_singleton_locks, ensure_session_restore};
use crate::socks5_bridge::Socks5Bridge;
use crate::version::detect_chromium_version;

const CDP_PORT_BASE: u16 = 9222;

pub struct BrowserHandle {
    pub profile_id: String,
    pub cdp_endpoint: String,
    pub pid: u32,
    pub started_at: String,
    child: Option<Child>,
    bridge: Option<Socks5Bridge>,
}

impl BrowserHandle {
    pub fn endpoint_info(&self) -> (String, String, u32) {
        (self.profile_id.clone(), self.cdp_endpoint.clone(), self.pid)
    }
}

pub struct BrowserLauncher {
    pm: Arc<ProfileManager>,
    registry: RunningRegistry,
    next_port: AtomicU16,
}

impl BrowserLauncher {
    pub fn new(pm: Arc<ProfileManager>) -> Self {
        Self {
            pm,
            registry: RunningRegistry::new(),
            next_port: AtomicU16::new(CDP_PORT_BASE),
        }
    }

    pub async fn is_running_async(&self, profile_id: &str) -> bool {
        self.registry.contains(profile_id).await
    }

    pub async fn launch(
        &self,
        profile_id: &str,
        binary_path: &Path,
        engine: BrowserEngine,
        companion_dir: Option<&Path>,
    ) -> Result<LaunchedProfile> {
        // 1. Idempotent: if already running, return the existing endpoint info.
        if self.registry.contains(profile_id).await {
            return self
                .registry
                .with(profile_id, |h| LaunchedProfile {
                    id: h.profile_id.clone(),
                    cdp_endpoint: h.cdp_endpoint.clone(),
                    pid: h.pid,
                    started_at: h.started_at.clone(),
                })
                .await
                .ok_or_else(|| MultizenError::Launch("lost handle".into()));
        }

        // 2. Load profile + mark opened.
        let profile = self
            .pm
            .get(profile_id)
            .map_err(|e| MultizenError::Launch(format!("profile get: {e}")))?
            .ok_or_else(|| MultizenError::NotFound(profile_id.to_string()))?;
        self.pm
            .mark_opened(profile_id)
            .map_err(|e| MultizenError::Launch(format!("mark_opened: {e}")))?;

        // 3. Allocate CDP port.
        let cdp_port = self.next_port.fetch_add(1, Ordering::SeqCst);

        // 4. Compute browser data dir.
        let browser_data_dir: PathBuf = match engine {
            BrowserEngine::Cloakbrowser => PathBuf::from(&profile.data_dir)
                .join("engines")
                .join("cloakbrowser"),
            BrowserEngine::Cft => PathBuf::from(&profile.data_dir),
        };
        std::fs::create_dir_all(&browser_data_dir)
            .map_err(|e| MultizenError::Launch(format!("data_dir: {e}")))?;

        // 5. Version probe (best-effort, not used for UA rewrite in this plan).
        let _version = detect_chromium_version(binary_path).await;

        // 6. Proxy: start socks5 bridge + geo probe (best-effort).
        let mut bridge_handle: Option<(Socks5Bridge, u16)> = None;
        let mut geo_coords: Option<(f64, f64)> = None;
        if let Some(proxy) = &profile.proxy {
            let (bridge, local_port) = Socks5Bridge::start(proxy.clone()).await?;
            bridge_handle = Some((bridge, local_port));
            if let Ok(geo) = probe_proxy_geo(proxy, 4000).await {
                if let (Some(lat), Some(lon)) = (geo.latitude, geo.longitude) {
                    geo_coords = Some((lat, lon));
                }
                let _ = self.pm.set_proxy_country(profile_id, Some(&geo.country));
            }
        }

        // 7. Session restore + singleton cleanup.
        ensure_session_restore(&browser_data_dir)?;
        clean_stale_singleton_locks(&browser_data_dir);

        // 8. Build spawn args.
        let bridge_url_str = bridge_handle
            .as_ref()
            .map(|(_, p)| format!("socks5://127.0.0.1:{p}"));
        let browser_data_dir_str = browser_data_dir.to_string_lossy().to_string();
        let companion_dir_str = companion_dir.map(|p| p.to_string_lossy().to_string());
        let args = build_spawn_args(
            &profile,
            engine,
            cdp_port,
            &browser_data_dir_str,
            bridge_url_str.as_deref(),
            geo_coords,
            companion_dir_str.as_deref(),
        );

        // 9. Spawn.
        let mut cmd = Command::new(binary_path);
        cmd.args(&args);
        let child = cmd
            .spawn()
            .map_err(|e| MultizenError::Launch(format!("spawn: {e}")))?;
        let pid = child.id().unwrap_or(0);
        let started_at = chrono::Utc::now().to_rfc3339();
        let cdp_endpoint = format!("http://127.0.0.1:{cdp_port}");

        // 10. Store handle.
        let handle = BrowserHandle {
            profile_id: profile_id.to_string(),
            cdp_endpoint: cdp_endpoint.clone(),
            pid,
            started_at: started_at.clone(),
            child: Some(child),
            bridge: bridge_handle.map(|(b, _)| b),
        };
        self.registry.insert(handle).await;

        // 11. Return launched profile info.
        Ok(LaunchedProfile {
            id: profile_id.to_string(),
            cdp_endpoint,
            pid,
            started_at,
        })
    }

    pub async fn close(&self, profile_id: &str) -> Result<()> {
        let mut handle = match self.registry.remove(profile_id).await {
            Some(h) => h,
            None => return Ok(()),
        };
        // Stop bridge first (cuts network traffic).
        if let Some(bridge) = handle.bridge.take() {
            let _ = bridge.stop().await;
        }
        // Graceful shutdown: SIGTERM → wait 2s → SIGKILL → wait 2s.
        if let Some(mut child) = handle.child.take() {
            let _ = child.start_kill();
            let _ =
                tokio::time::timeout(std::time::Duration::from_millis(2000), child.wait()).await;
            if child.try_wait().ok().flatten().is_none() {
                let _ = child.kill().await;
                let _ =
                    tokio::time::timeout(std::time::Duration::from_millis(2000), child.wait()).await;
            }
        }
        Ok(())
    }

    pub async fn close_all(&self) {
        let ids = self.registry.ids().await;
        for id in ids {
            let _ = self.close(&id).await;
        }
    }
}
