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

export interface MockOptions {
  section?: "profiles" | "settings" | "mcp";
  onboarded?: boolean;
  empty?: boolean;
  startupFailures?: Record<string, string>;
  mcpError?: string;
}

export async function installWailsMock(
  page: Page,
  initial: AppSettings = defaultSettings,
  options: MockOptions = {},
): Promise<void> {
  await page.addInitScript(({ initial, profile, options }) => {
    const key = "cloaksession.test.settings";
    const profileKey = "cloaksession.test.profiles";
    if (!localStorage.getItem(key)) localStorage.setItem(key, JSON.stringify(initial));
    if (!localStorage.getItem(profileKey)) localStorage.setItem(profileKey, JSON.stringify(options.empty ? [] : [profile]));
    if (!localStorage.getItem("multizen.ui.section")) localStorage.setItem("multizen.ui.section", JSON.stringify(options.section ?? "settings"));
    if (options.onboarded !== false) localStorage.setItem("multizen.ui.onboarded", "true");
    const listeners = new Map<string, Set<(payload: any) => void>>();
    const running = new Set<string>();
    const saveProfiles = (profiles: Profile[]) => localStorage.setItem(profileKey, JSON.stringify(profiles));
    const mock = {
      updates: [] as Record<string, unknown>[],
      profileUpdates: [] as Record<string, unknown>[],
      calls: [] as string[],
      invocations: [] as { command: string; args: Record<string, any> }[],
      failures: {} as Record<string, string>,
      startupFailures: { ...options.startupFailures },
      failNextSave: false,
      unsubscriptions: 0,
      settings: () => JSON.parse(localStorage.getItem(key)!),
      profiles: (): Profile[] => JSON.parse(localStorage.getItem(profileKey)!),
      emit: (name: string, payload: unknown) => {
        for (const callback of [...(listeners.get(name) ?? [])]) callback(payload);
      },
      listenerCount: (name: string) => listeners.get(name)?.size ?? 0,
    };
    const runtimeSettings = mock.settings();
    Object.assign(window, {
      __WAILS_MOCK__: mock,
      runtime: {
        EventsOn: (name: string, callback: (payload: any) => void) => {
          const callbacks = listeners.get(name) ?? new Set();
          listeners.set(name, callbacks);
          callbacks.add(callback);
          return () => {
            mock.unsubscriptions++;
            callbacks.delete(callback);
          };
        },
      },
      go: { main: { App: {
        Invoke: async (command: string, args: Record<string, any>) => {
          mock.calls.push(command);
          mock.invocations.push({ command, args: JSON.parse(JSON.stringify(args)) });
          if (mock.startupFailures[command]) throw mock.startupFailures[command];
          if (mock.failures[command]) {
            const message = mock.failures[command];
            delete mock.failures[command];
            throw message;
          }
          switch (command) {
            case "settings_get": return mock.settings();
            case "settings_update": {
              if (mock.failNextSave) {
                mock.failNextSave = false;
                throw "Fixture storage unavailable";
              }
              const patch = JSON.parse(JSON.stringify(args.patch));
              mock.updates.push(patch);
              const next = { ...mock.settings(), ...patch };
              if (patch.browserBinaryPath === "") next.browserBinaryPath = null;
              localStorage.setItem(key, JSON.stringify(next));
              return next;
            }
            case "system_info": return { mcpHttpUrl: runtimeSettings.mcpHttpEnabled && !options.mcpError ? `http://127.0.0.1:${runtimeSettings.mcpHttpPort}` : "", mcpError: options.mcpError, mcpAuthToken: "fixture-token-not-secret", appVersion: "1.4.3", platform: "macos" };
            case "profiles_list": return mock.profiles().map((current) => ({ ...current, isRunning: running.has(current.id), timezone: current.fingerprint.timezone }));
            case "profiles_get": return mock.profiles().find((current) => current.id === args.id) ?? null;
            case "profiles_create": {
              const created = { ...profile, ...args.input, id: `profile-${mock.calls.length}`, fingerprint: { ...profile.fingerprint, ...args.input.fingerprint } };
              saveProfiles([...mock.profiles(), created]);
              return created;
            }
            case "profiles_update": {
              const patch = JSON.parse(JSON.stringify(args.patch));
              mock.profileUpdates.push(patch);
              const next = { ...mock.profiles().find((current) => current.id === args.id)!, ...patch };
              saveProfiles(mock.profiles().map((current) => current.id === args.id ? next : current));
              return next;
            }
            case "profiles_delete":
              saveProfiles(mock.profiles().filter((current) => current.id !== args.id));
              return null;
            case "profiles_launch":
              running.add(args.id);
              mock.emit("profiles:running-changed", { kind: "launched", profileId: args.id });
              return { id: args.id, pid: 1234, cdpEndpoint: "http://127.0.0.1:9222", startedAt: profile.createdAt };
            case "profiles_close":
              running.delete(args.id);
              mock.emit("profiles:running-changed", { kind: "closed", profileId: args.id, reason: "user-close" });
              return null;
            case "activity_recent": return [];
            case "update_status":
            case "update_check": return { kind: "idle" };
            case "update_last_checked": return 0;
            case "update_download":
            case "update_install": return null;
            case "dialog_pick_browser_binary": return "/fixture/custom-chromix";
            case "dialog_pick_directory": return null;
            case "extensions_list":
            case "extensions_store_entries": return [];
            case "fingerprint_devices": return [{ family: "macbook-pro-14-m3", label: "MacBook Pro 14", screens: [{ width: 1440, height: 900, label: "1440 × 900" }] }];
            case "fingerprint_locales": return [{ id: "en-US", label: "English (US)", locale: "en-US", country: "us", timezones: ["America/New_York"] }];
            case "fingerprint_generate": return profile.fingerprint;
            default: throw new Error(`Unhandled fixture IPC command: ${command}`);
          }
        },
      } } },
    });
  }, { initial, profile: fixtureProfile, options });
}

export async function storedSettings(page: Page): Promise<AppSettings> {
  return page.evaluate(() => JSON.parse(localStorage.getItem("cloaksession.test.settings")!));
}

export async function settingsPatches(page: Page): Promise<Partial<AppSettings>[]> {
  return page.evaluate(() => (window as any).__WAILS_MOCK__.updates);
}

export async function profilePatches(page: Page): Promise<Record<string, unknown>[]> {
  return page.evaluate(() => (window as any).__WAILS_MOCK__.profileUpdates);
}
