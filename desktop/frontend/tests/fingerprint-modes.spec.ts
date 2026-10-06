import { expect, test, type Page } from "@playwright/test";
import { defaultSettings, installWailsMock, profilePatches, storedSettings } from "./wailsMock";

const pageErrors = new WeakMap<Page, string[]>();
test.beforeEach(({ page }) => {
  const errors: string[] = [];
  pageErrors.set(page, errors);
  page.on("pageerror", (error) => errors.push(error.message));
});
test.afterEach(({ page }) => { expect(pageErrors.get(page)).toEqual([]); });

const maxSeed = "18446744073709551615";
const legacy = {
  args: ["--fingerprint=42", "--fingerprint-seed=13", "--uxr-fingerprint-seed=7", "--future=kept"],
  launchOptions: { args: ["--fingerprint=99"], timeout: 30000 },
  contextOptions: { extraHTTPHeaders: { "X-Keep": "yes" } },
  futureOption: { nested: [true, null, "中文"] },
};
const globalSettings = (options: Record<string, unknown>) => ({ ...defaultSettings, browserEngine: "chromix" as const, chromix: { ...defaultSettings.chromix, options } });
const save = (page: Page) => page.getByRole("button", { name: "Save Chromix settings", exact: true });
const seed = (page: Page) => page.getByLabel("Fingerprint seed", { exact: true });
const mode = (page: Page, name: string) => page.getByRole("button", { name, exact: true });
const globalAdvanced = (page: Page) => page.locator("summary").filter({ hasText: /^Advanced: Playwright options/ });
const profileAdvanced = (page: Page) => page.locator("summary").filter({ hasText: /^Advanced: native flags and profile Playwright JSON$/ });

async function openProfile(page: Page): Promise<void> {
  await page.getByTitle("Profiles · ⌘1", { exact: true }).click();
  await page.getByRole("button", { name: /Regression profile/ }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Chromix fingerprint", exact: true }).click();
  await expect(page.getByRole("region", { name: "Fingerprint mode", exact: true })).toBeVisible();
}

async function profileOptions(page: Page): Promise<Record<string, unknown>> {
  return page.evaluate(() => (window as any).__WAILS_MOCK__.profiles()[0].chromixOptions ?? {});
}

async function saved(page: Page): Promise<void> {
  await save(page).click();
  await expect(page.getByRole("status")).toHaveText("Saved. Restart the app to apply.");
}

test("global modes preserve legacy options and exact seeds with explicit precedence", async ({ page }) => {
  await installWailsMock(page, globalSettings(legacy));
  await page.goto("/");
  await expect(mode(page, "Random each launch")).toHaveAttribute("aria-pressed", "false");
  await expect(page.getByLabel("Playwright options JSON", { exact: true })).not.toBeVisible();
  await saved(page);
  expect((await storedSettings(page)).chromix.options).toEqual(legacy);
  await mode(page, "Fixed seed").click();
  await expect(seed(page)).toHaveValue("7");
  await seed(page).fill(maxSeed);
  await saved(page);
  expect((await storedSettings(page)).chromix.options).toEqual({ ...legacy, fingerprintMode: "fixed", fingerprintSeed: maxSeed });
  await page.reload();
  await expect(seed(page)).toHaveValue(maxSeed);
  await expect(mode(page, "Fixed seed")).toHaveAttribute("aria-pressed", "true");
  await mode(page, "Random each launch").click();
  await expect(seed(page)).toHaveCount(0);
  await expect(page.getByText(/fresh cryptographic seed on every launch/)).toBeVisible();
  await expect(page.getByText(/priority over old seed flags, including nested/)).toBeVisible();
  await saved(page);
  expect((await storedSettings(page)).chromix.options).toEqual({ ...legacy, fingerprintMode: "random", fingerprintSeed: maxSeed });
  await mode(page, "Custom").click();
  await expect(seed(page)).toHaveValue(maxSeed);
  await page.getByRole("combobox", { name: "Platform", exact: true }).selectOption("windows");
  await page.getByRole("combobox", { name: "Language", exact: true }).selectOption("zh-CN");
  await page.getByRole("combobox", { name: "Timezone", exact: true }).selectOption("Asia/Shanghai");
  await page.getByRole("combobox", { name: "Screen size", exact: true }).selectOption("1920x1080");
  await page.getByRole("combobox", { name: "CPU cores", exact: true }).selectOption("8");
  await page.getByRole("combobox", { name: "Memory (GB)", exact: true }).selectOption("16");
  await saved(page);
  const stored = (await storedSettings(page)).chromix.options;
  expect(stored.fingerprintMode).toBe("custom");
  expect(stored.fingerprintSeed).toBe(maxSeed);
  expect(stored.launchOptions).toEqual(legacy.launchOptions);
  expect(stored.futureOption).toEqual(legacy.futureOption);
  expect(stored.args).toEqual(expect.arrayContaining([...legacy.args, "--fingerprint-platform=windows", "--uxr-languages=zh-CN", "--uxr-timezone=Asia/Shanghai", "--uxr-screen-width=1920", "--uxr-screen-height=1080", "--uxr-hw-concurrency=8", "--uxr-device-memory=16"]));
  await page.reload();
  await expect(page.getByRole("combobox", { name: "Screen size", exact: true })).toHaveValue("1920x1080");
});

test("Random seed uses cryptographic uint64 text and invalid input never replaces the saved seed", async ({ page }) => {
  await installWailsMock(page, globalSettings({ fingerprintMode: "fixed", fingerprintSeed: "42" }));
  await page.goto("/");
  await page.evaluate(() => {
    (window as any).__randomCalls = 0;
    crypto.getRandomValues = ((array: Uint32Array) => {
      (window as any).__randomCalls++;
      array.fill(0xffffffff);
      return array;
    }) as typeof crypto.getRandomValues;
  });
  await mode(page, "Random seed").click();
  await expect(seed(page)).toHaveValue(maxSeed);
  expect(await page.evaluate(() => (window as any).__randomCalls)).toBe(1);
  await saved(page);
  for (const invalid of ["18446744073709551616", "-1", "1.5", "1e9", "", "off", "0"]) {
    await seed(page).fill(invalid);
    await expect(seed(page)).toHaveAttribute("aria-invalid", "true");
    await expect(page.getByRole("alert")).toContainText("unsigned 64-bit");
    await expect(save(page)).toBeDisabled();
    expect((await storedSettings(page)).chromix.options.fingerprintSeed).toBe(maxSeed);
  }
  await seed(page).fill("1");
  await saved(page);
  expect((await storedSettings(page)).chromix.options.fingerprintSeed).toBe("1");
});

test("profile default inheritance is non-mutating and explicit overrides survive reopening and launches", async ({ page }) => {
  await installWailsMock(page, globalSettings({ ...legacy, fingerprintMode: "fixed", fingerprintSeed: maxSeed }));
  await page.goto("/");
  await openProfile(page);
  await expect(page.getByRole("note")).toContainText("Using global defaults (Fixed seed)");
  expect(await profileOptions(page)).toEqual({});
  expect(await profilePatches(page)).toEqual([]);
  await profileAdvanced(page).click();
  await expect(page.getByLabel("Profile Playwright options", { exact: true })).toHaveValue("{}");
  await profileAdvanced(page).click();
  await mode(page, "Custom").click();
  await expect(seed(page)).toHaveValue(maxSeed);
  await page.getByRole("combobox", { name: "Platform", exact: true }).selectOption("linux");
  await expect.poll(() => profileOptions(page)).toMatchObject({ fingerprintMode: "custom", fingerprintSeed: maxSeed, args: [...legacy.args, "--fingerprint-platform=linux"] });
  await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
  await openProfile(page);
  await expect(seed(page)).toHaveValue(maxSeed);
  await expect(page.getByRole("combobox", { name: "Platform", exact: true })).toHaveValue("linux");
  await mode(page, "Random each launch").click();
  await expect.poll(async () => (await profileOptions(page)).fingerprintMode).toBe("random");
  const before = await profileOptions(page);
  await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: "Launch", exact: true }).click();
  await page.getByRole("button", { name: "Stop", exact: true }).click();
  await page.getByRole("button", { name: "Launch", exact: true }).click();
  await page.getByRole("button", { name: "Stop", exact: true }).click();
  expect(await profileOptions(page)).toEqual(before);
  await openProfile(page);
  await mode(page, "Use global defaults / existing options").click();
  await expect.poll(() => profileOptions(page)).toEqual({ args: before.args });
  expect((await storedSettings(page)).chromix.options).toEqual({ ...legacy, fingerprintMode: "fixed", fingerprintSeed: maxSeed });
});

test("legacy profile JSON and advanced edits retain unknown keys until explicitly changed", async ({ page }) => {
  await installWailsMock(page, globalSettings({}));
  await page.goto("/");
  await openProfile(page);
  await profileAdvanced(page).click();
  const editor = page.getByLabel("Profile Playwright options", { exact: true });
  await editor.fill(JSON.stringify(legacy));
  await mode(page, "Apply profile JSON").click();
  await expect.poll(() => profileOptions(page)).toEqual(legacy);
  await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
  await openProfile(page);
  await expect(mode(page, "Fixed seed")).toHaveAttribute("aria-pressed", "false");
  expect(await profileOptions(page)).toEqual(legacy);
  await mode(page, "Fixed seed").click();
  await expect(seed(page)).toHaveValue("7");
  await mode(page, "Random seed").click();
  await expect(seed(page)).toHaveValue(/^[0-9]+$/);
  expect(await seed(page).inputValue()).not.toBe("7");
  await seed(page).fill(maxSeed);
  await expect.poll(() => profileOptions(page)).toEqual({ ...legacy, fingerprintMode: "fixed", fingerprintSeed: maxSeed });
  await profileAdvanced(page).click();
  await editor.fill(JSON.stringify({ ...legacy, fingerprintMode: "fixed", fingerprintSeed: 18446744073709551615 }));
  await mode(page, "Apply profile JSON").click();
  await expect(page.getByRole("alert")).toContainText("unsigned 64-bit");
  expect((await profileOptions(page)).fingerprintSeed).toBe(maxSeed);
});

test("ordinary engine Random seed rotates only its existing noise seed semantics", async ({ page }) => {
  await installWailsMock(page);
  await page.goto("/");
  const original = await page.evaluate(() => (window as any).__WAILS_MOCK__.profiles()[0].fingerprint);
  await page.getByTitle("Profiles · ⌘1", { exact: true }).click();
  await page.getByRole("button", { name: /Regression profile/ }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Fingerprint", exact: true }).click();
  await mode(page, "Random seed").click();
  await expect(page.getByLabel("Canvas noise seed", { exact: true })).toHaveValue(/^[0-9a-f]{8}$/);
  await expect.poll(async () => (await profilePatches(page)).at(-1)?.fingerprint).toMatchObject(original);
  const next = await page.evaluate(() => (window as any).__WAILS_MOCK__.profiles()[0].fingerprint);
  expect(next.seed).toMatch(/^[0-9a-f]{8}$/);
  delete next.seed;
  expect(next).toEqual(original);
});

test("simple modes and seed controls fit mobile widths without exposing advanced catalogs", async ({ page }, testInfo) => {
  await installWailsMock(page, globalSettings({}));
  await page.goto("/");
  await mode(page, "Custom").click();
  await seed(page).fill(maxSeed);
  if (testInfo.project.name === "mobile-chrome") await page.setViewportSize({ width: 320, height: 740 });
  const region = page.getByRole("region", { name: "Fingerprint mode", exact: true });
  const size = await region.evaluate((node) => ({ client: node.clientWidth, scroll: node.scrollWidth, right: node.getBoundingClientRect().right }));
  expect(size.scroll).toBeLessThanOrEqual(size.client + 1);
  expect(size.right).toBeLessThanOrEqual(page.viewportSize()!.width);
  await expect(page.getByLabel("Find a parameter", { exact: true })).not.toBeVisible();
  await globalAdvanced(page).click();
  await page.locator("summary").filter({ hasText: /^All native fingerprint parameters$/ }).click();
  await expect(page.getByLabel("Find a parameter", { exact: true })).toBeVisible();
});

test("new profile stores a chosen fixed seed without changing global defaults", async ({ page }) => {
  await installWailsMock(page, globalSettings({ fingerprintMode: "random" }));
  await page.goto("/");
  await page.getByTitle("Profiles · ⌘1", { exact: true }).click();
  await page.getByRole("button", { name: /^New profile/ }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByPlaceholder("e.g. acme — sales · west").fill("Simple fixed profile");
  await dialog.getByRole("button", { name: "Chromix fingerprint", exact: true }).click();
  await expect(page.getByRole("note")).toContainText("Using global defaults (Random each launch)");
  await mode(page, "Fixed seed").click();
  await seed(page).fill(maxSeed);
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await page.getByRole("button", { name: /Simple fixed profile/ }).click();
  await dialog.getByRole("button", { name: "Chromix fingerprint", exact: true }).click();
  await expect(seed(page)).toHaveValue(maxSeed);
  expect((await storedSettings(page)).chromix.options).toEqual({ fingerprintMode: "random" });
});
