//! Proxy commands. `detect_geo` would normally call the P2.7 proxy geo
//! probe helper, but that lives on the browser-launcher layer (spawned
//! per-session), not the profile layer. Wiring it requires routing a probe
//! through the launcher thread, which is deferred to P4.8. Return an
//! explicit error so the frontend can surface "not yet wired".

use tauri::State;

use crate::AppState;

#[tauri::command]
pub async fn proxy_detect_geo(
    _state: State<'_, AppState>,
    _proxy: serde_json::Value,
) -> Result<serde_json::Value, String> {
    Err("proxy_detect_geo not yet wired to launcher-thread geo probe (P4.8)".into())
}
