use multizen_core::{CreateProfileInput, UpdateProfileInput};
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
    };
    let p = mgr.create(input).unwrap();
    assert_eq!(p.name, "test");
    assert_eq!(p.tags, vec!["a".to_string()]);
    let fetched = mgr.get(&p.id).unwrap().unwrap();
    assert_eq!(fetched.id, p.id);
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
