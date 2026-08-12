use browser_launcher::BrowserLauncher;
use std::path::PathBuf;
use std::sync::Arc;
use tempfile::TempDir;

// These require a real CloakBrowser/CFT binary on disk. Set
// MULTIZEN_TEST_BINARY to the path and RUN_CDP_INTEGRATION=1 to enable.
fn binary() -> Option<PathBuf> {
    if std::env::var("RUN_CDP_INTEGRATION").ok().as_deref() != Some("1") {
        return None;
    }
    std::env::var("MULTIZEN_TEST_BINARY").ok().map(PathBuf::from)
}

#[tokio::test]
#[ignore]
async fn launch_and_close_round_trip() {
    let bin = match binary() {
        Some(b) => b,
        None => return,
    };
    let dir = TempDir::new().unwrap();
    let db = dir.path().join("profiles.db");
    // ProfileManager holds a rusqlite::Connection which is not Sync. We wrap
    // it in an Arc for the BrowserLauncher API; the launcher only ever calls
    // pm methods from within a single async task tree (no &self across await
    // points held by multiple threads simultaneously). The clippy lint is
    // overly conservative for this single-threaded test driver.
    #[allow(clippy::arc_with_non_send_sync)]
    let pm = Arc::new(
        profile_manager::ProfileManager::new(&db, &dir.path().join("profiles")).unwrap(),
    );
    let profile = pm
        .create(multizen_core::CreateProfileInput {
            name: "t".into(),
            ..Default::default()
        })
        .unwrap();
    let launcher = BrowserLauncher::new(pm);
    let launched = launcher
        .launch(
            &profile.id,
            &bin,
            multizen_core::BrowserEngine::Cloakbrowser,
            None,
        )
        .await
        .unwrap();
    assert!(launcher.is_running_async(&profile.id).await);
    assert!(launched.cdp_endpoint.starts_with("http://127.0.0.1:"));
    launcher.close(&profile.id).await.unwrap();
    assert!(!launcher.is_running_async(&profile.id).await);
}
