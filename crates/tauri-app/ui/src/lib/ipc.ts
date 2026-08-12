/**
 * Frontend IPC layer for MultiZen (P4.6).
 *
 * Wraps the Tauri 2.x `invoke` command channel and `listen` event channel.
 *
 * Naming conventions (verified against the Rust registration in P4.3):
 * - **Commands**: Tauri 2.x uses the snake_case Rust function name as the
 *   invoke channel. So the old Electron `profiles:list` becomes
 *   `profiles_list` here. Args are passed as a single camelCase-keyed
 *   object (Tauri 2.x converts snake_case Rust parameter names to
 *   camelCase on the JS side; all current command params are already
 *   single words, so camelCase == the Rust name).
 * - **Push events**: the Rust side emits literal strings with colons
 *   (`profiles:running-changed`, `chromium:status`, `activity:event`),
 *   so those event names stay colon-delimited.
 *
 * `listen` returns a `Promise<UnlistenFn>` in Tauri 2.x (async, because
 * it round-trips to the backend to register). The old Electron preload
 * returned a synchronous `() => void`; callers must now `await` the
 * registration before relying on the unlisten.
 */

import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

import type {
  ActivityEvent,
  AppSettings,
  ChromiumStatus,
  CreateProfileInput,
  FingerprintConfig,
  LaunchedProfile,
  Profile,
  ProfileId,
  ProfileSummary,
  ProxyConfig,
  RunningStateChange,
  SystemInfo,
  UpdateProfileInput,
} from "../types";

export * from "../types";

// ---------------------------------------------------------------------------
// Profiles
// ---------------------------------------------------------------------------

export const profiles = {
  /** `profiles_list` → `Vec<ProfileSummary>`. */
  list: (): Promise<ProfileSummary[]> => invoke<ProfileSummary[]>("profiles_list"),

  /** `profiles_get` → `Option<Profile>`. */
  get: (id: ProfileId): Promise<Profile | null> =>
    invoke<Profile | null>("profiles_get", { id }),

  /** `profiles_create` → `Profile`. */
  create: (input: CreateProfileInput): Promise<Profile> =>
    invoke<Profile>("profiles_create", { input }),

  /** `profiles_update` → `Profile`. */
  update: (id: ProfileId, patch: UpdateProfileInput): Promise<Profile> =>
    invoke<Profile>("profiles_update", { id, patch }),

  /** `profiles_delete` → `()`. */
  delete: (id: ProfileId): Promise<void> => invoke<void>("profiles_delete", { id }),

  /** `profiles_launch` → `LaunchedProfile`. */
  launch: (id: ProfileId): Promise<LaunchedProfile> =>
    invoke<LaunchedProfile>("profiles_launch", { id }),

  /** `profiles_close` → `()`. */
  close: (id: ProfileId): Promise<void> => invoke<void>("profiles_close", { id }),
};

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

export const settings = {
  /** `settings_get` → `AppSettings`. */
  get: (): Promise<AppSettings> => invoke<AppSettings>("settings_get"),

  /** `settings_update` → `AppSettings` (full settings returned). */
  update: (patch: AppSettings): Promise<AppSettings> =>
    invoke<AppSettings>("settings_update", { patch }),
};

// ---------------------------------------------------------------------------
// Dialog (tauri-plugin-dialog)
// ---------------------------------------------------------------------------

export const dialog = {
  /** `dialog_pick_browser_binary` → `Option<PathBuf>` (string | null). */
  pickBrowserBinary: (): Promise<string | null> =>
    invoke<string | null>("dialog_pick_browser_binary"),

  /** `dialog_pick_directory` → `Option<PathBuf>` (string | null). */
  pickDirectory: (): Promise<string | null> =>
    invoke<string | null>("dialog_pick_directory"),
};

// ---------------------------------------------------------------------------
// Activity
// ---------------------------------------------------------------------------

export const activity = {
  /** `activity_recent` → `Vec<ActivityEvent>`. `limit` defaults to 100 (capped 500). */
  recent: (limit?: number): Promise<ActivityEvent[]> =>
    invoke<ActivityEvent[]>("activity_recent", { limit: limit ?? null }),
};

// ---------------------------------------------------------------------------
// System
// ---------------------------------------------------------------------------

export const system = {
  /** `system_info` → `SystemInfo`. */
  info: (): Promise<SystemInfo> => invoke<SystemInfo>("system_info"),
};

// ---------------------------------------------------------------------------
// Fingerprint
// ---------------------------------------------------------------------------

export const fingerprint = {
  /** `fingerprint_generate` → `FingerprintConfig`. */
  generate: (seed: string): Promise<FingerprintConfig> =>
    invoke<FingerprintConfig>("fingerprint_generate", { seed }),

  /** `fingerprint_devices` → `Vec<&str>` (kebab-case family ids). */
  devices: (): Promise<string[]> => invoke<string[]>("fingerprint_devices"),

  /** `fingerprint_locales` → `Vec<&str>` (BCP-47 locale ids). */
  locales: (): Promise<string[]> => invoke<string[]>("fingerprint_locales"),

  /**
   * `fingerprint_reconcile` → not yet implemented in Rust (deferred to P5).
   * Returns an error string; typed as `unknown` so the UI can detect the gap.
   */
  reconcile: (fingerprint: unknown): Promise<unknown> =>
    invoke<unknown>("fingerprint_reconcile", { fingerprint }),

  /**
   * `fingerprint_locale_for_country` → not yet implemented (P5).
   * Returns an error string.
   */
  localeForCountry: (country: string): Promise<string> =>
    invoke<string>("fingerprint_locale_for_country", { country }),
};

// ---------------------------------------------------------------------------
// Proxy
// ---------------------------------------------------------------------------

export const proxy = {
  /**
   * `proxy_detect_geo` → not yet wired to the launcher-thread geo probe
   * (deferred to P4.8). Returns an error string. Typed as `unknown` so
   * the UI can surface "not yet wired".
   */
  detectGeo: (proxy: ProxyConfig): Promise<unknown> =>
    invoke<unknown>("proxy_detect_geo", { proxy }),
};

// ---------------------------------------------------------------------------
// Push event listeners (colon-delimited event names from Rust `emit`)
// ---------------------------------------------------------------------------

/**
 * `profiles:running-changed` push event.
 * Resolves to an `UnlistenFn` (await registration before relying on it).
 */
export function onRunningChanged(
  cb: (change: RunningStateChange) => void,
): Promise<UnlistenFn> {
  return listen<RunningStateChange>("profiles:running-changed", (event) => {
    cb(event.payload);
  });
}

/**
 * `chromium:status` push event.
 * Resolves to an `UnlistenFn`.
 */
export function onChromiumStatus(
  cb: (status: ChromiumStatus) => void,
): Promise<UnlistenFn> {
  return listen<ChromiumStatus>("chromium:status", (event) => {
    cb(event.payload);
  });
}

/**
 * `activity:event` push event.
 * Resolves to an `UnlistenFn`.
 */
export function onActivityEvent(
  cb: (event: ActivityEvent) => void,
): Promise<UnlistenFn> {
  return listen<ActivityEvent>("activity:event", (event) => {
    cb(event.payload);
  });
}
