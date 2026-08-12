use multizen_core::{
    ClientHints, DeviceFamily, FingerprintConfig, ScreenSize, WebGlConfig,
};

pub fn default_fingerprint(seed: &str) -> FingerprintConfig {
    FingerprintConfig {
        device: DeviceFamily::WindowsDesktopIntel,
        user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36".into(),
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
        screen: ScreenSize { width: 1920, height: 1080 },
        avail_screen: Some(ScreenSize { width: 1920, height: 1040 }),
        dpr: 1.0,
        webgl: WebGlConfig {
            vendor: "Google Inc. (Intel)".into(),
            renderer: "ANGLE (Intel, Intel(R) UHD Graphics 630 Direct3D11 vs_5_0 ps_5_0, D3D11)".into(),
        },
        hardware_concurrency: 8,
        device_memory: 8,
        fonts_dir: Some(r"C:\Windows\Fonts".into()),
        storage_quota: Some(2_000_000_000),
        seed: Some(seed.to_string()),
    }
}
