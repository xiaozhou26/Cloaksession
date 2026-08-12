//! Integration test for cdp-driver click/type/extract.
//!
//! Requires a real browser exposing a CDP endpoint. Skipped by default;
//! run with `RUN_CDP_INTEGRATION=1` and optionally `MULTIZEN_TEST_CDP=<url>`.

use cdp_driver::session::BrowserSession;
use multizen_core::BrowserEngine;

fn enabled() -> bool {
    std::env::var("RUN_CDP_INTEGRATION")
        .ok()
        .as_deref()
        == Some("1")
}

#[tokio::test]
#[ignore = "requires a real CDP browser; set RUN_CDP_INTEGRATION=1"]
async fn navigate_and_extract() {
    if !enabled() {
        return;
    }
    let endpoint =
        std::env::var("MULTIZEN_TEST_CDP").unwrap_or_else(|_| "http://127.0.0.1:9222".into());
    let session = BrowserSession::connect(&endpoint, BrowserEngine::Cloakbrowser)
        .await
        .expect("connect");
    let nav = session
        .navigate("https://example.com", 30000)
        .await
        .expect("navigate");
    assert!(nav.url.contains("example.com"));
    let ext = session.extract().await.expect("extract");
    assert!(ext.get("url").is_some());
    session.close().await;
}
