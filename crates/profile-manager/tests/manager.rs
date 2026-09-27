use multizen_core::{CreateProfileInput, PartialFingerprintInput, Profile, UpdateProfileInput};
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
        chromix_options: None,
        extensions: None,
        full_fingerprint: None,
    };
    let p = mgr.create(input).unwrap();
    assert_eq!(p.name, "test");
    assert_eq!(p.tags, vec!["a".to_string()]);
    let fetched = mgr.get(&p.id).unwrap().unwrap();
    assert_eq!(fetched.id, p.id);
    assert!(p.chromix_options.is_empty());
    assert!(fetched.chromix_options.is_empty());
    assert!(mgr.list().unwrap()[0].chromix_options.is_empty());
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
    mgr.create(CreateProfileInput {
        name: "p1".into(),
        ..Default::default()
    })
    .unwrap();
    let list = mgr.list().unwrap();
    assert_eq!(list.len(), 1);
    assert!(!list[0].is_running);
}

#[test]
fn update_changes_name_and_clears_icon() {
    let (_dir, mgr) = make();
    let p = mgr
        .create(CreateProfileInput {
            name: "orig".into(),
            icon: Some("🦊".into()),
            ..Default::default()
        })
        .unwrap();
    let updated = mgr
        .update(
            &p.id,
            UpdateProfileInput {
                name: Some("renamed".into()),
                icon: Some(None), // clear
                ..Default::default()
            },
        )
        .unwrap();
    assert_eq!(updated.name, "renamed");
    assert_eq!(updated.icon, None);
}

#[test]
fn update_persists_custom_user_agent() {
    let (_dir, mgr) = make();
    let p = mgr
        .create(CreateProfileInput {
            name: "ua".into(),
            ..Default::default()
        })
        .unwrap();
    let mut fingerprint = p.fingerprint.clone();
    fingerprint.user_agent = "Custom/99.1 test-agent".into();
    mgr.update(
        &p.id,
        UpdateProfileInput {
            fingerprint: Some(fingerprint),
            ..Default::default()
        },
    )
    .unwrap();
    let fetched = mgr.get(&p.id).unwrap().unwrap();
    assert_eq!(fetched.fingerprint.user_agent, "Custom/99.1 test-agent");
}

#[test]
fn update_proxy_clears_proxy_country() {
    let (_dir, mgr) = make();
    let p = mgr
        .create(CreateProfileInput {
            name: "p".into(),
            ..Default::default()
        })
        .unwrap();
    mgr.set_proxy_country(&p.id, Some("US")).unwrap();
    let _ = mgr
        .update(
            &p.id,
            UpdateProfileInput {
                proxy: Some(Some(multizen_core::ProxyConfig {
                    proxy_type: "http".into(),
                    host: "1.1.1.1".into(),
                    port: 8080,
                    username: None,
                    password: None,
                })),
                ..Default::default()
            },
        )
        .unwrap();
    let after = mgr.get(&p.id).unwrap().unwrap();
    assert_eq!(after.proxy_country, None); // stale country cleared on proxy change
}

#[test]
fn delete_removes_row_and_data_dir() {
    let (dir, mgr) = make();
    let p = mgr
        .create(CreateProfileInput {
            name: "p".into(),
            ..Default::default()
        })
        .unwrap();
    let data_dir = dir.path().join("profiles").join(&p.id);
    assert!(data_dir.exists());
    mgr.delete(&p.id).unwrap();
    assert!(mgr.get(&p.id).unwrap().is_none());
    assert!(!data_dir.exists());
}

#[test]
fn insert_imported_collides_on_existing_id() {
    let (_dir, mgr) = make();
    let p = mgr
        .create(CreateProfileInput {
            name: "p".into(),
            ..Default::default()
        })
        .unwrap();
    let result = mgr.insert_imported(p);
    assert!(result.is_err());
}

#[test]
fn mark_opened_sets_last_opened_at() {
    let (_dir, mgr) = make();
    let p = mgr
        .create(CreateProfileInput {
            name: "p".into(),
            ..Default::default()
        })
        .unwrap();
    assert!(mgr.get(&p.id).unwrap().unwrap().last_opened_at.is_none());
    mgr.mark_opened(&p.id).unwrap();
    assert!(mgr.get(&p.id).unwrap().unwrap().last_opened_at.is_some());
}

fn chromix_options() -> serde_json::Map<String, serde_json::Value> {
    serde_json::json!({
        "fingerprintSeed": "18446744073709551615",
        "enabled": false,
        "nullable": null,
        "unknownSdkOption": {
            "values": [false, null, "18446744073709551615", {"futureKey": true}],
            "fraction": 1.25,
            "empty": {},
            "unsigned": u64::MAX,
        },
    })
    .as_object()
    .unwrap()
    .clone()
}

fn assert_chromix_options(
    mgr: &ProfileManager,
    id: &str,
    expected: &serde_json::Map<String, serde_json::Value>,
) {
    let profile = mgr.get(id).unwrap().unwrap();
    assert_eq!(&profile.chromix_options, expected);
    let summary = mgr
        .list()
        .unwrap()
        .into_iter()
        .find(|p| p.id == id)
        .unwrap();
    assert_eq!(&summary.chromix_options, expected);
    let expected_json = serde_json::to_value(expected).unwrap();
    assert_eq!(
        serde_json::to_value(profile).unwrap()["chromixOptions"],
        expected_json
    );
    assert_eq!(
        serde_json::to_value(summary).unwrap()["chromixOptions"],
        expected_json
    );
}

#[test]
fn chromix_options_create_get_list_and_reopen_preserve_arbitrary_json() {
    let (dir, mgr) = make();
    let expected = chromix_options();
    let input: CreateProfileInput = serde_json::from_value(serde_json::json!({
        "name": "custom-sdk",
        "fingerprint": profile_manager::fingerprint::default_fingerprint("custom-sdk"),
        "chromixOptions": expected,
    }))
    .unwrap();
    assert_eq!(input.chromix_options.as_ref(), Some(&expected));
    assert!(input.full_fingerprint.is_some());
    let created = mgr.create(input).unwrap();
    assert_eq!(created.chromix_options, expected);
    assert_chromix_options(&mgr, &created.id, &expected);
    let other = mgr
        .create(CreateProfileInput {
            name: "other".into(),
            ..Default::default()
        })
        .unwrap();
    assert_chromix_options(&mgr, &other.id, &Default::default());

    drop(mgr);
    let mgr =
        ProfileManager::new(&dir.path().join("test.db"), &dir.path().join("profiles")).unwrap();
    assert_chromix_options(&mgr, &created.id, &expected);
    assert_chromix_options(&mgr, &other.id, &Default::default());
}

#[test]
fn chromix_options_update_replaces_preserves_omitted_and_clears_with_empty_object() {
    let (dir, mgr) = make();
    let expected = chromix_options();
    let created = mgr
        .create(CreateProfileInput {
            name: "replace".into(),
            chromix_options: Some(expected.clone()),
            ..Default::default()
        })
        .unwrap();
    let other = mgr
        .create(CreateProfileInput {
            name: "independent".into(),
            chromix_options: Some(expected.clone()),
            ..Default::default()
        })
        .unwrap();

    for json in [
        serde_json::json!({"name": "renamed"}),
        serde_json::json!({"chromixOptions": null}),
    ] {
        let patch: UpdateProfileInput = serde_json::from_value(json).unwrap();
        assert!(patch.chromix_options.is_none());
        let updated = mgr.update(&created.id, patch).unwrap();
        assert_eq!(updated.chromix_options, expected);
        assert_chromix_options(&mgr, &created.id, &expected);
    }

    let replacement = serde_json::json!({
        "replacement": false,
        "fingerprintSeed": "18446744073709551615",
        "nested": {"nullable": null, "array": [false, "future"]},
    });
    let patch: UpdateProfileInput = serde_json::from_value(serde_json::json!({
        "chromixOptions": replacement,
    }))
    .unwrap();
    assert_eq!(
        serde_json::to_value(&patch).unwrap()["chromixOptions"],
        replacement
    );
    let updated = mgr.update(&created.id, patch).unwrap();
    assert_eq!(&updated.chromix_options, replacement.as_object().unwrap());
    assert!(!updated.chromix_options.contains_key("unknownSdkOption"));
    assert_chromix_options(&mgr, &created.id, replacement.as_object().unwrap());
    assert_chromix_options(&mgr, &other.id, &expected);

    drop(mgr);
    let mgr =
        ProfileManager::new(&dir.path().join("test.db"), &dir.path().join("profiles")).unwrap();
    assert_chromix_options(&mgr, &created.id, replacement.as_object().unwrap());
    let patch: UpdateProfileInput = serde_json::from_value(serde_json::json!({
        "chromixOptions": {},
    }))
    .unwrap();
    assert!(patch.chromix_options.as_ref().unwrap().is_empty());
    assert!(mgr
        .update(&created.id, patch)
        .unwrap()
        .chromix_options
        .is_empty());
    assert_chromix_options(&mgr, &created.id, &Default::default());
    assert_chromix_options(&mgr, &other.id, &expected);

    drop(mgr);
    let mgr =
        ProfileManager::new(&dir.path().join("test.db"), &dir.path().join("profiles")).unwrap();
    assert_chromix_options(&mgr, &created.id, &Default::default());
    assert_chromix_options(&mgr, &other.id, &expected);
}

#[test]
fn chromix_options_profile_json_and_import_round_trip() {
    let (_source_dir, source) = make();
    let expected = chromix_options();
    let original = source
        .create(CreateProfileInput {
            name: "export".into(),
            chromix_options: Some(expected.clone()),
            ..Default::default()
        })
        .unwrap();
    let json = serde_json::to_vec(&original).unwrap();
    let mut restored: Profile = serde_json::from_slice(&json).unwrap();
    assert_eq!(restored.chromix_options, expected);
    assert_eq!(
        serde_json::to_value(&restored).unwrap(),
        serde_json::to_value(&original).unwrap()
    );

    let (dir, mgr) = make();
    restored.data_dir = dir
        .path()
        .join("profiles")
        .join(&restored.id)
        .to_string_lossy()
        .into_owned();
    let imported = mgr.insert_imported(restored).unwrap();
    assert_eq!(imported.chromix_options, expected);
    assert_chromix_options(&mgr, &imported.id, &expected);

    drop(mgr);
    let mgr =
        ProfileManager::new(&dir.path().join("test.db"), &dir.path().join("profiles")).unwrap();
    assert_chromix_options(&mgr, &imported.id, &expected);
}

#[test]
fn old_profile_json_import_defaults_chromix_options_to_empty() {
    let (_source_dir, source) = make();
    let original = source
        .create(CreateProfileInput {
            name: "legacy-json".into(),
            ..Default::default()
        })
        .unwrap();
    let mut json = serde_json::to_value(original).unwrap();
    json.as_object_mut().unwrap().remove("chromixOptions");
    let mut restored: Profile = serde_json::from_str(&json.to_string()).unwrap();
    assert!(restored.chromix_options.is_empty());
    assert_eq!(
        serde_json::to_value(&restored).unwrap()["chromixOptions"],
        serde_json::json!({})
    );

    let (dir, mgr) = make();
    restored.data_dir = dir
        .path()
        .join("profiles")
        .join(&restored.id)
        .to_string_lossy()
        .into_owned();
    let imported = mgr.insert_imported(restored).unwrap();
    assert!(imported.chromix_options.is_empty());
    assert_chromix_options(&mgr, &imported.id, &Default::default());

    drop(mgr);
    let mgr =
        ProfileManager::new(&dir.path().join("test.db"), &dir.path().join("profiles")).unwrap();
    assert_chromix_options(&mgr, &imported.id, &Default::default());
}

#[test]
fn old_schema_rows_default_to_empty_and_can_persist_chromix_options() {
    let dir = TempDir::new().unwrap();
    let db = dir.path().join("test.db");
    let profiles = dir.path().join("profiles");
    let conn = rusqlite::Connection::open(&db).unwrap();
    conn.execute_batch(
        "CREATE TABLE profiles (
            id TEXT PRIMARY KEY, name TEXT NOT NULL, notes TEXT,
            tags TEXT NOT NULL DEFAULT '[]', proxy TEXT, fingerprint TEXT NOT NULL,
            data_dir TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
            last_opened_at TEXT
        );",
    )
    .unwrap();
    conn.execute(
        "INSERT INTO profiles (id, name, fingerprint, data_dir, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?)",
        rusqlite::params![
            "legacy",
            "old-schema",
            serde_json::to_string(&profile_manager::fingerprint::default_fingerprint("legacy"))
                .unwrap(),
            profiles.join("legacy").to_string_lossy(),
            "2026-01-01T00:00:00Z",
            "2026-01-01T00:00:00Z",
        ],
    )
    .unwrap();
    drop(conn);

    let mgr = ProfileManager::new(&db, &profiles).unwrap();
    assert_chromix_options(&mgr, "legacy", &Default::default());
    assert_eq!(mgr.get("legacy").unwrap().unwrap().name, "old-schema");
    drop(mgr);
    let mgr = ProfileManager::new(&db, &profiles).unwrap();
    assert_chromix_options(&mgr, "legacy", &Default::default());

    let expected = chromix_options();
    mgr.update(
        "legacy",
        UpdateProfileInput {
            chromix_options: Some(expected.clone()),
            ..Default::default()
        },
    )
    .unwrap();
    drop(mgr);
    let mgr = ProfileManager::new(&db, &profiles).unwrap();
    assert_chromix_options(&mgr, "legacy", &expected);
}

#[test]
fn chromix_options_inputs_reject_non_objects() {
    let (_dir, mgr) = make();
    let profile = mgr
        .create(CreateProfileInput {
            name: "valid".into(),
            ..Default::default()
        })
        .unwrap();
    for invalid in [
        serde_json::json!(false),
        serde_json::json!(true),
        serde_json::json!(42),
        serde_json::json!("{}"),
        serde_json::json!([]),
        serde_json::json!([{}]),
    ] {
        let input = serde_json::json!({"name": "invalid", "chromixOptions": invalid});
        assert!(
            serde_json::from_value::<CreateProfileInput>(input.clone()).is_err(),
            "{invalid}"
        );
        assert!(
            serde_json::from_value::<UpdateProfileInput>(input).is_err(),
            "{invalid}"
        );
        let mut json = serde_json::to_value(&profile).unwrap();
        json["chromixOptions"] = invalid.clone();
        assert!(
            serde_json::from_value::<Profile>(json).is_err(),
            "{invalid}"
        );
    }
    let mut json = serde_json::to_value(&profile).unwrap();
    json["chromixOptions"] = serde_json::Value::Null;
    assert!(serde_json::from_value::<Profile>(json).is_err());
}

#[test]
fn chromix_options_optional_create_defaults_remain_compatible() {
    let (_dir, mgr) = make();
    for json in [
        serde_json::json!({"name": "omitted"}),
        serde_json::json!({"name": "null", "chromixOptions": null}),
        serde_json::json!({"name": "empty", "chromixOptions": {}}),
    ] {
        let input: CreateProfileInput = serde_json::from_value(json).unwrap();
        let created = mgr.create(input).unwrap();
        assert!(created.chromix_options.is_empty());
        assert_chromix_options(&mgr, &created.id, &Default::default());
    }
}
