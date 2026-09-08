import { expect, test } from "@playwright/test";
import { LOCALES, translate } from "../src/lib/i18n";

test("registration stays visible when SMTP is not configured", async ({ page }) => {
  await page.route("**/api/auth/session", (route) => route.fulfill({ json: {
    authenticated: false, access_required: true, needs_bootstrap: false, registration_available: false,
  } }));
  await page.goto("/login");
  await page.getByRole("tab", { name: "注册", exact: true }).click();
  await expect(page.locator("#registration-code")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("邮件服务尚未就绪");
  await expect(page.getByRole("button", { name: "发送验证码", exact: true })).toBeDisabled();
  await expect(page.locator('button[type="submit"]')).toBeDisabled();
  await page.getByRole("tab").first().click();
  await expect(page.locator('button[type="submit"]')).toBeEnabled();
});

for (const width of [390, 1440]) {
  for (const locale of LOCALES) {
    test(`email registration ${width} ${locale}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
      await page.clock.install();
      await page.addInitScript(({ locale, theme }) => {
        localStorage.setItem("productflow.locale", locale);
        localStorage.setItem("productflow.theme", theme);
      }, { locale, theme: width === 390 ? "light" : "dark" });
      let signedIn = false;
      let sent = 0;
      let registered = 0;
      await page.route("**/api/**", (route) => route.fulfill({ json: { items: [] } }));
      await page.route("**/api/auth/session", (route) => route.fulfill({ json: {
        authenticated: signedIn, access_required: true, needs_bootstrap: false, registration_available: true,
        ...(signedIn ? { preferences: { locale: "zh-CN", theme: "system" }, user: { id: "user", email: "member@example.com", display_name: "Member", is_operator: false }, merchant: { id: "merchant", name: "Shop", status: "active" } } : {}),
      } }));
      await page.route("**/api/auth/registration-code", (route) => {
        sent++;
        return route.fulfill(sent === 1 ? { status: 503, json: { detail: "Mail unavailable" } } : { json: { challenge_id: `proof-${sent}`, retry_after_seconds: 60 } });
      });
      await page.route("**/api/auth/register", (route) => {
        registered++;
        expect(route.request().postDataJSON()).toMatchObject({ email: "member@example.com", challenge_id: "proof-2", code: "123456", merchant_name: "Shop", password: "password123" });
        if (registered === 1) return route.fulfill({ status: 400, json: { detail: "Code invalid" } });
        signedIn = true;
        return route.fulfill({ json: { ok: true, user_id: "user", merchant_id: "merchant" } });
      });
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await page.goto("/login");
      await page.getByRole("tab", { name: translate(locale, "login.register"), exact: true }).click();
      await expect(page.locator('input[autocomplete="off"]')).toHaveCount(0);
      await page.locator('input[type="email"]').fill("member@example.com");
      await page.locator('input[autocomplete="organization"]').fill("Shop");
      await page.locator('input[type="password"]').fill("password123");
      const send = page.getByRole("button", { name: translate(locale, "login.sendCode"), exact: true });
      await send.click();
      await expect(page.getByRole("alert")).toHaveText("Mail unavailable");
      await send.click();
      await expect(page.getByText(translate(locale, "login.codeSent"), { exact: true })).toBeVisible();
      await expect(page.getByRole("button", { name: translate(locale, "login.resendAfter", { seconds: 60 }), exact: true })).toBeDisabled();
      await page.locator("#registration-code").fill("123456");
      const submit = page.locator('button[type="submit"]');
      await submit.click();
      await expect(page.getByRole("alert")).toHaveText("Code invalid");
      await expect(page.locator("#registration-code")).toHaveValue("123456");
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({ path: testInfo.outputPath("registration.png"), fullPage: true });
      await submit.click();
      await expect(page).toHaveURL(/\/products/);
      expect(sent).toBe(2);
      expect(registered).toBe(2);
      expect(errors).toEqual([]);
    });
  }
}

test("changing email discards an outstanding challenge response", async ({ page }) => {
  let release: () => void = () => {};
  const ready = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/auth/session", (route) => route.fulfill({ json: { authenticated: false, access_required: true, registration_available: true } }));
  await page.route("**/api/auth/registration-code", async (route) => {
    await ready;
    await route.fulfill({ json: { challenge_id: "old-email", retry_after_seconds: 60 } });
  });
  await page.goto("/login");
  await page.getByRole("tab").last().click();
  await page.locator('input[type="email"]').fill("old@example.com");
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await page.locator('input[type="email"]').fill("new@example.com");
  const response = page.waitForResponse("**/api/auth/registration-code");
  release();
  await response;
  await expect(page.locator('button[type="submit"]')).toBeDisabled();
  await expect(page.getByText("验证码已发送，请查看邮箱。", { exact: true })).toHaveCount(0);
});
