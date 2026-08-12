//! ProfileRegistry — tracks active `BrowserSession` handles per profile id.
//!
//! The registry owns an `Arc<Mutex<HashMap<ProfileId, Arc<BrowserSession>>>>`.
//! `get_or_connect` is idempotent: an existing session is reused; otherwise a
//! new `BrowserSession::connect` is performed and stored wrapped in `Arc` so
//! it can be shared with concurrent tool calls without being consumed.
//!
//! `remove` drops the `Arc<BrowserSession>`; the underlying CDP connection is
//! torn down when the last `Arc` is dropped. Callers that also want the
//! browser *process* killed must pair `remove` with
//! `BrowserLauncher::close`.

use std::collections::HashMap;
use std::sync::Arc;

use cdp_driver::session::BrowserSession;
use multizen_core::{BrowserEngine, Result};
use tokio::sync::Mutex;

pub type ProfileId = String;

pub struct ProfileRegistry {
    sessions: Arc<Mutex<HashMap<ProfileId, Arc<BrowserSession>>>>,
}

impl ProfileRegistry {
    pub fn new() -> Self {
        Self {
            sessions: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    /// Returns the existing session for `profile_id` if one is registered,
    /// otherwise connects a new `BrowserSession` to `endpoint` and registers
    /// it. The returned `Arc<BrowserSession>` can be held across await points
    /// and shared between concurrent tool invocations.
    pub async fn get_or_connect(
        &self,
        profile_id: &str,
        endpoint: &str,
        engine: BrowserEngine,
    ) -> Result<Arc<BrowserSession>> {
        if let Some(s) = self.sessions.lock().await.get(profile_id) {
            return Ok(s.clone());
        }
        let session = Arc::new(BrowserSession::connect(endpoint, engine).await?);
        self.sessions
            .lock()
            .await
            .insert(profile_id.to_string(), session.clone());
        Ok(session)
    }

    /// Returns the active session for `profile_id`, if registered. Cheap —
    /// bumps the `Arc` refcount, no allocation.
    pub async fn get(&self, profile_id: &str) -> Option<Arc<BrowserSession>> {
        self.sessions.lock().await.get(profile_id).cloned()
    }

    /// Drops the registered session for `profile_id` (if any). The CDP
    /// connection is closed when the last `Arc<BrowserSession>` is dropped.
    /// Does NOT kill the browser process — pair with `BrowserLauncher::close`
    /// for full teardown.
    pub async fn remove(&self, profile_id: &str) {
        self.sessions.lock().await.remove(profile_id);
    }

    /// Snapshot of currently registered profile ids.
    pub async fn ids(&self) -> Vec<String> {
        self.sessions.lock().await.keys().cloned().collect()
    }
}

impl Default for ProfileRegistry {
    fn default() -> Self {
        Self::new()
    }
}
