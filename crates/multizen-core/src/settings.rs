use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, Default, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "lowercase")]
pub enum BrowserEngine {
    Cft,
    #[default]
    Cloakbrowser,
    Chromix,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct ChromixSettings {
    pub node_path: String,
    pub options: serde_json::Map<String, serde_json::Value>,
    pub environment: std::collections::BTreeMap<String, String>,
}

impl ChromixSettings {
    pub fn with_profile_options(
        &self,
        options: &serde_json::Map<String, serde_json::Value>,
    ) -> Self {
        let mut config = self.clone();
        config.options.extend(options.clone());
        config
    }
}

impl Default for ChromixSettings {
    fn default() -> Self {
        Self {
            node_path: "node".into(),
            options: serde_json::Map::new(),
            environment: std::collections::BTreeMap::new(),
        }
    }
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
    #[serde(default)]
    pub chromix: ChromixSettings,
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
            chromix: ChromixSettings::default(),
            skip_browser_download: false,
            auto_update: false,
            usage_reporting: false,
        }
    }
}
