import { expect, test } from "@playwright/test";
import { createWorkflow } from "./canvasWorkflow";

for (const width of [1440, 390]) {
  test(`creating a conversation in the sidebar preserves the workbench at ${width}px`, async ({ page }) => {
    test.skip(process.env.PRODUCTFLOW_RUN_LIVE_BROWSER_GRAPH !== "1", "Requires the local live stack");
    await page.setViewportSize({ width, height: 900 });
    await page.addInitScript(() => localStorage.setItem("productflow.locale", "zh-CN"));
    const login = await page.request.post("/api/auth/session", { data: { admin_key: process.env.ADMIN_ACCESS_KEY } });
    expect(login.ok()).toBe(true);
    await createWorkflow(page);
    if (width < 1024) {
      await page.getByRole("button", { name: "展开右侧栏" }).click();
    }
    await page.locator('[data-sidebar-tool="agent"]').filter({ visible: true }).click();
    const start = page.locator("[data-open-canvas-conversation]");
    await expect(start).toBeVisible();
    const originalURL = page.url();
    const shell = await page.locator("[data-agent-workbench-shell]").elementHandle();
    const canvas = await page.locator("[data-graph-canvas-panel]").elementHandle();
    expect(shell).not.toBeNull();
    expect(canvas).not.toBeNull();
    await start.click();
    await expect(page.locator("[data-agent-composer] textarea")).toBeVisible();
    expect(await shell?.evaluate((element) => element.isConnected)).toBe(true);
    expect(await canvas?.evaluate((element) => element.isConnected)).toBe(true);
    expect(page.url()).toBe(originalURL);
    await page.screenshot({ path: `/tmp/productflow-sidebar-conversation-${width}.png` });
  });
}
