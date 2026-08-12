use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, Default, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "lowercase")]
pub enum BrowserEngine {
    Cft,
    #[default]
    Cloakbrowser,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AppSettings {
    pub theme: String,
    pub mcp_http_enabled: bool,
    pub mcp_http_port: u16,
    pub browser_engine: BrowserEngine,
    #[serde(default)]
    pub browser_binary_path: Option<String>,
    pub skip_browser_download: bool,
    pub auto_update: bool,
    pub usage_reporting: bool,
}

impl Default for AppSettings {
    fn default() -> Self {
        Self {
            theme: "dark".into(),
            mcp_http_enabled: true,
            mcp_http_port: 7777,
            browser_engine: BrowserEngine::Cloakbrowser,
            browser_binary_path: None,
            skip_browser_download: false,
            auto_update: true,
            usage_reporting: false,
        }
    }
}
