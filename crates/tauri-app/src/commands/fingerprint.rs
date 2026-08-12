//! Fingerprint commands.
//!
//! - `generate`: delegates to `profile_manager::fingerprint::default_fingerprint`,
//!   the only fingerprint generator that exists in the workspace. Takes a
//!   `seed` string (any non-empty value; the generator currently ignores it
//!   but the parameter is kept for forward compatibility with P5).
//! - `devices` / `locales`: return static lists mirroring the private
//!   `all_device_families()` / `common_locales()` in `mcp-server/src/tools.rs`.
//!   Those helpers are private; rather than widen mcp-server's API for two
//!   constant arrays, the lists are duplicated here. If the lists diverge,
//!   update both — they're tagged with a comment in tools.rs pointing here.
//! - `reconcile` / `locale_for_country`: not yet implemented (no matching
//!   helper exists in the workspace). Return an explicit error so the
//!   frontend can detect the gap; wiring deferred to P5.

use multizen_core::FingerprintConfig;
use serde::Serialize;
use tauri::State;

use crate::AppState;

/// Device families mirroring `mcp-server/src/tools.rs::all_device_families`.
/// Kept in sync manually — search for `multizen-core::DeviceFamily` serde
/// rename values when updating.
const DEVICE_FAMILIES: &[&str] = &[
    "macbook-pro-14-m3",
    "macbook-pro-14-m3-pro",
    "macbook-pro-16-m3-pro",
    "macbook-air-13-m3",
    "macbook-air-15-m3",
    "imac-24-m3",
    "mac-mini-m2",
    "windows-laptop-intel",
    "windows-laptop-intel-uhd",
    "windows-laptop-amd",
    "windows-laptop-nvidia",
    "windows-laptop-nvidia-4050",
    "windows-desktop-nvidia",
    "windows-desktop-nvidia-4080",
    "windows-desktop-amd",
    "windows-desktop-intel",
    "linux-desktop-intel",
    "linux-desktop-amd",
    "linux-desktop-nvidia",
];

/// Locales mirroring `mcp-server/src/tools.rs::common_locales`.
const COMMON_LOCALES: &[&str] = &[
    "en-US", "en-GB", "zh-CN", "zh-TW", "ja-JP", "ko-KR", "de-DE", "fr-FR",
    "es-ES", "pt-BR", "ru-RU", "it-IT", "nl-NL", "pl-PL", "tr-TR", "ar-SA",
    "hi-IN", "id-ID", "th-TH", "vi-VN",
];

/// `DeviceCatalogEntry` mirror — matches the frontend type in
/// `ui/src/types.ts` (serde camelCase + `#[serde(tag = "kind")]`-free flat
/// object). The frontend uses `family` (matching `fingerprint.device`),
/// `label` (display name), and `screens` (the dropdown list of screen
/// resolutions for the selected device).
#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DeviceCatalogEntry {
    pub family: &'static str,
    pub label: &'static str,
    pub screens: Vec<ScreenOption>,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ScreenOption {
    pub width: u32,
    pub height: u32,
    pub label: &'static str,
}

/// `LocaleCatalogEntry` mirror — matches the frontend type in
/// `ui/src/types.ts`. The frontend uses `id` (the value stored back into
/// `fingerprint.locale` via `reconcile({ localeId })`), `label` (display),
/// `locale` (matched against the existing `fingerprint.locale`), `country`
/// (ISO-3166 alpha-2, derived from the locale's region subtag), and
/// `timezones` (allowed values for the timezone dropdown).
#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct LocaleCatalogEntry {
    pub id: &'static str,
    pub label: &'static str,
    pub locale: &'static str,
    pub country: &'static str,
    pub timezones: Vec<&'static str>,
}

/// Display label for a device family. Falls back to the family id itself
/// for families without a friendly name — the frontend's `Select` shows
/// `label`, so a kebab-case fallback would look poor but still function.
fn device_label(family: &str) -> &'static str {
    match family {
        "macbook-pro-14-m3" => "MacBook Pro 14\" (M3)",
        "macbook-pro-14-m3-pro" => "MacBook Pro 14\" (M3 Pro)",
        "macbook-pro-16-m3-pro" => "MacBook Pro 16\" (M3 Pro)",
        "macbook-air-13-m3" => "MacBook Air 13\" (M3)",
        "macbook-air-15-m3" => "MacBook Air 15\" (M3)",
        "imac-24-m3" => "iMac 24\" (M3)",
        "mac-mini-m2" => "Mac mini (M2)",
        "windows-laptop-intel" => "Windows Laptop (Intel)",
        "windows-laptop-intel-uhd" => "Windows Laptop (Intel UHD)",
        "windows-laptop-amd" => "Windows Laptop (AMD)",
        "windows-laptop-nvidia" => "Windows Laptop (NVIDIA)",
        "windows-laptop-nvidia-4050" => "Windows Laptop (NVIDIA 4050)",
        "windows-desktop-nvidia" => "Windows Desktop (NVIDIA)",
        "windows-desktop-nvidia-4080" => "Windows Desktop (NVIDIA 4080)",
        "windows-desktop-amd" => "Windows Desktop (AMD)",
        "windows-desktop-intel" => "Windows Desktop (Intel)",
        "linux-desktop-intel" => "Linux Desktop (Intel)",
        "linux-desktop-amd" => "Linux Desktop (AMD)",
        "linux-desktop-nvidia" => "Linux Desktop (NVIDIA)",
        _ => "Unknown device",
    }
}

/// Default screen options per device class. Laptops/desktops get a 1080p
/// and a 1440p option; the 14\"/13\" MacBook sizes also get their native
/// panel. This is a reasonable static catalog — the frontend's screen
/// dropdown is informational; the actual screen injected comes from the
/// fingerprint generator, not from this catalog.
fn device_screens(family: &str) -> Vec<ScreenOption> {
    let mut screens = Vec::new();
    // MacBook 14"/13" get a native smaller panel first.
    match family {
        "macbook-pro-14-m3" | "macbook-pro-14-m3-pro" | "macbook-air-13-m3" => {
            screens.push(ScreenOption {
                width: 1512,
                height: 982,
                label: "1512 × 982 (native)",
            });
        }
        "macbook-air-15-m3" | "macbook-pro-16-m3-pro" => {
            screens.push(ScreenOption {
                width: 1728,
                height: 1117,
                label: "1728 × 1117 (native)",
            });
        }
        _ => {}
    }
    // Common 1080p fallback for every device.
    screens.push(ScreenOption {
        width: 1920,
        height: 1080,
        label: "1920 × 1080",
    });
    // 1440p for desktop-class devices.
    if family.starts_with("windows-desktop")
        || family.starts_with("linux-desktop")
        || family == "imac-24-m3"
    {
        screens.push(ScreenOption {
            width: 2560,
            height: 1440,
            label: "2560 × 1440",
        });
    }
    screens
}

/// `(label, country, timezones)` for a BCP-47 locale id. `country` is the
/// ISO-3166 alpha-2 code derived from the region subtag (the part after
/// the last `-`). `timezones` is a small static list of IANA tzids that
/// are culturally appropriate for the locale — the frontend's timezone
/// dropdown is bounded by this list. If a locale is missing from the
/// table, `label` falls back to the locale id itself and `timezones` to
/// `["UTC"]`; the country is still derived from the region subtag so the
/// proxy-coherence check continues to work.
fn locale_meta(locale: &str) -> (&'static str, &'static str, Vec<&'static str>) {
    let country: &'static str = match locale.rsplit('-').next() {
        Some(region) if region.len() == 2 => {
            // SAFETY: static string slicing — the region is a substring of a
            // &'static str, but we can't return a borrowed substring of a
            // runtime slice safely across the static boundary without
            // leaking. Instead use a static lookup.
            static_region(region)
        }
        _ => "",
    };
    let (label, tzs) = match locale {
        "en-US" => ("English (United States)", vec!["America/New_York", "America/Chicago", "America/Los_Angeles"]),
        "en-GB" => ("English (United Kingdom)", vec!["Europe/London"]),
        "zh-CN" => ("中文 (简体, 中国大陆)", vec!["Asia/Shanghai"]),
        "zh-TW" => ("中文 (繁體, 台灣)", vec!["Asia/Taipei"]),
        "ja-JP" => ("日本語 (日本)", vec!["Asia/Tokyo"]),
        "ko-KR" => ("한국어 (대한민국)", vec!["Asia/Seoul"]),
        "de-DE" => ("Deutsch (Deutschland)", vec!["Europe/Berlin"]),
        "fr-FR" => ("Français (France)", vec!["Europe/Paris"]),
        "es-ES" => ("Español (España)", vec!["Europe/Madrid"]),
        "pt-BR" => ("Português (Brasil)", vec!["America/Sao_Paulo", "America/Manaus"]),
        "ru-RU" => ("Русский (Россия)", vec!["Europe/Moscow"]),
        "it-IT" => ("Italiano (Italia)", vec!["Europe/Rome"]),
        "nl-NL" => ("Nederlands (Nederland)", vec!["Europe/Amsterdam"]),
        "pl-PL" => ("Polski (Polska)", vec!["Europe/Warsaw"]),
        "tr-TR" => ("Türkçe (Türkiye)", vec!["Europe/Istanbul"]),
        "ar-SA" => ("العربية (السعودية)", vec!["Asia/Riyadh"]),
        "hi-IN" => ("हिन्दी (भारत)", vec!["Asia/Kolkata"]),
        "id-ID" => ("Bahasa Indonesia", vec!["Asia/Jakarta"]),
        "th-TH" => ("ไทย (ประเทศไทย)", vec!["Asia/Bangkok"]),
        "vi-VN" => ("Tiếng Việt", vec!["Asia/Ho_Chi_Minh"]),
        _ => ("", vec!["UTC"]),
    };
    (label, country, tzs)
}

/// Map a runtime 2-letter region subtag to a `&'static str` country code.
/// Covers the 20 regions used by `COMMON_LOCALES`; unknown regions return
/// an empty string (the frontend's proxy-coherence check treats an empty
/// country as "no match", which is safe).
fn static_region(region: &str) -> &'static str {
    match region {
        "US" => "US",
        "GB" => "GB",
        "CN" => "CN",
        "TW" => "TW",
        "JP" => "JP",
        "KR" => "KR",
        "DE" => "DE",
        "FR" => "FR",
        "ES" => "ES",
        "BR" => "BR",
        "RU" => "RU",
        "IT" => "IT",
        "NL" => "NL",
        "PL" => "PL",
        "TR" => "TR",
        "SA" => "SA",
        "IN" => "IN",
        "ID" => "ID",
        "TH" => "TH",
        "VN" => "VN",
        _ => "",
    }
}

#[tauri::command]
pub async fn fingerprint_generate(
    _state: State<'_, AppState>,
    seed: String,
) -> Result<FingerprintConfig, String> {
    Ok(profile_manager::fingerprint::default_fingerprint(&seed))
}

#[tauri::command]
pub async fn fingerprint_devices(
    _state: State<'_, AppState>,
) -> Result<Vec<DeviceCatalogEntry>, String> {
    Ok(DEVICE_FAMILIES
        .iter()
        .map(|&family| DeviceCatalogEntry {
            family,
            label: device_label(family),
            screens: device_screens(family),
        })
        .collect())
}

#[tauri::command]
pub async fn fingerprint_locales(
    _state: State<'_, AppState>,
) -> Result<Vec<LocaleCatalogEntry>, String> {
    Ok(COMMON_LOCALES
        .iter()
        .map(|&locale| {
            let (label, country, timezones) = locale_meta(locale);
            LocaleCatalogEntry {
                id: locale,
                label: if label.is_empty() { locale } else { label },
                locale,
                country,
                timezones,
            }
        })
        .collect())
}

#[tauri::command]
pub async fn fingerprint_reconcile(
    _state: State<'_, AppState>,
    _fingerprint: serde_json::Value,
) -> Result<serde_json::Value, String> {
    Err("fingerprint_reconcile not yet implemented (deferred to P5)".into())
}

#[tauri::command]
pub async fn fingerprint_locale_for_country(
    _state: State<'_, AppState>,
    _country: String,
) -> Result<String, String> {
    Err("fingerprint_locale_for_country not yet implemented (deferred to P5)".into())
}
