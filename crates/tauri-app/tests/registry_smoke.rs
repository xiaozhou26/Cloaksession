//! Smoke tests for `ProfileRegistry` and `TauriBrowserDriver` wiring that do
//! NOT require a real Chromium binary or CDP endpoint. Full integration
//! coverage lives in P4.8.

use tauri_app::ProfileRegistry;

#[tokio::test]
async fn registry_get_or_connect_missing_endpoint_errors_cleanly() {
    // No real browser at this endpoint; BrowserSession::connect should fail
    // with a Cdp error, NOT panic. Verifies the error path is wired.
    let reg = ProfileRegistry::new();
    let res = reg
        .get_or_connect(
            "p1",
            "http://127.0.0.1:1", // nothing listening
            multizen_core::BrowserEngine::Cloakbrowser,
        )
        .await;
    assert!(res.is_err(), "connect to dead endpoint should error");
    // Registry should not retain a half-registered entry.
    assert!(reg.get("p1").await.is_none());
}

#[tokio::test]
async fn registry_remove_is_noop_when_absent() {
    let reg = ProfileRegistry::new();
    // Should not panic.
    reg.remove("does-not-exist").await;
    assert!(reg.ids().await.is_empty());
}

#[tokio::test]
async fn registry_ids_empty_initially() {
    let reg = ProfileRegistry::new();
    assert!(reg.ids().await.is_empty());
}
