//! System info command. Returns MCP HTTP URL, MCP auth token, app version,
//! and platform. The MCP auth token is generated in P4.4; until then it
//! stays `None` in `AppState` and is returned as `null` to the frontend.

use serde::Serialize;
use tauri::State;

use crate::AppState;

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SystemInfo {
    pub mcp_http_url: String,
    pub mcp_auth_token: Option<String>,
    pub app_version: &'static str,
    pub platform: &'static str,
}

#[tauri::command]
pub async fn system_info(state: State<'_, AppState>) -> Result<SystemInfo, String> {
    let token = state.mcp_token.lock().await.clone();
    // Default port matches `AppSettings::default().mcp_http_port`. P4.4
    // will resolve the actual bound port from the MCP server handle and
    // override this string.
    let mcp_http_url = "http://127.0.0.1:7777".to_string();
    Ok(SystemInfo {
        mcp_http_url,
        mcp_auth_token: token,
        app_version: env!("CARGO_PKG_VERSION"),
        platform: std::env::consts::OS,
    })
}
