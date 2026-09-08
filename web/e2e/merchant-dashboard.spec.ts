import { expect, test, type Page } from "@playwright/test";
import { emptyMerchantOverview } from "./fixtures/merchantOverview";
import { LOCALES, translate, type Locale } from "./fixtures/i18n";
import { overviewMessage } from "../src/pages/product-list/overviewMessages";
import type { ImageSessionDetail, MerchantWorkRecord } from "../src/lib/types";

test.use({ hasTouch: true });

const timestamp = "2026-09-08T00:00:00Z";
const work = (id: string, kind: MerchantWorkRecord["kind"], status: string, product_id: string | null = "product-a"): MerchantWorkRecord => ({ id, kind, status, product_id, product_name: product_id ? "Studio desk lamp · 長い商品名を安全に表示するための商品名称" : null, session_id: kind === "agent_task" ? "agent-session" : kind === "image_session" ? "page-two-session" : null, title: `Work ${id} · 商品画像の制作と確認`, created_at: status === "waiting_user" ? "2020-01-01T00:00:00Z" : timestamp, started_at: null, finished_at: status === "failed" ? timestamp : null, failure_reason: status === "failed" ? "The recorded operation failed. Its later resolution is not tracked here." : null });
const records = [work("old-waiting", "agent_task", "waiting_user"), work("graph", "workflow_run", "running"), work("image", "image_session", "awaiting_confirmation", null), work("edit", "local_edit", "failed"), work("global", "agent_task", "paused", null), work("unknown", "workflow_run", "unknown"), work("draft", "agent_task", "draft"), work("queued", "image_session", "queued", null), work("done", "local_edit", "succeeded"), work("canceled", "workflow_run", "canceled"), work("last", "agent_task", "running")];
const detail = (id: string): ImageSessionDetail => ({ id, title: `Session ${id}`, assets: [{ id: "reference-shared-id", kind: "reference_upload", original_filename: "Studio reference", mime_type: "image/svg+xml", download_url: "/test-reference.svg", preview_url: "/test-reference.svg", thumbnail_url: "/test-reference.svg", created_at: timestamp }], rounds: [], generation_tasks: [], rounds_count: 0, history_next_after: null, created_at: timestamp, updated_at: timestamp });

async function mockDashboard(page: Page, locale: Locale = "en-US", theme: "light" | "dark" = "light") {
  const state = { account: "alice" as string | null, overviewCalls: [] as URL[], detailCalls: [] as string[], listCalls: [] as string[], posts: [] as string[], failOverview: false, empty: false, failDetail: false };
  await page.route("**/test-reference.svg", (route) => route.fulfill({ contentType: "image/svg+xml", body: '<svg xmlns="http://www.w3.org/2000/svg" width="80" height="80"><rect width="80" height="80" fill="gray"/></svg>' }));
  await page.route("**/api/**", async (route) => {
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname;
    const json = (value: unknown, status = 200) => route.fulfill({ status, json: value });
    if (path === "/api/auth/session") {
      if (request.method() === "DELETE") { state.account = null; return json({ ok: true }); }
      if (request.method() === "POST") { state.account = request.postDataJSON().email.split("@")[0]; return json({ ok: true }); }
      return json(state.account ? { authenticated: true, access_required: true, preferences: { locale, theme }, user: { id: state.account, email: `${state.account}@example.com`, display_name: state.account, is_operator: state.account === "operator" }, merchant: state.account === "operator" ? null : { id: `${state.account}-merchant`, name: `${state.account} shop`, status: "active" } } : { authenticated: false, access_required: true });
    }
    if (path === "/api/v2/products/overview") {
      state.overviewCalls.push(url);
      if (state.failOverview) return json({ detail: "Unavailable" }, 503);
      const data = emptyMerchantOverview();
      const days = url.searchParams.get("days") === "7" ? 7 : 30;
      data.recent_window = { days, from: days === 7 ? "2026-09-01T00:00:00Z" : "2026-08-09T00:00:00Z", to: timestamp };
      data.products = state.empty ? { total: 0, current_adopted: 0 } : { total: 24, current_adopted: 8 };
      if (!state.empty) data.work.by_source = { agent_task: { active: 1, waiting: 1, unknown: 0, recent_failed: 0 }, workflow_run: { active: 1, waiting: 0, unknown: 1, recent_failed: 0 }, image_session: { active: 1, waiting: 1, unknown: 0, recent_failed: 0 }, local_edit: { active: 0, waiting: 0, unknown: 0, recent_failed: 1 } };
      const kind = url.searchParams.get("kind"); const status = url.searchParams.get("state");
      const filtered = state.empty ? [] : records.filter((item) => (kind === "all" || item.kind === kind) && (status === "all" || status === "active" && ["queued", "running"].includes(item.status) || status === "waiting" && ["waiting_user", "awaiting_confirmation"].includes(item.status) || item.status === status));
      const current = Number(url.searchParams.get("page"));
      data.work.records = { items: filtered.slice((current - 1) * 10, current * 10).map((record) => ({ ...record, title: `${state.account}: ${record.title}` })), total: filtered.length, page: current, page_size: 10 };
      return json(data);
    }
    if (path === "/api/v2/products") return json({ items: state.empty ? [] : [{ id: "product-a", name: `${state.account} product`, category: null, price: null, cover_image_asset_id: null, cover_image_filename: null, cover_image_download_url: null, cover_image_preview_url: null, cover_image_thumbnail_url: null, created_at: timestamp, updated_at: timestamp }], total: state.empty ? 0 : 24, page: Number(url.searchParams.get("page") || 1), page_size: 12 });
    if (path === "/api/image-sessions") {
      if (request.method() === "POST") { state.posts.push(path); return json(detail("created")); }
      state.listCalls.push(url.search);
      return json({ items: state.empty ? [] : [{ id: url.searchParams.has("after") ? "page-two-session" : "first-session", title: url.searchParams.has("after") ? "Session page-two-session" : "Session first-session", rounds_count: 0, latest_generated_asset: null, created_at: timestamp, updated_at: timestamp }], next_cursor: url.searchParams.has("after") ? null : "cursor-page-2" });
    }
    if (path.startsWith("/api/image-sessions/")) {
      const id = decodeURIComponent(path.slice("/api/image-sessions/".length)); state.detailCalls.push(id);
      if (state.failDetail || ["missing", "foreign"].includes(id)) return json({ detail: "Not found" }, 404);
      return json(detail(id));
    }
    if (path === "/api/runtime-config") return json({ deletion_enabled: false });
    if (path.includes("quota-price")) return json({ entries: [] });
    return json({ items: [], conversations: [], total: 0, page: 1, page_size: 20, next_cursor: null });
  });
  return state;
}

for (const locale of LOCALES) for (const width of [390, 1440]) for (const theme of ["light", "dark"] as const) {
  test(`merchant overview ${locale} ${width} ${theme}`, async ({ page }, testInfo) => {
    const m = (key: Parameters<typeof overviewMessage>[1]) => overviewMessage(locale, key);
    await page.setViewportSize({ width, height: 960 }); await page.emulateMedia({ reducedMotion: "reduce", colorScheme: theme });
    await mockDashboard(page, locale, theme); const errors: string[] = []; page.on("pageerror", (error) => errors.push(error.message));
    await page.goto("/products?view=overview");
    const overview = page.locator('section[aria-labelledby="merchant-overview-title"]');
    await expect(overview.getByRole("heading", { name: m("title") })).toBeVisible();
    await expect(overview.getByText("alice: Work old-waiting", { exact: false })).toBeVisible();
    await expect(overview.getByText(m("adoptionNote"))).toBeVisible();
    await expect(overview.getByText(m("filterNote"))).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", locale); await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    await page.screenshot({ path: testInfo.outputPath("overview.png"), fullPage: true });
    const geometry = await page.evaluate(() => ({ width: innerWidth, client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
    expect(geometry.width).toBe(width); expect(geometry.scroll).toBeLessThanOrEqual(geometry.client);
    const stateFilter = overview.getByRole("combobox", { name: m("allStates"), exact: true });
    if (width === 390) await stateFilter.tap(); else { await stateFilter.focus(); await page.keyboard.press("Enter"); }
    await page.getByRole("option", { name: m("waiting"), exact: true }).click();
    await expect(overview.getByText("alice: Work old-waiting", { exact: false })).toBeVisible();
    await expect(overview.locator("dl").first()).toContainText("24");
    await expect(overview.locator("dl").first()).toContainText("8");
    expect(errors).toEqual([]);
  });
}

test("filters, pages, old waiting records and real continuation destinations", async ({ page }) => {
  const state = await mockDashboard(page); await page.goto("/products?view=overview&q=lamp&page=2&sort=name_asc");
  const overview = page.locator('section[aria-labelledby="merchant-overview-title"]');
  await expect(overview.getByText("alice: Work old-waiting", { exact: false })).toBeVisible();
  await overview.getByRole("combobox", { name: "Recent record window", exact: true }).click();
  await page.getByRole("option", { name: "Last 7 days", exact: true }).click();
  await expect(overview.getByText("alice: Work old-waiting", { exact: false })).toBeVisible();
  expect(new URL(page.url()).searchParams.get("work_days")).toBe("7");
  await expect(overview.locator('a[href="/products/product-a?agent_session_id=agent-session&agent_task_id=old-waiting"]')).toBeVisible();
  await expect(overview.locator('li').filter({ hasText: "Work graph" }).getByRole("link")).toHaveAttribute("href", "/products/product-a");
  await expect(overview.locator('li').filter({ hasText: "Work edit" }).getByRole("link")).toHaveText("Open product");
  await page.evaluate(() => { window.addEventListener("productflow:open-agent", (event) => { document.documentElement.dataset.openAgent = JSON.stringify((event as CustomEvent).detail); }); });
  await overview.locator('li').filter({ hasText: "Work global" }).getByRole("button", { name: "Open goal" }).click();
  expect(JSON.parse((await page.locator("html").getAttribute("data-open-agent"))!)).toMatchObject({ sessionId: "agent-session", taskId: "global", tab: "tasks" });
  await page.keyboard.press("Escape");
  await overview.getByRole("button", { name: "Next", exact: true }).click();
  await expect(overview.getByText("alice: Work last", { exact: false })).toBeVisible();
  expect(new URL(page.url()).searchParams.get("page")).toBe("2");
  await overview.getByRole("combobox", { name: "All sources", exact: true }).click(); await page.getByRole("option", { name: "Image sessions", exact: true }).click();
  await expect(overview.getByText("alice: Work image", { exact: false })).toBeVisible();
  expect(new URL(page.url()).searchParams.get("work_page")).toBeNull();
  expect(new URL(page.url()).searchParams.get("q")).toBe("lamp");
  await overview.getByRole("link", { name: "Open session" }).first().click();
  await expect(page).toHaveURL(/image_session_id=page-two-session/);
  await expect(page.getByRole("heading", { name: "Session page-two-session", exact: true })).toBeVisible();
  expect(state.listCalls.every((query) => !query.includes("after="))).toBe(true);
  expect(state.detailCalls).toContain("page-two-session"); expect(state.posts).toEqual([]);
});

test("overview error retry and empty result preserve the product directory", async ({ page }, testInfo) => {
  const state = await mockDashboard(page); state.failOverview = true; await page.goto("/products?view=overview");
  const overview = page.locator('section[aria-labelledby="merchant-overview-title"]');
  await expect(overview.getByRole("alert")).toContainText(overviewMessage("en-US", "loadError")); await expect(page.getByText("alice product", { exact: true })).toBeHidden();
  await page.screenshot({ path: testInfo.outputPath("overview-error.png"), fullPage: true });
  state.failOverview = false; state.empty = true; await overview.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(overview.getByText(overviewMessage("en-US", "empty"))).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("overview-empty.png"), fullPage: true });
  await page.getByRole("button", {name: translate("en-US", "products.listTitle"), exact: true}).click();
  await expect(page.getByText("alice product", {exact: true})).toBeVisible();
});

for (const id of ["", "missing", "foreign"]) test(`explicit unavailable session ${id || "empty"} never creates or falls back`, async ({ page }) => {
  const state = await mockDashboard(page); state.empty = true; await page.goto(`/image-chat?image_session_id=${id}`);
  await expect(page.getByRole("alert")).toBeVisible(); expect(state.posts).toEqual([]); expect(state.detailCalls.includes("first-session")).toBe(false);
  await page.getByRole("button", { name: "Back to sessions", exact: true }).click();
  await expect(page).toHaveURL(/\/image-chat$/); await expect(page.getByText("No sessions", { exact: true }).first()).toBeVisible();
  expect(state.posts).toEqual([]);
});

test("explicit session retry, URL back, late reads and account changes keep the chosen identity", async ({ page }) => {
  const state = await mockDashboard(page); state.failDetail = true; await page.goto("/image-chat?image_session_id=page-two-session");
  await expect(page.getByRole("alert")).toBeVisible(); state.failDetail = false; await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Session page-two-session", exact: true })).toBeVisible();
  const resize = await page.getByRole("button", { name: "Resize session list", exact: true }).boundingBox();
  await page.mouse.move(resize!.x + resize!.width / 2, resize!.y + 100); await page.mouse.down();
  await page.mouse.move(resize!.x + resize!.width / 2 + 40, resize!.y + 100); await page.mouse.up();
  const resizedWidth = (await page.locator("main > aside").first().boundingBox())!.width;
  expect(resizedWidth).toBeGreaterThan(300);
  await page.locator("#image-chat-prompt").fill("Alice private draft");
  await page.getByRole("checkbox", { name: translate("en-US", "chat.useReference"), exact: true }).check();
  await page.getByRole("button", { name: /Session first-session/ }).first().click();
  await expect(page).toHaveURL(/image_session_id=first-session/);
  expect((await page.locator("main > aside").first().boundingBox())!.width).toBe(resizedWidth);
  await expect(page.getByRole("checkbox", { name: translate("en-US", "chat.useReference"), exact: true })).not.toBeChecked();
  await page.goBack();
  await expect(page.getByRole("heading", { name: "Session page-two-session", exact: true })).toBeVisible();
  let release = () => {}; const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/image-sessions/slow-session", async (route) => { await gate; await route.fulfill({ json: detail("slow-session") }); });
  await page.evaluate(() => { history.pushState({}, "", "/image-chat?image_session_id=slow-session"); window.dispatchEvent(new PopStateEvent("popstate")); });
  await expect(page.getByRole("status").filter({ hasText: "Loading" })).toBeVisible();
  await page.evaluate(() => { history.pushState({}, "", "/image-chat?image_session_id=first-session"); window.dispatchEvent(new PopStateEvent("popstate")); });
  await expect(page.getByRole("heading", { name: "Session first-session", exact: true })).toBeVisible();
  const slowResponse = page.waitForResponse("**/api/image-sessions/slow-session"); release(); await slowResponse;
  await expect(page.getByRole("heading", { name: "Session slow-session", exact: true })).toHaveCount(0);
  let releaseRename = () => {}; const renameGate = new Promise<void>((resolve) => { releaseRename = resolve; });
  await page.route("**/api/image-sessions/first-session", async (route) => {
    if (route.request().method() !== "PATCH") return route.fallback();
    await renameGate; await route.fulfill({ json: { ...detail("first-session"), title: "ALICE PRIVATE LATE NAME" } });
  });
  await page.getByRole("button", { name: translate("en-US", "chat.rename"), exact: true }).filter({ visible: true }).click();
  await page.locator("input").filter({ visible: true }).first().fill("ALICE PRIVATE LATE NAME");
  const renameStarted = page.waitForRequest((request) => request.method() === "PATCH" && request.url().endsWith("/api/image-sessions/first-session"));
  await page.getByRole("button", { name: translate("en-US", "chat.saveSessionName"), exact: true }).filter({ visible: true }).click(); await renameStarted;
  // Return through a real logout/login so the existing global account boundary runs.
  await page.getByRole("button", { name: "Log out", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  const anonymousLocale = await page.locator("html").getAttribute("lang") as Locale;
  await page.getByLabel(translate(anonymousLocale, "login.email"), { exact: true }).fill("bob@example.com");
  await page.getByLabel(translate(anonymousLocale, "login.password"), { exact: true }).fill("password123");
  await page.getByRole("button", { name: translate(anonymousLocale, "login.submit"), exact: true }).click();
  await expect(page).toHaveURL(/\/products$/);
  await page.locator('nav a[href="/image-chat"]').filter({ visible: true }).click();
  const renameResponse = page.waitForResponse((response) => response.request().method() === "PATCH" && response.url().endsWith("/api/image-sessions/first-session")); releaseRename(); await renameResponse;
  await expect(page.getByRole("heading", { name: "Session first-session", exact: true })).toBeVisible();
  await expect(page.getByText("ALICE PRIVATE LATE NAME")).toHaveCount(0);
  await expect(page.locator("#image-chat-prompt")).toHaveValue("");
  await expect(page.getByRole("checkbox", { name: translate("en-US", "chat.useReference"), exact: true })).not.toBeChecked();
  expect(state.posts).toEqual([]);
});


test("operator without a merchant uses Ops and does not request a merchant overview", async ({ page }) => {
  const state = await mockDashboard(page); state.account = "operator";
  await page.goto("/products?view=overview"); await expect(page).toHaveURL(/\/ops$/);
  expect(state.overviewCalls).toEqual([]);
});

test("mobile explicit session failure has visible retry and list recovery", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const state = await mockDashboard(page, "vi-VN", "dark"); state.failDetail = true;
  await page.goto("/image-chat?image_session_id=page-two-session");
  await expect(page.getByRole("alert")).toBeVisible();
  const retry = page.getByRole("button", { name: "Thử lại", exact: true });
  await expect(retry).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath("image-session-error-mobile.png"), fullPage: true });
  state.failDetail = false; await retry.click();
  await expect(page.getByText("Session page-two-session", { exact: true }).filter({ visible: true })).toBeVisible();
  expect(state.posts).toEqual([]);
});


test("a late overview response cannot restore the previous account's records", async ({ page }) => {
  const state = await mockDashboard(page);
  await page.goto("/products?view=overview");
  await expect(page.getByText("alice: Work old-waiting", { exact: false })).toBeVisible();
  let release = () => {}; const gate = new Promise<void>((resolve) => { release = resolve; });
  let delayed = false;
  await page.route("**/api/v2/products/overview?*", async (route) => {
    if (delayed) return route.fallback(); delayed = true;
    const old = emptyMerchantOverview(); old.work.records = { items: [{ ...records[0], title: "ALICE PRIVATE LATE RECORD" }], total: 1, page: 1, page_size: 10 };
    await gate; await route.fulfill({ json: old });
  });
  const started = page.waitForRequest("**/api/v2/products/overview?*");
  await page.getByRole("button", { name: "Refresh overview", exact: true }).click(); await started;
  await page.getByRole("button", { name: "Log out", exact: true }).click(); await expect(page).toHaveURL(/\/login$/);
  const locale = await page.locator("html").getAttribute("lang") as Locale;
  await page.getByLabel(translate(locale, "login.email"), { exact: true }).fill("bob@example.com");
  await page.getByLabel(translate(locale, "login.password"), { exact: true }).fill("password123");
  await page.getByRole("button", { name: translate(locale, "login.submit"), exact: true }).click();
  await page.getByRole("button", {name: overviewMessage("en-US", "title"), exact: true}).click();
  await expect(page.getByText("bob: Work old-waiting", { exact: false })).toBeVisible();
  const lateResponse = page.waitForResponse("**/api/v2/products/overview?*"); release(); await lateResponse;
  await expect(page.getByText("ALICE PRIVATE LATE RECORD")).toHaveCount(0);
  expect(state.account).toBe("bob");
});


test("narrow desktop keeps overview controls and source columns within their surface", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1024, height: 900 }); await mockDashboard(page, "ja-JP", "dark");
  await page.goto("/products?view=overview");
  const overview = page.locator('section[aria-labelledby="merchant-overview-title"]');
  await expect(overview.getByText("alice: Work old-waiting", { exact: false })).toBeVisible();
  const bounds = await overview.locator("table").evaluate((table) => {
    const rect = table.getBoundingClientRect();
    return { left: rect.left, right: rect.right, width: innerWidth, fits: Array.from(table.querySelectorAll("th,td")).every((cell) => { const box = cell.getBoundingClientRect(); return box.left >= rect.left && box.right <= rect.right + 1; }) };
  });
  expect(bounds.left).toBeGreaterThanOrEqual(0); expect(bounds.right).toBeLessThanOrEqual(bounds.width); expect(bounds.fits).toBe(true);
  await page.screenshot({ path: testInfo.outputPath("overview-narrow-desktop.png"), fullPage: true });
});

test("a session refresh changing the account hides cached image detail and permits the new account's edits", async ({ page }) => {
  const state = await mockDashboard(page);
  let release = () => {}; const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/image-sessions/first-session", async (route) => {
    if (route.request().method() === "PATCH") return route.fulfill({ json: { ...detail("first-session"), title: "Bob saved title" } });
    const account = state.account;
    if (account === "bob") await gate;
    return route.fulfill({ json: { ...detail("first-session"), title: `${account} session detail` } });
  });
  await page.goto("/image-chat?image_session_id=first-session");
  await expect(page.getByRole("heading", { name: "alice session detail", exact: true })).toBeVisible();
  await page.locator("#image-chat-prompt").fill("Alice only draft");
  state.account = "bob";
  const refreshed = page.waitForResponse("**/api/auth/session");
  await page.evaluate(() => { history.pushState({}, "", "/image-chat"); window.dispatchEvent(new PopStateEvent("popstate")); }); await refreshed;
  await expect(page.getByRole("heading", { name: "alice session detail", exact: true })).toHaveCount(0);
  release(); await expect(page.getByRole("heading", { name: "bob session detail", exact: true })).toBeVisible();
  await expect(page.locator("#image-chat-prompt")).toHaveValue("");
  await page.getByRole("button", { name: translate("en-US", "chat.rename"), exact: true }).filter({ visible: true }).click();
  await page.locator("input").filter({ visible: true }).first().fill("Bob saved title");
  await page.getByRole("button", { name: translate("en-US", "chat.saveSessionName"), exact: true }).filter({ visible: true }).click();
  await expect(page.getByRole("heading", { name: "Bob saved title", exact: true })).toBeVisible();
});


test("directory is the default and switching overview preserves search and sort", async ({page}) => {
  await mockDashboard(page);
  await page.goto("/products?q=lamp&sort=name_asc");
  await expect(page.getByText("alice product", {exact: true})).toBeVisible();
  await expect(page.locator('section[aria-labelledby="merchant-overview-title"]')).toHaveCount(0);
  await page.getByRole("button", {name: overviewMessage("en-US", "title"), exact: true}).click();
  await expect(page.getByText("alice: Work old-waiting", {exact: false})).toBeVisible();
  await page.reload();
  await expect(page.getByText("alice: Work old-waiting", {exact: false})).toBeVisible();
  await page.getByRole("button", {name: translate("en-US", "products.listTitle"), exact: true}).click();
  expect(new URL(page.url()).searchParams.get("q")).toBe("lamp");
  expect(new URL(page.url()).searchParams.get("sort")).toBe("name_asc");
  await page.getByRole("button", {name: `${translate("en-US", "products.table.actions")} · alice product`, exact: true}).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByRole("dialog").getByRole("button", {name: translate("en-US", "products.delete"), exact: true})).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toBeHidden();
});

test("product actions require confirmation before deleting", async ({page}) => {
  const state = await mockDashboard(page);
  let deletes = 0;
  await page.route("**/api/settings/runtime", route => route.fulfill({json: {deletion_enabled: true}}));
  await page.route("**/api/v2/products/product-a", route => {
    expect(route.request().method()).toBe("DELETE");
    deletes += 1; state.empty = true;
    return route.fulfill({json: {ok:true}});
  });
  await page.goto("/products");
  await page.getByRole("button", {name: `${translate("en-US", "products.table.actions")} · alice product`, exact:true}).click();
  await page.getByRole("dialog").getByRole("button", {name: translate("en-US", "products.delete"), exact:true}).click();
  await expect(page.getByRole("dialog")).toContainText(translate("en-US", "products.deleteConfirm", {name:"alice product"}));
  expect(deletes).toBe(0);
  await page.getByRole("dialog").getByRole("button", {name: translate("en-US", "confirm.delete.confirm"), exact:true}).click();
  await expect(page.getByRole("dialog")).toBeHidden();
  await expect(page.getByText("alice product", {exact:true})).toHaveCount(0);
  expect(deletes).toBe(1);
});
