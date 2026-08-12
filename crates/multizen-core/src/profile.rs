use serde::{Deserialize, Serialize};

pub type ProfileId = String;

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProxyConfig {
    #[serde(rename = "type")]
    pub proxy_type: String, // "http" | "socks5"
    pub host: String,
    pub port: u16,
    pub username: Option<String>,
    pub password: Option<String>,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum DeviceFamily {
    #[serde(rename = "macbook-pro-14-m3")]
    MacbookPro14M3,
    #[serde(rename = "macbook-pro-14-m3-pro")]
    MacbookPro14M3Pro,
    #[serde(rename = "macbook-pro-16-m3-pro")]
    MacbookPro16M3Pro,
    #[serde(rename = "macbook-air-13-m3")]
    MacbookAir13M3,
    #[serde(rename = "macbook-air-15-m3")]
    MacbookAir15M3,
    #[serde(rename = "imac-24-m3")]
    Imac24M3,
    #[serde(rename = "mac-mini-m2")]
    MacMiniM2,
    #[serde(rename = "windows-laptop-intel")]
    WindowsLaptopIntel,
    #[serde(rename = "windows-laptop-intel-uhd")]
    WindowsLaptopIntelUhd,
    #[serde(rename = "windows-laptop-amd")]
    WindowsLaptopAmd,
    #[serde(rename = "windows-laptop-nvidia")]
    WindowsLaptopNvidia,
    #[serde(rename = "windows-laptop-nvidia-4050")]
    WindowsLaptopNvidia4050,
    #[serde(rename = "windows-desktop-nvidia")]
    WindowsDesktopNvidia,
    #[serde(rename = "windows-desktop-nvidia-4080")]
    WindowsDesktopNvidia4080,
    #[serde(rename = "windows-desktop-amd")]
    WindowsDesktopAmd,
    #[serde(rename = "windows-desktop-intel")]
    WindowsDesktopIntel,
    #[serde(rename = "linux-desktop-intel")]
    LinuxDesktopIntel,
    #[serde(rename = "linux-desktop-amd")]
    LinuxDesktopAmd,
    #[serde(rename = "linux-desktop-nvidia")]
    LinuxDesktopNvidia,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ClientHints {
    pub sec_ch_ua: String,
    pub sec_ch_ua_platform: String,
    pub sec_ch_ua_platform_version: String,
    pub sec_ch_ua_arch: String, // "arm" | "x86"
    pub sec_ch_ua_bitness: String, // "64" | "32"
    pub sec_ch_ua_mobile: String, // "?0" | "?1"
    pub sec_ch_ua_model: String,
    pub sec_ch_ua_full_version_list: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ScreenSize {
    pub width: u32,
    pub height: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct WebGlConfig {
    pub vendor: String,
    pub renderer: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct FingerprintConfig {
    pub device: DeviceFamily,
    pub user_agent: String,
    pub platform: String, // "MacIntel" | "Win32" | "Linux x86_64"
    pub client_hints: ClientHints,
    pub locale: String,
    pub languages: Vec<String>,
    pub accept_language: String,
    pub timezone: String,
    pub country: String,
    pub screen: ScreenSize,
    pub avail_screen: Option<ScreenSize>,
    pub dpr: f64,
    pub webgl: WebGlConfig,
    pub hardware_concurrency: u32,
    pub device_memory: u32,
    pub fonts_dir: Option<String>,
    pub storage_quota: Option<u64>,
    pub seed: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ExtensionConfig {
    pub id: String,
    pub name: String,
    pub version: String,
    pub enabled: bool,
    pub scope: String, // "shared" | "profile"
    pub dir: String,
    pub source: String, // "web-store" | "file" | "folder"
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Profile {
    pub id: ProfileId,
    pub name: String,
    pub notes: Option<String>,
    pub tags: Vec<String>,
    pub proxy: Option<ProxyConfig>,
    pub fingerprint: FingerprintConfig,
    pub extensions: Option<Vec<ExtensionConfig>>,
    pub icon: Option<String>,
    pub start_url: Option<String>,
    pub search_provider: Option<String>,
    pub data_dir: String,
    pub created_at: String,
    pub updated_at: String,
    pub last_opened_at: Option<String>,
    pub proxy_country: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProfileSummary {
    pub id: ProfileId,
    pub name: String,
    pub tags: Vec<String>,
    pub last_opened_at: Option<String>,
    pub is_running: bool,
    pub icon: Option<String>,
    pub proxy: Option<ProxyConfig>,
    pub timezone: Option<String>,
    pub proxy_country: Option<String>,
    pub device: Option<DeviceFamily>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CreateProfileInput {
    pub name: String,
    pub notes: Option<String>,
    pub tags: Option<Vec<String>>,
    pub icon: Option<String>,
    pub start_url: Option<String>,
    pub search_provider: Option<String>,
    pub proxy: Option<ProxyConfig>,
    pub fingerprint: Option<PartialFingerprintInput>,
    pub extensions: Option<Vec<ExtensionConfig>>,
}

/// Partial fingerprint patch — all fields optional, merges over existing.
/// Used by `CreateProfileInput` where only a few fields are seeded; the
/// rest default via `default_fingerprint`. `UpdateProfileInput` uses the
/// full `FingerprintConfig` (whole-replace) because the UI always holds a
/// complete config (from `fingerprint_reconcile` / `fingerprint_generate`).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PartialFingerprintInput {
    pub user_agent: Option<String>,
    pub locale: Option<String>,
    pub timezone: Option<String>,
    pub country: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct UpdateProfileInput {
    pub name: Option<String>,
    pub notes: Option<String>,
    pub tags: Option<Vec<String>>,
    pub icon: Option<Option<String>>, // None=keep, Some(None)=clear, Some(Some)=set
    pub start_url: Option<Option<String>>,
    pub search_provider: Option<Option<String>>,
    pub proxy: Option<Option<ProxyConfig>>,
    /// Whole-replace: the UI always holds a complete FingerprintConfig
    /// (produced by `fingerprint_reconcile` / `fingerprint_generate`), so
    /// merging individual fields would silently drop fields the
    /// `PartialFingerprintInput` struct doesn't model (fontsDir,
    /// storageQuota, seed, screen, device, …).
    pub fingerprint: Option<FingerprintConfig>,
    pub extensions: Option<Vec<ExtensionConfig>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct LaunchedProfile {
    pub id: ProfileId,
    pub cdp_endpoint: String,
    pub pid: u32,
    pub started_at: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct McpToolError {
    pub code: String,
    pub message: String,
    pub details: Option<serde_json::Value>,
}
