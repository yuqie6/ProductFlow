import { expect, test } from "@playwright/test";

import { LOCALES, translate } from "../src/lib/i18n";

for (const width of [390, 1440]) {
  for (const locale of LOCALES) {
    for (const theme of ["light", "dark"] as const) {
      test(`login recovery ${width} ${locale} ${theme}`, async ({ page }, testInfo) => {
        await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
        await page.clock.install();
        await page.addInitScript(({ locale, theme }) => {
          localStorage.setItem("productflow.locale", locale);
          localStorage.setItem("productflow.theme", theme);
        }, { locale, theme });
        let attempts = 0;
        await page.route("**/api/auth/session", (route) => {
          if (route.request().method() === "GET") {
            return route.fulfill({ json: { authenticated: false, access_required: true, needs_bootstrap: false } });
          }
          attempts++;
          return route.fulfill(attempts === 2
            ? { status: 429, headers: { "Retry-After": "3" }, json: { detail: "Too many attempts" } }
            : { status: 503, json: { detail: "Temporarily unavailable" } });
        });
        const errors: string[] = [];
        page.on("pageerror", (error) => errors.push(error.message));
        await page.goto("/login");
        await page.locator('input[type="email"]').fill("operator@example.com");
        await page.locator('input[type="password"]').fill("incorrect-password");
        const submit = page.getByRole("button", { name: translate(locale, "login.submit"), exact: true });
        await submit.click();
        await expect(page.getByRole("alert")).toHaveText("Temporarily unavailable");
        await expect(submit).toBeEnabled();
        await submit.click();
        await expect(page.getByRole("alert")).toHaveText("Too many attempts");
        await expect(submit).toBeDisabled();
        await expect(page.getByRole("status")).toHaveText(translate(locale, "login.retryAfter", { seconds: 3 }));
        await page.locator('input[type="password"]').press("Enter");
        expect(attempts).toBe(2);
        const geometry = await page.evaluate(() => ({
          width: innerWidth,
          client: document.documentElement.clientWidth,
          scroll: document.documentElement.scrollWidth,
          overflow: Array.from(document.querySelectorAll<HTMLElement>("form button, form p, [role=alert]")).some((el) =>
            el.scrollWidth > el.clientWidth + 1 || el.getBoundingClientRect().right > innerWidth + 1),
        }));
        expect(geometry.width).toBe(width);
        expect(geometry.scroll).toBeLessThanOrEqual(geometry.client);
        expect(geometry.overflow).toBe(false);
        await page.screenshot({ path: testInfo.outputPath("login-cooldown.png"), fullPage: true });
        await page.clock.fastForward(3100);
        await expect(submit).toBeEnabled();
        await expect(page.getByRole("status")).toHaveCount(0);
        await submit.click();
        await expect(page.getByRole("alert")).toHaveText("Temporarily unavailable");
        expect(attempts).toBe(3);
        await expect(page.locator('input[type="email"]')).toHaveValue("operator@example.com");
        expect(errors).toEqual([]);
      });
    }
  }
}

test("bootstrap obeys the same cooldown", async ({ page }) => {
  await page.clock.install();
  await page.route("**/api/auth/session", (route) => route.fulfill({
    json: { authenticated: false, access_required: true, needs_bootstrap: true },
  }));
  let attempts = 0;
  await page.route("**/api/auth/bootstrap", (route) => {
    attempts++;
    return route.fulfill({ status: 429, headers: { "Retry-After": "2" }, json: { detail: "Too many attempts" } });
  });
  await page.goto("/login");
  await page.locator('input[type="email"]').fill("new@example.com");
  await page.locator('input[autocomplete="organization"]').fill("Test merchant");
  await page.locator('input[type="password"]').first().fill("test-admin-key");
  await page.locator('input[type="password"]').last().fill("test-password");
  const submit = page.locator('button[type="submit"]');
  await submit.click();
  await expect(submit).toBeDisabled();
  await page.locator('input[type="email"]').press("Enter");
  expect(attempts).toBe(1);
  await page.clock.fastForward(2100);
  await expect(submit).toBeEnabled();
  await submit.click();
  await expect.poll(() => attempts).toBe(2);
});
