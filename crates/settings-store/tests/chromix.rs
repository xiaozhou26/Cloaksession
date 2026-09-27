use multizen_core::{AppSettings, BrowserEngine};
use serde_json::json;
use settings_store::{SettingsStore, default_settings_path};
use tempfile::TempDir;

#[test]
fn chromix_options_survive_reload_without_losing_sdk_fields() {
    let dir = TempDir::new().unwrap();
    let path = default_settings_path(dir.path());
    let mut settings = AppSettings {
        browser_engine: BrowserEngine::Chromix,
        ..Default::default()
    };
    settings.chromix.node_path = "/custom/node".into();
    settings.chromix.options = json!({
        "headless": false,
        "viewport": null,
        "stealthArgs": false,
        "args": ["--fingerprint=18446744073709551615", "--fingerprint-noise=false"],
        "humanConfig": {"typing": {"delay": 23}},
        "launchOptions": {"timeout": 45678},
        "contextOptions": {"permissions": ["geolocation"]},
        "futureSdkOption": {"enabled": true}
    })
    .as_object()
    .unwrap()
    .clone();
    settings
        .chromix
        .environment
        .insert("CHROMIX_CACHE_DIR".into(), "/custom/cache".into());
    SettingsStore::new(&path).update(settings.clone()).unwrap();
    let actual = SettingsStore::new(&path).load().unwrap();
    assert_eq!(actual.browser_engine, BrowserEngine::Chromix);
    assert_eq!(actual.chromix.node_path, "/custom/node");
    assert_eq!(actual.chromix.options, settings.chromix.options);
    assert_eq!(actual.chromix.environment, settings.chromix.environment);
}

#[test]
fn profile_options_override_global_values_without_merging_arrays_or_objects() {
    let global = multizen_core::ChromixSettings {
        options: json!({
            "args": ["--fingerprint=1"],
            "contextOptions": {"locale": "en-US", "timezoneId": "UTC"},
            "headless": false,
            "viewport": {"width": 1280, "height": 720}
        })
        .as_object()
        .unwrap()
        .clone(),
        ..Default::default()
    };
    let profile = json!({
        "args": [], "contextOptions": {"locale": "zh-CN"}, "viewport": null
    });
    let config = global.with_profile_options(profile.as_object().unwrap());
    assert_eq!(config.options["args"], json!([]));
    assert_eq!(config.options["contextOptions"], json!({"locale": "zh-CN"}));
    assert_eq!(config.options["headless"], false);
    assert_eq!(config.options["viewport"], serde_json::Value::Null);
    assert_eq!(global.options["args"], json!(["--fingerprint=1"]));
}

#[test]
fn old_settings_get_empty_chromix_configuration() {
    let dir = TempDir::new().unwrap();
    let path = default_settings_path(dir.path());
    std::fs::write(
        &path,
        r#"{"browserEngine":"cft","browserBinaryPath":"/local/chrome"}"#,
    )
    .unwrap();
    let actual = SettingsStore::new(&path).load().unwrap();
    assert_eq!(actual.browser_engine, BrowserEngine::Cft);
    assert_eq!(actual.browser_binary_path.as_deref(), Some("/local/chrome"));
    assert_eq!(actual.chromix.node_path, "node");
    assert!(actual.chromix.options.is_empty());
    assert!(actual.chromix.environment.is_empty());
}

#[test]
fn chromix_configuration_requires_objects_and_string_environment_values() {
    for chromix in [
        json!({"options": []}),
        json!({"options": null}),
        json!({"environment": {"CHROMIX_CACHE_DIR": 42}}),
    ] {
        let mut value = serde_json::to_value(AppSettings::default()).unwrap();
        value["chromix"] = chromix;
        assert!(serde_json::from_value::<AppSettings>(value).is_err());
    }
}
