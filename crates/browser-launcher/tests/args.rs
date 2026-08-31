use browser_launcher::args::{
    build_cloak_fingerprint_args, build_spawn_args, device_memory_api_value, fingerprint_seed_value,
};
use multizen_core::{BrowserEngine, Profile};

fn base_profile() -> Profile {
    use multizen_core::*;
    Profile {
        id: "p1".into(),
        name: "t".into(),
        notes: None,
        tags: vec![],
        proxy: None,
        fingerprint: FingerprintConfig {
            device: DeviceFamily::WindowsDesktopIntel,
            user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/148".into(),
            platform: "Win32".into(),
            client_hints: ClientHints {
                sec_ch_ua: r#""Chromium";v="148", "Google Chrome";v="148", "Not?A_Brand";v="99""#.into(),
                sec_ch_ua_platform: "Windows".into(),
                sec_ch_ua_platform_version: "10.0.0".into(),
                sec_ch_ua_arch: "x86".into(),
                sec_ch_ua_bitness: "64".into(),
                sec_ch_ua_mobile: "?0".into(),
                sec_ch_ua_model: "".into(),
                sec_ch_ua_full_version_list: r#""Chromium";v="148.0.0.0", "Google Chrome";v="148.0.0.0", "Not?A_Brand";v="99.0.0.0""#.into(),
            },
            locale: "en-US".into(),
            languages: vec!["en-US".into(), "en".into()],
            accept_language: "en-US,en;q=0.9".into(),
            timezone: "America/New_York".into(),
            country: "US".into(),
            screen: multizen_core::ScreenSize { width: 1920, height: 1080 },
            avail_screen: Some(multizen_core::ScreenSize { width: 1920, height: 1040 }),
            dpr: 1.0,
            webgl: multizen_core::WebGlConfig {
                vendor: "Google Inc. (Intel)".into(),
                renderer: "ANGLE (Intel UHD)".into(),
            },
            hardware_concurrency: 8,
            device_memory: 8,
            fonts_dir: None,
            storage_quota: Some(2_000_000_000),
            seed: Some("abc".into()),
        },
        extensions: None,
        icon: None,
        start_url: Some("https://example.com".into()),
        search_provider: None,
        data_dir: "/tmp/p1".into(),
        created_at: "2026-01-01T00:00:00Z".into(),
        updated_at: "2026-01-01T00:00:00Z".into(),
        last_opened_at: None,
        proxy_country: None,
    }
}

#[test]
fn base_args_always_present() {
    let p = base_profile();
    let args = build_spawn_args(&p, BrowserEngine::Cloakbrowser, 9222, "/tmp/p1/engines/cloakbrowser", None, None, None);
    assert!(args.iter().any(|a| a == "--user-data-dir=/tmp/p1/engines/cloakbrowser"));
    assert!(args.iter().any(|a| a == "--remote-debugging-port=9222"));
    assert!(args.iter().any(|a| a == "--no-first-run"));
    assert!(args.iter().any(|a| a == "--no-default-browser-check"));
    assert!(args.iter().any(|a| a == "--restore-last-session"));
    assert!(args.iter().any(|a| a == "--lang=en-US"));
    assert!(args.iter().any(|a| a == "--accept-lang=en-US,en;q=0.9"));
    assert!(args.iter().any(|a| a == "--window-size=1920,1080"));
    assert!(args.iter().any(|a| a == "--force-device-scale-factor=1"));
    assert!(args.iter().all(|a| a != "--incognito" && a != "--guest"));
}

#[test]
fn cloak_engine_adds_fingerprint_flags() {
    let p = base_profile();
    let args = build_spawn_args(&p, BrowserEngine::Cloakbrowser, 9222, "/tmp/p1/engines/cloakbrowser", None, None, None);
    assert!(args.iter().any(|a| a.starts_with("--fingerprint=")), "cloak must pass --fingerprint=");
    assert!(args.iter().any(|a| a.starts_with("--fingerprint-platform=")));
    assert!(args.iter().any(|a| a.starts_with("--fingerprint-timezone=America/New_York")));
}

#[test]
fn cft_engine_adds_user_agent_and_test_type() {
    let p = base_profile();
    let args = build_spawn_args(&p, BrowserEngine::Cft, 9222, "/tmp/p1", None, None, None);
    assert!(args.iter().any(|a| a.starts_with("--user-agent=")));
    assert!(args.iter().any(|a| a == "--test-type=gpu"));
    // CFT must NOT pass --fingerprint-*
    assert!(args.iter().all(|a| !a.starts_with("--fingerprint=")));
}

#[test]
fn cft_engine_preserves_custom_user_agent() {
    let mut p = base_profile();
    p.fingerprint.user_agent = "Custom/99.1 test-agent".into();
    let args = build_spawn_args(&p, BrowserEngine::Cft, 9222, "/tmp/p1", None, None, None);
    assert!(args.iter().any(|a| a == "--user-agent=Custom/99.1 test-agent"));
}

#[test]
fn cloak_engine_passes_custom_user_agent() {
    let mut p = base_profile();
    p.fingerprint.user_agent = "Custom/99.1 test-agent".into();
    let args = build_spawn_args(&p, BrowserEngine::Cloakbrowser, 9222, "/tmp/p1", None, None, None);
    assert!(args.iter().any(|a| a == "--fingerprint-user-agent=Custom/99.1 test-agent"));
}

#[test]
fn proxy_adds_bridge_url_and_dns_flags() {
    let p = base_profile();
    let args = build_spawn_args(&p, BrowserEngine::Cloakbrowser, 9222, "/d", Some("socks5://127.0.0.1:1080"), None, None);
    assert!(args.iter().any(|a| a == "--proxy-server=socks5://127.0.0.1:1080"));
    assert!(args.iter().any(|a| a == "--force-webrtc-ip-handling-policy=disable_non_proxied_udp"));
    assert!(args.iter().any(|a| a == "--dns-over-https-mode=off"));
    assert!(args.iter().any(|a| a == "--disable-background-networking"));
}

#[test]
fn geo_coords_add_fingerprint_location_and_webrtc_ip() {
    let p = base_profile();
    let args = build_spawn_args(&p, BrowserEngine::Cloakbrowser, 9222, "/d", Some("socks5://127.0.0.1:1080"), Some((40.7, -74.0)), None);
    assert!(args.iter().any(|a| a == "--fingerprint-location=40.7,-74"));
    assert!(args.iter().any(|a| a == "--fingerprint-webrtc-ip=auto"));
}

#[test]
fn fingerprint_seed_is_5_digit_numeric() {
    let s = fingerprint_seed_value("p1", Some("abc"));
    assert!(s.len() == 5, "seed must be 5 digits, got {s}");
    assert!(s.chars().all(|c| c.is_ascii_digit()));
    let n: u32 = s.parse().unwrap();
    assert!((10000..=99999).contains(&n));
}

#[test]
fn device_memory_api_value_clamps() {
    assert_eq!(device_memory_api_value(8), 8);
    assert_eq!(device_memory_api_value(16), 8, "clamped to 8");
    assert_eq!(device_memory_api_value(4), 4);
    assert_eq!(device_memory_api_value(6), 8, "round(log2(6))=3 → 2^3=8");
    assert_eq!(device_memory_api_value(2), 2);
}

#[test]
fn cloak_storage_quota_clears_browser_scan_private_mode_heuristic() {
    let p = base_profile();
    let args = build_cloak_fingerprint_args(&p.id, &p.fingerprint);
    assert!(args.iter().any(|a| a == "--fingerprint-storage-quota=16384"));
}

#[test]
fn cloak_fingerprint_args_include_gpu_and_storage() {
    let p = base_profile();
    let fp_args = build_cloak_fingerprint_args(&p.id, &p.fingerprint);
    assert!(fp_args.iter().any(|a| a.starts_with("--fingerprint-gpu-vendor=Google Inc. (Intel)")));
    assert!(fp_args.iter().any(|a| a.starts_with("--fingerprint-storage-quota=")));
}
