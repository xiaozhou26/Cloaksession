use multizen_core::{BrowserEngine, FingerprintConfig, Result};

use crate::scripts::{
    build_fingerprint_preload_script, build_webrtc_block_script, build_webrtc_spoof_script,
};
use crate::session::BrowserSession;

pub async fn bootstrap_targets(
    session: &BrowserSession,
    fp: &FingerprintConfig,
    engine: BrowserEngine,
    webrtc_spoof_ip: Option<&str>,
) -> Result<()> {
    // C1: document the existing correct behavior. For CloakBrowser, only
    // the locale evaluate runs below (webrtc/preload are gated to CFT), and
    // CloakBrowser relies on launch-time `--fingerprint-*` flags (P2.5)
    // rather than bootstrap emulation. Log it so integrators understand why
    // bootstrap is a no-op for CloakBrowser.
    if engine == BrowserEngine::Cloakbrowser {
        tracing::warn!(
            engine = "cloakbrowser",
            "full bootstrap emulation is CFT-only; CloakBrowser relies on launch-time \
             --fingerprint-* flags. Only the locale evaluate will run (single evaluate, \
             not a domain enable — safe under the safe_cdp gate)."
        );
    }
    let pages = session
        .browser
        .pages()
        .await
        .map_err(|e| multizen_core::MultizenError::Cdp(format!("pages: {e}")))?;
    for page in pages {
        // WebRTC (CFT + proxy only)
        if engine == BrowserEngine::Cft && webrtc_spoof_ip.is_some() {
            let script = match webrtc_spoof_ip {
                Some(ip) => build_webrtc_spoof_script(ip),
                None => build_webrtc_block_script().to_string(),
            };
            // Page.addScriptToEvaluateOnNewDocument
            let _ = page.evaluate(script.as_str()).await;
        }
        // Fingerprint preload (CFT only). Register it for every future
        // document and also evaluate it in the current document so the first
        // already-open tab is updated immediately.
        if engine == BrowserEngine::Cft {
            let preload = build_fingerprint_preload_script(fp);
            page.evaluate_on_new_document(preload.clone())
                .await
                .map_err(|e| {
                    multizen_core::MultizenError::Cdp(format!("fingerprint preload: {e}"))
                })?;
            page.evaluate(preload.as_str()).await.map_err(|e| {
                multizen_core::MultizenError::Cdp(format!("fingerprint evaluate: {e}"))
            })?;
            page.set_user_agent(
                chromiumoxide::cdp::browser_protocol::network::SetUserAgentOverrideParams {
                    user_agent: fp.user_agent.clone(),
                    accept_language: Some(fp.accept_language.clone()),
                    platform: Some(fp.platform.clone()),
                    user_agent_metadata: None,
                },
            )
            .await
            .map_err(|e| multizen_core::MultizenError::Cdp(format!("user agent override: {e}")))?;
        }
        // Locale (both engines)
        let _ = page
            .evaluate(
                format!(
                    "(function(){{try{{document.documentElement.lang={lang:?};}}catch(e){{}}}})()",
                    lang = fp.locale
                )
                .as_str(),
            )
            .await;
        // UA + Accept-Language + platform are applied above via
        // Emulation.setUserAgentOverride for CFT. Client-hint metadata is not
        // synthesized here because the Rust config stores serialized header
        // strings rather than the structured CDP brand/version array.
    }
    Ok(())
}
