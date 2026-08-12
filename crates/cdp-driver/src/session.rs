use chromiumoxide::Browser;
use multizen_core::{BrowserEngine, MultizenError, Result};

use crate::safe_cdp::SafeEnableRefcount;

pub struct BrowserSession {
    pub browser: Browser,
    pub engine: BrowserEngine,
    pub safe: SafeEnableRefcount,
}

impl BrowserSession {
    pub async fn connect(cdp_endpoint: &str, engine: BrowserEngine) -> Result<Self> {
        // cdp_endpoint is http://127.0.0.1:{port}. Fetch webSocketDebuggerUrl.
        let version_url = format!("{cdp_endpoint}/json/version");
        let resp = reqwest::get(&version_url)
            .await
            .map_err(|e| MultizenError::Cdp(format!("version fetch: {e}")))?
            .json::<serde_json::Value>()
            .await
            .map_err(|e| MultizenError::Cdp(format!("version json: {e}")))?;
        let ws_url = resp
            .get("webSocketDebuggerUrl")
            .and_then(|v| v.as_str())
            .ok_or_else(|| MultizenError::Cdp("no webSocketDebuggerUrl".into()))?
            .to_string();

        let (browser, mut handler) = Browser::connect(&ws_url)
            .await
            .map_err(|e| MultizenError::Cdp(format!("connect: {e}")))?;
        // Drive the CDP handler in background. `Handler` implements
        // `futures::Stream`; poll it forever so the CDP connection stays alive.
        tokio::spawn(async move {
            use futures::StreamExt;
            while let Some(_h) = handler.next().await {}
        });

        Ok(Self {
            browser,
            engine,
            safe: SafeEnableRefcount::new(),
        })
    }
}
