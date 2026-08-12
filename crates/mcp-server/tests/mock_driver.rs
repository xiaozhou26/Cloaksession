//! Shared `MockBrowserDriver` for mcp-server tool tests.
//!
//! Intended to be pulled into per-tool integration tests via
//! `include!` or `mod mock_driver;` from the crate's `tests/` directory.
//! Implements the `BrowserDriver` trait with canned successes so tests can
//! exercise tool wiring without a real Chromium.

use async_trait::async_trait;
use mcp_server::driver::BrowserDriver;
use multizen_core::{LaunchedProfile, Result};
use std::collections::HashMap;
use std::sync::Mutex;

pub struct MockBrowserDriver {
    pub running: Mutex<HashMap<String, LaunchedProfile>>,
}

impl Default for MockBrowserDriver {
    fn default() -> Self {
        Self::new()
    }
}

impl MockBrowserDriver {
    pub fn new() -> Self {
        Self {
            running: Mutex::new(HashMap::new()),
        }
    }
}

#[async_trait]
impl BrowserDriver for MockBrowserDriver {
    async fn launch(&self, profile_id: &str) -> Result<LaunchedProfile> {
        let launched = LaunchedProfile {
            id: profile_id.into(),
            cdp_endpoint: "http://127.0.0.1:9".into(),
            pid: 1,
            started_at: "2026-01-01T00:00:00Z".into(),
        };
        self.running
            .lock()
            .unwrap()
            .insert(profile_id.into(), launched.clone());
        Ok(launched)
    }

    async fn close(&self, profile_id: &str) -> Result<()> {
        self.running.lock().unwrap().remove(profile_id);
        Ok(())
    }

    /// NOTE: sync per trait contract (not async). The task brief showed this
    /// as `async fn`; corrected to match `BrowserDriver::is_running`.
    fn is_running(&self, profile_id: &str) -> bool {
        self.running.lock().unwrap().contains_key(profile_id)
    }

    async fn navigate(&self, _profile_id: &str, url: &str) -> Result<String> {
        Ok(url.into())
    }

    async fn click(&self, _profile_id: &str, _selector: &str) -> Result<()> {
        Ok(())
    }

    async fn type_text(&self, _profile_id: &str, _selector: &str, _text: &str) -> Result<()> {
        Ok(())
    }

    async fn extract(&self, _profile_id: &str) -> Result<serde_json::Value> {
        Ok(serde_json::json!({ "text": "" }))
    }

    async fn screenshot(&self, _profile_id: &str) -> Result<String> {
        Ok("b64".into())
    }

    async fn cdp_send(
        &self,
        _profile_id: &str,
        _method: &str,
        _params: Option<serde_json::Value>,
        _session_id: Option<&str>,
        _safe: bool,
    ) -> Result<serde_json::Value> {
        Ok(serde_json::json!({}))
    }
}
