use profile_manager::fingerprint::default_fingerprint;

#[test]
fn default_is_windows_chrome_us() {
    let fp = default_fingerprint("abc");
    assert_eq!(fp.locale, "en-US");
    assert_eq!(fp.timezone, "America/New_York");
    assert_eq!(fp.country, "US");
    assert!(fp.user_agent.contains("Windows NT 10.0"));
    assert!(fp.user_agent.contains("Chrome/148"));
    assert_eq!(fp.platform, "Win32");
    assert_eq!(fp.client_hints.sec_ch_ua_platform, "Windows");
    assert_eq!(fp.dpr, 1.0);
    assert_eq!(fp.hardware_concurrency, 8);
    assert_eq!(fp.device_memory, 8);
    assert_eq!(fp.seed, Some("abc".to_string()));
}

#[test]
fn screen_and_webgl_populated() {
    let fp = default_fingerprint("x");
    assert!(fp.screen.width > 0 && fp.screen.height > 0);
    assert!(!fp.webgl.vendor.is_empty());
    assert!(!fp.webgl.renderer.is_empty());
}
