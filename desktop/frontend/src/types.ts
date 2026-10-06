/**
 * Compatible camelCase JSON types returned by the pure-Go Wails service.
 * Nullable and optional fields retain the persisted domain contract.
 * Sources: desktop/internal/store, desktop/internal/browser,
 * desktop/internal/mcp, desktop/app.go, and desktop/service.go.
 */

// ---------------------------------------------------------------------------
// Profile domain (desktop/internal/store)
// ---------------------------------------------------------------------------

export type ProfileId = string;

/** Proxy protocol ("http" | "socks5"). Kept as string for forward compat. */
export type ProxyType = string;

export interface ProxyConfig {
  /** Compatible JSON key for the proxy protocol. */
  type: ProxyType;
  host: string;
  port: number;
  username?: string | null;
  password?: string | null;
}

/** Compatible kebab-case device IDs; enumerate available values with fingerprint_devices. */
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
  chromixOptions?: Record<string, unknown>;
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
  /** Full UI fingerprint, or the legacy partial MCP-compatible patch. */
  fingerprint?: FingerprintConfig | PartialFingerprintInput;
  chromixOptions?: Record<string, unknown>;
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
  /** Whole-replace — the UI always holds a complete FingerprintConfig. */
  fingerprint?: FingerprintConfig;
  chromixOptions?: Record<string, unknown>;
  extensions?: ExtensionConfig[];
}

export interface LaunchedProfile {
  id: ProfileId;
  cdpEndpoint: string;
  pid: number;
  startedAt: string;
}

// ---------------------------------------------------------------------------
// Settings (desktop/internal/store)
// ---------------------------------------------------------------------------

export type BrowserEngine = "cft" | "cloakbrowser" | "chromix";

export interface ChromixSettings {
  nodePath: string;
  options: Record<string, unknown>;
  environment: Record<string, string>;
}

export interface AppSettings {
  theme: string;
  mcpHttpEnabled: boolean;
  mcpHttpPort: number;
  browserEngine: BrowserEngine;
  browserBinaryPath?: string | null;
  chromix: ChromixSettings;
  skipBrowserDownload: boolean;
  autoUpdate: boolean;
  usageReporting: boolean;
}

// ---------------------------------------------------------------------------
// Activity (desktop/internal/mcp)
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
// Push event payloads (desktop/internal/browser)
// Wails forwards the Go event payloads without an envelope.
// ---------------------------------------------------------------------------

export type RunningStateChange =
  | { kind: "launched"; profileId: ProfileId }
  | { kind: "closing"; profileId: ProfileId }
  | { kind: "closed"; profileId: ProfileId; reason: "user-close" | "external-exit" };

/** Legacy global bootstrap status, distinct from per-profile Go launch events. */
export type ChromiumStatus =
  | { kind: "ready" }
  | { kind: "dev-system" }
  | { kind: "missing" }
  | { kind: "fetching-manifest" }
  | { kind: "downloading"; version: string; bytesReceived: number; bytesTotal: number }
  | { kind: "verifying" }
  | { kind: "extracting"; version: string }
  | { kind: "error"; message: string };

/** Legacy alias kept for any code referencing `ChromiumStatusV1`. */
export type ChromiumStatusEvent = ChromiumStatus;

// ---------------------------------------------------------------------------
// System info (desktop/service.go)
// ---------------------------------------------------------------------------

export interface SystemInfo {
  mcpHttpUrl: string;
  mcpError?: string;
  mcpAuthToken?: string | null;
  appVersion: string;
  platform: string;
}

// ---------------------------------------------------------------------------
// Catalog and companion event types
// ---------------------------------------------------------------------------

/** `extensions:installed` push payload ("Add to Cloaksession" companion event). */
export type ExtensionInstalledEvent =
  | { ok: true; profileId: string; extension: ExtensionConfig }
  | { ok: false; profileId: string; error: string };

export interface DeviceCatalogEntry {
  family: DeviceFamily;
  label: string;
  screens: ReadonlyArray<{ width: number; height: number; label: string }>;
}

export interface LocaleCatalogEntry {
  id: string;
  label: string;
  locale: string;
  country: string;
  timezones: ReadonlyArray<string>;
}

export interface FingerprintReconcilePatch {
  device?: DeviceFamily;
  localeId?: string;
  screen?: { width: number; height: number };
  timezone?: string;
  hardwareConcurrency?: number;
  deviceMemory?: number;
  country?: string;
}

export interface ProxyGeoResult {
  country: string;
  countryName: string;
  timezone: string;
  city: string;
  ip: string;
}

/** Update-checker status emitted by the Go service. */
export type UpdateStatus =
  | { kind: "idle" }
  | { kind: "checking" }
  | { kind: "available"; version: string; releaseNotes?: string }
  | { kind: "downloading"; version: string; received: number; total: number; percent: number }
  | { kind: "ready"; version: string }
  | { kind: "no-update" }
  | { kind: "up-to-date" }
  | { kind: "error"; message: string };
