import { expect, test } from "@playwright/test";

import { LOCALES, translate } from "../src/lib/i18n";

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
          json: { authenticated: true, access_required: false },
        }));
        const errors: string[] = [];
        page.on("pageerror", (error) => errors.push(error.message));
        await page.goto("/home");
        const t = (key: Parameters<typeof translate>[1]) => translate(locale, key);
        await expect(page.getByRole("heading", { name: "ProductFlow.", exact: true })).toBeVisible();
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
        expect(errors).toEqual([]);
      });
    }
  }
}
