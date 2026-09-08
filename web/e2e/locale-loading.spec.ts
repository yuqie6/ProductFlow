import { expect, test, type Page } from "@playwright/test";
import { LOCALES, LOCALE_LABEL_KEYS, translate, type Locale } from "./fixtures/i18n";
import { emptyMerchantOverview } from "./fixtures/merchantOverview";
import type { AccountProfile } from "../src/lib/types";

const resource = (locale: Locale) => new RegExp(`/assets/${locale}-[^/]+\\.json$`);
const account = (id: string, locale: Locale): AccountProfile => ({ user: { id, email: `${id}@example.com`, display_name: id, is_operator: false }, merchant: { id: `${id}-shop`, name: `${id} shop`, status: "active" }, preferences: { locale, theme: "light" } });
async function mock(page: Page, initial: string | null = "alice", locale: Locale = "en-US") {
  const state = { current: initial, accounts: { alice: account("alice", locale), bob: account("bob", "ja-JP") } as Record<string, AccountProfile>, writes: [] as unknown[] };
  await page.route("**/api/**", (route) => route.fulfill({ json: { items: [], total: 0, page: 1, page_size: 20 } }));
  await page.route("**/api/v2/products/overview?*", (route) => route.fulfill({ json: emptyMerchantOverview() }));
  await page.route("**/api/account/sessions?*", (route) => route.fulfill({ json: { items: [], next_cursor: null } }));
  await page.route("**/api/account", (route) => route.fulfill({ json: state.accounts[state.current!] }));
  await page.route("**/api/auth/session", (route) => {
    if (route.request().method() === "DELETE") { state.current = null; return route.fulfill({ json: { ok: true } }); }
    if (route.request().method() === "POST") { state.current = "bob"; return route.fulfill({ json: { ok: true } }); }
    return route.fulfill({ json: state.current ? { authenticated: true, access_required: true, ...state.accounts[state.current] } : { authenticated: false, access_required: true } });
  });
  await page.route("**/api/account/preferences", (route) => {
    state.writes.push({ user: state.current, ...route.request().postDataJSON() });
    const current = state.accounts[state.current!];
    current.preferences = { ...current.preferences, ...route.request().postDataJSON() };
    return route.fulfill({ json: current.preferences });
  });
  return state;
}
async function choose(page: Page, current: Locale, next: Locale) {
  await page.locator(`summary[aria-label^="${translate(current, "nav.language")}:" ]`).filter({ visible: true }).click();
  await page.getByRole("button", { name: translate(current, LOCALE_LABEL_KEYS[next]), exact: true }).filter({ visible: true }).click();
}

for (const locale of LOCALES) for (const width of [390, 1440]) {
  test(`cold authoritative locale ${locale} ${width} downloads one resource`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 844 });
    await page.addInitScript(() => localStorage.setItem("productflow.locale", "vi-VN"));
    const state = await mock(page, "alice", locale);
    const requests: string[] = [];
    page.on("request", (request) => { if (/\/assets\/[^/]+\.json$/.test(request.url())) requests.push(request.url()); });
    await page.goto("/account");
    await expect(page.getByLabel(translate(locale, "account.displayName"), { exact: true })).toHaveValue("alice");
    await page.waitForLoadState("networkidle");
    expect(requests).toHaveLength(1); expect(requests[0]).toMatch(resource(locale));
    expect(state.writes).toEqual([]);
    await page.screenshot({ path: testInfo.outputPath("cold-locale.png"), fullPage: true });
  });
}

test("session lookup finishes before selecting a resource", async ({ page }) => {
  await mock(page);
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/auth/session", async (route) => { await gate; await route.fulfill({ json: { authenticated: true, access_required: true, ...account("alice", "en-US") } }); });
  const requests: string[] = [];
  page.on("request", (request) => { if (request.url().endsWith(".json")) requests.push(request.url()); });
  await page.goto("/account");
  await expect(page.getByRole("status")).toBeVisible();
  expect(requests).toEqual([]);
  release();
  await expect(page.getByLabel("Display name", { exact: true })).toHaveValue("alice");
  expect(requests).toHaveLength(1); expect(requests[0]).toMatch(resource("en-US"));
});

for (const width of [390, 1440]) test(`cold resource failure has keyboard retry ${width}`, async ({ page }, testInfo) => {
  await page.setViewportSize({ width, height: 844 });
  await mock(page);
  let count = 0;
  await page.route(resource("en-US"), (route) => ++count === 1 ? route.fulfill({ status: 503, body: "unavailable" }) : route.continue());
  await page.goto("/account");
  await expect(page.getByRole("alert")).toContainText("language could not be loaded");
  await expect(page.getByLabel("Display name", { exact: true })).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("language-failed.png") });
  await page.getByRole("button", { name: "Retry", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByLabel("Display name", { exact: true })).toHaveValue("alice");
  expect(count).toBe(2);
});

test("failed selected language keeps old preference and retries before saving", async ({ page }) => {
  const state = await mock(page);
  let count = 0;
  await page.route(resource("ja-JP"), (route) => ++count === 1 ? route.fulfill({ status: 200, contentType: "application/json", body: "{}" }) : route.continue());
  await page.goto("/account");
  await choose(page, "en-US", "ja-JP");
  await expect(page.getByRole("alert")).toContainText("Preferences were not saved");
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  expect(state.writes).toEqual([]);
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("lang", "ja-JP");
  expect(state.writes).toEqual([{ user: "alice", locale: "ja-JP" }]);
  await choose(page, "ja-JP", "en-US");
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  await choose(page, "en-US", "ja-JP");
  await expect(page.locator("html")).toHaveAttribute("lang", "ja-JP");
  expect(count).toBe(2);
});

test("late language download cannot send the old account preference with a new session", async ({ page }) => {
  const state = await mock(page);
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route(resource("vi-VN"), async (route) => { await gate; await route.continue(); });
  await page.goto("/account");
  const requested = page.waitForRequest(resource("vi-VN"));
  await choose(page, "en-US", "vi-VN");
  await requested;
  state.current = "bob";
  await page.evaluate(() => { window.dispatchEvent(new Event("offline")); window.dispatchEvent(new Event("online")); });
  await expect(page.getByLabel(translate("ja-JP", "account.displayName"), { exact: true })).toHaveValue("bob");
  const response = page.waitForResponse(resource("vi-VN")); release(); await (await response).finished();
  await page.waitForLoadState("networkidle");
  expect(state.writes).toEqual([]);
  await expect(page.locator("html")).toHaveAttribute("lang", "ja-JP");
  await expect(page.getByLabel(translate("ja-JP", "account.merchant"), { exact: true })).toHaveValue("bob shop");
});


test("a saved authoritative locale that fails to load has resource retry without repeating the save", async ({ page }) => {
  const state = await mock(page);
  let attempts = 0;
  await page.route(resource("ja-JP"), (route) => ++attempts === 1 ? route.fulfill({ status: 503, body: "unavailable" }) : route.continue());
  await page.route("**/api/account/preferences", (route) => {
    state.writes.push(route.request().postDataJSON());
    state.accounts.alice.preferences = { locale: "ja-JP", theme: "dark" };
    return route.fulfill({ json: state.accounts.alice.preferences });
  });
  await page.goto("/account");
  await page.getByRole("button", { name: "Theme: Light", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("言語を読み込めませんでした");
  await expect(page.getByLabel("Display name", { exact: true })).not.toBeVisible();
  await page.getByRole("button", { name: "再試行", exact: true }).click();
  await expect(page.getByLabel(translate("ja-JP", "account.displayName"), { exact: true })).toHaveValue("alice");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(state.writes).toHaveLength(1);
});
