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
) -> Result<Vec<&'static str>, String> {
    Ok(DEVICE_FAMILIES.to_vec())
}

#[tauri::command]
pub async fn fingerprint_locales(
    _state: State<'_, AppState>,
) -> Result<Vec<&'static str>, String> {
    Ok(COMMON_LOCALES.to_vec())
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
