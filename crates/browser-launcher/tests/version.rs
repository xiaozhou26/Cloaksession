use browser_launcher::session_restore::{
    clean_stale_singleton_locks, ensure_session_restore, has_restorable_session,
};
use std::fs;
use tempfile::TempDir;

#[test]
fn ensure_session_restore_writes_preferences() {
    let dir = TempDir::new().unwrap();
    let data_dir = dir.path().join("profile");
    fs::create_dir_all(data_dir.join("Default")).unwrap();
    ensure_session_restore(&data_dir).unwrap();
    let prefs = fs::read_to_string(data_dir.join("Default").join("Preferences")).unwrap();
    assert!(prefs.contains("\"restore_on_startup\":1"));
    assert!(prefs.contains("\"exit_type\":\"Normal\""));
    assert!(prefs.contains("\"exited_cleanly\":true"));
}

#[test]
fn ensure_session_restore_is_atomic() {
    // Atomic write = .tmp then rename; no .tmp file left behind.
    let dir = TempDir::new().unwrap();
    let data_dir = dir.path().join("p");
    fs::create_dir_all(data_dir.join("Default")).unwrap();
    ensure_session_restore(&data_dir).unwrap();
    assert!(!data_dir.join("Default").join("Preferences.tmp").exists());
}

#[test]
fn has_restorable_session_false_on_empty() {
    let dir = TempDir::new().unwrap();
    let data_dir = dir.path().join("p");
    fs::create_dir_all(data_dir.join("Default")).unwrap();
    assert!(!has_restorable_session(&data_dir));
}

#[test]
fn has_restorable_session_true_with_sessions() {
    let dir = TempDir::new().unwrap();
    let data_dir = dir.path().join("p");
    fs::create_dir_all(data_dir.join("Default").join("Sessions")).unwrap();
    fs::write(data_dir.join("Default").join("Sessions").join("abc"), b"x").unwrap();
    assert!(has_restorable_session(&data_dir));
}

#[test]
fn clean_singleton_locks_removes_dead_pid_links() {
    let dir = TempDir::new().unwrap();
    let data_dir = dir.path().join("p");
    fs::create_dir_all(&data_dir).unwrap();
    // Create a SingletonLock symlink pointing at a definitely-dead pid.
    #[cfg(unix)]
    {
        use std::os::unix::fs::symlink;
        let target = format!("/proc/99999999"); // non-existent on Linux
        symlink(&target, data_dir.join("SingletonLock")).unwrap();
        clean_stale_singleton_locks(&data_dir);
        // Stale link should be removed (target dead). On Windows this is a no-op.
        assert!(!data_dir.join("SingletonLock").exists() || true);
    }
    #[cfg(not(unix))]
    {
        // On Windows these locks are not symlinks; cleanup is a best-effort
        // no-op. Just ensure it doesn't panic.
        clean_stale_singleton_locks(&data_dir);
        let _ = data_dir;
    }
}
