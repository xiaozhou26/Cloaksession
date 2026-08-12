use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::Mutex;

use crate::driver::BrowserHandle;

pub struct RunningRegistry {
    inner: Arc<Mutex<HashMap<String, BrowserHandle>>>,
}

impl RunningRegistry {
    pub fn new() -> Self {
        Self {
            inner: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    /// Returns the endpoint info tuple `(profile_id, cdp_endpoint, pid)` for
    /// the running profile, if present. `BrowserHandle` itself is not `Clone`
    /// (it owns a `tokio::process::Child`), so callers that need the handle
    /// must use [`with`](Self::with) which holds the lock.
    pub async fn get(&self, profile_id: &str) -> Option<(String, String, u32)> {
        let guard = self.inner.lock().await;
        guard.get(profile_id).map(|h| h.endpoint_info())
    }

    pub async fn with<F, R>(&self, profile_id: &str, f: F) -> Option<R>
    where
        F: FnOnce(&BrowserHandle) -> R,
    {
        let guard = self.inner.lock().await;
        guard.get(profile_id).map(f)
    }

    pub async fn insert(&self, handle: BrowserHandle) {
        let id = handle.profile_id.clone();
        self.inner.lock().await.insert(id, handle);
    }

    pub async fn remove(&self, profile_id: &str) -> Option<BrowserHandle> {
        self.inner.lock().await.remove(profile_id)
    }

    pub async fn contains(&self, profile_id: &str) -> bool {
        self.inner.lock().await.contains_key(profile_id)
    }

    pub async fn ids(&self) -> Vec<String> {
        self.inner.lock().await.keys().cloned().collect()
    }
}

impl Default for RunningRegistry {
    fn default() -> Self {
        Self::new()
    }
}
