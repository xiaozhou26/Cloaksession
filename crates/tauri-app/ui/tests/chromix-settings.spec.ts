import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { defaultSettings, installTauriMock, profilePatches, settingsPatches, storedSettings } from "./tauriMock";

const settingsRegion = (page: Page) => page.getByRole("region", { name: "Settings", exact: true });
const optionsEditor = (page: Page) => page.getByLabel("SDK options JSON", { exact: true });
const environmentEditor = (page: Page) => page.getByLabel("Environment JSON", { exact: true });
const nodeEditor = (page: Page) => page.getByLabel("Node.js executable", { exact: true });
const save = (page: Page) => page.getByRole("button", { name: "Save Chromix settings", exact: true });

async function chooseChromix(page: Page): Promise<void> {
  await settingsRegion(page).getByRole("button", { name: /^Chromix / }).click();
  await expect(nodeEditor(page)).toBeVisible();
}

async function assertSettingsFit(page: Page): Promise<void> {
  const size = await settingsRegion(page).evaluate((element) => ({
    client: element.clientWidth,
    scroll: element.scrollWidth,
    left: element.getBoundingClientRect().left,
    right: element.getBoundingClientRect().right,
  }));
  expect(size.scroll).toBeLessThanOrEqual(size.client + 1);
  expect(size.left).toBeGreaterThanOrEqual(0);
  expect(size.right).toBeLessThanOrEqual(page.viewportSize()!.width);
  for (const editor of [nodeEditor(page), optionsEditor(page), environmentEditor(page), save(page)]) {
    const bounds = await editor.boundingBox();
    expect(bounds!.width).toBeGreaterThan(100);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  }
}

async function screenshot(page: Page, testInfo: TestInfo, name: string): Promise<void> {
  const path = testInfo.outputPath(`${name}.png`);
  await page.screenshot({ path });
  await testInfo.attach(name, { path, contentType: "image/png" });
}

test.beforeEach(async ({ page }) => {
  await installTauriMock(page);
  await page.goto("/");
  await expect(settingsRegion(page)).toBeVisible();
});

test("explicit save preserves full SDK JSON and unknown keys across navigation and reload", async ({ page }, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await expect(settingsRegion(page).getByRole("button", { name: /^CloakBrowser / })).toHaveAttribute("aria-pressed", "true");
  await chooseChromix(page);
  await expect(nodeEditor(page)).toHaveValue("node");
  await expect(optionsEditor(page)).toHaveValue("{}");
  await expect(environmentEditor(page)).toHaveValue("{}");
  const options = {
    headless: false,
    proxy: { server: "http://proxy.example:8080", username: "fixture", password: "not-secret" },
    args: ["--fingerprint=42"],
    stealthArgs: true,
    timezone: "Asia/Shanghai", locale: "zh-CN", geoip: true,
    humanize: true, humanPreset: "careful", humanConfig: { typingDelay: 130, seed: 42 },
    userAgent: "Fixture user agent", viewport: { width: 1440, height: 900 }, colorScheme: "dark",
    extensionPaths: ["/fixture/extension"], browserVersion: "stable", releaseChannel: "stable", licenseKey: "",
    contextOptions: { acceptDownloads: true, extraHTTPHeaders: { "X-Fixture": "kept" } },
    launchOptions: { timeout: 30000, slowMo: 20 }, userDataDir: "/fixture/profile", startMaximized: false,
    fontsDir: "/fixture/fonts", devicePool: { python: "python", records: ["fixture.json"], host: "fixture.json", seed: "42" },
    futureSdkField: { nested: [true, null, 7, "保留未知字段"] },
  };
  const environment = { CHROMIX_CACHE_DIR: "/fixture/cache", CLOAKBROWSER_WIDEVINE: "0", FUTURE_SDK_ENV: "retained" };
  await nodeEditor(page).fill("/fixture/bin/node");
  await optionsEditor(page).fill(JSON.stringify(options));
  await environmentEditor(page).fill(JSON.stringify(environment));
  expect((await storedSettings(page)).chromix).toEqual(defaultSettings.chromix);
  await save(page).click();
  await expect(page.getByRole("status")).toHaveText("Saved. Restart the app to apply.");
  const expected = { nodePath: "/fixture/bin/node", options, environment };
  expect((await storedSettings(page)).chromix).toEqual(expected);
  expect((await settingsPatches(page)).at(-1)).toEqual({ chromix: expected });
  await assertSettingsFit(page);
  await optionsEditor(page).scrollIntoViewIfNeeded();
  await screenshot(page, testInfo, "chromix-options-saved");
  await environmentEditor(page).scrollIntoViewIfNeeded();
  await screenshot(page, testInfo, "chromix-environment-saved");

  await page.getByTitle("Profiles · ⌘1", { exact: true }).click();
  await expect(page.getByText("All profiles", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: /Regression profile/ }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByText("Edit Regression profile", { exact: true })).toBeVisible();
  await page.getByRole("dialog").getByRole("button", { name: "Browser", exact: true }).click();
  await expect(page.getByText("Start page", { exact: true })).toBeVisible();
  await screenshot(page, testInfo, "shared-profile-browser");
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByTitle("MCP · ⌘2", { exact: true }).click();
  await expect(page.getByText("Connect an agent", { exact: true })).toBeVisible();
  await expect(page.getByText("Live tool calls", { exact: true })).toBeVisible();
  await screenshot(page, testInfo, "shared-mcp");
  await page.getByTitle("Settings · ⌘,", { exact: true }).click();
  await expect(nodeEditor(page)).toHaveValue(expected.nodePath);
  expect(JSON.parse(await optionsEditor(page).inputValue())).toEqual(options);
  await page.reload();
  await expect(nodeEditor(page)).toHaveValue(expected.nodePath);
  expect(JSON.parse(await optionsEditor(page).inputValue())).toEqual(options);
  expect(JSON.parse(await environmentEditor(page).inputValue())).toEqual(environment);
  await assertSettingsFit(page);
  expect(errors).toEqual([]);
});

test("Profile Chromix JSON autosaves independently and survives closing and reopening", async ({ page }) => {
  await page.getByTitle("Profiles · ⌘1", { exact: true }).click();
  await page.getByRole("button", { name: /Regression profile/ }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Chromix fingerprint", exact: true }).click();
  await expect(dialog.getByText("Chromix fingerprint parameters", { exact: true })).toBeVisible();

  const options = {
    args: ["--fingerprint=18446744073709551615", "--fingerprint-noise=false", "--future=kept"],
    headless: false,
    futureSdkField: { nested: [true, null, "preserve"] },
  };
  const profileJson = dialog.locator("details").filter({ hasText: "Complete profile SDK options (JSON)" });
  await profileJson.locator("summary").click({ force: true });
  const editor = dialog.getByLabel("Profile Chromix SDK options", { exact: true });
  await editor.fill(JSON.stringify(options, null, 2));
  await dialog.getByRole("button", { name: "Apply profile JSON", exact: true }).click();
  await expect.poll(async () => (await profilePatches(page)).at(-1)?.chromixOptions).toEqual(options);
  await expect(dialog.getByText("All changes saved", { exact: true })).toBeVisible({ timeout: 5000 });

  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("button", { name: /Regression profile/ }).click();
  const reopened = page.getByRole("dialog");
  await reopened.getByRole("button", { name: "Chromix fingerprint", exact: true }).click();
  const reopenedJson = reopened.locator("details").filter({ hasText: "Complete profile SDK options (JSON)" });
  await reopenedJson.locator("summary").click({ force: true });
  expect(JSON.parse(await reopened.getByLabel("Profile Chromix SDK options", { exact: true }).inputValue())).toEqual(options);
});

test("invalid JSON, non-object roots and non-string environment values never overwrite settings", async ({ page }, testInfo) => {
  await chooseChromix(page);
  await optionsEditor(page).fill('{"keepUnknown":{"nested":[1,true,null]}}');
  await environmentEditor(page).fill('{"KEEP_ENV":"yes"}');
  await save(page).click();
  await expect(page.getByRole("status")).toHaveText("Saved. Restart the app to apply.");
  const before = await storedSettings(page);
  const count = (await settingsPatches(page)).length;
  for (const invalid of ['{"headless":', "[]", "null", '"string"', "12", "true"]) {
    await optionsEditor(page).fill(invalid);
    await save(page).click();
    await expect(optionsEditor(page)).toHaveAttribute("aria-invalid", "true");
    await expect(page.getByRole("alert")).toContainText(invalid.startsWith("{") ? "invalid JSON" : "top-level JSON object");
    expect(await storedSettings(page)).toEqual(before);
    expect((await settingsPatches(page)).length).toBe(count);
  }
  await optionsEditor(page).fill('{"replacement":"must not save on an environment error"}');
  for (const invalid of ['{"ENV":', "[]", "null", '"env"', '{"PORT":123}', '{"FLAG":true}', '{"VALUE":null}', '{"VALUE":{"nested":"no"}}', '{"VALUE":["no"]}']) {
    await environmentEditor(page).fill(invalid);
    await save(page).click();
    await expect(environmentEditor(page)).toHaveAttribute("aria-invalid", "true");
    await expect(page.getByRole("alert")).toBeVisible();
    expect(await storedSettings(page)).toEqual(before);
    expect((await settingsPatches(page)).length).toBe(count);
  }
  await screenshot(page, testInfo, "chromix-invalid-environment");
  await environmentEditor(page).fill("{}");
  await nodeEditor(page).fill("   ");
  await save(page).click();
  await expect(page.getByRole("alert")).toHaveText("Enter a Node.js executable path or node.");
  expect(await storedSettings(page)).toEqual(before);
  await page.reload();
  await expect(nodeEditor(page)).toHaveValue("node");
  expect(JSON.parse(await optionsEditor(page).inputValue())).toEqual(before.chromix.options);
  expect(JSON.parse(await environmentEditor(page).inputValue())).toEqual(before.chromix.environment);
});

test("save failure retains the draft and permits retry without false success", async ({ page }) => {
  await chooseChromix(page);
  await optionsEditor(page).fill('{"futureOption":"draft"}');
  await page.evaluate(() => { (window as any).__TEST_IPC__.failNextSave = true; });
  await save(page).click();
  await expect(page.getByRole("alert")).toContainText("Fixture storage unavailable");
  await expect(optionsEditor(page)).toHaveValue('{"futureOption":"draft"}');
  expect((await storedSettings(page)).chromix.options).toEqual({});
  await expect(page.getByRole("status")).toContainText("Unsaved changes");
  await save(page).click();
  await expect(page.getByRole("status")).toHaveText("Saved. Restart the app to apply.");
  expect((await storedSettings(page)).chromix.options).toEqual({ futureOption: "draft" });
});

test("legacy engines keep their defaults and Chromix data; binary reset sends an empty string", async ({ page }) => {
  await chooseChromix(page);
  await optionsEditor(page).fill('{"unknownOption":{"keep":true}}');
  await save(page).click();
  await expect(page.getByRole("status")).toHaveText("Saved. Restart the app to apply.");
  const savedChromix = (await storedSettings(page)).chromix;
  await page.getByRole("checkbox", { name: "Automatically check for updates", exact: true }).check();
  await expect(optionsEditor(page)).toHaveValue(JSON.stringify(savedChromix.options, null, 2));
  for (const engine of ["Chrome for Testing", "CloakBrowser"]) {
    const button = settingsRegion(page).getByRole("button", { name: new RegExp(`^${engine} `) });
    await button.click();
    await expect(button).toHaveAttribute("aria-pressed", "true");
    await expect(nodeEditor(page)).toHaveCount(0);
    await expect(page.getByRole("checkbox", { name: "Skip auto-download (use cached binary or the custom path above)", exact: true })).toBeVisible();
    expect((await storedSettings(page)).chromix).toEqual(savedChromix);
    await page.reload();
    await expect(button).toHaveAttribute("aria-pressed", "true");
  }
  await chooseChromix(page);
  expect(JSON.parse(await optionsEditor(page).inputValue())).toEqual(savedChromix.options);
  await expect(page.getByRole("checkbox", { name: /Skip Chromix SDK auto-download/ })).toBeVisible();
  const binary = page.getByRole("textbox", { name: "Browser binary path", exact: true });
  await page.getByRole("button", { name: "Browse…", exact: true }).click();
  await expect(binary).toHaveValue("/fixture/custom-chromix");
  await page.getByRole("button", { name: "Reset", exact: true }).click();
  await expect(binary).toHaveValue("");
  expect((await settingsPatches(page)).at(-1)).toEqual({ browserBinaryPath: "" });
  await page.getByRole("button", { name: "Browse…", exact: true }).click();
  await expect(binary).toHaveValue("/fixture/custom-chromix");
  await binary.fill("");
  expect((await settingsPatches(page)).at(-1)).toEqual({ browserBinaryPath: "" });
  await page.reload();
  await expect(binary).toHaveValue("");
  expect((await storedSettings(page)).chromix).toEqual(savedChromix);
});

test("SDK examples, official link and runtime boundaries fit narrow settings viewports", async ({ page }, testInfo) => {
  await chooseChromix(page);
  await expect(page.getByRole("link", { name: "Official Chromix Node SDK README ↗" })).toHaveAttribute("href", "https://github.com/xiaozhou26/Chromix/blob/main/sdk/node/README.md");
  await expect(page.getByText(/SDK humanize only affects SDK Playwright/)).toBeVisible();
  await expect(page.getByText(/devicePool requires the matching Python SDK/)).toBeVisible();
  await expect(page.getByText(/reserves debugging arguments/)).toBeVisible();
  await expect(page.getByText(/JSON cannot store functions or callbacks/)).toBeVisible();
  await page.getByText("Complete SDK JSON examples", { exact: true }).click();
  const examples = page.locator("details pre");
  await expect(examples).toHaveCount(3);
  const options = JSON.parse((await examples.nth(0).textContent())!);
  expect(Object.keys(options)).toEqual(expect.arrayContaining([
    "headless", "proxy", "args", "stealthArgs", "timezone", "locale", "geoip", "humanize",
    "humanPreset", "humanConfig", "userAgent", "viewport", "colorScheme", "extensionPaths",
    "browserVersion", "releaseChannel", "licenseKey", "contextOptions", "launchOptions", "userDataDir", "startMaximized", "fontsDir",
  ]));
  expect(Object.values(JSON.parse((await examples.nth(1).textContent())!)).every((value) => typeof value === "string")).toBe(true);
  expect(JSON.parse((await examples.nth(2).textContent())!)).toHaveProperty("devicePool.python", "python");
  await assertSettingsFit(page);
  await examples.nth(0).scrollIntoViewIfNeeded();
  await screenshot(page, testInfo, "chromix-sdk-examples");
  if (testInfo.project.name === "mobile-chrome") {
    await page.setViewportSize({ width: 320, height: 740 });
    await assertSettingsFit(page);
    await optionsEditor(page).scrollIntoViewIfNeeded();
    await screenshot(page, testInfo, "chromix-320px-options");
  }
});
