//! Proxy commands. `detect_geo` probes the exit IP / geo of a proxy by
//! sending a request to ipapi.co through it, reusing the P2.7 geo probe
//! helper from `browser-launcher::proxy_geo`. The probe is a standalone
//! async fn (it doesn't touch `ProfileManager` or `BrowserLauncher`), so it
//! runs directly on the Tauri async runtime — no launcher-thread routing
//! needed.

use browser_launcher::proxy_geo::{probe_proxy_geo, ProxyGeoResult};
use multizen_core::ProxyConfig;
use tauri::State;

use crate::AppState;

/// IPapi probe timeout. ipapi.co's free tier can be slow under proxy
/// chaining; 12s is a generous ceiling before we surface a timeout error.
const GEO_PROBE_TIMEOUT_MS: u64 = 12_000;

#[tauri::command]
pub async fn proxy_detect_geo(
    _state: State<'_, AppState>,
    proxy: ProxyConfig,
) -> Result<ProxyGeoResult, String> {
    probe_proxy_geo(&proxy, GEO_PROBE_TIMEOUT_MS)
        .await
        .map_err(|e| e.to_string())
}
