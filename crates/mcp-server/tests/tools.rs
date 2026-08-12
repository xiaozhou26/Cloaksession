//! Integration tests for the 22 MCP tool handlers in `mcp_server::tools`.
//!
//! These tests drive the pure handler layer with the `MockBrowserDriver`
//! (shared via `mod mock_driver;`) and a `ProfileManager` backed by a
//! tempfile directory. No real Chromium is launched.

mod mock_driver;

use std::path::PathBuf;

use mcp_server::activity::ActivityLog;
use mcp_server::driver::BrowserDriver;
use mcp_server::schema::*;
use mcp_server::tools::*;
use mock_driver::MockBrowserDriver;
use profile_manager::ProfileManager;
use tempfile::TempDir;

fn pm_stub() -> ProfileManager {
    // each call gets its own tempdir so tests don't share state; we keep the
    // TempDir so it survives the ProfileManager (manager doesn't own the dir
    // lifecycle; tests are short-lived).
    let path = TempDir::new().expect("tempdir").keep();
    let db = path.join("profiles.db");
    let profiles_root = path.join("profiles");
    ProfileManager::new(&db, &profiles_root).expect("profile manager")
}

// keep a handle to the tempdir so it isn't dropped early
fn pm_stub_with_dir() -> (ProfileManager, TempDir) {
    let tmp = TempDir::new().expect("tempdir");
    let db = tmp.path().join("profiles.db");
    let profiles_root = tmp.path().join("profiles");
    let pm = ProfileManager::new(&db, &profiles_root).expect("profile manager");
    (pm, tmp)
}

fn _silence_pathbuf_warning() -> PathBuf {
    PathBuf::new()
}

// ---------------------------------------------------------------------------
// navigate
// ---------------------------------------------------------------------------

#[tokio::test]
async fn navigate_calls_driver_and_logs() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = navigate(
        &driver,
        &pm,
        &log,
        NavigateArgs {
            profile_id: "p1".into(),
            url: "https://example.com".into(),
        },
    )
    .await
    .unwrap();
    assert_eq!(r.get("url").unwrap().as_str(), Some("https://example.com"));
    let recent = log.recent(10).await;
    assert_eq!(recent[0].tool, "navigate");
    assert_eq!(recent[0].status, "ok");
}

#[tokio::test]
async fn navigate_rejects_blocked_scheme() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = navigate(
        &driver,
        &pm,
        &log,
        NavigateArgs {
            profile_id: "p1".into(),
            url: "file:///etc/passwd".into(),
        },
    )
    .await;
    assert!(r.is_err(), "file: scheme must be rejected");
    let recent = log.recent(10).await;
    assert_eq!(recent[0].status, "error");
}

#[tokio::test]
async fn navigate_rejects_not_running() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    let pm = pm_stub();
    let r = navigate(
        &driver,
        &pm,
        &log,
        NavigateArgs {
            profile_id: "ghost".into(),
            url: "https://example.com".into(),
        },
    )
    .await;
    assert!(r.is_err(), "must reject when profile not running");
}

// ---------------------------------------------------------------------------
// cdp_send security gates
// ---------------------------------------------------------------------------

#[tokio::test]
async fn cdp_send_disabled_by_default() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    // ensure env var is not set during this test
    std::env::remove_var("MULTIZEN_MCP_ALLOW_RAW_CDP");
    let r = cdp_send(
        &driver,
        &pm,
        &log,
        CdpSendArgs {
            profile_id: "p1".into(),
            method: "Page.navigate".into(),
            params: None,
            session_id: None,
        },
    )
    .await;
    assert!(
        r.is_err(),
        "cdp_send must be off without MULTIZEN_MCP_ALLOW_RAW_CDP"
    );
}

#[tokio::test]
async fn cdp_send_denies_io_read() {
    // SAFETY: this is a sync env var set/remove within a single test; other
    // tests that depend on the default state remove the var themselves.
    std::env::set_var("MULTIZEN_MCP_ALLOW_RAW_CDP", "1");
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = cdp_send(
        &driver,
        &pm,
        &log,
        CdpSendArgs {
            profile_id: "p1".into(),
            method: "IO.read".into(),
            params: None,
            session_id: None,
        },
    )
    .await;
    assert!(r.is_err(), "IO.read must be denied even when raw CDP on");
    std::env::remove_var("MULTIZEN_MCP_ALLOW_RAW_CDP");
}

#[tokio::test]
async fn cdp_send_blocks_blocked_scheme_in_params() {
    std::env::set_var("MULTIZEN_MCP_ALLOW_RAW_CDP", "1");
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = cdp_send(
        &driver,
        &pm,
        &log,
        CdpSendArgs {
            profile_id: "p1".into(),
            method: "Page.navigate".into(),
            params: Some(serde_json::json!({ "url": "file:///etc/passwd" })),
            session_id: None,
        },
    )
    .await;
    assert!(r.is_err(), "blocked scheme in params must be rejected");
    std::env::remove_var("MULTIZEN_MCP_ALLOW_RAW_CDP");
}

// ---------------------------------------------------------------------------
// get_cookies / new_tab security
// ---------------------------------------------------------------------------

#[tokio::test]
async fn get_cookies_rejects_blocked_url() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = get_cookies(
        &driver,
        &pm,
        &log,
        GetCookiesArgs {
            profile_id: "p1".into(),
            urls: vec!["file:///etc/passwd".into()],
            session_id: None,
        },
    )
    .await;
    assert!(r.is_err(), "file: url must be rejected by get_cookies");
}

#[tokio::test]
async fn new_tab_rejects_blocked_scheme() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = new_tab(
        &driver,
        &pm,
        &log,
        NewTabArgs {
            profile_id: "p1".into(),
            url: "chrome://settings".into(),
        },
    )
    .await;
    assert!(r.is_err(), "chrome: scheme must be rejected by new_tab");
}

// ---------------------------------------------------------------------------
// profile lifecycle
// ---------------------------------------------------------------------------

#[tokio::test]
async fn list_profiles_empty() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    let pm = pm_stub();
    let r = list_profiles(&driver, &pm, &log, ListProfilesArgs::default())
        .await
        .unwrap();
    assert!(r.get("profiles").unwrap().is_array());
    assert_eq!(
        r.get("profiles").unwrap().as_array().unwrap().len(),
        0
    );
}

#[tokio::test]
async fn launch_profile_rejects_unknown() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    let pm = pm_stub();
    let r = launch_profile(
        &driver,
        &pm,
        &log,
        ProfileIdArgs {
            profile_id: "ghost".into(),
        },
    )
    .await;
    assert!(r.is_err(), "launch must reject unknown profile");
}

#[tokio::test]
async fn create_and_list_and_delete_profile() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    let (pm, _tmp) = pm_stub_with_dir();
    let created = create_profile(
        &driver,
        &pm,
        &log,
        CreateProfileArgs {
            name: "tester".into(),
            notes: None,
            tags: Some(vec!["qa".into()]),
            proxy: None,
            fingerprint: None,
            seed: None,
        },
    )
    .await
    .unwrap();
    let id = created.get("id").unwrap().as_str().unwrap().to_string();
    assert_eq!(created.get("name").unwrap().as_str(), Some("tester"));

    // list should now have 1
    let listed = list_profiles(&driver, &pm, &log, ListProfilesArgs::default())
        .await
        .unwrap();
    assert_eq!(
        listed
            .get("profiles")
            .unwrap()
            .as_array()
            .unwrap()
            .len(),
        1
    );

    // delete it
    let deleted = delete_profile(
        &driver,
        &pm,
        &log,
        ProfileIdArgs { profile_id: id.clone() },
    )
    .await
    .unwrap();
    assert_eq!(deleted.get("deleted").unwrap().as_bool(), Some(true));

    // list empty again
    let listed = list_profiles(&driver, &pm, &log, ListProfilesArgs::default())
        .await
        .unwrap();
    assert_eq!(
        listed
            .get("profiles")
            .unwrap()
            .as_array()
            .unwrap()
            .len(),
        0
    );
}

// ---------------------------------------------------------------------------
// click / type / extract / screenshot
// ---------------------------------------------------------------------------

#[tokio::test]
async fn click_type_extract_screenshot_run_when_running() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();

    let r = click(
        &driver,
        &pm,
        &log,
        ClickArgs {
            profile_id: "p1".into(),
            selector: "#go".into(),
        },
    )
    .await
    .unwrap();
    assert_eq!(r.get("clicked").unwrap().as_bool(), Some(true));

    let r = type_text(
        &driver,
        &pm,
        &log,
        TypeArgs {
            profile_id: "p1".into(),
            selector: "#q".into(),
            text: "hi".into(),
        },
    )
    .await
    .unwrap();
    assert_eq!(r.get("typed").unwrap().as_bool(), Some(true));

    let r = extract(
        &driver,
        &pm,
        &log,
        ProfileIdArgs {
            profile_id: "p1".into(),
        },
    )
    .await
    .unwrap();
    assert!(r.get("data").is_some());

    let r = screenshot(
        &driver,
        &pm,
        &log,
        ProfileIdArgs {
            profile_id: "p1".into(),
        },
    )
    .await
    .unwrap();
    assert!(r.get("data").is_some());
}

// ---------------------------------------------------------------------------
// close_profile
// ---------------------------------------------------------------------------

#[tokio::test]
async fn close_profile_works() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = close_profile(
        &driver,
        &pm,
        &log,
        ProfileIdArgs {
            profile_id: "p1".into(),
        },
    )
    .await
    .unwrap();
    assert_eq!(r.get("closed").unwrap().as_bool(), Some(true));
    assert!(!driver.is_running("p1"));
}

// ---------------------------------------------------------------------------
// list_fingerprint_options
// ---------------------------------------------------------------------------

#[tokio::test]
async fn list_fingerprint_options_returns_catalogs() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    let pm = pm_stub();
    let r = list_fingerprint_options(
        &driver,
        &pm,
        &log,
        ListProfilesArgs::default(),
    )
    .await
    .unwrap();
    let devices = r.get("devices").unwrap().as_array().unwrap();
    let locales = r.get("locales").unwrap().as_array().unwrap();
    assert!(devices.len() >= 10, "device catalog non-trivial");
    assert!(locales.len() >= 10, "locale catalog non-trivial");
}

// ---------------------------------------------------------------------------
// evaluate_js / wait_for_selector / wait_for_load / list_tabs
// ---------------------------------------------------------------------------

#[tokio::test]
async fn evaluate_js_returns_driver_result() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = evaluate_js(
        &driver,
        &pm,
        &log,
        EvaluateJsArgs {
            profile_id: "p1".into(),
            expression: "1+1".into(),
            session_id: None,
        },
    )
    .await
    .unwrap();
    // mock returns {}
    assert!(r.is_object());
}

#[tokio::test]
async fn wait_for_selector_times_out_with_mock() {
    // mock cdp_send returns {} → no result.value → treated as not found → timeout
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = wait_for_selector(
        &driver,
        &pm,
        &log,
        WaitForSelectorArgs {
            profile_id: "p1".into(),
            selector: "#never".into(),
            timeout_ms: 200,
        },
    )
    .await
    .unwrap();
    // timed out, not found
    assert_eq!(r.get("found").unwrap().as_bool(), Some(false));
}

#[tokio::test]
async fn list_tabs_runs() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = list_tabs(
        &driver,
        &pm,
        &log,
        ProfileIdArgs {
            profile_id: "p1".into(),
        },
    )
    .await
    .unwrap();
    assert!(r.is_object());
}

// ---------------------------------------------------------------------------
// set_cookies
// ---------------------------------------------------------------------------

#[tokio::test]
async fn set_cookies_runs() {
    let driver = MockBrowserDriver::new();
    let log = ActivityLog::new();
    driver.launch("p1").await.unwrap();
    let pm = pm_stub();
    let r = set_cookies(
        &driver,
        &pm,
        &log,
        SetCookiesArgs {
            profile_id: "p1".into(),
            cookies: vec![serde_json::json!({
                "name": "k",
                "value": "v",
                "domain": "example.com"
            })],
            session_id: None,
        },
    )
    .await
    .unwrap();
    assert_eq!(r.get("set").unwrap().as_bool(), Some(true));
}

// ---------------------------------------------------------------------------
// error_json helper
// ---------------------------------------------------------------------------

#[test]
fn error_json_maps_not_found() {
    use multizen_core::MultizenError;
    let e = MultizenError::NotFound("p1".into());
    let v = error_json(&e);
    assert_eq!(
        v.get("error").unwrap().get("code").unwrap().as_str(),
        Some("PROFILE_NOT_FOUND")
    );
}

#[test]
fn error_json_maps_launch_failed() {
    use multizen_core::MultizenError;
    let e = MultizenError::Launch("boom".into());
    let v = error_json(&e);
    assert_eq!(
        v.get("error").unwrap().get("code").unwrap().as_str(),
        Some("LAUNCH_FAILED")
    );
}

// silence unused helper in case future tests drop pm_stub_with_dir usage
#[allow(dead_code)]
fn _silence_pm_stub_with_dir() {
    let _ = pm_stub_with_dir();
}
