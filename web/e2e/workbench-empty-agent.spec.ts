import { expect, test, type Page } from "@playwright/test";

import {
  assertLiveBrowserGraphEnabled,
  loginAsAdmin,
  requiredEnv,
} from "./liveGraph";

const PRESETS = [
  { name: "1440-light", width: 1440, height: 900, theme: "light" },
  { name: "1024-dark", width: 1024, height: 768, theme: "dark" },
  { name: "390-light", width: 390, height: 844, theme: "light" },
] as const;

function collectBrowserFailures(page: Page) {
  const failures: string[] = [];
  const currentGraphRequests: string[] = [];
  page.on("pageerror", (error) => failures.push(error.message));
  page.on("console", (message) => {
    if (message.type() !== "error") return;
    const text = message.text();
    if (text.includes("favicon") || text.includes("Download the React DevTools")) return;
    failures.push(text);
  });
  page.on("request", (request) => {
    if (/\/workflows\/current(?:\?|$)/.test(request.url())) {
      currentGraphRequests.push(request.url());
    }
  });
  return () => {
    expect(failures, failures.join("\n")).toEqual([]);
    expect(currentGraphRequests, currentGraphRequests.join("\n")).toEqual([]);
  };
}

for (const preset of PRESETS) {
  test(`empty Agent workbench opens one visible composer at ${preset.name}`, async ({ page }) => {
    assertLiveBrowserGraphEnabled();
    await page.setViewportSize({ width: preset.width, height: preset.height });
    await page.addInitScript((theme) => {
      window.localStorage.setItem("productflow.locale", "zh-CN");
      window.localStorage.setItem("productflow.theme", theme);
    }, preset.theme);
    await page.emulateMedia({ colorScheme: preset.theme, reducedMotion: "reduce" });
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));

    const created = await page.request.post("/api/v2/agent-product-workspaces/drafts", {
      headers: { "Idempotency-Key": `e2e-empty-workbench-${preset.name}-${Date.now()}` },
      data: { name: `e2e-empty-workbench ${preset.name} ${Date.now()}` },
    });
    expect(created.ok(), await created.text()).toBeTruthy();
    const snapshot = await created.json() as { product: { id: string } };

    const assertClean = collectBrowserFailures(page);
    await page.goto(`/products/${encodeURIComponent(snapshot.product.id)}`);
    const hero = page.locator("[data-workflow-onboarding-hero]");
    await expect(hero).toBeVisible();
    await expect(page.locator("[data-global-agent-launcher]")).toHaveCount(0);

    const canvas = page.locator("[data-agent-workbench-canvas-slot]");
    const initialGeometry = await canvas.evaluate((element) => ({
      width: element.clientWidth,
      height: element.clientHeight,
      inert: element.inert,
    }));
    expect(initialGeometry.width).toBeGreaterThan(0);
    expect(initialGeometry.height).toBeGreaterThan(0);
    expect(initialGeometry.inert).toBe(false);

    await hero.getByRole("button", { name: "打开对话" }).click();
    const composer = page.locator("[data-agent-composer] textarea");
    await expect(composer).toBeVisible();
    await expect(composer).toBeFocused();
    const composerGeometry = await composer.evaluate((element) => ({
      width: element.clientWidth,
      height: element.clientHeight,
      inertAncestor: Boolean(element.closest("[inert]")),
    }));
    expect(composerGeometry.width).toBeGreaterThan(0);
    expect(composerGeometry.height).toBeGreaterThan(0);
    expect(composerGeometry.inertAncestor).toBe(false);

    const inspector = page.locator("[data-product-workbench-inspector]");
    await expect(inspector).toBeVisible();
    expect(await inspector.getAttribute("inert")).toBeNull();
    await expect(page.locator("[data-global-agent-launcher]")).toHaveCount(0);
    await page.screenshot({ path: `/tmp/productflow-empty-workbench-${preset.name}.png` });
    assertClean();
  });
}

test("starts an Agent workspace while image type options are still loading", async ({ page }) => {
  assertLiveBrowserGraphEnabled();
  await page.setViewportSize({ width: 1024, height: 768 });
  await page.addInitScript(() => {
    window.localStorage.setItem("productflow.locale", "zh-CN");
    window.localStorage.setItem("productflow.theme", "light");
  });
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));

  let releaseOptions!: () => void;
  let markOptionsRequested!: () => void;
  const optionsBlocked = new Promise<void>((resolve) => {
    releaseOptions = resolve;
  });
  const optionsRequested = new Promise<void>((resolve) => {
    markOptionsRequested = resolve;
  });
  await page.route("**/api/v2/agent-product-workspaces/options", async (route) => {
    markOptionsRequested();
    await optionsBlocked;
    await route.continue();
  });

  try {
    const assertClean = collectBrowserFailures(page);
    await page.goto("/products/new");
    await optionsRequested;
    await page.locator("#agent-product-name").fill(`e2e-options-race ${Date.now()}`);
    const submit = page.getByRole("button", { name: "开始对话" });
    await expect(submit).toBeEnabled();
    await Promise.all([
      page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/, { timeout: 30_000 }),
      submit.click(),
    ]);
    await expect(page.locator("[data-workflow-onboarding-hero]")).toBeVisible();
    assertClean();
  } finally {
    releaseOptions();
  }
});
