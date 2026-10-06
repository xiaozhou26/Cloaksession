import { expect, test, type Locator, type Page } from "@playwright/test";
import { defaultSettings, installWailsMock } from "./wailsMock";

async function fitsDocument(page: Page): Promise<void> {
  const width = page.viewportSize()!.width;
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  await expect.poll(() => page.evaluate(() => document.body.scrollWidth)).toBeLessThanOrEqual(width);
}

async function fitsViewport(page: Page, locator: Locator): Promise<void> {
  await expect(locator).toBeVisible();
  const bounds = await locator.boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
}

test("header and shared views fit the document and retain usable controls", async ({ page }, testInfo) => {
  await installWailsMock(page, { ...defaultSettings, browserEngine: "chromix" });
  await page.goto("/");
  const header = page.getByRole("banner");
  const search = header.getByRole("button", { name: "Search profiles, tags, URLs", exact: true });
  const settings = header.getByRole("button", { name: "Settings", exact: true });
  await expect(page.getByRole("region", { name: "Settings", exact: true })).toBeVisible();
  await fitsDocument(page);
  await fitsViewport(page, search);
  await fitsViewport(page, settings);
  expect((await search.boundingBox())!.height).toBeLessThanOrEqual(44);
  if (testInfo.project.name === "desktop-chrome") {
    await expect(header.getByText("running", { exact: true })).toBeVisible();
    await expect(header.getByText("⌘ K", { exact: true })).toBeVisible();
    await expect(header.getByText("Cloaksession", { exact: true })).toBeVisible();
    await expect(header.getByText("MCP · :7777", { exact: true })).toBeVisible();
  } else {
    expect(page.viewportSize()!.width).toBe(390);
    await expect(header.getByText("running", { exact: true })).not.toBeVisible();
    await expect(header.getByText("⌘ K", { exact: true })).not.toBeVisible();
  }
  await page.getByRole("button", { name: "Custom", exact: true }).click();
  await page.getByLabel("Fingerprint seed", { exact: true }).fill("18446744073709551615");
  await fitsDocument(page);
  await fitsViewport(page, page.getByLabel("Fingerprint seed", { exact: true }));

  await page.getByTitle("Profiles · ⌘1", { exact: true }).click();
  await expect(page.getByText("All profiles", { exact: true })).toBeVisible();
  await fitsDocument(page);
  await fitsViewport(page, page.getByRole("button", { name: /^New profile/ }));
  await page.getByRole("button", { name: /Regression profile/ }).click();
  const dialog = page.getByRole("dialog");
  await fitsViewport(page, dialog);
  await dialog.getByRole("button", { name: "Chromix fingerprint", exact: true }).click();
  await dialog.getByRole("button", { name: "Custom", exact: true }).click();
  await dialog.getByLabel("Fingerprint seed", { exact: true }).fill("42");
  await dialog.getByRole("combobox", { name: "Platform", exact: true }).selectOption("linux");
  await fitsDocument(page);
  await fitsViewport(page, dialog.getByRole("button", { name: "Close", exact: true }));
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await page.getByTitle("List view", { exact: true }).click();
  await fitsDocument(page);
  await page.getByTitle("Grid view", { exact: true }).click();

  await page.getByTitle("MCP · ⌘2", { exact: true }).click();
  await expect(page.getByText("Connect an agent", { exact: true })).toBeVisible();
  await fitsDocument(page);
  await fitsViewport(page, page.getByRole("button", { name: "Copy for LLM", exact: true }));
  await settings.click();
  await expect(page.getByRole("region", { name: "Settings", exact: true })).toBeVisible();
  await fitsDocument(page);
  await search.click();
  const input = page.getByPlaceholder("Search profiles, tags, actions…");
  await fitsViewport(page, input);
  await input.fill("Regression");
  await fitsDocument(page);
  await page.keyboard.press("Escape");
  await expect(input).toHaveCount(0);
});
