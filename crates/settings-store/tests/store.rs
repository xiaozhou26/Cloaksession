use multizen_core::{AppSettings, BrowserEngine};
use settings_store::{default_settings_path, SettingsStore};
use tempfile::TempDir;

#[test]
fn load_returns_defaults_when_file_missing() {
    let dir = TempDir::new().unwrap();
    let mut store = SettingsStore::new(&default_settings_path(dir.path()));
    let s = store.load().unwrap();
    assert_eq!(s.mcp_http_port, 7777);
    assert!(s.mcp_http_enabled);
    assert_eq!(s.browser_engine, BrowserEngine::Cloakbrowser);
    assert!(!s.usage_reporting);
}

#[test]
fn update_persists_and_caches() {
    let dir = TempDir::new().unwrap();
    let path = default_settings_path(dir.path());
    let mut store = SettingsStore::new(&path);
    let _ = store.load().unwrap();
    let patch = AppSettings {
        mcp_http_port: 9999,
        usage_reporting: true,
        ..Default::default()
    };
    let saved = store.update(patch.clone()).unwrap();
    assert_eq!(saved.mcp_http_port, 9999);
    // cache hit without re-reading file
    let cached = store.load().unwrap();
    assert_eq!(cached.mcp_http_port, 9999);
    // new store reads persisted file
    let mut store2 = SettingsStore::new(&path);
    let reloaded = store2.load().unwrap();
    assert_eq!(reloaded.mcp_http_port, 9999);
    assert!(reloaded.usage_reporting);
}

#[test]
fn load_recovers_from_corrupt_json() {
    let dir = TempDir::new().unwrap();
    let path = default_settings_path(dir.path());
    std::fs::write(&path, "{ not valid json").unwrap();
    let mut store = SettingsStore::new(&path);
    let s = store.load().unwrap();
    assert_eq!(s.mcp_http_port, 7777); // fell back to defaults
}

#[test]
fn load_normalizes_invalid_browser_engine() {
    let dir = TempDir::new().unwrap();
    let path = default_settings_path(dir.path());
    std::fs::write(&path, r#"{"mcpHttpPort": 7777, "browserEngine": "bogus"}"#).unwrap();
    let mut store = SettingsStore::new(&path);
    let s = store.load().unwrap();
    assert_eq!(s.browser_engine, BrowserEngine::Cloakbrowser); // reset to default
}

#[test]
fn load_clears_empty_browser_binary_path() {
    let dir = TempDir::new().unwrap();
    let path = default_settings_path(dir.path());
    std::fs::write(&path, r#"{"mcpHttpPort": 7777, "browserBinaryPath": "   "}"#).unwrap();
    let mut store = SettingsStore::new(&path);
    let s = store.load().unwrap();
    assert_eq!(s.browser_binary_path, None);
}
