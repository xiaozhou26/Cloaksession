import type { Page } from "@playwright/test";
import type { AppSettings, Profile } from "../src/types";

export const defaultSettings: AppSettings = {
  theme: "dark",
  mcpHttpEnabled: true,
  mcpHttpPort: 7777,
  browserEngine: "cloakbrowser",
  browserBinaryPath: null,
  chromix: { nodePath: "node", options: {}, environment: {} },
  skipBrowserDownload: false,
  autoUpdate: false,
  usageReporting: false,
};

const fixtureProfile: Profile = {
  id: "fixture-profile",
  name: "Regression profile",
  tags: ["fixture"],
  notes: "Local UI fixture only",
  startUrl: "https://example.com/",
  dataDir: "/fixture/profile",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  fingerprint: {
    device: "macbook-pro-14-m3",
    userAgent: "Fixture Chrome",
    platform: "MacIntel",
    clientHints: {
      secChUa: "Chromium", secChUaPlatform: "macOS", secChUaPlatformVersion: "14",
      secChUaArch: "arm", secChUaBitness: "64", secChUaMobile: "?0",
      secChUaModel: "", secChUaFullVersionList: "Chrome/140",
    },
    locale: "en-US", languages: ["en-US"], acceptLanguage: "en-US", timezone: "America/New_York",
    country: "us", screen: { width: 1440, height: 900 }, dpr: 2,
    webgl: { vendor: "Apple", renderer: "Apple M3" }, hardwareConcurrency: 8, deviceMemory: 8,
  },
};

export async function installTauriMock(page: Page, initial: AppSettings = defaultSettings): Promise<void> {
  await page.addInitScript(({ initial, profile }) => {
    const key = "cloaksession.test.settings";
    const profileKey = "cloaksession.test.profile";
    if (!localStorage.getItem(key)) localStorage.setItem(key, JSON.stringify(initial));
    if (!localStorage.getItem(profileKey)) localStorage.setItem(profileKey, JSON.stringify(profile));
    if (!localStorage.getItem("multizen.ui.section")) localStorage.setItem("multizen.ui.section", JSON.stringify("settings"));
    localStorage.setItem("multizen.ui.onboarded", "true");
    let callbackId = 0;
    const callbacks = new Map<number, (...args: unknown[]) => void>();
    const mock = {
      updates: [] as Record<string, unknown>[],
      profileUpdates: [] as Record<string, unknown>[],
      calls: [] as string[],
      failNextSave: false,
      settings: () => JSON.parse(localStorage.getItem(key)!),
      profile: () => JSON.parse(localStorage.getItem(profileKey)!),
    };
    Object.assign(window, {
      __TEST_IPC__: mock,
      __TAURI_EVENT_PLUGIN_INTERNALS__: { unregisterListener: () => {} },
      __TAURI_INTERNALS__: {
        transformCallback: (callback: (...args: unknown[]) => void) => {
          callbacks.set(++callbackId, callback);
          return callbackId;
        },
        unregisterCallback: (id: number) => callbacks.delete(id),
        invoke: async (command: string, args: Record<string, any> = {}) => {
          mock.calls.push(command);
          switch (command) {
            case "settings_get": return mock.settings();
            case "settings_update": {
              if (mock.failNextSave) {
                mock.failNextSave = false;
                throw new Error("Fixture storage unavailable");
              }
              const patch = JSON.parse(JSON.stringify(args.patch));
              mock.updates.push(patch);
              const next = { ...mock.settings(), ...patch };
              // Empty string explicitly clears the Rust Option<String> field.
              if (patch.browserBinaryPath === "") next.browserBinaryPath = null;
              localStorage.setItem(key, JSON.stringify(next));
              return next;
            }
            case "system_info": return { mcpHttpUrl: "http://127.0.0.1:7777", mcpAuthToken: "fixture-token-not-secret", appVersion: "1.1.1", platform: "darwin" };
            case "profiles_list": {
              const current = mock.profile();
              return [{ id: current.id, name: current.name, tags: current.tags, isRunning: false, timezone: current.fingerprint.timezone }];
            }
            case "profiles_get": return mock.profile();
            case "profiles_update": {
              const patch = JSON.parse(JSON.stringify(args.patch));
              mock.profileUpdates.push(patch);
              const next = { ...mock.profile(), ...patch };
              localStorage.setItem(profileKey, JSON.stringify(next));
              return next;
            }
            case "activity_recent": return [];
            case "update_status":
            case "update_check": return { kind: "idle" };
            case "update_last_checked": return 0;
            case "dialog_pick_browser_binary": return "/fixture/custom-chromix";
            case "plugin:event|listen": return ++callbackId;
            case "plugin:event|unlisten": return;
            case "extensions_list":
            case "extensions_store_entries": return [];
            case "fingerprint_devices": return [{ family: "macbook-pro-14-m3", label: "MacBook Pro 14", screens: [{ width: 1440, height: 900, label: "1440 × 900" }] }];
            case "fingerprint_locales": return [{ id: "en-US", label: "English (US)", locale: "en-US", country: "us", timezones: ["America/New_York"] }];
            case "fingerprint_generate": return profile.fingerprint;
            default: throw new Error(`Unhandled fixture IPC command: ${command}`);
          }
        },
      },
    });
  }, { initial, profile: fixtureProfile });
}

export async function storedSettings(page: Page): Promise<AppSettings> {
  return page.evaluate(() => JSON.parse(localStorage.getItem("cloaksession.test.settings")!));
}

export async function settingsPatches(page: Page): Promise<Partial<AppSettings>[]> {
  return page.evaluate(() => (window as any).__TEST_IPC__.updates);
}

export async function profilePatches(page: Page): Promise<Record<string, unknown>[]> {
  return page.evaluate(() => (window as any).__TEST_IPC__.profileUpdates);
}
