import { expect, test } from "@playwright/test";

// Isolate the route handoff from providers and the canvas; no server writes.
for (const failFirst of [false, true]) {
  test(`Agent creation retains the form until the workbench is ready (retry=${failFirst})`, async ({ page }) => {
    let releaseBootstrap!: () => void;
    const bootstrapGate = new Promise<void>((resolve) => { releaseBootstrap = resolve; });
    let releaseModule!: () => void;
    const moduleGate = new Promise<void>((resolve) => { releaseModule = resolve; });
    let drafts = 0;
    let bootstrapRequests = 0;
    let moduleRequested = false;
    const documents: string[] = [];
    const product = { id: "transition-product", name: "Transition product" };
    const conversation = { id: "transition-conversation", session_id: "transition-session" };
    page.on("request", (request) => {
      if (request.resourceType() === "document") documents.push(request.url());
    });
    await page.addInitScript(() => localStorage.setItem("productflow.locale", "zh-CN"));
    await page.route("**/ProductWorkbenchSurface.tsx*", async (route) => {
      moduleRequested = true;
      await moduleGate;
      await route.fulfill({
        contentType: "application/javascript",
        body: 'import React from "/node_modules/.vite/deps/react.js"; export function ProductWorkbenchSurface() { return React.createElement("main", {"data-transition-ready": true}, "Workbench ready"); }',
      });
    });
    await page.route("**/api/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === "/api/auth/session") {
        await route.fulfill({ json: { authenticated: true } });
      } else if (path.endsWith("/drafts")) {
        drafts += 1;
        await route.fulfill({ json: { product, conversation, task_id: "transition-task", intake_finalized: false } });
      } else if (path.endsWith("/agent-workbench")) {
        bootstrapRequests += 1;
        await bootstrapGate;
        if (failFirst && bootstrapRequests === 1) {
          await route.fulfill({ status: 503, json: { detail: "Transition bootstrap failed" } });
        } else {
          await route.fulfill({ json: { mode: "agent", product, conversation, graph: null, latest_workflow_revision: 0 } });
        }
      } else {
        await route.fulfill({ status: 503, json: { detail: "Unrelated API disabled in transition test" } });
      }
    });
    try {
      await page.setViewportSize(failFirst ? { width: 390, height: 844 } : { width: 1440, height: 900 });
      await page.goto("/products/new");
      const name = page.locator("#agent-product-name");
      await name.fill(product.name);
      await page.evaluate(() => {
        const observer = new MutationObserver(() => {
          if (document.querySelector(".min-h-screen > svg.animate-spin")) {
            document.documentElement.dataset.transitionLoadingFlash = "true";
          }
        });
        observer.observe(document.body, { childList: true, subtree: true });
      });
      await page.getByRole("button", { name: "开始对话" }).click();
      await expect.poll(() => bootstrapRequests).toBe(1);
      await expect(name).toBeVisible();
      await expect(page.locator('button[type="submit"]')).toBeDisabled();
      await expect(page).toHaveURL(/\/products\/new$/);
      await expect.poll(() => moduleRequested).toBe(true);
      releaseBootstrap();
      if (failFirst) {
        await expect(page.getByText("Transition bootstrap failed", { exact: true })).toBeVisible();
        await page.getByRole("button", { name: "开始对话" }).click();
        await expect.poll(() => bootstrapRequests).toBe(2);
      }
      await expect(name).toBeVisible();
      await expect(page.locator('button[type="submit"]')).toBeDisabled();
      await expect(page).toHaveURL(/\/products\/new$/);
      releaseModule();
      await expect(page.locator("[data-transition-ready]")).toBeVisible();
      await expect(page).toHaveURL(/agent_session_id=transition-session&agent_task_id=transition-task/);
      expect(drafts).toBe(1);
      expect(documents).toHaveLength(1);
      await expect(page.locator("html")).not.toHaveAttribute("data-transition-loading-flash", "true");
    } finally {
      releaseBootstrap();
      releaseModule();
    }
  });
}
