use multizen_core::{BrowserEngine, FingerprintConfig, Profile};
use sha2::{Digest, Sha256};

/// `min(8, 2^round(log2(gb)))` — matches CloakBrowser's deviceMemory API clamping.
pub fn device_memory_api_value(gb: u32) -> u32 {
    if gb == 0 {
        return 0;
    }
    let log2 = (gb as f64).log2().round() as u32;
    (1u32 << log2).min(8)
}

/// Derives the numeric `--fingerprint=` seed from the profile's entropy seed
/// (or the profile id if no seed): SHA256, first 8 hex chars →
/// `10000 + (parseInt(hex,16) % 90000)`, yielding a 5-digit string in [10000,99999].
pub fn fingerprint_seed_value(profile_id: &str, fp_seed: Option<&str>) -> String {
    let input = fp_seed.unwrap_or(profile_id);
    let mut hasher = Sha256::new();
    hasher.update(input.as_bytes());
    let digest = hasher.finalize();
    let hex8 = &format!("{:x}", digest)[..8];
    let n = 10000u32 + (u32::from_str_radix(hex8, 16).unwrap_or(0) % 90000);
    n.to_string()
}

fn cloak_platform(device: &multizen_core::DeviceFamily) -> &'static str {
    use multizen_core::DeviceFamily::*;
    match device {
        MacbookPro14M3 | MacbookPro14M3Pro | MacbookPro16M3Pro | MacbookAir13M3
        | MacbookAir15M3 | Imac24M3 | MacMiniM2 => "macos",
        WindowsLaptopIntel | WindowsLaptopIntelUhd | WindowsLaptopAmd | WindowsLaptopNvidia
        | WindowsLaptopNvidia4050 | WindowsDesktopNvidia | WindowsDesktopNvidia4080
        | WindowsDesktopAmd | WindowsDesktopIntel => "windows",
        LinuxDesktopIntel | LinuxDesktopAmd | LinuxDesktopNvidia => "linux",
    }
}

/// First non-Chromium / non-GREASE brand from the sec-ch-ua-full-version-list.
/// Returns (brand, version) where brand is Chrome|Edge|Opera|Vivaldi|Brave.
fn primary_brand(sec_ch_ua: &str) -> Option<(&'static str, String)> {
    // sec-ch-ua looks like: `"Chromium";v="148", "Google Chrome";v="148", "Not?A_Brand";v="99"`
    // We want the "Google Chrome" → "Chrome" mapping. For simplicity here, return
    // the brand whose name (excluding Chromium/Not*) maps via the brand map.
    let brand_map = [("Google Chrome", "Chrome"), ("Microsoft Edge", "Edge"), ("Opera", "Opera"), ("Vivaldi", "Vivaldi"), ("Brave", "Brave")];
    for entry in sec_ch_ua.split(',') {
        let trimmed = entry.trim();
        if let Some(start) = trimmed.find('"') {
            if let Some(end) = trimmed[start + 1..].find('"') {
                let name = &trimmed[start + 1..start + 1 + end];
                for (long, short) in brand_map {
                    if name == long {
                        let v_start = trimmed.rfind("v=\"").map(|i| i + 3).unwrap_or(0);
                        let v_end = trimmed.rfind('"').unwrap_or(trimmed.len());
                        let version = trimmed[v_start..v_end].to_string();
                        return Some((short, version));
                    }
                }
            }
        }
    }
    None
}

pub fn build_cloak_fingerprint_args(profile_id: &str, fp: &FingerprintConfig) -> Vec<String> {
    let mut args = vec![
        format!("--fingerprint={}", fingerprint_seed_value(profile_id, fp.seed.as_deref())),
        format!("--fingerprint-platform={}", cloak_platform(&fp.device)),
        format!("--fingerprint-locale={}", fp.locale),
        format!("--fingerprint-timezone={}", fp.timezone),
        format!("--fingerprint-screen-width={}", fp.screen.width),
        format!("--fingerprint-screen-height={}", fp.screen.height),
        format!("--fingerprint-hardware-concurrency={}", fp.hardware_concurrency),
        format!("--fingerprint-device-memory={}", device_memory_api_value(fp.device_memory)),
        // Canvas noise off — FingerprintJS detects canvas noise injection as
        // tampering. The seed-based fingerprint already provides stable
        // canvas/audio/WebGL values; noise on top triggers anti-detect.
        "--fingerprint-noise=false".to_string(),
    ];
    if let Some((brand, version)) = primary_brand(&fp.client_hints.sec_ch_ua) {
        args.push(format!("--fingerprint-brand={brand}"));
        args.push(format!("--fingerprint-brand-version={version}"));
    }
    if !fp.webgl.vendor.is_empty() {
        args.push(format!("--fingerprint-gpu-vendor={}", fp.webgl.vendor));
    }
    if !fp.webgl.renderer.is_empty() {
        args.push(format!("--fingerprint-gpu-renderer={}", fp.webgl.renderer));
    }
    if !fp.client_hints.sec_ch_ua_platform_version.is_empty() {
        args.push(format!("--fingerprint-platform-version={}", fp.client_hints.sec_ch_ua_platform_version));
    }
    // Taskbar height (Windows persona only): screen.height - availScreen.height
    if matches!(fp.device, multizen_core::DeviceFamily::WindowsLaptopIntel
        | multizen_core::DeviceFamily::WindowsLaptopIntelUhd | multizen_core::DeviceFamily::WindowsLaptopAmd
        | multizen_core::DeviceFamily::WindowsLaptopNvidia | multizen_core::DeviceFamily::WindowsLaptopNvidia4050
        | multizen_core::DeviceFamily::WindowsDesktopNvidia | multizen_core::DeviceFamily::WindowsDesktopNvidia4080
        | multizen_core::DeviceFamily::WindowsDesktopAmd | multizen_core::DeviceFamily::WindowsDesktopIntel)
    {
        if let Some(avail) = &fp.avail_screen {
            let reserved = fp.screen.height as i64 - avail.height as i64;
            if reserved > 0 {
                args.push(format!("--fingerprint-taskbar-height={reserved}"));
                args.push("--fingerprint-windows-font-metrics".to_string());
            }
        }
    }
    if let Some(dir) = &fp.fonts_dir {
        if !dir.is_empty() {
            args.push(format!("--fingerprint-fonts-dir={dir}"));
        }
    }
    if let Some(q) = fp.storage_quota {
        if q > 0 {
            // CloakBrowser expects storage quota in MB, not bytes.
            args.push(format!("--fingerprint-storage-quota={}", q / 1_000_000));
        }
    }
    args
}

pub fn build_spawn_args(
    profile: &Profile,
    engine: BrowserEngine,
    port: u16,
    browser_data_dir: &str,
    proxy_bridge_url: Option<&str>,
    geo_coords: Option<(f64, f64)>,
    companion_dir: Option<&str>,
) -> Vec<String> {
    let fp = &profile.fingerprint;
    let mut args = vec![
        format!("--user-data-dir={browser_data_dir}"),
        format!("--remote-debugging-port={port}"),
        "--no-first-run".to_string(),
        "--no-default-browser-check".to_string(),
        "--restore-last-session".to_string(),
        "--disable-features=Translate".to_string(),
        format!("--lang={}", fp.locale),
        format!("--accept-lang={}", fp.accept_language),
        format!("--window-size={},{}", fp.screen.width, fp.screen.height),
    ];

    // Platform-specific
    #[cfg(target_os = "macos")]
    args.push("--use-mock-keychain".to_string());
    #[cfg(target_os = "linux")]
    args.push("--password-store=basic".to_string());

    match engine {
        BrowserEngine::Cloakbrowser => {
            args.extend(build_cloak_fingerprint_args(&profile.id, fp));
            if proxy_bridge_url.is_some() {
                args.push("--fingerprint-webrtc-ip=auto".to_string());
            }
            if let Some((lat, lon)) = geo_coords {
                args.push(format!("--fingerprint-location={lat},{lon}"));
            }
        }
        BrowserEngine::Cft => {
            args.push(format!("--user-agent={}", fp.user_agent));
            args.push("--test-type=gpu".to_string());
        }
    }

    if let Some(url) = proxy_bridge_url {
        args.push(format!("--proxy-server={url}"));
        args.push("--force-webrtc-ip-handling-policy=disable_non_proxied_udp".to_string());
        args.push("--enforce-webrtc-ip-permission-check".to_string());
        args.push(
            "--disable-features=DnsOverHttps,DnsOverHttpsUpgrade,EncryptedClientHello,AsyncDns,DnsHttpsSvcb,DnsHttpsSvcbAlpn,NetworkPrediction".to_string(),
        );
        args.push("--dns-over-https-mode=off".to_string());
        args.push("--dns-prefetch-disable".to_string());
        args.push("--disable-async-dns".to_string());
        args.push("--no-prerender".to_string());
        args.push("--no-pings".to_string());
        args.push("--disable-background-networking".to_string());
        args.push("--disable-component-update".to_string());
        args.push("--disable-domain-reliability".to_string());
        args.push("--disable-client-side-phishing-detection".to_string());
    }

    // Extensions: companion + profile.extensions (enabled, dir exists)
    let mut ext_dirs: Vec<String> = Vec::new();
    if let Some(c) = companion_dir {
        ext_dirs.push(c.to_string());
    }
    if let Some(exts) = &profile.extensions {
        for e in exts {
            if e.enabled && !e.dir.is_empty() {
                ext_dirs.push(e.dir.clone());
            }
        }
    }
    if !ext_dirs.is_empty() {
        let joined = ext_dirs.join(",");
        args.push(format!("--load-extension={joined}"));
        args.push(format!("--disable-extensions-except={joined}"));
    }

    // Start URL only if no restorable session — caller decides; here we always
    // append the sanitized start URL as positional last arg if present.
    if let Some(url) = &profile.start_url {
        if url.starts_with("http://") || url.starts_with("https://") || url == "about:blank" {
            args.push(url.clone());
        }
    }
    args
}
