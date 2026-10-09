import { expect, test } from "@playwright/test";
import { installWailsMock } from "./wailsMock";

async function setup(page: Parameters<typeof installWailsMock>[0]) {
  await installWailsMock(page, undefined, { section: "mcp" });
  await page.addInitScript(() => {
    const app = window.go!.main!.App!;
    const original = app.Invoke;
    let attached = false;
    const session = { debugSessionId: "debug-fixture", profileId: "fixture-profile", allowedRoot: "C:/fixture/debug", status: "attached" };
    const calls: { command: string; args: Record<string, any> }[] = [];
    Object.assign(window, { __DEBUG_CALLS__: calls });
    app.Invoke = async (command, args) => {
      if (!command.startsWith("debugger_")) return original(command, args);
      calls.push({ command, args });
      switch (command) {
        case "debugger_sessions": return { sessions: attached ? [session] : [] };
        case "debugger_attach": attached = true; return session;
        case "debugger_detach": attached = false; return { detached: true };
        case "debugger_windows": return { content: [], structuredContent: { data: { windows: [{ windowId: 7, tabs: [{ targetId: "page-fixture", title: "Fixture page", url: "https://example.com/" }] }] } } };
        case "debugger_call": return { content: [{ type: "text", text: "Selected Fixture page" }] };
        default: throw new Error(`Unhandled debug fixture: ${command}`);
      }
    };
    const originalList = app.Invoke;
    app.Invoke = async (command, args) => {
      const result = await originalList(command, args);
      return command === "profiles_list" ? result.map((p: object) => ({ ...p, isRunning: true })) : result;
    };
  });
  await page.goto("/");
}

test("attach a profile, select its page, and detach the debugger", async ({ page }) => {
  await setup(page);
  const panel = page.getByRole("region", { name: "Reverse debugging" });
  await panel.getByRole("combobox", { name: "Browser profile" }).selectOption("fixture-profile");
  await panel.getByRole("button", { name: "Attach debugger" }).click();
  await expect(panel.locator("code").filter({ hasText: "debug-fixture" })).toBeVisible();
  await panel.getByRole("button", { name: "Select Fixture page" }).click();
  await expect(panel.getByText("Selected Fixture page", { exact: true })).toBeVisible();
  await panel.getByRole("button", { name: "Detach debugger" }).click();
  await expect(panel.getByRole("button", { name: "Attach debugger" })).toBeVisible();
  const calls = await page.evaluate(() => (window as any).__DEBUG_CALLS__);
  expect(calls).toContainEqual({ command: "debugger_attach", args: { profileId: "fixture-profile" } });
  expect(calls).toContainEqual({ command: "debugger_call", args: { debugSessionId: "debug-fixture", name: "select_page", arguments: { targetId: "page-fixture" } } });
  expect(calls).toContainEqual({ command: "debugger_detach", args: { debugSessionId: "debug-fixture" } });
});

test("debugger connection failure stays actionable", async ({ page }) => {
  await setup(page);
  await page.evaluate(() => {
    const app = window.go!.main!.App!;
    const original = app.Invoke;
    app.Invoke = async (command, args) => {
      if (command === "debugger_attach") throw "Reverse runtime missing. Run prepare.";
      return original(command, args);
    };
  });
  const panel = page.getByRole("region", { name: "Reverse debugging" });
  await panel.getByRole("combobox", { name: "Browser profile" }).selectOption("fixture-profile");
  await panel.getByRole("button", { name: "Attach debugger" }).click();
  await expect(panel.getByRole("alert")).toContainText("Reverse runtime missing");
  await expect(panel.getByRole("button", { name: "Attach debugger" })).toBeEnabled();
});

test("debugger exit events clear the selected session and allow reconnection", async ({ page }) => {
  await setup(page);
  const panel = page.getByRole("region", { name: "Reverse debugging" });
  await panel.getByRole("combobox", { name: "Browser profile" }).selectOption("fixture-profile");
  await panel.getByRole("button", { name: "Attach debugger" }).click();
  await expect(panel.locator("code").filter({ hasText: "debug-fixture" })).toBeVisible();
  await page.evaluate(() => (window as any).__WAILS_MOCK__.emit("debugger:session-changed", {
    debugSessionId: "debug-fixture", profileId: "fixture-profile", status: "error", error: "Debugger process exited",
  }));
  await expect(panel.getByRole("alert")).toContainText("Debugger process exited");
  await expect(panel.getByRole("button", { name: "Attach debugger" })).toBeEnabled();
});
