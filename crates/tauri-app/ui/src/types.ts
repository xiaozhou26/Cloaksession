/**
 * TypeScript mirrors of the Rust serde types consumed by the Tauri
 * commands registered in P4.3. All Rust structs use
 * `#[serde(rename_all = "camelCase")]`, so the TS field names are the
 * camelCase versions of the Rust field names.
 *
 * These types are intentionally conservative: every field that the
 * command signatures return is modeled. Fields that are not yet needed
 * by the UI are still included when they are part of the Rust struct,
 * to keep the mirrors faithful. Where a Rust type leaves a field
 * optional (`Option<T>`), the TS field is `T | null` (serde serializes
 * `None` as `null`).
 *
 * Source of truth:
 *   crates/multizen-core/src/profile.rs
 *   crates/multizen-core/src/settings.rs
 *   crates/mcp-server/src/activity.rs
 *   crates/tauri-app/src/driver.rs
 *   crates/tauri-app/src/commands/system.rs
 */

// ---------------------------------------------------------------------------
// Profile domain (crates/multizen-core/src/profile.rs)
// ---------------------------------------------------------------------------

export type ProfileId = string;

/** `ProxyConfig.proxy_type` ("http" | "socks5"). Kept as string for forward compat. */
export type ProxyType = string;

export interface ProxyConfig {
  /** Rust field `proxy_type` renamed via `#[serde(rename = "type")]`. */
  type: ProxyType;
  host: string;
  port: number;
  username?: string | null;
  password?: string | null;
}

/**
 * `DeviceFamily` enum uses `#[serde(rename_all = "kebab-case")]` plus
 * per-variant `#[serde(rename = "...")]`, so the serialized form is the
 * kebab-case string (e.g. "macbook-pro-14-m3"). We model it as a string
 * union for type-safety; the UI can use the `fingerprint_devices`
 * command to enumerate valid values at runtime.
 */
export type DeviceFamily =
  | "macbook-pro-14-m3"
  | "macbook-pro-14-m3-pro"
  | "macbook-pro-16-m3-pro"
  | "macbook-air-13-m3"
  | "macbook-air-15-m3"
  | "imac-24-m3"
  | "mac-mini-m2"
  | "windows-laptop-intel"
  | "windows-laptop-intel-uhd"
  | "windows-laptop-amd"
  | "windows-laptop-nvidia"
  | "windows-laptop-nvidia-4050"
  | "windows-desktop-nvidia"
  | "windows-desktop-nvidia-4080"
  | "windows-desktop-amd"
  | "windows-desktop-intel"
  | "linux-desktop-intel"
  | "linux-desktop-amd"
  | "linux-desktop-nvidia"
  | (string & {}); // allow unknown families without breaking narrowing

export interface ClientHints {
  secChUa: string;
  secChUaPlatform: string;
  secChUaPlatformVersion: string;
  secChUaArch: string;
  secChUaBitness: string;
  secChUaMobile: string;
  secChUaModel: string;
  secChUaFullVersionList: string;
}

export interface ScreenSize {
  width: number;
  height: number;
}

export interface WebGLConfig {
  vendor: string;
  renderer: string;
}

export interface FingerprintConfig {
  device: DeviceFamily;
  userAgent: string;
  platform: string;
  clientHints: ClientHints;
  locale: string;
  languages: string[];
  acceptLanguage: string;
  timezone: string;
  country: string;
  screen: ScreenSize;
  availScreen?: ScreenSize | null;
  dpr: number;
  webgl: WebGLConfig;
  hardwareConcurrency: number;
  deviceMemory: number;
  fontsDir?: string | null;
  storageQuota?: number | null;
  seed?: string | null;
}

export interface ExtensionConfig {
  id: string;
  name: string;
  version: string;
  enabled: boolean;
  scope: string;
  dir: string;
  source: string;
}

export interface Profile {
  id: ProfileId;
  name: string;
  notes?: string | null;
  tags: string[];
  proxy?: ProxyConfig | null;
  fingerprint: FingerprintConfig;
  extensions?: ExtensionConfig[] | null;
  icon?: string | null;
  startUrl?: string | null;
  searchProvider?: string | null;
  dataDir: string;
  createdAt: string;
  updatedAt: string;
  lastOpenedAt?: string | null;
  proxyCountry?: string | null;
}

export interface ProfileSummary {
  id: ProfileId;
  name: string;
  tags: string[];
  lastOpenedAt?: string | null;
  isRunning: boolean;
  icon?: string | null;
  proxy?: ProxyConfig | null;
  timezone?: string | null;
  proxyCountry?: string | null;
  device?: DeviceFamily | null;
}

export interface PartialFingerprintInput {
  userAgent?: string;
  locale?: string;
  timezone?: string;
  country?: string;
}

export interface CreateProfileInput {
  name: string;
  notes?: string;
  tags?: string[];
  icon?: string;
  startUrl?: string;
  searchProvider?: string;
  proxy?: ProxyConfig;
  fingerprint?: PartialFingerprintInput;
  extensions?: ExtensionConfig[];
}

export interface UpdateProfileInput {
  name?: string;
  notes?: string;
  tags?: string[];
  icon?: string | null;
  startUrl?: string | null;
  searchProvider?: string | null;
  proxy?: ProxyConfig | null;
  fingerprint?: PartialFingerprintInput;
  extensions?: ExtensionConfig[];
}

export interface LaunchedProfile {
  id: ProfileId;
  cdpEndpoint: string;
  pid: number;
  startedAt: string;
}

// ---------------------------------------------------------------------------
// Settings (crates/multizen-core/src/settings.rs)
// ---------------------------------------------------------------------------

export type BrowserEngine = "cft" | "cloakbrowser";

export interface AppSettings {
  theme: string;
  mcpHttpEnabled: boolean;
  mcpHttpPort: number;
  browserEngine: BrowserEngine;
  browserBinaryPath?: string | null;
  skipBrowserDownload: boolean;
  autoUpdate: boolean;
  usageReporting: boolean;
}

// ---------------------------------------------------------------------------
// Activity (crates/mcp-server/src/activity.rs)
// ---------------------------------------------------------------------------

export interface ActivityEvent {
  id: string;
  timestamp: string;
  tool: string;
  profileId?: string | null;
  args: unknown;
  status: string;
  summary?: string | null;
  durationMs?: number | null;
}

// ---------------------------------------------------------------------------
// Push event payloads (crates/tauri-app/src/driver.rs)
// ---------------------------------------------------------------------------

/** `profiles:running-changed` payload. */
export interface RunningStateChange {
  profileId: string;
  running: boolean;
}

/** `chromium:status` payload. `status` is "started" | "stopped" | "failed". */
export interface ChromiumStatus {
  profileId: string;
  status: string;
  error?: string;
}

// ---------------------------------------------------------------------------
// System info (crates/tauri-app/src/commands/system.rs)
// ---------------------------------------------------------------------------

export interface SystemInfo {
  mcpHttpUrl: string;
  mcpAuthToken?: string | null;
  appVersion: string;
  platform: string;
}
