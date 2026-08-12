/**
 * Frontend IPC layer for Cloaksession (P4.6).
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
  CreateProfileInput,
  ExtensionConfig,
  ExtensionInstalledEvent,
  FingerprintConfig,
  LaunchedProfile,
  Profile,
  ProfileId,
  ProfileSummary,
  ProxyConfig,
  ProxyGeoResult,
  RunningStateChange,
  SystemInfo,
  UpdateProfileInput,
  UpdateStatus,
  ChromiumStatus,
  DeviceCatalogEntry,
  LocaleCatalogEntry,
  FingerprintReconcilePatch,
} from "../types";

export * from "../types";

// ---------------------------------------------------------------------------
// Stub error helper — scope-excluded namespaces (extensions / chromium /
// update / archive import-export) reject at runtime so the UI degrades
// gracefully. Components are expected to try/catch.
// ---------------------------------------------------------------------------

function notImplemented(name: string): Promise<never> {
  return Promise.reject(
    new Error(`Cloaksession: '${name}' is not implemented in the Tauri build (scope-excluded)`),
  );
}

/** No-op unlisten — returns a Promise resolving to a no-op so `await` callers work. */
function noopUnlisten(): Promise<() => void> {
  return Promise.resolve(() => {});
}

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

  /**
   * `profiles_export_archive` → `{ ok: true, path } | { ok: false, reason }`.
   * Serializes the profile (JSON + data-dir files + shared extensions) into
   * an AES-256-GCM-encrypted `.mzar` archive. The backend shows a native
   * save dialog for the output path.
   */
  exportArchive: (
    id: ProfileId,
    passphrase: string,
  ): Promise<{ ok: true; path: string } | { ok: false; reason: string }> =>
    invoke<{ ok: true; path: string } | { ok: false; reason: string }>(
      "profiles_export_archive",
      { id, passphrase },
    ),

  /**
   * `profiles_import_archive` → `{ ok: true, id } | { ok: false, reason }`.
   * The backend shows a native open dialog for the `.mzar` file, decrypts
   * with the passphrase, restores the profile into a new data dir, and
   * returns the new profile id.
   */
  importArchive: (
    passphrase: string,
  ): Promise<{ ok: true; id: ProfileId } | { ok: false; reason: string }> =>
    invoke<{ ok: true; id: ProfileId } | { ok: false; reason: string }>(
      "profiles_import_archive",
      { passphrase },
    ),
};

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

export const settings = {
  /** `settings_get` → `AppSettings`. */
  get: (): Promise<AppSettings> => invoke<AppSettings>("settings_get"),

  /**
   * `settings_update` → `AppSettings` (full settings returned).
   * Accepts a partial patch (renderer passes `Partial<AppSettings>`);
   * the Rust side merges with existing settings.
   */
  update: (patch: Partial<AppSettings>): Promise<AppSettings> =>
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
//
// NOTE: the Rust `fingerprint_generate` command takes a `seed` parameter, but
// the legacy renderer calls `fingerprint.generate()` with no argument. We
// pass an empty-string seed; the Rust side generates a random fingerprint
// when seed is empty (P4.3 behavior). The catalog commands (`devices`,
// `locales`) return `Vec<&str>` from Rust, but the renderer expects the
// richer `DeviceCatalogEntry` / `LocaleCatalogEntry` shapes. We type the
// return as the catalog shapes (the renderer consumes them), but at
// runtime the Tauri command returns plain strings — P4.8 reconciles.
// `reconcile` is still a stub in Rust (returns Err; P5); `localeForCountry`
// is wired — returns a matching locale id or null (no preset / no fallback).

export const fingerprint = {
  /**
   * `fingerprint_generate` → `FingerprintConfig`.
   * Renderer calls with no args; we pass an empty seed (Rust generates
   * random fingerprint for empty seed).
   */
  generate: (): Promise<FingerprintConfig> =>
    invoke<FingerprintConfig>("fingerprint_generate", { seed: "" }),

  /**
   * `fingerprint_devices` → `Vec<DeviceCatalogEntry>`.
   */
  devices: (): Promise<DeviceCatalogEntry[]> =>
    invoke<DeviceCatalogEntry[]>("fingerprint_devices"),

  /**
   * `fingerprint_locales` → `Vec<LocaleCatalogEntry>`.
   */
  locales: (): Promise<LocaleCatalogEntry[]> =>
    invoke<LocaleCatalogEntry[]>("fingerprint_locales"),

  /**
   * `fingerprint_reconcile` → `FingerprintConfig`. Applies a partial patch
   * (locale/timezone/device/screen/hardware/memory/country) to the given
   * fingerprint and returns the updated config. Locale changes re-derive
   * `languages`, `accept_language`, and `country`; the `country` override
   * (from the proxy geo probe) takes precedence over the locale's region.
   */
  reconcile: (
    current: FingerprintConfig,
    patch: FingerprintReconcilePatch,
  ): Promise<FingerprintConfig> =>
    invoke<FingerprintConfig>("fingerprint_reconcile", {
      fingerprint: current,
      patch,
    }),

  /**
  /**
   * `fingerprint_locale_for_country` → `string | null`. Given a 2-letter
   * country code (from the proxy geo probe, lowercase or uppercase),
   * returns the best-matching locale id from the catalog, or `null` when
   * no preset matches and no culturally adjacent fallback exists (the
   * frontend then asks the user to pick manually).
   */
  localeForCountry: (country: string): Promise<string | null> =>
    invoke<string | null>("fingerprint_locale_for_country", { country }),
};

// ---------------------------------------------------------------------------
// Proxy
// ---------------------------------------------------------------------------

export const proxy = {
  /**
   /**
   * `proxy_detect_geo` → `ProxyGeoResult`. Probes the exit IP / geo of the
   * proxy via ipapi.co (12s timeout). Runs on the Tauri async runtime using
   * the P2.7 `browser-launcher::proxy_geo::probe_proxy_geo` helper — no
   * launcher-thread routing needed. `profileId` is accepted for future
   * "persist resolved country onto the profile" wiring but not yet used
   * by the backend.
   */
  detectGeo: (
    proxy: ProxyConfig,
    _profileId?: string,
  ): Promise<ProxyGeoResult> =>
    invoke<ProxyGeoResult>("proxy_detect_geo", { proxy }),
};

// ---------------------------------------------------------------------------
// Extensions (scope-excluded in P4 — backend not registered).
// Stubbed so migrated components compile; runtime calls reject gracefully.
// ---------------------------------------------------------------------------

export const extensions = {
  list: (profileId: string): Promise<ExtensionConfig[]> =>
    invoke<ExtensionConfig[]>("extensions_list", { profileId }),
  addFromWebStore: (profileId: string, urlOrId: string): Promise<ExtensionConfig[]> =>
    invoke<ExtensionConfig[]>("extensions_add_from_web_store", { profileId, urlOrId }),
  addFromFile: (profileId: string): Promise<ExtensionConfig[]> =>
    invoke<ExtensionConfig[]>("extensions_add_from_file", { profileId }),
  addFromFolder: (profileId: string): Promise<ExtensionConfig[]> =>
    invoke<ExtensionConfig[]>("extensions_add_from_folder", { profileId }),
  remove: (profileId: string, extId: string): Promise<ExtensionConfig[]> =>
    invoke<ExtensionConfig[]>("extensions_remove", { profileId, extId }),
  toggle: (profileId: string, extId: string, enabled: boolean): Promise<ExtensionConfig[]> =>
    invoke<ExtensionConfig[]>("extensions_toggle", { profileId, extId, enabled }),
  storeEntries: (): Promise<ExtensionConfig[]> =>
    invoke<ExtensionConfig[]>("extensions_store_entries"),
  prepareFromWebStore: (urlOrId: string): Promise<ExtensionConfig> =>
    invoke<ExtensionConfig>("extensions_prepare_from_web_store", { urlOrId }),
  prepareFromFile: (): Promise<ExtensionConfig | null> =>
    invoke<ExtensionConfig | null>("extensions_prepare_from_file"),
  prepareFromFolder: (): Promise<ExtensionConfig | null> =>
    invoke<ExtensionConfig | null>("extensions_prepare_from_folder"),
  icon: (ext: ExtensionConfig, profileId: string | null): Promise<string | null> =>
    invoke<string | null>("extensions_icon", { ext, profileId }),
};

// ---------------------------------------------------------------------------
// Chromium runtime (scope-excluded in P4 — backend not registered).
// Stubbed; `status()` returns a "ready"-shaped object so the bootstrap modal
// stays hidden, and `retry()` rejects. `onStatus` returns a no-op unlisten.
// ---------------------------------------------------------------------------

export const chromium = {
  status: (): Promise<ChromiumStatus> =>
    // Return a ready-shaped status so the modal stays hidden in the Tauri build.
    Promise.resolve({ kind: "ready" } as ChromiumStatus),
  retry: (): Promise<ChromiumStatus> => notImplemented("chromium.retry"),
};

// ---------------------------------------------------------------------------
// Update checker — GitHub Releases based.
// `status()` returns the current cached status; `check()` probes GitHub and
// emits `update:status` events during the check. On Windows, a newer
// release triggers an auto-download of the NSIS installer (progress via
// `downloading` status), after which `ready` is emitted and `install()`
// launches the installer. On macOS, `available` is terminal and `download()`
// opens the release page in the browser.
// ---------------------------------------------------------------------------

export const update = {
  /** `update_status` → `UpdateStatus`. */
  status: (): Promise<UpdateStatus> =>
    invoke<UpdateStatus>("update_status"),

  /** `update_last_checked` → epoch ms (0 = never). */
  lastChecked: (): Promise<number> =>
    invoke<number>("update_last_checked"),

  /** `update_check` → `UpdateStatus`. Probes GitHub Releases. */
  check: (): Promise<UpdateStatus> =>
    invoke<UpdateStatus>("update_check"),

  /** `update_install` → launches the downloaded NSIS installer (Windows). */
  install: (): Promise<void> =>
    invoke<void>("update_install"),

  /** `update_download` → opens the release page in browser (macOS fallback). */
  download: (version: string): Promise<void> =>
    invoke<void>("update_download", { version }),
};

// ---------------------------------------------------------------------------
// Push event listeners (colon-delimited event names from Rust `emit`)
//
// `onRunningChanged`, `onChromiumStatus`, `onActivityEvent` are wired to real
// Tauri events. The renderer expects sync `() => void` unlistens but Tauri
// `listen` is async; callers `await` registration (documented in P4.6).
//
// `onProxyCountryUpdated` and `onExtensionInstalled` are now wired to real
// backend emitters (`profiles:proxy-country-updated` and
// `extensions:installed` respectively).
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
 *
 * The Tauri backend emits a flat `{ profileId, status, error }` payload,
 * but the renderer expects the legacy discriminated-union `ChromiumStatus`.
 * Until P4.8 reconciles the shape, we stub this listener to a no-op so the
 * renderer's `status.kind` accesses don't crash; the initial
 * `chromium.status()` poll (stubbed to `{ kind: "ready" }`) sets the
 * ready state, and subsequent runtime status changes are not delivered.
 */
export function onChromiumStatus(
  _cb: (status: ChromiumStatus) => void,
): Promise<() => void> {
  return noopUnlisten();
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

/**
 * `profiles:proxy-country-updated` push event. Emitted by the startup
 * backfill task after each successful proxy geo probe. Resolves to an
 * `UnlistenFn` (await registration before relying on it).
 */
export function onProxyCountryUpdated(
  cb: (update: { id: string; country: string }) => void,
): Promise<UnlistenFn> {
  return listen<{ id: string; country: string }>(
    "profiles:proxy-country-updated",
    (event) => {
      cb(event.payload);
    },
  );
}

/**
 * `extensions:installed` push event — emitted by the companion poller after
 * an "Add to Cloaksession" button click on a Chrome Web Store page. Resolves to
 * an `UnlistenFn` (await registration before relying on it).
 */
export function onExtensionInstalled(
  cb: (e: ExtensionInstalledEvent) => void,
): Promise<UnlistenFn> {
  return listen<ExtensionInstalledEvent>("extensions:installed", (event) => {
    cb(event.payload);
  });
}

/**
 * `update:status` push event. Emitted by the backend on every status
 * transition during a check or download. Resolves to an `UnlistenFn`.
 */
export function onUpdateStatus(
  cb: (s: UpdateStatus) => void,
): Promise<UnlistenFn> {
  return listen<UpdateStatus>("update:status", (event) => {
    // Backend emits `{ status: UpdateStatus }` — unwrap.
    const payload = event.payload as unknown as { status?: UpdateStatus } | UpdateStatus;
    if (payload && typeof payload === "object" && "status" in payload) {
      cb((payload as { status: UpdateStatus }).status);
    } else {
      cb(payload as UpdateStatus);
    }
  });
}
