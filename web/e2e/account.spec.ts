import { emptyMerchantOverview } from "./fixtures/merchantOverview";
import { expect, test, type Page } from "@playwright/test";
import { LOCALES, translate } from "./fixtures/i18n";

const user = { id: "account-user", email: "member@example.com", display_name: "Member", is_operator: false };
const merchant = { id: "account-merchant", name: "Merchant with a deliberately long name 商家名称", status: "active" };
const current = { id: "current", current: true, created_at: "2026-09-08T00:00:00Z", expires_at: "2026-10-08T00:00:00Z" };
const other = { ...current, id: "other/session", current: false };
async function baseRoutes(page: Page) {
  await page.route("**/api/**", (route) => route.fulfill({ json: { items: [], conversations: [] } }));
  await page.route("**/api/v2/products/overview?*", (route) => route.fulfill({ json: emptyMerchantOverview() }));
}
async function checkWidth(page: Page, width: number) {
  const geometry = await page.evaluate(() => ({ inner: innerWidth, client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
  expect(geometry.inner).toBe(width);
  expect(geometry.client).toBeGreaterThanOrEqual(width - 20);
  expect(geometry.scroll).toBeLessThanOrEqual(geometry.client);
}

for (const locale of LOCALES) for (const width of [390, 1440]) for (const theme of ["light", "dark"]) {
  test(`account profile and sessions ${locale} ${width} ${theme}`, async ({ page }, testInfo) => {
    const t = (key: Parameters<typeof translate>[1]) => translate(locale, key);
    await page.setViewportSize({ width, height: 960 });
    await page.addInitScript(({ locale, theme }) => { localStorage.setItem("productflow.locale", locale); localStorage.setItem("productflow.theme", theme); }, { locale, theme });
    await baseRoutes(page);
    const operator = theme === "dark";
    let authenticated = true;
    let profile = { preferences: { locale, theme }, user: { ...user, is_operator: operator }, merchant: operator ? null : merchant };
    let saves = 0;
    let revoked = false;
    let revokes = 0;
    await page.route("**/api/auth/session", (route) => route.fulfill({ json: authenticated ? { authenticated, access_required: true, ...profile } : { authenticated, access_required: true } }));
    await page.route("**/api/account", (route) => {
      if (route.request().method() === "PATCH") {
        saves++;
        if (saves === 1) return route.fulfill({ status: 503, json: { detail: "Save unavailable" } });
        profile = { ...profile, user: { ...profile.user, display_name: route.request().postDataJSON().display_name } };
      }
      return route.fulfill({ json: profile });
    });
    await page.route("**/api/account/sessions?*", (route) => {
      const after = new URL(route.request().url()).searchParams.get("after");
      return route.fulfill({ json: { items: after ? revoked ? [] : [other] : [current], next_cursor: after ? null : "page+2/=" } });
    });
    await page.route("**/api/account/sessions/*", (route) => {
      if (route.request().url().endsWith("/current")) { authenticated = false; return route.fulfill({ json: { ok: true } }); }
      revokes++;
      if (revokes === 1) return route.fulfill({ status: 503, json: { detail: "Revoke unavailable" } });
      revoked = true;
      return route.fulfill({ json: { ok: true } });
    });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto("/account");
    await expect(page.getByRole("heading", { name: t("account.title"), exact: true })).toBeVisible();
    await expect(page.getByLabel(t("login.email"), { exact: true })).toHaveAttribute("readonly", "");
    await expect(page.getByText(t("account.operator"), { exact: true })).toHaveCount(operator ? 1 : 0);
    await page.getByLabel(t("account.displayName"), { exact: true }).fill("Updated name");
    await page.locator("form").filter({ has: page.getByLabel(t("account.displayName"), { exact: true }) }).getByRole("button", { name: t("account.save"), exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText("Save unavailable");
    await expect(page.getByLabel(t("account.displayName"), { exact: true })).toHaveValue("Updated name");
    await page.locator("form").filter({ has: page.getByLabel(t("account.displayName"), { exact: true }) }).getByRole("button", { name: t("account.save"), exact: true }).click();
    await expect(page.getByRole("status")).toContainText(t("account.saved"));
    await checkWidth(page, width);
    await page.screenshot({ path: testInfo.outputPath("account.png"), fullPage: true });
    await page.getByRole("button", { name: t("account.next"), exact: true }).click();
    await page.getByRole("button", { name: t("account.revoke"), exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: t("account.revoke"), exact: true }).click();
    await expect(dialog.getByRole("alert")).toHaveText("Revoke unavailable");
    await dialog.getByRole("button", { name: t("account.revoke"), exact: true }).click();
    await expect(page.getByText(t("account.emptySessions"), { exact: true })).toBeVisible();
    await page.getByRole("button", { name: t("account.previous"), exact: true }).click();
    await page.getByRole("button", { name: t("account.revokeCurrent"), exact: true }).click();
    await dialog.getByRole("button", { name: t("account.revokeCurrent"), exact: true }).click();
    await expect(page).toHaveURL(/\/login$/);
    expect(errors).toEqual([]);
  });

  test(`password recovery ${locale} ${width} ${theme}`, async ({ page }, testInfo) => {
    const t = (key: Parameters<typeof translate>[1]) => translate(locale, key);
    await page.setViewportSize({ width, height: 960 });
    await page.clock.install();
    await page.addInitScript(({ locale, theme }) => { localStorage.setItem("productflow.locale", locale); localStorage.setItem("productflow.theme", theme); }, { locale, theme });
    await baseRoutes(page);
    await page.route("**/api/auth/session", (route) => route.fulfill({ json: { authenticated: false, access_required: true } }));
    let sends = 0;
    let confirmations = 0;
    await page.route("**/api/auth/password-recovery/request", (route) => {
      sends++;
      if (sends === 1) return route.fulfill({ status: 503, json: { detail: "Mail unavailable" } });
      return route.fulfill({ status: 202, json: { challenge_id: `proof-${sends}`, expires_in_seconds: 600, resend_after_seconds: 60 } });
    });
    await page.route("**/api/auth/password-recovery/confirm", (route) => {
      confirmations++;
      if (confirmations === 1) return route.fulfill({ status: 400, json: { detail: "验证码无效或已过期，请重新申请", code: "invalid_recovery_code" } });
      expect(route.request().postDataJSON()).toEqual({ email: "member@example.com", challenge_id: "proof-3", code: "123456", new_password: "new-password123" });
      return route.fulfill({ json: { ok: true } });
    });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto("/login");
    await page.getByRole("link", { name: t("recovery.title"), exact: true }).click();
    await expect(page.getByRole("heading", { name: t("recovery.title"), exact: true })).toBeVisible();
    await page.getByLabel(t("login.email"), { exact: true }).fill("member@example.com");
    await page.getByRole("button", { name: t("recovery.request"), exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText("Mail unavailable");
    await page.getByRole("button", { name: t("recovery.request"), exact: true }).click();
    await expect(page.getByRole("status")).toHaveText(t("recovery.accepted"));
    await page.getByLabel(t("login.verificationCode"), { exact: true }).fill("000000");
    await page.getByLabel(t("account.newPassword"), { exact: true }).fill("new-password123");
    await page.getByLabel(t("account.confirmPassword"), { exact: true }).fill("new-password123");
    await page.getByRole("button", { name: t("recovery.confirm"), exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText(t("recovery.invalidCode"));
    await checkWidth(page, width);
    await page.screenshot({ path: testInfo.outputPath("recovery.png"), fullPage: true });
    await page.clock.fastForward(61_000);
    await page.getByRole("button", { name: t("recovery.resend"), exact: true }).click();
    await page.getByLabel(t("login.verificationCode"), { exact: true }).fill("123456");
    await page.getByRole("button", { name: t("recovery.confirm"), exact: true }).click();
    await expect(page.getByRole("status")).toHaveText(t("recovery.success"));
    await page.getByRole("link", { name: t("recovery.back"), exact: true }).last().click();
    await expect(page).toHaveURL(/\/login$/);
    expect(errors).toEqual([]);
  });
}

test("password validation, rejected current password, and successful sign-out", async ({ page }) => {
  await baseRoutes(page);
  let authenticated = true;
  let changes = 0;
  await page.route("**/api/auth/session", (route) => route.fulfill({ json: authenticated ? { authenticated, preferences: { locale: "zh-CN", theme: "system" }, access_required: true, user, merchant } : { authenticated, access_required: true } }));
  await page.route("**/api/account", (route) => route.fulfill({ json: { user, merchant, preferences: { locale: "zh-CN", theme: "system" } } }));
  await page.route("**/api/account/sessions?*", (route) => route.fulfill({ json: { items: [current], next_cursor: null } }));
  await page.route("**/api/account/password", (route) => {
    changes++;
    if (changes === 1) return route.fulfill({ status: 400, json: { detail: "Wrong current password" } });
    authenticated = false;
    return route.fulfill({ json: { ok: true } });
  });
  await page.goto("/account");
  await page.getByLabel("当前密码", { exact: true }).fill("current-password");
  await page.getByLabel("新密码", { exact: true }).fill("new-password123");
  await page.getByLabel("确认新密码", { exact: true }).fill("different-password");
  await page.getByRole("button", { name: "修改密码并退出", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("两次输入的新密码不一致。");
  expect(changes).toBe(0);
  await page.getByLabel("确认新密码", { exact: true }).fill("new-password123");
  await page.getByRole("button", { name: "修改密码并退出", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("Wrong current password");
  await page.getByLabel("当前密码", { exact: true }).fill("correct-password");
  await page.getByLabel("确认新密码", { exact: true }).press("Enter");
  await expect(page).toHaveURL(/\/login$/);
});

test("recoverable loading and read errors, empty sessions, keyboard focus and expired code", async ({ page }) => {
  await baseRoutes(page);
  await page.route("**/api/auth/session", (route) => route.fulfill({ json: { authenticated: true, preferences: { locale: "zh-CN", theme: "system" }, access_required: true, user, merchant } }));
  let release: () => void = () => {};
  const wait = new Promise<void>((resolve) => { release = resolve; });
  let reads = 0;
  await page.route("**/api/account", async (route) => { reads++; if (reads === 1) { await wait; return route.fulfill({ status: 503, json: { detail: "Profile unavailable" } }); } return route.fulfill({ json: { user, merchant, preferences: { locale: "zh-CN", theme: "system" } } }); });
  let sessionReads = 0;
  await page.route("**/api/account/sessions?*", (route) => { sessionReads++; return route.fulfill(sessionReads === 1 ? { status: 503, json: { detail: "Sessions unavailable" } } : { json: { items: [], next_cursor: null } }); });
  await page.goto("/account");
  await expect(page.getByRole("region", { name: "个人资料", exact: true }).getByRole("status")).toHaveText("加载中");
  release();
  await expect(page.getByRole("alert").filter({ hasText: "Profile unavailable" })).toBeVisible();
  await page.getByRole("region", { name: "个人资料", exact: true }).getByRole("button", { name: "重试", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Sessions unavailable" })).toBeVisible();
  await page.getByRole("region", { name: "有效登录会话", exact: true }).getByRole("button", { name: "重试", exact: true }).click();
  await expect(page.getByLabel("显示名称", { exact: true })).toHaveValue("Member");
  await expect(page.getByText("暂无有效会话", { exact: true })).toBeVisible();
  await page.getByLabel("显示名称", { exact: true }).focus();
  await expect(page.getByLabel("显示名称", { exact: true })).toBeFocused();
  await page.getByLabel("显示名称", { exact: true }).fill(" ");
  await page.getByLabel("显示名称", { exact: true }).press("Enter");
  await expect(page.getByRole("alert")).toHaveText("请输入 1 至 160 个字符的显示名称。");
  await page.clock.install();
  await page.route("**/api/auth/password-recovery/request", (route) => route.fulfill({ status: 202, json: { challenge_id: "proof", expires_in_seconds: 600, resend_after_seconds: 60 } }));
  await page.goto("/password-recovery");
  await page.getByLabel("邮箱", { exact: true }).fill("unknown@example.com");
  await page.getByRole("button", { name: "请求验证码", exact: true }).click();
  await page.clock.fastForward(601_000);
  await expect(page.getByText("验证码已过期，请重新请求。", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "重设密码", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "重新请求", exact: true })).toBeEnabled();
});

test.describe("touch account navigation", () => {
  test.use({ hasTouch: true, viewport: { width: 390, height: 844 }, contextOptions: { reducedMotion: "reduce" } });
  test("opens account, cancels session revocation with keyboard focus restored, and retries logout", async ({ page }) => {
    await baseRoutes(page);
    let authenticated = true;
    let logouts = 0;
    await page.route("**/api/auth/session", (route) => {
      if (route.request().method() === "DELETE") {
        logouts++;
        if (logouts === 1) return route.fulfill({ status: 503, json: { detail: "Logout unavailable" } });
        authenticated = false;
        return route.fulfill({ json: { ok: true } });
      }
      return route.fulfill({ json: authenticated ? { authenticated, preferences: { locale: "zh-CN", theme: "system" }, access_required: true, user, merchant } : { authenticated, access_required: true } });
    });
    await page.route("**/api/account", (route) => route.fulfill({ json: { user, merchant, preferences: { locale: "zh-CN", theme: "system" } } }));
    await page.route("**/api/account/sessions?*", (route) => route.fulfill({ json: { items: [current], next_cursor: null } }));
    await page.goto("/help");
    await page.getByRole("link", { name: "个人账户", exact: true }).last().tap();
    await expect(page.getByRole("heading", { name: "个人账户", exact: true })).toBeVisible();
    const revoke = page.getByRole("button", { name: "退出当前会话", exact: true });
    await revoke.tap();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).not.toBeVisible();
    await expect(revoke).toBeFocused();
    await page.getByRole("button", { name: "退出登录", exact: true }).tap();
    await expect(page.getByRole("alert")).toHaveText("Logout unavailable");
    await page.getByRole("button", { name: "退出登录", exact: true }).tap();
    await expect(page).toHaveURL(/\/login$/);
    await page.goto("/account");
    await expect(page).toHaveURL(/\/login$/);
  });
});
