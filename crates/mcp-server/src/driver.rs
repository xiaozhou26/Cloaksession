use async_trait::async_trait;
use multizen_core::{LaunchedProfile, Result};

#[async_trait]
pub trait BrowserDriver: Send + Sync {
    async fn launch(&self, profile_id: &str) -> Result<LaunchedProfile>;
    async fn close(&self, profile_id: &str) -> Result<()>;
    fn is_running(&self, profile_id: &str) -> bool;
    async fn navigate(&self, profile_id: &str, url: &str) -> Result<String>;
    async fn click(&self, profile_id: &str, selector: &str) -> Result<()>;
    async fn type_text(&self, profile_id: &str, selector: &str, text: &str) -> Result<()>;
    async fn extract(&self, profile_id: &str) -> Result<serde_json::Value>;
    async fn screenshot(&self, profile_id: &str) -> Result<String>;
    async fn cdp_send(
        &self,
        profile_id: &str,
        method: &str,
        params: Option<serde_json::Value>,
        session_id: Option<&str>,
        safe: bool,
    ) -> Result<serde_json::Value>;
}
