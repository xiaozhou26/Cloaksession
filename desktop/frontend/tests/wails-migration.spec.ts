import { expect, test, type Page } from "@playwright/test";
import { defaultSettings, installWailsMock, storedSettings } from "./wailsMock";

const pageErrors = new WeakMap<Page, string[]>();

test.beforeEach(async ({ page }) => {
  const errors: string[] = [];
  pageErrors.set(page, errors);
  page.on("pageerror", (error) => errors.push(error.message));
});

test.afterEach(async ({ page }) => {
  expect(pageErrors.get(page)).toEqual([]);
});

async function openProfiles(page: Page): Promise<void> {
  await installWailsMock(page, defaultSettings, { section: "profiles" });
  await page.goto("/");
  await expect(page.getByText("All profiles", { exact: true })).toBeVisible();
}

async function emit(page: Page, name: string, payload: unknown): Promise<void> {
  await page.evaluate(({ name, payload }) => (window as any).__WAILS_MOCK__.emit(name, payload), { name, payload });
}

test("real bridge preserves decoded Go values, argument objects, and error messages", async ({ page }) => {
  await openProfiles(page);
  const result = await page.evaluate(async () => {
    const modulePath = "/src/lib/wails.ts";
    const { invoke } = await import(modulePath);
    const app = window.go!.main!.App!;
    const original = app.Invoke;
    const calls: unknown[] = [];
    const values = [{ nested: [true, null, "中文"] }, [1, 2], "{\"not\":\"parsed\"}", 0, false, null];
    try {
      app.Invoke = async function (command, args) {
        calls.push({ command, args, bound: this === app });
        return values[calls.length - 1];
      };
      const actual = [];
      for (let i = 0; i < values.length; i++) actual.push(await invoke("probe", i ? { id: "x", patch: { proxy: null } } : undefined));
      app.Invoke = async () => { throw "Go storage failure"; };
      let error = "";
      try { await invoke("fail"); } catch (e) { error = e instanceof Error ? e.message : "not an Error"; }
      app.Invoke = () => { throw new Error("Synchronous host failure"); };
      let synchronous = "";
      try { await invoke("fail"); } catch (e) { synchronous = (e as Error).message; }
      return { actual, calls, error, synchronous };
    } finally { app.Invoke = original; }
  });
  expect(result.actual).toEqual([{ nested: [true, null, "中文"] }, [1, 2], '{"not":"parsed"}', 0, false, null]);
  expect(result.calls[0]).toEqual({ command: "probe", args: {}, bound: true });
  expect(result.calls[1]).toEqual({ command: "probe", args: { id: "x", patch: { proxy: null } }, bound: true });
  expect(result.error).toBe("Go storage failure");
  expect(result.synchronous).toBe("Synchronous host failure");
});

test("all 37 public IPC command names and argument shapes remain compatible", async ({ page }) => {
  await openProfiles(page);
  const calls = await page.evaluate(async () => {
    const modulePath = "/src/lib/ipc.ts";
    const ipc = await import(modulePath);
    const app = window.go!.main!.App!;
    const original = app.Invoke;
    const calls: unknown[] = [];
    app.Invoke = async (command, args) => { calls.push([command, args]); return null; };
    try {
      await ipc.profiles.list();
      await ipc.profiles.get("id");
      await ipc.profiles.create({ name: "New", tags: ["a"] });
      await ipc.profiles.update("id", { proxy: null, name: "Edit" });
      await ipc.profiles.delete("id");
      await ipc.profiles.launch("id");
      await ipc.profiles.close("id");
      await ipc.profiles.exportArchive("id", "passphrase");
      await ipc.profiles.importArchive("passphrase");
      await ipc.settings.get();
      await ipc.settings.update({ browserBinaryPath: "", chromix: { options: { unknown: true } } });
      await ipc.dialog.pickBrowserBinary();
      await ipc.dialog.pickDirectory();
      await ipc.activity.recent();
      await ipc.activity.recent(12);
      await ipc.system.info();
      await ipc.fingerprint.generate();
      await ipc.fingerprint.devices();
      await ipc.fingerprint.locales();
      await ipc.fingerprint.reconcile({ seed: "1" }, { country: "us" });
      await ipc.fingerprint.localeForCountry("us");
      await ipc.proxy.detectGeo({ type: "http", host: "localhost", port: 8080 }, "id");
      await ipc.extensions.list("id");
      await ipc.extensions.addFromWebStore("id", "url");
      await ipc.extensions.addFromFile("id");
      await ipc.extensions.addFromFolder("id");
      await ipc.extensions.remove("id", "ext");
      await ipc.extensions.toggle("id", "ext", false);
      await ipc.extensions.storeEntries();
      await ipc.extensions.prepareFromWebStore("url");
      await ipc.extensions.prepareFromFile();
      await ipc.extensions.prepareFromFolder();
      await ipc.extensions.icon({ id: "ext" }, null);
      await ipc.update.status();
      await ipc.update.lastChecked();
      await ipc.update.check();
      await ipc.update.install();
      await ipc.update.download("1.4.0");
      return calls;
    } finally { app.Invoke = original; }
  });
  expect(new Set((calls as [string, unknown][]).map(([command]) => command)).size).toBe(37);
  expect(calls).toEqual([
    ["profiles_list", {}], ["profiles_get", { id: "id" }],
    ["profiles_create", { input: { name: "New", tags: ["a"] } }],
    ["profiles_update", { id: "id", patch: { proxy: null, name: "Edit" } }],
    ...["delete", "launch", "close"].map((action) => [`profiles_${action}`, { id: "id" }]),
    ["profiles_export_archive", { id: "id", passphrase: "passphrase" }],
    ["profiles_import_archive", { passphrase: "passphrase" }],
    ["settings_get", {}], ["settings_update", { patch: { browserBinaryPath: "", chromix: { options: { unknown: true } } } }],
    ["dialog_pick_browser_binary", {}], ["dialog_pick_directory", {}],
    ["activity_recent", { limit: null }], ["activity_recent", { limit: 12 }], ["system_info", {}],
    ["fingerprint_generate", { seed: "" }], ["fingerprint_devices", {}], ["fingerprint_locales", {}],
    ["fingerprint_reconcile", { fingerprint: { seed: "1" }, patch: { country: "us" } }],
    ["fingerprint_locale_for_country", { country: "us" }],
    ["proxy_detect_geo", { proxy: { type: "http", host: "localhost", port: 8080 } }],
    ["extensions_list", { profileId: "id" }], ["extensions_add_from_web_store", { profileId: "id", urlOrId: "url" }],
    ["extensions_add_from_file", { profileId: "id" }], ["extensions_add_from_folder", { profileId: "id" }],
    ["extensions_remove", { profileId: "id", extId: "ext" }], ["extensions_toggle", { profileId: "id", extId: "ext", enabled: false }],
    ["extensions_store_entries", {}], ["extensions_prepare_from_web_store", { urlOrId: "url" }],
    ["extensions_prepare_from_file", {}], ["extensions_prepare_from_folder", {}], ["extensions_icon", { ext: { id: "ext" }, profileId: null }],
    ...["status", "last_checked", "check", "install"].map((action) => [`update_${action}`, {}]),
    ["update_download", { version: "1.4.0" }],
  ]);
});

test("event bridge delivers raw payloads and cancels only its own subscription", async ({ page }) => {
  await openProfiles(page);
  const result = await page.evaluate(async () => {
    const modulePath = "/src/lib/ipc.ts";
    const ipc = await import(modulePath);
    const mock = (window as any).__WAILS_MOCK__;
    const delivered: unknown[] = [];
    const pairs = [
      ["onRunningChanged", "profiles:running-changed", { kind: "closing", profileId: "fixture-profile" }],
      ["onActivityEvent", "activity:event", { id: "event", timestamp: new Date().toISOString(), tool: "list_profiles", args: { payload: "not a host envelope" }, status: "ok" }],
      ["onProxyCountryUpdated", "profiles:proxy-country-updated", { id: "fixture-profile", country: "jp" }],
      ["onExtensionInstalled", "extensions:installed", { ok: false, profileId: "fixture-profile", error: "fixture failure" }],
      ["onUpdateStatus", "update:status", { status: { kind: "checking" } }],
    ] as const;
    for (const [method, name, payload] of pairs) {
      const first: unknown[] = [];
      const second: unknown[] = [];
      const before = mock.listenerCount(name);
      const registration = ipc[method]((value: unknown) => first.push(value));
      if (!(registration instanceof Promise)) throw new Error("Public registration must remain asynchronous");
      const off = await registration;
      const offSecond = await ipc[method]((value: unknown) => second.push(value));
      mock.emit(name, payload);
      const cancellations = mock.unsubscriptions;
      off();
      off();
      const cancelCount = mock.unsubscriptions - cancellations;
      mock.emit(name, payload);
      offSecond();
      mock.emit(name, payload);
      delivered.push({ method, first, second, cancelCount, remaining: mock.listenerCount(name) - before });
    }
    const statuses: unknown[] = [];
    const off = await ipc.onUpdateStatus((value: unknown) => statuses.push(value));
    mock.emit("update:status", { kind: "up-to-date" });
    off();
    let chromiumEvents = 0;
    const offChromium = await ipc.onChromiumStatus(() => chromiumEvents++);
    mock.emit("chromium:status", { profileId: "fixture-profile", status: "failed", error: "launch failure" });
    offChromium();
    let retryError = "";
    try { await ipc.chromium.retry(); } catch (e) { retryError = (e as Error).message; }
    return { delivered, statuses, chromiumEvents, chromiumStatus: await ipc.chromium.status(), retryError };
  });
  for (const entry of result.delivered as any[]) {
    expect(entry.first).toHaveLength(1);
    expect(entry.second).toEqual([entry.first[0], entry.first[0]]);
    expect(entry.cancelCount).toBe(1);
    expect(entry.remaining).toBe(0);
  }
  expect((result.delivered[0] as any).first[0]).toEqual({ kind: "closing", profileId: "fixture-profile" });
  expect((result.delivered[4] as any).first[0]).toEqual({ kind: "checking" });
  expect(result.statuses).toEqual([{ kind: "up-to-date" }]);
  expect(result.chromiumEvents).toBe(0);
  expect(result.chromiumStatus).toEqual({ kind: "ready" });
  expect(result.retryError).toContain("not implemented");
});

test("missing host bridges reject with actionable errors", async ({ page }) => {
  await openProfiles(page);
  const errors = await page.evaluate(async () => {
    const modulePath = "/src/lib/wails.ts";
    const { invoke, listen } = await import(modulePath);
    const go = window.go;
    const runtime = window.runtime;
    try {
      delete window.go;
      delete window.runtime;
      const errors: string[] = [];
      for (const call of [() => invoke("profiles_list"), () => listen("activity:event", () => {})]) {
        try { await call(); } catch (e) { errors.push((e as Error).message); }
      }
      return errors;
    } finally { window.go = go; window.runtime = runtime; }
  });
  expect(errors).toEqual([
    "Wails command bridge is unavailable. Open Cloaksession in the desktop app.",
    "Wails event bridge is unavailable. Open Cloaksession in the desktop app.",
  ]);
});

test("profile CRUD, launch/close, shared sheets and grid/list views survive migration", async ({ page }) => {
  await openProfiles(page);
  await page.getByRole("button", { name: /^New profile/ }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByPlaceholder("e.g. acme — sales · west").fill("Wails profile");
  await dialog.getByPlaceholder("comma-separated (optional)").fill("migration, shared");
  await dialog.getByRole("button", { name: "Browser", exact: true }).click();
  await expect(dialog.getByText("Start page", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await page.getByRole("button", { name: /Wails profile/ }).click();
  await expect(dialog.getByText("Edit Wails profile", { exact: true })).toBeVisible();
  await dialog.getByRole("textbox").first().fill("Renamed Wails profile");
  await expect(dialog.getByText("All changes saved", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await page.getByTitle("List view", { exact: true }).click();
  const row = page.getByRole("row").filter({ hasText: "Renamed Wails profile" });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "Launch", exact: true }).click();
  await expect(row.getByRole("button", { name: "Stop", exact: true })).toBeVisible();
  await row.getByRole("button", { name: "Stop", exact: true }).click();
  await expect(row.getByRole("button", { name: "Launch", exact: true })).toBeVisible();
  await row.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("button", { name: "Delete profile", exact: true }).click();
  await page.getByRole("button", { name: /^Yes, delete/ }).click();
  await expect(row).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole("button", { name: /Renamed Wails profile/ })).toHaveCount(0);
  expect(await page.evaluate(() => (window as any).__WAILS_MOCK__.profiles().map((p: any) => p.name))).toEqual(["Regression profile"]);
});

test("Go string failures surface in create and launch flows and allow retry", async ({ page }) => {
  await openProfiles(page);
  await page.evaluate(() => { (window as any).__WAILS_MOCK__.failures.profiles_launch = "Browser binary missing"; });
  await page.getByRole("button", { name: "Launch", exact: true }).click();
  await expect(page.getByText("Launch failed: Browser binary missing", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Launch", exact: true }).click();
  await expect(page.getByRole("button", { name: "Stop", exact: true })).toBeVisible();
  await page.getByRole("button", { name: /^New profile/ }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByPlaceholder("e.g. acme — sales · west").fill("Retry draft");
  await page.evaluate(() => { (window as any).__WAILS_MOCK__.failures.profiles_create = "Profile storage unavailable"; });
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog.getByText("Profile storage unavailable", { exact: true })).toBeVisible();
  await expect(dialog.getByPlaceholder("e.g. acme — sales · west")).toHaveValue("Retry draft");
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole("button", { name: /Retry draft/ })).toBeVisible();
});

for (const consent of [false, true]) {
  test(`onboarding persists profile and explicit telemetry choice (${consent})`, async ({ page }) => {
    await installWailsMock(page, defaultSettings, { section: "profiles", onboarded: false, empty: true });
    await page.goto("/");
    await page.getByRole("button", { name: "Continue", exact: true }).click();
    expect((await storedSettings(page)).usageReporting).toBe(false);
    await page.getByRole("button", { name: consent ? "Enable" : "Not now", exact: true }).click();
    await page.getByPlaceholder("My first profile").fill("Onboarded with Wails");
    await page.getByRole("button", { name: "Create profile", exact: true }).click();
    await expect(page.getByRole("button", { name: /Onboarded with Wails/ })).toBeVisible();
    expect((await storedSettings(page)).usageReporting).toBe(consent);
    expect(await page.evaluate(() => localStorage.getItem("multizen.ui.onboarded"))).toBe("true");
    await page.reload();
    await expect(page.getByRole("button", { name: /Onboarded with Wails/ })).toBeVisible();
    await expect(page.getByRole("button", { name: "Continue", exact: true })).toHaveCount(0);
  });
}

test("MCP config, live event replacement, profile refresh and update banners use Wails payloads", async ({ page }) => {
  await openProfiles(page);
  await page.getByTitle("MCP · ⌘2", { exact: true }).click();
  await expect(page.getByText("Connect an agent", { exact: true })).toBeVisible();
  await expect(page.locator("pre").filter({ hasText: '"mcpServers"' }).first()).toContainText('"Authorization": "Bearer fixture-token-not-secret"');
  await expect(page.locator("pre").filter({ hasText: "[mcp_servers.multizen]" })).toContainText('url = "http://127.0.0.1:7777/mcp"');
  const event = { id: "mcp-1", timestamp: new Date().toISOString(), tool: "create_profile", profileId: "fixture-profile", args: {}, status: "pending", summary: "Creating from MCP" };
  const before = await page.evaluate(() => (window as any).__WAILS_MOCK__.calls.filter((c: string) => c === "profiles_list").length);
  await emit(page, "activity:event", event);
  await expect(page.getByText("Creating from MCP", { exact: true })).toBeVisible();
  await emit(page, "activity:event", { ...event, status: "ok", summary: "MCP profile created", durationMs: 42 });
  await expect(page.getByText("Creating from MCP", { exact: true })).toHaveCount(0);
  await expect(page.getByText("MCP profile created", { exact: true })).toHaveCount(1);
  await expect(page.getByText("1 total", { exact: true })).toBeVisible();
  await expect.poll(() => page.evaluate(() => (window as any).__WAILS_MOCK__.calls.filter((c: string) => c === "profiles_list").length)).toBeGreaterThan(before);
  await emit(page, "update:status", { status: { kind: "available", version: "1.5.0" } });
  await expect(page.getByText("Cloaksession 1.5.0 is available.")).toBeVisible();
  await page.getByRole("button", { name: "Download", exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).__WAILS_MOCK__.invocations.at(-1))).toEqual({ command: "update_download", args: { version: "1.5.0" } });
  await page.getByRole("button", { name: "Dismiss", exact: true }).click();
  await expect(page.getByText("Cloaksession 1.5.0 is available.")).toHaveCount(0);
});

test("StrictMode and shared view navigation do not leak Wails listeners", async ({ page }) => {
  await openProfiles(page);
  const count = (name: string) => page.evaluate((name) => (window as any).__WAILS_MOCK__.listenerCount(name), name);
  for (const name of ["profiles:running-changed", "activity:event", "profiles:proxy-country-updated", "extensions:installed", "update:status"]) {
    await expect.poll(() => count(name)).toBe(1);
  }
  for (let i = 0; i < 3; i++) {
    await page.getByTitle("Settings · ⌘,", { exact: true }).click();
    await expect(page.getByText("Cloaksession v1.4.0 · macos · Wails v2", { exact: true })).toBeVisible();
    await expect.poll(() => count("update:status")).toBe(2);
    await page.getByTitle("Profiles · ⌘1", { exact: true }).click();
    await expect.poll(() => count("update:status")).toBe(1);
  }
  await page.getByRole("button", { name: /Regression profile/ }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Extensions", exact: true }).click();
  await expect.poll(() => count("extensions:installed")).toBe(2);
  await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
  await expect.poll(() => count("extensions:installed")).toBe(1);
  const before = await page.evaluate(() => (window as any).__WAILS_MOCK__.calls.filter((c: string) => c === "profiles_list").length);
  await emit(page, "profiles:proxy-country-updated", { id: "fixture-profile", country: "jp" });
  await expect.poll(() => page.evaluate(() => (window as any).__WAILS_MOCK__.calls.filter((c: string) => c === "profiles_list").length)).toBe(before + 1);
  await emit(page, "extensions:installed", { ok: true, profileId: "fixture-profile", extension: { id: "ext", name: "Wails extension" } });
  await expect(page.getByText('Added "Wails extension" — profile is reopening…', { exact: true })).toHaveCount(1);
  expect(await page.locator(".drag-region").first().evaluate((node) => getComputedStyle(node).getPropertyValue("--wails-draggable").trim())).toBe("drag");
});

test("disabled MCP state remains usable on shared views", async ({ page }) => {
  await installWailsMock(page, { ...defaultSettings, mcpHttpEnabled: false }, { section: "mcp" });
  await page.goto("/");
  await expect(page.getByText("server off", { exact: true })).toBeVisible();
  await expect(page.getByText(/The MCP server is disabled/)).toBeVisible();
  await expect(page.getByRole("button", { name: "Copy for LLM", exact: true })).toHaveCount(0);
  await page.getByTitle("Settings · ⌘,", { exact: true }).click();
  await expect(page.getByRole("region", { name: "Settings", exact: true })).toBeVisible();
});

test("onboarding create errors retain the name and allow retry without completing onboarding", async ({ page }) => {
  await installWailsMock(page, defaultSettings, { section: "profiles", onboarded: false, empty: true });
  await page.goto("/");
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await page.getByRole("button", { name: "Not now", exact: true }).click();
  await page.getByPlaceholder("My first profile").fill("Keep onboarding draft");
  await page.evaluate(() => { (window as any).__WAILS_MOCK__.failures.profiles_create = "Onboarding storage unavailable"; });
  await page.getByRole("button", { name: "Create profile", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("Onboarding storage unavailable");
  await expect(page.getByPlaceholder("My first profile")).toHaveValue("Keep onboarding draft");
  expect(await page.evaluate(() => localStorage.getItem("multizen.ui.onboarded"))).toBeNull();
  await page.getByRole("button", { name: "Create profile", exact: true }).click();
  await expect(page.getByRole("button", { name: /Keep onboarding draft/ })).toBeVisible();
});

for (const command of ["profiles_list", "system_info", "activity_recent"]) {
  test(`startup ${command} failure is visible and retry restores the app without leaked listeners`, async ({ page }) => {
    await installWailsMock(page, defaultSettings, {
      section: "profiles",
      startupFailures: { [command]: "Go service is not ready" },
    });
    await page.goto("/");
    await expect(page.getByRole("alert")).toContainText("Could not initialize Cloaksession: Go service is not ready");
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
    await page.evaluate(() => { (window as any).__WAILS_MOCK__.startupFailures = {}; });
    await page.getByRole("button", { name: "Retry connection", exact: true }).click();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByRole("button", { name: /Regression profile/ })).toBeVisible();
    await expect.poll(() => page.evaluate(() => (window as any).__WAILS_MOCK__.listenerCount("activity:event"))).toBe(1);
    await expect.poll(() => page.evaluate(() => (window as any).__WAILS_MOCK__.listenerCount("profiles:running-changed"))).toBe(1);
  });
}

for (const command of ["settings_get", "system_info", "update_status", "update_last_checked"]) {
  test(`settings startup ${command} failure is visible and recoverable`, async ({ page }) => {
    await installWailsMock(page, defaultSettings, { startupFailures: { [command]: "Go settings unavailable" } });
    await page.goto("/");
    await expect(page.getByRole("alert").filter({ hasText: "Could not load settings:" })).toContainText("Go settings unavailable");
    await page.evaluate(() => { (window as any).__WAILS_MOCK__.startupFailures = {}; });
    await page.getByRole("button", { name: "Retry settings", exact: true }).click();
    await expect(page.getByRole("region", { name: "Settings", exact: true })).toBeVisible();
    await expect(page.getByRole("alert").filter({ hasText: "Could not load settings:" })).toHaveCount(0);
    await expect.poll(() => page.evaluate(() => (window as any).__WAILS_MOCK__.listenerCount("update:status"))).toBe(2);
    await expect(page.getByText("Cloaksession v1.4.0 · macos · Wails v2", { exact: true })).toBeVisible();
  });
}

test("startup errors remain visible before onboarding completes", async ({ page }) => {
  await installWailsMock(page, defaultSettings, { onboarded: false, empty: true, startupFailures: { system_info: "Service startup failed" } });
  await page.goto("/");
  await expect(page.getByRole("alert").filter({ hasText: "Could not initialize Cloaksession:" })).toContainText("Service startup failed");
  await page.evaluate(() => { (window as any).__WAILS_MOCK__.startupFailures = {}; });
  await page.getByRole("button", { name: "Retry connection", exact: true }).click();
  await expect(page.getByRole("button", { name: "Continue", exact: true })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem("multizen.ui.onboarded"))).toBeNull();
});

test("optional MCP port conflict stays visible while profiles and settings remain usable", async ({ page }) => {
  const conflict = "listen tcp 127.0.0.1:7777: bind: address already in use";
  await installWailsMock(page, defaultSettings, { section: "mcp", mcpError: conflict });
  await page.goto("/");
  await expect(page.getByRole("alert")).toContainText(`MCP could not start: ${conflict}`);
  await expect(page.getByText("server unavailable", { exact: true })).toBeVisible();
  await expect(page.getByText("listening", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Copy for LLM", exact: true })).toHaveCount(0);
  await expect(page.getByText(/Could not initialize Cloaksession:/)).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);

  await page.getByTitle("Profiles · ⌘1", { exact: true }).click();
  await page.getByRole("button", { name: "Launch", exact: true }).click();
  await page.getByRole("button", { name: "Stop", exact: true }).click();
  await page.getByRole("button", { name: /Regression profile/ }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
  await page.getByTitle("MCP · ⌘2", { exact: true }).click();
  await page.getByRole("button", { name: "Configure MCP in Settings", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(conflict);
  await expect(page.getByText("unavailable", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Copy URL", exact: true })).toHaveCount(0);
  await expect(page.getByText(/^running on :/)).toHaveCount(0);
  const port = page.getByLabel("MCP HTTP port", { exact: true });
  const savePort = page.getByRole("button", { name: "Save MCP port", exact: true });
  for (const invalid of ["", "0", "65536", "1.5", "abc"]) {
    await port.fill(invalid);
    await expect(port).toHaveAttribute("aria-invalid", "true");
    await expect(savePort).toBeDisabled();
    expect((await storedSettings(page)).mcpHttpPort).toBe(7777);
  }
  await port.fill("7788");
  await savePort.click();
  await expect.poll(async () => (await storedSettings(page)).mcpHttpPort).toBe(7788);
  await expect(page.getByRole("alert")).toContainText(conflict);
  await expect(page.getByText(/changes apply after restarting Cloaksession, not immediately/)).toBeVisible();
  await page.getByRole("checkbox", { name: "Auto-start MCP HTTP transport on app launch", exact: true }).uncheck();
  await expect.poll(async () => (await storedSettings(page)).mcpHttpEnabled).toBe(false);
  await expect(page.getByRole("alert")).toContainText(conflict);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
  await page.getByTitle("MCP · ⌘2", { exact: true }).click();
  await expect(page.getByText("server unavailable", { exact: true })).toBeVisible();
  await expect(page.getByText("listening", { exact: true })).toHaveCount(0);
});

test("saving a new MCP port does not mislabel the currently running endpoint", async ({ page }) => {
  await installWailsMock(page);
  await page.goto("/");
  await expect(page.getByText("running on :7777", { exact: true })).toBeVisible();
  await page.getByLabel("MCP HTTP port", { exact: true }).fill("7788");
  await page.getByRole("button", { name: "Save MCP port", exact: true }).click();
  await expect.poll(async () => (await storedSettings(page)).mcpHttpPort).toBe(7788);
  await expect(page.getByText("running on :7777", { exact: true })).toBeVisible();
  await expect(page.getByText("running on :7788", { exact: true })).toHaveCount(0);
  await page.getByTitle("MCP · ⌘2", { exact: true }).click();
  await expect(page.locator("pre").filter({ hasText: "[mcp_servers.multizen]" })).toContainText('url = "http://127.0.0.1:7777/mcp"');
});
