import { expect, test } from "@playwright/test";

import { LOCALES, translate } from "./fixtures/i18n";

const viewports = [
  { width: 1440, height: 900 },
  { width: 1024, height: 768 },
  { width: 390, height: 844 },
] as const;

for (const viewport of viewports) {
  for (const locale of LOCALES) {
    for (const theme of ["light", "dark"] as const) {
      test(`home ${viewport.width} ${locale} ${theme}`, async ({ page }, testInfo) => {
        await page.setViewportSize(viewport);
        await page.emulateMedia({ reducedMotion: "reduce" });
        await page.addInitScript(({ locale, theme }) => {
          localStorage.setItem("productflow.locale", locale);
          localStorage.setItem("productflow.theme", theme);
        }, { locale, theme });
        // The homepage has no business data dependency; isolate only the login gate.
        await page.route("**/api/auth/session", (route) => route.fulfill({
          json: { authenticated: true, user: { id: "home-user", email: "home@example.com", display_name: "Home", is_operator: false }, merchant: { id: "home-merchant", name: "Home merchant", status: "active" }, preferences: { locale, theme }, access_required: false },
        }));
        const errors: string[] = [];
        page.on("pageerror", (error) => errors.push(error.message));
        await page.goto("/home");
        const t = (key: Parameters<typeof translate>[1]) => translate(locale, key);
        await expect(page.getByRole("heading", { name: t("home.headline"), exact: true })).toBeVisible();
        await expect(page.locator("html")).toHaveAttribute("lang", locale);
        await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
        await expect(page.locator(".home-photo img")).toHaveCount(3);
        await expect.poll(() => page.locator(".home-photo img").evaluateAll((images) =>
          images.every((image) => image instanceof HTMLImageElement && image.complete && image.naturalWidth > 0),
        )).toBe(true);
        const geometry = await page.evaluate(() => ({
          viewport: window.innerWidth,
          client: document.documentElement.clientWidth,
          scroll: document.documentElement.scrollWidth,
          overflow: Array.from(document.querySelectorAll<HTMLElement>("main a, main button, main h1, main h2, main h3, main p")).filter((element) => {
            // The miniature canvas intentionally pans horizontally on narrow screens.
            if (element.closest(".home-real-canvas")) return element.scrollWidth > element.clientWidth + 1;
            const bounds = element.getBoundingClientRect();
            return bounds.right > document.documentElement.clientWidth + 1 || bounds.left < -1 || element.scrollWidth > element.clientWidth + 1;
          }).map((element) => element.textContent),
        }));
        expect(geometry.viewport).toBe(viewport.width);
        expect(geometry.scroll).toBeLessThanOrEqual(geometry.client);
        expect(geometry.overflow).toEqual([]);
        await expect(page.locator(".home-intro .home-create")).toHaveAttribute("href", "/products/new");
        expect(await page.locator(".home-workspace-link").evaluateAll((links) => links.map((link) => link.getAttribute("href"))))
          .toEqual(["/products", "/image-chat", "/media-library"]);

        for (const section of await page.locator(".home-story-section").all()) {
          await section.scrollIntoViewIfNeeded();
          await expect.poll(() => section.locator("img").evaluateAll((images) => images.every((image) => image instanceof HTMLImageElement && image.complete && image.naturalWidth > 0))).toBe(true);
        }
        await page.evaluate(() => window.scrollTo(0, 0));
        await page.screenshot({ path: testInfo.outputPath("home.png"), fullPage: true });
        const trigger = page.locator(".home-photo-button").first();
        await trigger.click();
        const dialog = page.getByRole("dialog");
        await expect(dialog).toBeVisible();
        expect(await page.locator("body").evaluate((body) => getComputedStyle(body).overflow)).toBe("hidden");
        await expect(dialog.locator(".home-preview-stage img")).toHaveAttribute("src", /forma-hero.webp$/);
        await dialog.getByRole("button", { name: t("home.reference"), exact: true }).click();
        await expect(dialog.locator(".home-preview-stage img")).toHaveAttribute("src", /forma-reference.webp$/);
        await expect(dialog.getByRole("button", { name: t("home.reference"), exact: true })).toHaveAttribute("aria-pressed", "true");
        await dialog.getByRole("button", { name: t("home.nextShot"), exact: true }).click();
        await expect(dialog.locator(".home-preview-stage img")).toHaveAttribute("src", /forma-scene.webp$/);
        await expect(dialog.getByRole("button", { name: t("home.result"), exact: true })).toHaveAttribute("aria-pressed", "true");
        await dialog.getByRole("button", { name: t("home.previous"), exact: true }).click();
        await dialog.getByRole("button", { name: t("home.previous"), exact: true }).click();
        await expect(dialog.locator(".home-preview-stage img")).toHaveAttribute("src", /forma-detail.webp$/);
        await page.keyboard.press("ArrowRight");
        await expect(dialog.locator(".home-preview-stage img")).toHaveAttribute("src", /forma-hero.webp$/);
        for (let index = 0; index < 8; index++) {
          await page.keyboard.press("Tab");
          expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true);
        }
        const bounds = await dialog.boundingBox();
        expect(bounds).not.toBeNull();
        expect(bounds!.y).toBeGreaterThanOrEqual(0);
        expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(viewport.height);
        await page.keyboard.press("Escape");
        await expect(dialog).not.toBeVisible();
        await expect(trigger).toBeFocused();
        const source = page.locator(".home-source-button");
        await source.click();
        await expect(dialog.locator(".home-preview-stage img")).toHaveAttribute("src", /forma-reference.webp$/);
        await page.keyboard.press("Escape");
        await expect(source).toBeFocused();
        await page.getByRole("button", { name: t("home.demo.direction"), exact: true }).click();
        await page.locator(".studio-direction-options button").nth(1).click();
        await expect(page.locator(".studio-direction-image > img")).toHaveAttribute("src", /forma-scene.webp$/);
        expect(await page.locator(".studio-story").evaluate((element) =>
          element.getAnimations({ subtree: true }).filter((animation) => animation.playState === "running" && Number(animation.effect?.getComputedTiming().activeDuration) > 20).length,
        )).toBe(0);
        await page.getByRole("button", { name: t("home.demo.input"), exact: true }).click();
        await expect(page.locator(".studio-brief")).toBeVisible();
        await page.locator(".studio-reference-mount button").click();
        await expect(dialog.locator(".home-preview-stage img")).toHaveAttribute("src", /forma-reference.webp$/);
        await page.keyboard.press("Escape");
        await page.getByRole("button", { name: t("home.demo.result"), exact: true }).click();
        await page.locator(".home-real-workbench > .home-story-options button").nth(2).click();
        await expect(page.locator('[data-demo-node="image_generation"] img')).toHaveAttribute("src", /forma-detail.webp$/);
        await page.locator(".home-edit-options button").nth(1).click();
        await expect(page.locator(".home-edit-image")).toHaveAttribute("data-area", "subject");
        await expect(page.locator(".home-edit-request")).toContainText(t("home.story.subjectRequest"));
        await page.locator(".home-reuse-tabs button").nth(1).click();
        await expect(page.locator(".home-recipe-note")).toContainText(t("home.story.recipeNote"));
        await page.locator(".home-questions summary").first().click();
        await expect(page.locator(".home-questions details").first()).toHaveAttribute("open", "");
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
        expect(errors).toEqual([]);
      });
    }
  }
}

for (const width of [1440, 390]) {
  test(`home walkthrough playback ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({ reducedMotion: "no-preference" });
    await page.route("**/api/auth/session", (route) => route.fulfill({
      json: { authenticated: true, user: { id: "home-user", email: "home@example.com", display_name: "Home", is_operator: false }, merchant: { id: "home-merchant", name: "Home merchant", status: "active" }, preferences: { locale: "zh-CN", theme: "system" }, access_required: false },
    }));
    await page.goto("/home");
    await page.locator(".studio-play").click();
    await expect(page.locator(".studio-story")).toHaveAttribute("data-step", "0");
    await expect(page.locator(".studio-reference-mount img")).toBeVisible();
    await expect.poll(() => page.locator(".studio-scan").evaluate((element) => element.getAnimations().length)).toBe(1);
    await page.locator(".studio-play").click();
    await expect(page.locator(".studio-story")).toHaveAttribute("data-playing", "false");
    await page.waitForTimeout(3000);
    await expect(page.locator(".studio-story")).toHaveAttribute("data-step", "0");
    await page.locator(".studio-play").click();
    await expect(page.locator(".studio-story")).toHaveAttribute("data-step", "1", { timeout: 4000 });
    await expect(page.locator(".studio-direction-image > img")).toBeVisible();
    await expect(page.locator(".studio-story")).toHaveAttribute("data-step", "2", { timeout: 5000 });
    await expect(page.locator(".studio-story")).toHaveAttribute("data-playing", "false", { timeout: 4000 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.locator(".home-photo-button").last().click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.locator(".home-photo-button").last()).toBeFocused();
  });
}

for (const width of [1440, 390]) {
  test(`home actual scenes and camera guide ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.route("**/api/auth/session", route => route.fulfill({ json: { authenticated: true, user: { id: "home-user", email: "home@example.com", display_name: "Home", is_operator: false }, merchant: { id: "home-merchant", name: "Home merchant", status: "active" }, preferences: { locale: "zh-CN", theme: "system" }, access_required: false } }));
    const writes: string[] = [];
    page.on("request", request => { if (["POST", "PUT", "PATCH", "DELETE"].includes(request.method())) writes.push(request.url()); });
    await page.goto("/home");
    const invitation = page.locator(".home-guide-launch");
    await expect(invitation).toBeVisible();
    const invitationBox = await invitation.boundingBox();
    expect(invitationBox!.y).toBeGreaterThanOrEqual(0);
    expect(invitationBox!.y + invitationBox!.height).toBeLessThan(840);
    await invitation.focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".home-guide")).toHaveAttribute("data-stop", "0");
    await expect(page.locator(".home-guide")).toHaveAttribute("data-phase", "active");
    await expect(page.locator(".home-guide-next")).toBeDisabled();
    await page.locator(".home-guide-skip").click();
    await expect(page.locator(".home-guide")).toHaveAttribute("data-stop", "1");
    await expect(page.locator("[data-workflow-node-id]")).toHaveCount(6);
    await page.locator(".home-guide-close").click();
    await expect(page.locator(".home-guide")).toHaveAttribute("data-phase", "exiting");
    await expect(page.locator(".home-guide")).not.toBeVisible();
    await expect(invitation).toBeFocused();
    const node = page.locator('[data-demo-node="product_source"]');
    await node.scrollIntoViewIfNeeded();
    await node.click({ trial: true });
    const start = await node.boundingBox();
    expect(start).not.toBeNull();
    const before = await node.getAttribute("style");
    const wire = page.locator(".home-node-wires path").first();
    const edgeBefore = await wire.getAttribute("d");
    await page.mouse.move(start!.x + 35, start!.y + 30);
    await page.mouse.down();
    await page.mouse.move(start!.x + 65, start!.y + 65, { steps: 8 });
    await page.mouse.up();
    expect(await node.getAttribute("style")).not.toBe(before);
    expect(await wire.getAttribute("d")).not.toBe(edgeBefore);
    await page.locator(".home-scene-footer button").click();
    await expect(node).toHaveAttribute("style", before!);
    await page.locator(".home-editor-launch").click();
    const dialog = page.locator("[data-local-edit-dialog]");
    await expect(dialog).toBeVisible();
    const canvas = page.locator("[data-local-edit-mask-canvas]");
    await canvas.scrollIntoViewIfNeeded();
    await expect(canvas).not.toHaveAttribute("aria-disabled", "true");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    await page.mouse.move(box!.x + box!.width * .25, box!.y + Math.min(80, box!.height * .2));
    await page.mouse.down();
    await page.mouse.move(box!.x + box!.width * .5, box!.y + Math.min(130, box!.height * .3), { steps: 12 });
    await page.mouse.up();
    await expect.poll(async () => Number(await page.locator("[data-local-edit-mask-summary]").getAttribute("data-edit-pixels"))).toBeGreaterThan(0);
    await page.getByRole("button", { name: translate("zh-CN", "home.scene.previewMask"), exact: true }).click();
    await expect(dialog).toContainText(translate("zh-CN", "home.scene.maskSaved"));
    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();
    await expect(page.locator(".home-editor-launch")).toBeFocused();
    expect(writes).toEqual([]);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
}

test("home tutorial completes four interactive levels", async ({ page }) => {
  await page.route("**/api/auth/session", route => route.fulfill({ json: { authenticated: true, user: { id: "home-user", email: "home@example.com", display_name: "Home", is_operator: false }, merchant: { id: "home-merchant", name: "Home merchant", status: "active" }, preferences: { locale: "zh-CN", theme: "system" }, access_required: false } }));
  await page.goto("/home");
  await page.locator(".home-guide-launch").click();
  const guide = page.locator(".home-guide");
  const next = page.locator(".home-guide-next");
  await expect(guide).toHaveAttribute("data-phase", "active");
  await expect(next).toBeDisabled();
  await page.locator(".studio-play").click();
  await expect(next).toBeEnabled({ timeout: 12_000 });
  await next.click();
  await expect(guide).toHaveAttribute("data-stop", "1");
  await page.locator(".home-scene-node-tabs button").nth(2).click();
  await expect(next).toBeEnabled();
  await next.click();
  await page.locator(".home-editor-launch").click();
  const canvas = page.locator("[data-local-edit-mask-canvas]");
  await canvas.scrollIntoViewIfNeeded();
  await expect(canvas).not.toHaveAttribute("aria-disabled", "true");
  const box = await canvas.boundingBox();
  await page.mouse.move(box!.x + 100, box!.y + 50);
  await page.mouse.down();
  await page.mouse.move(box!.x + 160, box!.y + 110, { steps: 8 });
  await page.mouse.up();
  await page.getByRole("button", { name: translate("zh-CN", "home.scene.previewMask"), exact: true }).click();
  await expect(page.locator("[data-local-edit-target-impact]")).toContainText(translate("zh-CN", "home.scene.maskSaved"));
  await page.keyboard.press("Escape");
  await expect(guide).toHaveAttribute("data-stop", "2");
  await expect(next).toBeEnabled();
  await next.click();
  await page.locator(".home-reuse-tabs button").nth(1).click();
  await expect(next).toBeEnabled();
  await expect(page.locator('.home-guide-progress [data-done="true"]')).toHaveCount(4);
  await next.click();
  await expect(guide).toContainText(translate("zh-CN", "home.guide.clear"));
  await next.click();
  await expect(guide).toHaveAttribute("data-phase", "exiting");
  await expect(guide).not.toBeVisible();
  await expect(page.locator(".home-guide-launch")).toBeFocused();
  const source = await page.locator(".home-guide-entry-camera").boundingBox();
  expect(source!.y).toBeGreaterThanOrEqual(0);
  expect(source!.y + source!.height).toBeLessThan(900);
});
