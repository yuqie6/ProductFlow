import { emptyMerchantOverview } from "./fixtures/merchantOverview";
import { expect, test, type Page } from "@playwright/test";
import { accountPreferencesMessage } from "../src/lib/accountPreferencesMessages";
import { LOCALES, LOCALE_LABEL_KEYS, translate } from "./fixtures/i18n";
import type { AccountPreferences, AccountProfile } from "../src/lib/types";

const profile = (id: string, preferences: AccountPreferences, status = "active"): AccountProfile => ({
  user: { id, email: `${id}@example.com`, display_name: id, is_operator: false },
  merchant: { id: `${id}-merchant`, name: `${id} shop`, status }, preferences,
});
async function mockPreferences(page: Page, initial = profile("alice", { locale: "en-US", theme: "dark" })) {
  const state = { current: initial.user.id as string | null, accounts: { [initial.user.id]: initial, bob: profile("bob", { locale: "ja-JP", theme: "light" }) }, writes: [] as Array<{ user: string; input: Partial<AccountPreferences> }>, merchantWrites: [] as string[], failPreferences: 0, failMerchant: 0 };
  await page.route("**/api/**", (route) => route.fulfill({ json: { items: [], conversations: [], total: 0, page: 1, page_size: 20 } }));
  await page.route("**/api/v2/products/overview?*", (route) => route.fulfill({ json: emptyMerchantOverview() }));
  await page.route("**/api/auth/session", (route) => {
    if (route.request().method() === "DELETE") { state.current = null; return route.fulfill({ json: { ok: true } }); }
    if (route.request().method() === "POST") { state.current = route.request().postDataJSON().email.split("@")[0]; return route.fulfill({ json: { ok: true } }); }
    return route.fulfill({ json: state.current ? { authenticated: true, access_required: true, ...state.accounts[state.current] } : { authenticated: false, access_required: true } });
  });
  await page.route("**/api/account", (route) => {
    if (!state.current) return route.fulfill({ status: 401, json: { detail: "Sign in required" } });
    const current = state.accounts[state.current];
    if (route.request().method() === "PATCH") current.user = { ...current.user, display_name: route.request().postDataJSON().display_name };
    return route.fulfill({ json: current });
  });
  await page.route("**/api/account/preferences", (route) => {
    if (!state.current) return route.fulfill({ status: 401, json: { detail: "Sign in required" } });
    const input = route.request().postDataJSON();
    state.writes.push({ user: state.current, input });
    if (state.failPreferences-- > 0) return route.fulfill({ status: 503, json: { detail: "Preferences unavailable" } });
    const current = state.accounts[state.current];
    current.preferences = { ...current.preferences, ...input };
    return route.fulfill({ json: current.preferences });
  });
  await page.route("**/api/account/merchant", (route) => {
    const current = state.current && state.accounts[state.current];
    if (!current || !current.merchant || current.merchant.status === "suspended") return route.fulfill({ status: 403, json: { detail: "Merchant unavailable" } });
    const name = route.request().postDataJSON().name;
    state.merchantWrites.push(name);
    if (state.failMerchant-- > 0) return route.fulfill({ status: 503, json: { detail: "Name unavailable" } });
    current.merchant = { ...current.merchant, name };
    return route.fulfill({ json: current.merchant });
  });
  await page.route("**/api/account/sessions?*", (route) => route.fulfill({ json: { items: [], next_cursor: null } }));
  return state;
}

for (const locale of LOCALES) for (const width of [390, 1440]) for (const theme of ["light", "dark"] as const) {
  test(`account preferences ${locale} ${width} ${theme}`, async ({ page }, testInfo) => {
    const t = (key: Parameters<typeof translate>[1]) => translate(locale, key);
    await page.setViewportSize({ width, height: 960 });
    await page.emulateMedia({ reducedMotion: "reduce", colorScheme: theme });
    await page.addInitScript(() => { localStorage.setItem("productflow.locale", "en-US"); localStorage.setItem("productflow.theme", "system"); });
    const state = await mockPreferences(page, profile("alice", { locale, theme }));
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto("/account");
    await expect(page.getByRole("heading", { name: accountPreferencesMessage(locale, "title"), exact: true })).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", locale);
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    expect(state.writes).toHaveLength(0);
    await page.screenshot({ path: testInfo.outputPath("account-preferences.png"), fullPage: true });
    const merchantSection = page.locator('section[aria-labelledby="merchant-heading"]');
    await merchantSection.getByLabel(t("account.merchant"), { exact: true }).fill("  Atelier 商品  ");
    await merchantSection.getByRole("button", { name: t("account.save"), exact: true }).click();
    await expect(merchantSection.getByText(t("account.saved"), { exact: true })).toBeVisible();
    expect(state.merchantWrites).toEqual(["Atelier 商品"]);
    await page.getByRole("radio", { name: t("theme.system"), exact: true }).click();
    await expect(page.locator("html")).toHaveAttribute("data-theme-preference", "system");
    expect(state.writes.at(-1)?.input).toEqual({ theme: "system" });
    const nextLocale = locale === "en-US" ? "ja-JP" : "en-US";
    await page.getByRole("combobox", { name: t("nav.language"), exact: true }).click();
    await page.getByRole("option", { name: t(LOCALE_LABEL_KEYS[nextLocale]), exact: true }).click();
    await expect(page.locator("html")).toHaveAttribute("lang", nextLocale);
    await page.locator(`summary[aria-label^="${translate(nextLocale, "nav.language")}:" ]`).filter({ visible: true }).click();
    await page.getByRole("button", { name: t(LOCALE_LABEL_KEYS[locale]), exact: true }).filter({ visible: true }).click();
    await expect(page.getByRole("combobox", { name: t("nav.language"), exact: true })).toHaveText(t(LOCALE_LABEL_KEYS[locale]));
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("lang", locale);
    await expect(page.getByLabel(t("account.merchant"), { exact: true })).toHaveValue("Atelier 商品");
    expect(await page.evaluate(() => [localStorage.getItem("productflow.locale"), localStorage.getItem("productflow.theme")])).toEqual(["en-US", "system"]);
    const geometry = await page.evaluate(() => ({ width: innerWidth, client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
    expect(geometry.width).toBe(width); expect(geometry.scroll).toBeLessThanOrEqual(geometry.client);
    if (width === 390) {
      const labelsFit = await page.locator(`div[aria-label="${t("nav.mobile")}"] a`).evaluateAll((links) => links.every((link) => {
        const label = link.querySelector("span")!.getBoundingClientRect();
        const bounds = link.getBoundingClientRect();
        return label.left >= bounds.left && label.right <= bounds.right;
      }));
      expect(labelsFit).toBe(true);
    }
    expect(errors).toEqual([]);
  });
}

test("top navigation and account share pending, failure, retry and saved preferences", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 960 });
  const state = await mockPreferences(page);
  state.failPreferences = 1;
  await page.goto("/account");
  await page.getByRole("button", { name: "Theme: Light", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Preferences were not saved");
  await expect(page.getByRole("radio", { name: "Dark", exact: true })).toBeChecked();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  expect(state.writes.map((entry) => entry.input)).toEqual([{ theme: "light" }, { theme: "light" }]);
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/account/preferences", async (route) => {
    state.writes.push({ user: "alice", input: route.request().postDataJSON() });
    await gate;
    state.accounts.alice.preferences = { locale: "ja-JP", theme: "light" };
    await route.fulfill({ json: state.accounts.alice.preferences });
  });
  await page.getByRole("combobox", { name: "Language", exact: true }).click();
  await page.getByRole("option", { name: "日本語", exact: true }).click();
  await expect(page.getByRole("radio", { name: "Light", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Theme: Dark", exact: true })).toBeDisabled();
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  release();
  await expect(page.locator("html")).toHaveAttribute("lang", "ja-JP");
  await expect(page.locator('summary[aria-label^="言語:"]').filter({ visible: true })).toContainText("日本語");
});

test("logout and new account ignore a late old preference response and anonymous values never upload", async ({ page }) => {
  await page.addInitScript(() => { localStorage.setItem("productflow.locale", "vi-VN"); localStorage.setItem("productflow.theme", "light"); });
  const state = await mockPreferences(page);
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/account/preferences", async (route) => { state.writes.push({ user: "alice", input: route.request().postDataJSON() }); await gate; await route.fulfill({ json: { locale: "zh-CN", theme: "dark" } }); });
  await page.goto("/account");
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  expect(state.writes).toHaveLength(0);
  await page.getByRole("combobox", { name: "Language", exact: true }).click();
  await page.getByRole("option", { name: "中文", exact: true }).click();
  await page.getByRole("button", { name: "Log out", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.locator("html")).toHaveAttribute("lang", "vi-VN");
  await page.getByLabel(translate("vi-VN", "login.email"), { exact: true }).fill("bob@example.com");
  await page.getByLabel(translate("vi-VN", "login.password"), { exact: true }).fill("password123");
  await page.getByRole("button", { name: translate("vi-VN", "login.submit"), exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("lang", "ja-JP");
  release();
  await page.locator('nav a[href="/account"]').filter({ visible: true }).click();
  await expect(page.getByLabel(translate("ja-JP", "account.displayName"), { exact: true })).toHaveValue("bob");
  await expect(page.locator("html")).toHaveAttribute("lang", "ja-JP");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  expect(state.writes).toHaveLength(1);
  expect(state.accounts.bob.preferences).toEqual({ locale: "ja-JP", theme: "light" });
});

test("merchant name validation, failed save recovery and suspended or absent merchant", async ({ page }) => {
  const state = await mockPreferences(page);
  state.failMerchant = 1;
  await page.goto("/account");
  const section = page.locator('section[aria-labelledby="merchant-heading"]');
  const input = section.getByLabel("Your merchant", { exact: true });
  const save = section.getByRole("button", { name: "Save profile", exact: true });
  await input.fill(" "); await save.click();
  await expect(section.getByRole("alert")).toContainText("1–160");
  await input.fill("😀".repeat(161)); await save.click();
  await expect(section.getByRole("alert")).toContainText("1–160");
  expect(state.merchantWrites).toHaveLength(0);
  await input.fill("😀".repeat(160)); await save.click();
  await expect(section.getByRole("alert")).toHaveText("Name unavailable");
  await expect(input).toHaveValue("😀".repeat(160));
  await save.click(); await expect(section.getByText("Profile saved", { exact: true })).toBeVisible();
  state.accounts.alice.merchant!.status = "suspended";
  await page.reload(); await expect(input).toBeDisabled();
  await page.getByRole("radio", { name: "Light", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  state.accounts.alice = { ...state.accounts.alice, merchant: null, user: { ...state.accounts.alice.user, is_operator: true } };
  await page.reload();
  await expect(section).toHaveCount(0);
  await expect(page.getByRole("combobox", { name: "Language", exact: true })).toBeEnabled();
});

test("late display-name response cannot overwrite newly saved preferences", async ({ page }) => {
  const state = await mockPreferences(page);
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/account", async (route) => {
    if (route.request().method() !== "PATCH") return route.fulfill({ json: state.accounts.alice });
    const old = structuredClone(state.accounts.alice);
    old.user.display_name = "Updated name";
    await gate;
    await route.fulfill({ json: old });
  });
  await page.goto("/account");
  const form = page.locator("form").filter({ has: page.getByLabel("Display name", { exact: true }) });
  await form.getByLabel("Display name", { exact: true }).fill("Updated name");
  await form.getByRole("button", { name: "Save profile", exact: true }).click();
  await page.getByRole("radio", { name: "Light", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  release();
  await expect(form.getByText("Profile saved", { exact: true })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
});

test("late merchant response cannot restore the previous account or its merchant", async ({ page }) => {
  const state = await mockPreferences(page);
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/account/merchant", async (route) => { await gate; await route.fulfill({ json: { ...state.accounts.alice.merchant, name: "Old account changed" } }); });
  await page.goto("/account");
  const section = page.locator('section[aria-labelledby="merchant-heading"]');
  await section.getByLabel("Your merchant", { exact: true }).fill("Old account changed");
  await section.getByRole("button", { name: "Save profile", exact: true }).click();
  await page.getByRole("button", { name: "Log out", exact: true }).click();
  await page.getByLabel(translate("zh-CN", "login.email"), { exact: true }).fill("bob@example.com");
  await page.getByLabel(translate("zh-CN", "login.password"), { exact: true }).fill("password123");
  await page.getByRole("button", { name: translate("zh-CN", "login.submit"), exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("lang", "ja-JP");
  await page.locator('nav a[href="/account"]').filter({ visible: true }).click();
  await expect(page.getByLabel(translate("ja-JP", "account.merchant"), { exact: true })).toHaveValue("bob shop");
  release();
  await expect(page.getByLabel(translate("ja-JP", "account.merchant"), { exact: true })).toHaveValue("bob shop");
  await expect(page.getByLabel(translate("ja-JP", "account.displayName"), { exact: true })).toHaveValue("bob");
});

test.describe("preference touch and keyboard controls", () => {
  test.use({ hasTouch: true, viewport: { width: 390, height: 844 } });
  test("saves from the mobile theme button and resolves system changes without saving new preferences", async ({ page }) => {
    const state = await mockPreferences(page);
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await page.goto("/account");
    await page.getByRole("button", { name: "Theme: Dark", exact: true }).filter({ visible: true }).tap();
    await expect(page.locator("html")).toHaveAttribute("data-theme-preference", "system");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await page.emulateMedia({ colorScheme: "light" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(state.writes).toHaveLength(1);
    await page.getByRole("combobox", { name: "Language", exact: true }).tap();
    await page.getByRole("option", { name: "日本語", exact: true }).tap();
    await expect(page.locator("html")).toHaveAttribute("lang", "ja-JP");
    const theme = page.getByRole("radio", { name: translate("ja-JP", "theme.system"), exact: true });
    await theme.focus();
    await expect(theme).toBeFocused();
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("radio", { name: translate("ja-JP", "theme.light"), exact: true })).toBeChecked();
    await expect(page.locator("html")).toHaveAttribute("data-theme-preference", "light");
    expect(state.writes.at(-1)?.input).toEqual({ theme: "light" });
  });
});

for (const operation of ["logout", "password", "revoke"] as const) {
  test(`late ${operation} callback cannot sign out the account that replaced its owner`, async ({ page }) => {
    const state = await mockPreferences(page);
    const endpoint = operation === "logout" ? "/api/auth/session" : operation === "password" ? "/api/account/password" : "/api/account/sessions/current";
    const method = operation === "password" ? "POST" : "DELETE";
    let release = () => {};
    const gate = new Promise<void>((resolve) => { release = resolve; });
    await page.route(`**${endpoint}`, async (route) => {
      if (route.request().method() !== method) return route.fallback();
      await gate;
      await route.fulfill({ json: { ok: true } });
    });
    await page.route("**/api/account/sessions?*", (route) => route.fulfill({ json: { items: [{ id: "current", current: true, created_at: "2026-09-08T00:00:00Z", expires_at: "2026-10-08T00:00:00Z" }], next_cursor: null } }));
    await page.goto("/account");
    await page.getByLabel("Display name", { exact: true }).fill("Unsaved Alice draft");
    const request = page.waitForRequest((request) => new URL(request.url()).pathname === endpoint && request.method() === method);
    if (operation === "logout") await page.getByRole("button", { name: "Log out", exact: true }).click();
    if (operation === "password") {
      await page.getByLabel("Current password", { exact: true }).fill("current-password");
      await page.getByLabel("New password", { exact: true }).fill("new-password123");
      await page.getByLabel("Confirm new password", { exact: true }).fill("new-password123");
      await page.getByRole("button", { name: translate("en-US", "account.changePassword"), exact: true }).click();
    }
    if (operation === "revoke") {
      await page.getByRole("button", { name: translate("en-US", "account.revokeCurrent"), exact: true }).click();
      await page.getByRole("dialog").getByRole("button", { name: translate("en-US", "account.revokeCurrent"), exact: true }).click();
    }
    await request;
    // A different authenticated session becomes authoritative before A's request finishes.
    state.current = "bob";
    // Reconnect events arrive in separate browser tasks; retain the mounted route.
    await page.evaluate(() => window.dispatchEvent(new Event("offline")));
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect(page.locator("html")).toHaveAttribute("lang", "ja-JP");
    await expect(page.getByLabel(translate("ja-JP", "account.displayName"), { exact: true })).toHaveValue("bob");
    const response = page.waitForResponse((response) => new URL(response.url()).pathname === endpoint && response.request().method() === method);
    release();
    await (await response).finished();
    await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    await expect(page).toHaveURL(/\/account$/);
    await expect(page.getByLabel(translate("ja-JP", "account.displayName"), { exact: true })).toHaveValue("bob");
    await expect(page.getByLabel(translate("ja-JP", "account.currentPassword"), { exact: true })).toHaveValue("");
    await expect(page.getByLabel(translate("ja-JP", "account.merchant"), { exact: true })).toHaveValue("bob shop");
  });
}

test("mobile preference failure stays visible while its controls are scrolled into view", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const state = await mockPreferences(page);
  state.failPreferences = 1;
  await page.goto("/account");
  await page.getByRole("radio", { name: "Light", exact: true }).scrollIntoViewIfNeeded();
  await page.getByRole("radio", { name: "Light", exact: true }).click();
  const alert = page.getByRole("alert");
  await expect(alert).toContainText("Preferences were not saved");
  const bounds = await alert.boundingBox();
  expect(bounds!.y).toBeGreaterThanOrEqual(0);
  expect(bounds!.y + bounds!.height).toBeLessThan(844);
  await page.screenshot({ path: testInfo.outputPath("mobile-save-error.png") });
  await alert.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
});

test("account section navigation preserves drafts and theme previews support keyboard at narrow desktop", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1024, height: 900 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await mockPreferences(page);
  await page.goto("/account");
  const name = page.getByLabel("Display name", { exact: true });
  await name.fill("Unsaved account name");
  await page.locator('a[href="#preferences"]').click();
  const dark = page.getByRole("radio", { name: "Dark", exact: true });
  await dark.focus();
  await page.keyboard.press("ArrowLeft");
  await expect(page.getByRole("radio", { name: "Light", exact: true })).toBeChecked();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.locator('a[href="#profile"]').click();
  await expect(name).toHaveValue("Unsaved account name");
  const bounds = await page.evaluate(() => ({ width: innerWidth, client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
  expect(bounds.width).toBe(1024);
  expect(bounds.scroll).toBeLessThanOrEqual(bounds.client);
  await page.screenshot({ path: testInfo.outputPath("account-narrow.png"), fullPage: true });
});
