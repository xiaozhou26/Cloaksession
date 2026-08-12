use std::collections::HashMap;
use std::sync::Mutex;

use multizen_core::BrowserEngine;

pub const SAFE_PAIRED_DISABLE_DOMAINS: &[&str] =
    &["Runtime", "Network", "DOM", "Accessibility", "Log", "Performance"];
pub const CLOAK_RISKY_ENABLE_DOMAINS: &[&str] = &["Runtime", "Network"];

pub struct SafeEnableRefcount {
    inner: Mutex<HashMap<String, u32>>,
}

impl SafeEnableRefcount {
    pub fn new() -> Self {
        Self { inner: Mutex::new(HashMap::new()) }
    }
    pub fn count(&self, domain: &str) -> u32 {
        *self.inner.lock().unwrap().get(domain).unwrap_or(&0)
    }
    /// True if this domain is not yet enabled (refcount == 0).
    pub fn should_enable(&self, domain: &str) -> bool {
        self.count(domain) == 0
    }
    /// True if a disable would bring refcount to 0 (i.e., current count == 1).
    pub fn should_disable(&self, domain: &str) -> bool {
        self.count(domain) == 1
    }
    pub fn enable(&self, domain: &str) {
        let mut m = self.inner.lock().unwrap();
        *m.entry(domain.to_string()).or_insert(0) += 1;
    }
    pub fn disable(&self, domain: &str) {
        let mut m = self.inner.lock().unwrap();
        if let Some(c) = m.get_mut(domain) {
            if *c > 0 {
                *c -= 1;
            }
        }
    }
}

impl Default for SafeEnableRefcount {
    fn default() -> Self { Self::new() }
}

/// CloakBrowser rejects Runtime/Network enables (a paired disable cannot
/// undo the DCHECK tripwire). CFT allows everything.
pub fn cloak_allows_domain(domain: &str, engine: BrowserEngine) -> bool {
    match engine {
        BrowserEngine::Cloakbrowser => !CLOAK_RISKY_ENABLE_DOMAINS.contains(&domain),
        BrowserEngine::Cft => true,
    }
}
