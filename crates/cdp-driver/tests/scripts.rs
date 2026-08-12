use cdp_driver::scripts::{build_fingerprint_preload_script, build_webrtc_block_script, build_webrtc_spoof_script};
use multizen_core::{ClientHints, DeviceFamily, FingerprintConfig, ScreenSize, WebGlConfig};

fn fp() -> FingerprintConfig {
    FingerprintConfig {
        device: DeviceFamily::WindowsDesktopIntel,
        user_agent: "UA".into(),
        platform: "Win32".into(),
        client_hints: ClientHints {
            sec_ch_ua: "x".into(),
            sec_ch_ua_platform: "Windows".into(),
            sec_ch_ua_platform_version: "10.0.0".into(),
            sec_ch_ua_arch: "x86".into(),
            sec_ch_ua_bitness: "64".into(),
            sec_ch_ua_mobile: "?0".into(),
            sec_ch_ua_model: "".into(),
            sec_ch_ua_full_version_list: "x".into(),
        },
        locale: "en-US".into(),
        languages: vec!["en-US".into()],
        accept_language: "en-US".into(),
        timezone: "America/New_York".into(),
        country: "US".into(),
        screen: ScreenSize { width: 1920, height: 1080 },
        avail_screen: Some(ScreenSize { width: 1920, height: 1040 }),
        dpr: 1.0,
        webgl: WebGlConfig { vendor: "Intel".into(), renderer: "ANGLE".into() },
        hardware_concurrency: 8,
        device_memory: 8,
        fonts_dir: None,
        storage_quota: None,
        seed: None,
    }
}

#[test]
fn webrtc_block_script_disables_rtp() {
    let s = build_webrtc_block_script();
    assert!(s.contains("RTCPeerConnection"));
}

#[test]
fn webrtc_spoof_script_includes_ip() {
    let s = build_webrtc_spoof_script("1.2.3.4");
    assert!(s.contains("1.2.3.4"));
}

#[test]
fn preload_script_overrides_platform_and_webgl() {
    let s = build_fingerprint_preload_script(&fp());
    assert!(s.contains("Win32"));
    assert!(s.contains("hardwareConcurrency"));
    assert!(s.contains("UNMASKED_VENDOR_WEBGL"));
    assert!(s.contains("Intel"));
}
