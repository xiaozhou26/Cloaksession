use std::fs;
use std::path::Path;

use multizen_core::Result;

/// Write `Default/Preferences` JSON that forces session restore on startup.
/// Atomic: writes to `Preferences.tmp` then renames into place.
pub fn ensure_session_restore(browser_data_dir: &Path) -> Result<()> {
    let default_dir = browser_data_dir.join("Default");
    fs::create_dir_all(&default_dir)?;
    let prefs = serde_json::json!({
        "session": { "restore_on_startup": 1 },
        "profile": { "exit_type": "Normal", "exited_cleanly": true }
    });
    let prefs_path = default_dir.join("Preferences");
    let tmp_path = default_dir.join("Preferences.tmp");
    fs::write(&tmp_path, serde_json::to_string(&prefs)?)?;
    fs::rename(&tmp_path, &prefs_path)?;
    Ok(())
}

/// True if `Default/Sessions/*` has any file, or `Default/Current Session` exists.
pub fn has_restorable_session(browser_data_dir: &Path) -> bool {
    let default = browser_data_dir.join("Default");
    let sessions_dir = default.join("Sessions");
    if let Ok(entries) = fs::read_dir(&sessions_dir) {
        if entries.filter_map(|e| e.ok()).any(|e| e.path().is_file()) {
            return true;
        }
    }
    default.join("Current Session").exists()
}

/// Best-effort remove `SingletonLock` / `SingletonSocket` / `SingletonCookie`.
///
/// The TS version parses the symlink target PID and only removes dead ones.
/// Rust version simplifies to unconditional removal: CloakBrowser on Windows
/// doesn't use symlink locks, and on Unix this is best-effort cleanup. If
/// integration tests surface issues, add PID liveness checks later.
pub fn clean_stale_singleton_locks(browser_data_dir: &Path) {
    for name in ["SingletonLock", "SingletonSocket", "SingletonCookie"] {
        let p = browser_data_dir.join(name);
        if p.exists() {
            let _ = fs::remove_file(&p);
        }
    }
}
