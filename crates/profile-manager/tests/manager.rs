use multizen_core::{CreateProfileInput, PartialFingerprintInput, UpdateProfileInput};
use profile_manager::ProfileManager;
use tempfile::TempDir;

fn make() -> (TempDir, ProfileManager) {
    let dir = TempDir::new().unwrap();
    let db = dir.path().join("test.db");
    let profiles_root = dir.path().join("profiles");
    let mgr = ProfileManager::new(&db, &profiles_root).unwrap();
    (dir, mgr)
}

#[test]
fn create_and_get_profile() {
    let (_dir, mgr) = make();
    let input = CreateProfileInput {
        name: "test".into(),
        notes: None,
        tags: Some(vec!["a".into()]),
        icon: None,
        start_url: None,
        search_provider: None,
        proxy: None,
        fingerprint: None,
        extensions: None,
        full_fingerprint: None,
    };
    let p = mgr.create(input).unwrap();
    assert_eq!(p.name, "test");
    assert_eq!(p.tags, vec!["a".to_string()]);
    let fetched = mgr.get(&p.id).unwrap().unwrap();
    assert_eq!(fetched.id, p.id);
}

#[test]
fn create_full_fingerprint_json_round_trips_without_loss() {
    let (_dir, mgr) = make();
    let mut expected = profile_manager::fingerprint::default_fingerprint("ui-seed");
    expected.user_agent = "Custom/99.1 test-agent".into();
    expected.locale = "ja-JP".into();
    expected.languages = vec!["ja-JP".into(), "ja".into()];
    expected.accept_language = "ja-JP,ja;q=0.8".into();
    expected.screen.width = 2560;
    expected.avail_screen = None;
    expected.dpr = 1.25;
    expected.hardware_concurrency = 12;
    expected.device_memory = 16;
    expected.fonts_dir = None;
    expected.storage_quota = Some(3_210_000_000);

    let expected_value = serde_json::to_value(&expected).unwrap();
    let input: CreateProfileInput = serde_json::from_value(serde_json::json!({
        "name": "full",
        "fingerprint": expected_value.clone(),
    }))
    .unwrap();
    assert!(input.full_fingerprint.is_some());
    assert!(input.fingerprint.is_none());

    let created = mgr.create(input).unwrap();
    let fetched = mgr.get(&created.id).unwrap().unwrap();
    let actual = serde_json::to_value(&fetched.fingerprint).unwrap();
    assert_eq!(actual, expected_value);
    assert_eq!(actual["screen"]["width"], 2560);
    assert_eq!(actual["availScreen"], serde_json::Value::Null);
    assert_eq!(actual["acceptLanguage"], "ja-JP,ja;q=0.8");
    assert_eq!(actual["storageQuota"], 3_210_000_000u64);
}

#[test]
fn create_legacy_partial_fingerprint_remains_compatible() {
    let (_dir, mgr) = make();
    let input: CreateProfileInput = serde_json::from_value(serde_json::json!({
        "name": "partial",
        "fingerprint": {
            "locale": "de-DE",
            "timezone": "Europe/Berlin",
            "country": "DE"
        }
    }))
    .unwrap();
    assert!(input.full_fingerprint.is_none());
    assert!(matches!(
        input.fingerprint,
        Some(PartialFingerprintInput { locale: Some(ref locale), .. }) if locale == "de-DE"
    ));

    let created = mgr.create(input).unwrap();
    assert_eq!(created.fingerprint.locale, "de-DE");
    assert_eq!(created.fingerprint.timezone, "Europe/Berlin");
    assert_eq!(created.fingerprint.country, "DE");
    assert_eq!(created.fingerprint.platform, "Win32");
}

#[test]
fn list_returns_summary_with_running_false() {
    let (_dir, mgr) = make();
    mgr.create(CreateProfileInput { name: "p1".into(), ..Default::default() }).unwrap();
    let list = mgr.list().unwrap();
    assert_eq!(list.len(), 1);
    assert!(!list[0].is_running);
}

#[test]
fn update_changes_name_and_clears_icon() {
    let (_dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "orig".into(), icon: Some("🦊".into()), ..Default::default() }).unwrap();
    let updated = mgr.update(&p.id, UpdateProfileInput {
        name: Some("renamed".into()),
        icon: Some(None), // clear
        ..Default::default()
    }).unwrap();
    assert_eq!(updated.name, "renamed");
    assert_eq!(updated.icon, None);
}

#[test]
fn update_persists_custom_user_agent() {
    let (_dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "ua".into(), ..Default::default() }).unwrap();
    let mut fingerprint = p.fingerprint.clone();
    fingerprint.user_agent = "Custom/99.1 test-agent".into();
    mgr.update(&p.id, UpdateProfileInput {
        fingerprint: Some(fingerprint),
        ..Default::default()
    }).unwrap();
    let fetched = mgr.get(&p.id).unwrap().unwrap();
    assert_eq!(fetched.fingerprint.user_agent, "Custom/99.1 test-agent");
}

#[test]
fn update_proxy_clears_proxy_country() {
    let (_dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "p".into(), ..Default::default() }).unwrap();
    mgr.set_proxy_country(&p.id, Some("US")).unwrap();
    let _ = mgr.update(&p.id, UpdateProfileInput {
        proxy: Some(Some(multizen_core::ProxyConfig {
            proxy_type: "http".into(), host: "1.1.1.1".into(), port: 8080,
            username: None, password: None,
        })),
        ..Default::default()
    }).unwrap();
    let after = mgr.get(&p.id).unwrap().unwrap();
    assert_eq!(after.proxy_country, None); // stale country cleared on proxy change
}

#[test]
fn delete_removes_row_and_data_dir() {
    let (dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "p".into(), ..Default::default() }).unwrap();
    let data_dir = dir.path().join("profiles").join(&p.id);
    assert!(data_dir.exists());
    mgr.delete(&p.id).unwrap();
    assert!(mgr.get(&p.id).unwrap().is_none());
    assert!(!data_dir.exists());
}

#[test]
fn insert_imported_collides_on_existing_id() {
    let (_dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "p".into(), ..Default::default() }).unwrap();
    let result = mgr.insert_imported(p);
    assert!(result.is_err());
}

#[test]
fn mark_opened_sets_last_opened_at() {
    let (_dir, mgr) = make();
    let p = mgr.create(CreateProfileInput { name: "p".into(), ..Default::default() }).unwrap();
    assert!(mgr.get(&p.id).unwrap().unwrap().last_opened_at.is_none());
    mgr.mark_opened(&p.id).unwrap();
    assert!(mgr.get(&p.id).unwrap().unwrap().last_opened_at.is_some());
}
