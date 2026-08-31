import { expect, test, type Page } from "@playwright/test";

import {
  REFERENCE_PRODUCT_IMAGE,
  assertLiveBrowserGraphEnabled,
  requiredEnv,
  selectCreateImageType,
} from "./liveGraph";

type Locale = "zh-CN" | "en-US" | "ja-JP" | "vi-VN";
type Theme = "light" | "dark";

const LOCALES: readonly Locale[] = ["zh-CN", "en-US", "ja-JP", "vi-VN"];
const PRESETS = [
  { name: "1440-light", width: 1440, height: 900, theme: "light" },
  { name: "1440-dark", width: 1440, height: 900, theme: "dark" },
  { name: "1024-light", width: 1024, height: 768, theme: "light" },
  { name: "1024-dark", width: 1024, height: 768, theme: "dark" },
  { name: "390-light", width: 390, height: 844, theme: "light" },
  { name: "390-dark", width: 390, height: 844, theme: "dark" },
] as const satisfies ReadonlyArray<{
  name: string;
  width: number;
  height: number;
  theme: Theme;
}>;

async function login(page: Page): Promise<void> {
  await page.goto("/login");
  const submit = page.locator('button[type="submit"]');
  await Promise.race([
    page.waitForURL("**/products"),
    submit.waitFor({ state: "visible" }),
  ]);
  if (new URL(page.url()).pathname !== "/login") return;
  await page.locator('input[type="password"]').fill(requiredEnv("ADMIN_ACCESS_KEY"));
  await Promise.all([
    page.waitForURL("**/products"),
    submit.click(),
  ]);
}

async function createNoCostWorkbench(page: Page, name: string): Promise<void> {
  await page.goto("/products/new");
  await page.locator("#agent-product-name").fill(name);
  await page.locator("#agent-product-brief").fill("Workbench visual acceptance matrix.");
  await selectCreateImageType(page, "detail");
  const shotCount = (page.viewportSize()?.width ?? 1440) <= 390 ? "4" : "1";
  await page.locator('[data-image-type="detail"] input[type="number"]').fill(shotCount);
  await page.locator("[data-agent-product-intake-form] input[type='file']").setInputFiles(
    REFERENCE_PRODUCT_IMAGE,
  );
  await expect(page.locator('[data-create-reference-count="1"]')).toBeVisible();
  const directCreate = page.locator("[data-create-direct]");
  await expect(directCreate).toBeEnabled();
  await Promise.all([
    page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/, { timeout: 60_000 }),
    directCreate.click(),
  ]);
  await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
}

async function assertViewportGeometry(page: Page): Promise<void> {
  if ((page.viewportSize()?.width ?? 1440) <= 1023) {
    await expect(page.locator("[data-product-workbench-inspector]")).not.toBeVisible();
    await expect(page.locator("[data-product-workbench-drawer-handle]")).toBeVisible();
  }
  await expect(page.locator(".react-flow__viewport")).toBeVisible();
  await expect(page.locator(".react-flow__node").first()).toBeVisible();

  const geometry = await page.evaluate(() => {
    const viewportWidth = document.documentElement.clientWidth;
    const viewportHeight = document.documentElement.clientHeight;
    const canvas = document.querySelector<HTMLElement>("[data-graph-canvas-panel]");
    const canvasRect = canvas?.getBoundingClientRect();
    const inspectorRect = document.querySelector<HTMLElement>("[data-product-workbench-inspector]")?.getBoundingClientRect();
    const filmstripRect = document.querySelector<HTMLElement>("[data-graph-shot-filmstrip]")?.getBoundingClientRect();
    const drawerHandleRect = document.querySelector<HTMLElement>("[data-product-workbench-drawer-handle]")?.getBoundingClientRect();
    const mobileModeTabsRect = document.querySelector<HTMLElement>("[data-workflow-canvas-mobile-mode-tabs]")?.getBoundingClientRect();
    const controlsRect = document.querySelector<HTMLElement>(".workflow-canvas-controls")?.getBoundingClientRect();
    const minimapRect = document.querySelector<HTMLElement>(".workflow-canvas-minimap")?.getBoundingClientRect();
    return {
      viewportWidth,
      viewportHeight,
      documentWidth: document.documentElement.scrollWidth,
      bodyWidth: document.body.scrollWidth,
      canvasWidth: canvasRect?.width ?? 0,
      canvasHeight: canvasRect?.height ?? 0,
      canvasLeft: canvasRect?.left ?? -1,
      canvasRight: canvasRect?.right ?? viewportWidth + 1,
      canvasTop: canvasRect?.top ?? -1,
      canvasBottom: canvasRect?.bottom ?? viewportHeight + 1,
      inspectorLeft: inspectorRect?.left ?? null,
      filmstrip: filmstripRect ? { top: filmstripRect.top, bottom: filmstripRect.bottom } : null,
      drawerHandle: drawerHandleRect ? { top: drawerHandleRect.top, bottom: drawerHandleRect.bottom } : null,
      mobileModeTabs: mobileModeTabsRect ? { top: mobileModeTabsRect.top, bottom: mobileModeTabsRect.bottom } : null,
      controls: controlsRect ? { top: controlsRect.top, bottom: controlsRect.bottom } : null,
      minimap: minimapRect && minimapRect.width > 0 ? { top: minimapRect.top, bottom: minimapRect.bottom } : null,
    };
  });
  expect(geometry.documentWidth).toBeLessThanOrEqual(geometry.viewportWidth + 1);
  expect(geometry.bodyWidth).toBeLessThanOrEqual(geometry.viewportWidth + 1);
  expect(geometry.canvasWidth).toBeGreaterThan(200);
  expect(geometry.canvasHeight).toBeGreaterThan(240);
  expect(geometry.canvasLeft).toBeGreaterThanOrEqual(0);
  expect(geometry.canvasRight).toBeLessThanOrEqual(geometry.viewportWidth + 1);
  expect(geometry.canvasTop).toBeGreaterThanOrEqual(0);
  expect(geometry.canvasBottom).toBeLessThanOrEqual(geometry.viewportHeight + 1);
  if (geometry.viewportWidth >= 1024 && geometry.inspectorLeft !== null) {
    expect(Math.abs(geometry.inspectorLeft - geometry.canvasRight)).toBeLessThanOrEqual(1);
  }
  if (geometry.filmstrip && geometry.drawerHandle) {
    expect(
      geometry.drawerHandle.bottom <= geometry.filmstrip.top
      || geometry.filmstrip.bottom <= geometry.drawerHandle.top,
    ).toBe(true);
  }
  if (geometry.filmstrip && geometry.mobileModeTabs) {
    expect(geometry.mobileModeTabs.bottom).toBeLessThanOrEqual(geometry.filmstrip.top);
  }
  if (geometry.controls && geometry.mobileModeTabs) {
    expect(geometry.mobileModeTabs.bottom).toBeLessThanOrEqual(geometry.controls.top);
  }
  if (geometry.filmstrip && geometry.minimap) {
    expect(geometry.minimap.bottom).toBeLessThanOrEqual(geometry.filmstrip.top);
  }
}

async function assertFilmstripFocus(page: Page): Promise<void> {
  const shots = page.locator("[data-graph-shot-id]");
  const targetShot = shots.last();
  const primaryNodeId = await targetShot.getAttribute("data-graph-shot-primary-node-id");
  const focus = targetShot.locator("[data-graph-shot-focus]");
  if (await focus.count() === 0) return;
  if (await shots.count() > 1 && primaryNodeId) {
    const scroller = page.locator("[data-graph-shot-filmstrip-scroll]");
    await page.locator(`[data-workflow-node-id="${primaryNodeId}"]`).click();
    await expect(focus).toHaveAttribute("aria-pressed", "true");
    await expect(focus).toBeVisible();
    await expect.poll(async () => scroller.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0);
  }
  if (!await focus.isVisible()) return;
  await focus.click();
  await expect(focus).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("body")).not.toContainText("docs/ARCHITECTURE.md");
  await expect(page.locator("body")).not.toContainText("2026-08-24");
  await expect(page.locator("body")).not.toContainText("内置模板仅提供便捷默认值");
  const selectedNode = page.locator(".react-flow__node.selected").first();
  await expect(selectedNode).toBeVisible();
  const geometry = await page.evaluate(() => {
    const node = document.querySelector<HTMLElement>(".react-flow__node.selected")?.getBoundingClientRect();
    const filmstrip = document.querySelector<HTMLElement>("[data-graph-shot-filmstrip]")?.getBoundingClientRect();
    return node && filmstrip ? { nodeBottom: node.bottom, filmstripTop: filmstrip.top } : null;
  });
  expect(geometry).not.toBeNull();
  expect(geometry!.nodeBottom).toBeLessThanOrEqual(geometry!.filmstripTop);
}

for (const locale of LOCALES) {
  for (const preset of PRESETS) {
    test(`${locale} ${preset.name} keeps the workbench visible and bounded`, async ({ page }) => {
      assertLiveBrowserGraphEnabled();
      await page.setViewportSize({ width: preset.width, height: preset.height });
      await page.emulateMedia({ colorScheme: preset.theme, reducedMotion: "reduce" });
      await page.addInitScript(
        ({ nextLocale, nextTheme }: { nextLocale: Locale; nextTheme: Theme }) => {
          window.localStorage.setItem("productflow.locale", nextLocale);
          window.localStorage.setItem("productflow.theme", nextTheme);
        },
        { nextLocale: locale, nextTheme: preset.theme },
      );

      await login(page);
      await createNoCostWorkbench(page, `visual-matrix ${locale} ${preset.name} ${Date.now()}`);
      await expect(page.locator("html")).toHaveAttribute("data-theme", preset.theme);
      await expect(page.locator("html")).toHaveAttribute("lang", locale);
      await assertViewportGeometry(page);
      await page.screenshot({
        path: `/tmp/productflow-workbench-matrix-${locale}-${preset.name}.png`,
        fullPage: false,
      });
      await assertFilmstripFocus(page);
      await page.screenshot({
        path: `/tmp/productflow-workbench-matrix-${locale}-${preset.name}-focused.png`,
        fullPage: false,
      });
    });
  }
}
