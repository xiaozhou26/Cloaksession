//! Registry smoke tests and an opt-in full-driver local Chromium integration.

use desktop_core::ProfileRegistry;

#[test]
fn about_uses_released_app_version() {
    assert_eq!(env!("CARGO_PKG_VERSION"), "1.3.0");
}

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

#[tokio::test]
#[ignore = "requires a local browser, installed npm runtime, and RUN_CDP_INTEGRATION=1"]
async fn chromix_real_browser_launch_bootstrap_and_close() {
    use desktop_core::DesktopBrowserDriver;
    use futures_util::FutureExt;
    use mcp_server::driver::BrowserDriver;
    use multizen_core::{BrowserEngine, ChromixSettings, CreateProfileInput};
    use serde_json::json;
    use std::path::PathBuf;
    use std::sync::Arc;
    use std::time::Duration;

    assert_eq!(std::env::var("RUN_CDP_INTEGRATION").as_deref(), Ok("1"));
    let binary =
        PathBuf::from(std::env::var_os("MULTIZEN_TEST_BINARY").expect("set MULTIZEN_TEST_BINARY"));
    assert!(binary.is_file());
    let directory = tempfile::tempdir().unwrap();
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let origin = format!("http://{}", listener.local_addr().unwrap());
    let server = tokio::spawn(async move {
        axum::serve(listener, axum::Router::new().route("/", axum::routing::get(|| async {
            axum::response::Html("<title>Rust Playwright integration</title><body>Local browser smoke</body>")
        }))).await.unwrap();
    });
    let registry = Arc::new(ProfileRegistry::new());
    let config = ChromixSettings {
        options: json!({"headless":true,"extensionPaths":[],"fingerprintMode":"fixed","fingerprintSeed":"18446744073709551615"})
            .as_object().unwrap().clone(),
        ..Default::default()
    };
    let driver = DesktopBrowserDriver::start(
        directory.path().join("profiles.db"),
        directory.path().join("profiles"),
        directory.path().join("extensions"),
        registry.clone(),
        BrowserEngine::Chromix,
        binary,
        None,
    )
    .unwrap()
    .with_chromix(
        config,
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("resources/chromix"),
        true,
    );
    let profile = driver
        .create_profile(CreateProfileInput {
            name: "Rust Playwright integration".into(),
            start_url: Some(origin.clone()),
            ..Default::default()
        })
        .await
        .unwrap();
    let exercise = async {
        let client = reqwest::Client::builder()
            .no_proxy()
            .timeout(Duration::from_secs(3))
            .build()
            .unwrap();
        for iteration in 0..2 {
            // The desktop driver runs sidecar startup, CDP attachment, and bootstrap before returning.
            let launched = driver
                .launch(&profile.id)
                .await
                .expect("launch and bootstrap");
            assert!(driver.is_running(&profile.id));
            let session = registry
                .get(&profile.id)
                .await
                .expect("registered Rust CDP session");
            assert_eq!(session.engine, BrowserEngine::Chromix);
            // CDP target discovery continues asynchronously after websocket attachment.
            tokio::time::timeout(Duration::from_secs(10), async {
                while session.browser.pages().await.unwrap().is_empty() {
                    tokio::time::sleep(Duration::from_millis(20)).await;
                }
            })
            .await
            .expect("CDP target discovery");
            assert_eq!(
                session.evaluate("document.title").await.unwrap(),
                "Rust Playwright integration"
            );
            let nav = session
                .navigate(&format!("{origin}/?iteration={iteration}"), 10000)
                .await
                .unwrap();
            assert!(nav.url.contains(&format!("iteration={iteration}")));
            assert_eq!(
                session.extract().await.unwrap()["title"],
                "Rust Playwright integration"
            );
            let stored = session
                .evaluate("({ stored: localStorage.getItem('rust-playwright-smoke') })")
                .await
                .unwrap();
            assert_eq!(
                stored["stored"],
                if iteration == 0 {
                    json!(null)
                } else {
                    json!("saved")
                }
            );
            session
                .evaluate("localStorage.setItem('rust-playwright-smoke', 'saved'); true")
                .await
                .unwrap();
            drop(session);
            driver.close(&profile.id).await.unwrap();
            assert!(!driver.is_running(&profile.id));
            assert!(registry.get(&profile.id).await.is_none());
            assert!(client
                .get(format!("{}/json/version", launched.cdp_endpoint))
                .send()
                .await
                .is_err());
        }
        let stored = driver.get_profile(&profile.id).await.unwrap().unwrap();
        assert_eq!(stored.fingerprint.seed, profile.fingerprint.seed);
        assert_eq!(stored.chromix_options, profile.chromix_options);
        assert!(stored.last_opened_at.is_some());
    };
    let outcome = tokio::time::timeout(
        Duration::from_secs(90),
        std::panic::AssertUnwindSafe(exercise).catch_unwind(),
    )
    .await;
    driver.shutdown().await;
    server.abort();
    if let Err(panic) = outcome.expect("full Rust browser integration timed out") {
        std::panic::resume_unwind(panic);
    }
}
