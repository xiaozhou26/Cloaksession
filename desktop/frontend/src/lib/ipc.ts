/** Public IPC API backed by Wails v2; command names and compatible JSON payloads stay unchanged. */
import { invoke, listen, type UnlistenFn } from "./wails";

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
// Unsupported global Chromium bootstrap operations reject explicitly.
// ---------------------------------------------------------------------------

function notImplemented(name: string): Promise<never> {
  return Promise.reject(
    new Error(`Cloaksession: '${name}' is not implemented in the desktop build (scope-excluded)`),
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
  /** `profiles_list` → `ProfileSummary[]`. */
  list: (): Promise<ProfileSummary[]> => invoke<ProfileSummary[]>("profiles_list"),

  /** `profiles_get` → `Profile | null`. */
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
   * the Go service merges with existing settings.
   */
  update: (patch: Partial<AppSettings>): Promise<AppSettings> =>
    invoke<AppSettings>("settings_update", { patch }),
};

// ---------------------------------------------------------------------------
// Native dialogs (Wails)
// ---------------------------------------------------------------------------

export const dialog = {
  /** `dialog_pick_browser_binary` → `string | null`. */
  pickBrowserBinary: (): Promise<string | null> =>
    invoke<string | null>("dialog_pick_browser_binary"),

  /** `dialog_pick_directory` → `string | null`. */
  pickDirectory: (): Promise<string | null> =>
    invoke<string | null>("dialog_pick_directory"),
};

// ---------------------------------------------------------------------------
// Activity
// ---------------------------------------------------------------------------

export const activity = {
  /** `activity_recent` → `ActivityEvent[]`. `limit` defaults to 100 (capped 500). */
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
// An empty seed requests a random fingerprint from the Go service.

export const fingerprint = {
  /**
   * `fingerprint_generate` → `FingerprintConfig`.
   * Renderer calls with no args; we pass an empty seed (the backend generates
   * random fingerprint for empty seed).
   */
  generate: (): Promise<FingerprintConfig> =>
    invoke<FingerprintConfig>("fingerprint_generate", { seed: "" }),

  /**
   * `fingerprint_devices` → `DeviceCatalogEntry[]`.
   */
  devices: (): Promise<DeviceCatalogEntry[]> =>
    invoke<DeviceCatalogEntry[]>("fingerprint_devices"),

  /**
   * `fingerprint_locales` → `LocaleCatalogEntry[]`.
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
   * `proxy_detect_geo` → `ProxyGeoResult`. Probes the proxy exit IP and region.
   * The optional profile ID remains reserved for compatibility.
   */
  detectGeo: (
    proxy: ProxyConfig,
    _profileId?: string,
  ): Promise<ProxyGeoResult> =>
    invoke<ProxyGeoResult>("proxy_detect_geo", { proxy }),
};

// ---------------------------------------------------------------------------
// Extensions
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
// Legacy global Chromium bootstrap (no matching backend commands).
// Stubbed; `status()` returns a "ready"-shaped object so the bootstrap modal
// stays hidden, and `retry()` rejects. `onChromiumStatus` returns a no-op unlisten.
// ---------------------------------------------------------------------------

export const chromium = {
  status: (): Promise<ChromiumStatus> =>
    // Return a ready-shaped status so the modal stays hidden in the desktop build.
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
// Push events retain the Go payload; Wails passes it directly to callbacks.
// ---------------------------------------------------------------------------

/**
 * `profiles:running-changed` push event.
 * Resolves to an `UnlistenFn` (await registration before relying on it).
 */
export function onRunningChanged(
  cb: (change: RunningStateChange) => void,
): Promise<UnlistenFn> {
  return listen<RunningStateChange>("profiles:running-changed", cb);
}

/**
 * `chromium:status` push event.
 *
 * The Go service emits a flat `{ profileId, status, error }` payload,
 * but the renderer expects the legacy discriminated-union `ChromiumStatus`.
 * Keep this listener a no-op so the
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
  return listen<ActivityEvent>("activity:event", cb);
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
    cb,
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
  return listen<ExtensionInstalledEvent>("extensions:installed", cb);
}

/**
 * `update:status` push event. Emitted by the backend on every status
 * transition during a check or download. Resolves to an `UnlistenFn`.
 */
export function onUpdateStatus(
  cb: (s: UpdateStatus) => void,
): Promise<UnlistenFn> {
  return listen<{ status: UpdateStatus } | UpdateStatus>("update:status", (payload) => {
    // Backend emits `{ status: UpdateStatus }` — unwrap.
    if (payload && typeof payload === "object" && "status" in payload) {
      cb((payload as { status: UpdateStatus }).status);
    } else {
      cb(payload as UpdateStatus);
    }
  });
}
