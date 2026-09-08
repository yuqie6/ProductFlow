import { expect, test, type Page } from "@playwright/test";
import { LOCALES, translate } from "../src/lib/i18n";
import type { GalleryAsset, AccountPreferences, OpsAction, OpsQuotaEvent, OpsTask, ProductFactsResponse } from "../src/lib/types";

const created = "2026-09-08T00:00:00Z";
const merchants = [{ id: "merchant-a", name: "Merchant A 商品商家", status: "active" }, { id: "merchant-b", name: "Merchant B", status: "suspended" }];
const product = (id = "product-a", name = "Lamp A") => ({ id, name, category: "Lighting", price: "12.00", source_note: "Steel lamp", cover_image_asset_id: null, created_at: created, updated_at: created });
const factResponse = (version: number, name: string): ProductFactsResponse => ({ product: product("product-a", name), fact_set: { id: `version-${version}`, version, facts: [{ key: "dimensions", value: { width: 20, unit: "cm" }, source_type: "user", status: "confirmed", layer: "performance", evidence_asset_ids: ["asset-a"] }] }, current_fact_set_version_id: `version-${version}`, current_fact_version: version });
const asset: GalleryAsset = { id: "asset-a", product_id: "product-a", media_object_id: "media-a", origin_type: "upload", display_name: "Lamp reference", original_filename: "lamp.png", image_type_key: null, user_folder_id: null, parent_asset_id: null, source_image_session_asset_id: null, source_library_asset_id: null, mime_type: "image/png", byte_size: 100, width: 100, height: 100, verification_status: "verified", download_url: "/api/ops/merchants/merchant-a/product-image-assets/asset-a/download", preview_url: "/api/ops/merchants/merchant-a/product-image-assets/asset-a/download?variant=preview", thumbnail_url: "/api/ops/merchants/merchant-a/product-image-assets/asset-a/download?variant=thumbnail", created_at: created, updated_at: created, user_folder_name: null, image_type_title: null, generation: null, rendition: null };
const taskKinds: OpsTask["kind"][] = ["agent_task", "workflow_run", "image_session", "local_edit"];
const taskRows: OpsTask[] = taskKinds.map((kind, index) => ({ id: `task-${index}`, kind, product_id: kind === "image_session" ? null : "product-a", title: ["Assistant inspection", "Product generation", "Continuous image", "Local correction"][index], status: ["waiting_user", "unknown", "running", "draft"][index], created_at: created, started_at: index === 0 ? null : created, finished_at: null, failure_reason: index === 1 ? "Review required" : null }));

async function mockOps(page: Page, ordinary = false, preferences: AccountPreferences = { locale: "zh-CN", theme: "system" }) {
  const state = { facts: factResponse(1, "Lamp A"), saves: 0, deletes: 0, deleted: false, adjustments: [] as Array<{ idempotency_key: string; delta_units: number; reason: string }>, actions: [] as OpsAction[], events: [] as OpsQuotaEvent[], requested: [] as string[] };
  await page.route("**/api/**", (route) => route.fulfill({ json: { items: [], conversations: [] } }));
  await page.route("**/api/auth/session", (route) => route.fulfill({ json: { authenticated: true, preferences, access_required: true, user: { id: "operator", email: "ops@example.com", display_name: "Operator", is_operator: !ordinary }, merchant: ordinary ? merchants[0] : null } }));
  await page.route("**/api/account", (route) => route.fulfill({ json: { preferences, user: { id: "operator", email: "ops@example.com", display_name: "Operator", is_operator: !ordinary }, merchant: ordinary ? merchants[0] : null } }));
  await page.route("**/api/account/sessions?*", (route) => route.fulfill({ json: { items: [], next_cursor: null } }));
  await page.route("**/api/ops/**", (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    const pageNumber = Number(url.searchParams.get("page") ?? "1");
    state.requested.push(path + url.search);
    if (ordinary) return route.fulfill({ status: 403, json: { detail: "Operator required" } });
    const paged = (items: unknown[], total = items.length) => ({ items, total, page: pageNumber, page_size: 20 });
    if (path === "/api/ops/merchants") {
      const q = url.searchParams.get("q")?.toLowerCase() ?? "";
      const status = url.searchParams.get("status");
      return route.fulfill({ json: paged(merchants.filter((merchant) => merchant.name.toLowerCase().includes(q) && (!status || merchant.status === status))) });
    }
    if (/\/merchants\/merchant-[ab]$/.test(path)) return route.fulfill({ json: path.endsWith("merchant-a") ? merchants[0] : merchants[1] });
    if (path.includes("product-image-assets")) return route.fulfill({ contentType: "image/svg+xml", headers: url.search ? {} : { "Content-Disposition": 'attachment; filename="lamp.svg"' }, body: '<svg xmlns="http://www.w3.org/2000/svg" width="160" height="160"><rect width="160" height="160" fill="#e3e9e2"/><path d="M50 100 L110 100 L95 45 L65 45 Z" fill="#6c8272"/><path d="M80 100 V135 M55 135 H105" stroke="#475b4d" stroke-width="8"/></svg>' });
    if (path.endsWith("/products")) {
      const rows = path.includes("merchant-b") ? [{ ...product("product-b", "Lamp B"), cover_image_filename: null, cover_image_download_url: null, cover_image_preview_url: null, cover_image_thumbnail_url: null }] : state.deleted ? [] : [{ ...state.facts.product, cover_image_filename: null, cover_image_download_url: null, cover_image_preview_url: null, cover_image_thumbnail_url: null }];
      const q = url.searchParams.get("q")?.toLowerCase() ?? "";
      return route.fulfill({ json: paged(rows.filter((row) => row.name.toLowerCase().includes(q))) });
    }
    if (path.endsWith("/facts")) {
      if (path.includes("merchant-b") && path.includes("product-a")) return route.fulfill({ status: 404, json: { detail: "Product not found" } });
      if (path.includes("product-b")) return route.fulfill({ json: { product: product("product-b", "Lamp B"), fact_set: null } });
      if (route.request().method() === "PUT") {
        state.saves++;
        const body = route.request().postDataJSON();
        expect(body).not.toHaveProperty("update_node_ids");
        state.actions.push({ id: `action-${state.saves}`, actor_user_id: "operator", actor_name: "Operator", merchant_id: "merchant-a", product_id: "product-a", product_name: body.name, action: "product.facts.update", created_at: created, result: state.saves === 1 ? "rejected" : "succeeded", failure_reason: state.saves === 1 ? "Version conflict" : null });
        if (state.saves === 1) { state.facts = factResponse(2, "Updated elsewhere"); return route.fulfill({ status: 409, json: { detail: "Version conflict" } }); }
        expect(body.expected_fact_version).toBe(2);
        expect(body.facts[0].value).toEqual({ width: 20, unit: "cm" });
        expect(body.facts[0].evidence_asset_ids).toEqual(["asset-a"]);
        state.facts = factResponse(3, body.name);
      }
      return route.fulfill({ json: state.facts });
    }
    if (path.endsWith("/image-assets")) return route.fulfill({ json: { items: url.searchParams.has("after") ? [] : [asset], next_cursor: url.searchParams.has("after") ? null : "next+/=" } });
    if (path.endsWith("/tasks")) return route.fulfill({ json: paged(taskRows.filter((task) => !url.searchParams.get("product_id") || task.product_id === url.searchParams.get("product_id"))) });
    if (path.endsWith("/actions")) return route.fulfill({ json: paged(state.actions) });
    if (path.endsWith("/quota/events")) return route.fulfill({ json: paged(state.events) });
    if (path.endsWith("/quota/adjust")) {
      const body = route.request().postDataJSON();
      state.adjustments.push(body);
      if (state.adjustments.length === 1) {
        state.events.push({ id: "event-1", merchant_id: "merchant-a", hold_id: null, event_type: "adjust", amount_units: body.delta_units, available_after: 100 + body.delta_units, reserved_after: 0, reason: body.reason, actor_user_id: "operator", created_at: created });
        return route.fulfill({ status: 503, json: { detail: "Response lost" } });
      }
      expect(body).toEqual(state.adjustments[0]);
      return route.fulfill({ json: { merchant_id: "merchant-a", currency: "quota", available_units: 110, reserved_units: 0, price_version_id: "price" } });
    }
    if (path.endsWith("/quota")) return route.fulfill({ json: { merchant_id: "merchant-a", currency: "quota", available_units: state.events.length ? 110 : 100, reserved_units: 0, price_version_id: "price" } });
    if (route.request().method() === "DELETE") {
      state.deletes++;
      if (state.deletes === 1) return route.fulfill({ status: 503, json: { detail: "Delete unavailable" } });
      state.deleted = true;
      state.actions.push({ id: "delete-1", actor_user_id: "operator", actor_name: "Operator", merchant_id: "merchant-a", product_id: "product-a", product_name: state.facts.product.name, action: "product.delete", created_at: created, result: "succeeded", failure_reason: null });
      return route.fulfill({ status: 204 });
    }
    return route.fulfill({ status: 404, json: { detail: "Not found" } });
  });
  return state;
}

async function widthCheck(page: Page, width: number) {
  const value = await page.evaluate(() => ({ inner: innerWidth, client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
  expect(value.inner).toBe(width);
  expect(value.scroll).toBeLessThanOrEqual(value.client);
}

for (const locale of LOCALES) for (const width of [390, 1440]) for (const theme of ["light", "dark"]) {
  test(`ops merchant product quota ${locale} ${width} ${theme}`, async ({ page }, testInfo) => {
    const t = (key: Parameters<typeof translate>[1]) => translate(locale, key);
    await page.setViewportSize({ width, height: 960 });
    await page.addInitScript(({ locale, theme }) => { localStorage.setItem("productflow.locale", locale); localStorage.setItem("productflow.theme", theme); }, { locale, theme });
    const state = await mockOps(page, false, { locale, theme: theme as "light" | "dark" });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto("/ops");
    await expect(page.getByRole("heading", { name: t("ops.merchants"), exact: true })).toBeVisible();
    await widthCheck(page, width);
    await page.screenshot({ path: testInfo.outputPath("directory.png"), fullPage: true });
    await page.getByLabel(t("ops.searchMerchants"), { exact: true }).fill("Merchant A");
    await page.getByRole("button", { name: t("ops.search"), exact: true }).click();
    await expect(page.getByRole("link", { name: "Merchant B", exact: true })).toHaveCount(0);
    await page.getByRole("link", { name: merchants[0].name, exact: true }).click();
    await page.getByRole("link", { name: /Lamp A/ }).click();
    const name = page.getByLabel(t("create.productName"), { exact: true });
    await name.fill("Edited A");
    await page.getByRole("button", { name: t("graph.inspector.productFactsSave"), exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText(t("ops.conflict"));
    await expect(name).toHaveValue("Edited A");
    await page.getByRole("tab", { name: t("ops.actions"), exact: true }).click();
    await expect(page.getByText(t("ops.rejected"), { exact: true })).toBeVisible();
    await page.getByRole("tab", { name: t("graph.inspector.productFacts"), exact: true }).click();
    await expect(name).toHaveValue("Edited A");
    await page.getByRole("button", { name: t("ops.discard"), exact: true }).click();
    await expect(name).toHaveValue("Updated elsewhere");
    await name.fill("Edited A");
    await page.getByRole("button", { name: t("graph.inspector.productFactsSave"), exact: true }).click();
    await expect(page.getByRole("status")).toHaveText(t("graph.inspector.productFactsSaved"));
    await widthCheck(page, width);
    await page.screenshot({ path: testInfo.outputPath("product.png"), fullPage: true });
    await page.getByRole("tab", { name: t("ops.media"), exact: true }).click();
    await page.getByRole("button", { name: `${t("graph.results.preview")}: Lamp reference`, exact: true }).click();
    await expect(page.getByRole("dialog").getByRole("img")).toBeVisible();
    await page.getByRole("button", { name: t("account.cancel"), exact: true }).click();
    const download = page.waitForEvent("download");
    await page.getByRole("button", { name: t("gallery.download"), exact: true }).click();
    expect((await download).suggestedFilename()).toBe("lamp.png");
    await page.getByRole("button", { name: t("account.next"), exact: true }).click();
    await expect(page.getByText(t("ops.empty"), { exact: true })).toBeVisible();
    await page.getByRole("button", { name: t("account.previous"), exact: true }).click();
    await expect(page.getByText("Lamp reference", { exact: true })).toBeVisible();
    await page.getByRole("tab", { name: t("ops.tasks"), exact: true }).click();
    await expect(page.getByText(t("globalAgent.taskStatus.waitingUser"), { exact: true })).toBeVisible();
    await expect(page.getByText(t("status.draft"), { exact: true })).toBeVisible();
    await expect(page.getByText("Continuous image", { exact: true })).toHaveCount(0);
    await page.getByRole("link", { name: merchants[0].name, exact: true }).click();
    await page.getByRole("tab", { name: t("ops.quota"), exact: true }).click();
    await page.getByLabel(t("ops.delta"), { exact: true }).fill("10");
    await page.getByLabel(t("ops.reason"), { exact: true }).fill("Correction");
    await page.getByRole("button", { name: t("ops.adjust"), exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText("Response lost");
    await page.getByRole("tab", { name: t("ops.actions"), exact: true }).click();
    await page.getByRole("tab", { name: t("ops.quota"), exact: true }).click();
    await expect(page.getByLabel(t("ops.delta"), { exact: true })).toBeDisabled();
    await page.getByRole("button", { name: t("account.retry"), exact: true }).click();
    await expect(page.getByRole("status")).toHaveText(t("ops.adjusted"));
    expect(state.adjustments).toHaveLength(2);
    expect(state.events).toHaveLength(1);
    await expect(page.getByRole("listitem").getByText("Correction", { exact: true })).toBeVisible();
    await widthCheck(page, width);
    await page.screenshot({ path: testInfo.outputPath("quota.png"), fullPage: true });
    await page.getByRole("tab", { name: t("ops.products"), exact: true }).click();
    await page.getByRole("link", { name: /Edited A/ }).click();
    await page.getByRole("button", { name: t("ops.productDelete"), exact: true }).click();
    await page.getByRole("dialog").getByRole("button", { name: t("ops.productDelete"), exact: true }).click();
    await expect(page.getByRole("dialog").getByRole("alert")).toHaveText("Delete unavailable");
    await page.getByRole("dialog").getByRole("button", { name: t("ops.productDelete"), exact: true }).click();
    await expect(page).toHaveURL(/\/ops\/merchants\/merchant-a$/);
    await expect(page.getByText(t("ops.empty"), { exact: true })).toBeVisible();
    expect(errors).toEqual([]);
  });
}

test("ordinary accounts have no operations entry and direct routes remain closed", async ({ page }) => {
  const state = await mockOps(page, true);
  await page.goto("/ops/merchants/merchant-b/products/product-b");
  await expect(page).toHaveURL(/\/account$/);
  await expect(page.getByRole("link", { name: "运营管理", exact: true })).toHaveCount(0);
  expect(state.requested).toEqual([]);
});

test("merchant B stays explicit and foreign product ids surface denial", async ({ page }) => {
  await mockOps(page);
  await page.goto("/ops/merchants/merchant-b");
  await expect(page.getByRole("heading", { name: "Merchant B", exact: true })).toBeVisible();
  await page.getByRole("link", { name: /Lamp B/ }).click();
  await expect(page.getByLabel("商品名称", { exact: true })).toHaveValue("Lamp B");
  await expect(page.getByText("所属商家: Merchant B", { exact: false })).toBeVisible();
  await page.goto("/ops/merchants/merchant-b/products/product-a");
  await expect(page.getByRole("alert")).toHaveText("Product not found");
  await expect(page.getByRole("button", { name: "删除商品", exact: true })).toBeDisabled();
});

test("independent operator login lands in operations and mobile navigation contains only usable entries", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockOps(page);
  let authenticated = false;
  await page.route("**/api/auth/session", (route) => {
    if (route.request().method() === "POST") { authenticated = true; return route.fulfill({ json: { ok: true } }); }
    return route.fulfill({ json: authenticated ? { authenticated, preferences: { locale: "zh-CN", theme: "system" }, access_required: true, user: { id: "operator", email: "ops@example.com", display_name: "Operator", is_operator: true }, merchant: null } : { authenticated, access_required: true } });
  });
  await page.goto("/login");
  await page.getByLabel("邮箱", { exact: true }).fill("ops@example.com");
  await page.getByLabel("密码", { exact: true }).fill("password123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).toHaveURL(/\/ops$/);
  for (const href of ["/products", "/image-chat", "/media-library"]) await expect(page.locator(`a[href="${href}"]`)).toHaveCount(0);
  for (const href of ["/home", "/help", "/account", "/ops", "/settings"]) await expect(page.locator(`a[href="${href}"]`).last()).toBeVisible();
  await page.goto("/login");
  await expect(page).toHaveURL(/\/ops$/);
});

test("late media pages cannot replace a different merchant product and pagination resets", async ({ page }) => {
  await mockOps(page);
  let release: () => void = () => {};
  const wait = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/merchant-a/products/product-a/image-assets?*", async (route) => {
    if (new URL(route.request().url()).searchParams.has("after")) { await wait; return route.fulfill({ json: { items: [asset], next_cursor: null } }); }
    return route.fulfill({ json: { items: [asset], next_cursor: "page-2" } });
  });
  const bReads: string[] = [];
  await page.route("**/merchant-b/products/product-b/image-assets?*", (route) => { bReads.push(route.request().url()); return route.fulfill({ json: { items: [], next_cursor: null } }); });
  await page.goto("/ops/merchants/merchant-a/products/product-a");
  await page.getByRole("tab", { name: "商品图片", exact: true }).click();
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("加载中");
  await page.evaluate(() => { history.pushState({}, "", "/ops/merchants/merchant-b/products/product-b"); dispatchEvent(new PopStateEvent("popstate")); });
  await expect(page.getByLabel("商品名称", { exact: true })).toHaveValue("Lamp B");
  await page.getByRole("tab", { name: "商品图片", exact: true }).click();
  await expect(page.getByText("暂无记录", { exact: true })).toBeVisible();
  release();
  await expect(page.getByText("Lamp reference", { exact: true })).toHaveCount(0);
  expect(bReads).toHaveLength(1);
  expect(new URL(bReads[0]).searchParams.has("after")).toBe(false);
});

test("read errors recover and rejected media downloads show their authorization error", async ({ page }) => {
  await mockOps(page);
  let reads = 0;
  await page.route("**/api/ops/merchants/merchant-a", (route) => { reads++; return route.fulfill(reads === 1 ? { status: 503, json: { detail: "Merchant unavailable" } } : { json: merchants[0] }); });
  await page.goto("/ops/merchants/merchant-a");
  await expect(page.getByRole("alert")).toHaveText("Merchant unavailable");
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await page.getByRole("link", { name: /Lamp A/ }).click();
  await page.getByRole("tab", { name: "商品图片", exact: true }).click();
  await page.route("**/product-image-assets/asset-a/download", (route) => route.fulfill({ status: 403, json: { detail: "Asset access denied" } }));
  await page.getByRole("button", { name: "下载原图", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("Asset access denied");
});

test.describe("operations touch and keyboard", () => {
  test.use({ hasTouch: true, viewport: { width: 390, height: 844 }, contextOptions: { reducedMotion: "reduce" } });
  test("merchant selection, tabs, filtering, and adjustment validation", async ({ page }) => {
    await mockOps(page);
    await page.goto("/ops");
    await page.getByRole("combobox", { name: "全部状态", exact: true }).tap();
    await page.getByRole("option", { name: "已停用", exact: true }).tap();
    await expect(page.getByRole("link", { name: merchants[0].name, exact: true })).toHaveCount(0);
    await page.getByRole("combobox", { name: "全部状态", exact: true }).tap();
    await page.getByRole("option", { name: "全部状态", exact: true }).tap();
    await page.getByRole("link", { name: merchants[0].name, exact: true }).tap();
    await page.getByRole("tab", { name: "额度记录", exact: true }).tap();
    await page.getByLabel("调整数量（正数增加，负数扣减）", { exact: true }).fill("0");
    await page.getByLabel("操作原因", { exact: true }).fill("Correction");
    await page.getByRole("button", { name: "调整额度", exact: true }).tap();
    await expect(page.getByRole("alert")).toHaveText("请输入非零整数和操作原因。");
    await page.getByRole("tab", { name: "额度记录", exact: true }).focus();
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("tab", { name: "操作记录", exact: true })).toBeFocused();
  });
});

test("all operations lists request bounded pages and search resets product pagination", async ({ page }) => {
  await mockOps(page);
  const reads: string[] = [];
  const pageOf = (url: URL) => Number(url.searchParams.get("page") ?? "1");
  await page.route("**/api/ops/merchants?*", (route) => {
    const url = new URL(route.request().url()); reads.push(url.href);
    return route.fulfill({ json: { items: [merchants[pageOf(url) === 1 ? 0 : 1]], total: 21, page: pageOf(url), page_size: 20 } });
  });
  await page.route("**/merchant-a/products?*", (route) => {
    const url = new URL(route.request().url()); reads.push(url.href);
    return route.fulfill({ json: { items: [{ ...product("product-a", pageOf(url) === 1 ? "Lamp A" : "Second page product"), cover_image_filename: null, cover_image_download_url: null, cover_image_preview_url: null, cover_image_thumbnail_url: null }], total: 21, page: pageOf(url), page_size: 20 } });
  });
  for (const suffix of ["tasks", "actions", "quota/events"]) await page.route(`**/merchant-a/${suffix}?*`, (route) => {
    const url = new URL(route.request().url()); reads.push(url.href);
    const number = pageOf(url);
    const row = suffix === "tasks" ? { ...taskRows[0], title: `Task page ${number}` }
      : suffix === "actions" ? { id: `action-${number}`, actor_user_id: "operator", actor_name: "Operator", merchant_id: "merchant-a", product_id: "product-a", product_name: `Action page ${number}`, action: "product.facts.update", created_at: created, result: "succeeded", failure_reason: null }
      : { id: `event-${number}`, merchant_id: "merchant-a", hold_id: null, event_type: "adjust", amount_units: 1, available_after: 100, reserved_after: 0, reason: `Event page ${number}`, actor_user_id: "operator", created_at: created };
    return route.fulfill({ json: { items: [row], total: 21, page: number, page_size: 20 } });
  });
  await page.goto("/ops");
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await expect(page.getByRole("link", { name: "Merchant B", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "上一页", exact: true }).click();
  await page.getByRole("link", { name: merchants[0].name, exact: true }).click();
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await expect(page.getByRole("link", { name: /Second page product/ })).toBeVisible();
  await page.getByLabel("搜索商品名称", { exact: true }).fill("Lamp");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await expect(page.getByRole("link", { name: /Lamp A/ })).toBeVisible();
  for (const [tab, label] of [["任务记录", "Task page 2"], ["操作记录", "Action page 2"], ["额度记录", "Event page 2"]]) {
    await page.getByRole("tab", { name: tab, exact: true }).click();
    await page.getByRole("button", { name: "下一页", exact: true }).click();
    await expect(page.getByText(label, { exact: true })).toBeVisible();
  }
  expect(reads.every((url) => new URL(url).searchParams.get("page_size") === "20")).toBe(true);
  expect(reads.some((url) => new URL(url).searchParams.get("q") === "Lamp" && new URL(url).searchParams.get("page") === "1")).toBe(true);
});

test("merchant Dock is absent for independent operators on every page", async ({ page }) => {
  await mockOps(page);
  for (const path of ["/ops", "/ops/merchants/merchant-a", "/account"]) {
    await page.goto(path);
    await expect(page.locator("nav").first()).toBeVisible();
    await expect(page.locator("[data-global-agent-dock]")).toHaveCount(0);
  }
});

test("merchant Dock remains on ordinary merchant pages", async ({ page }) => {
  await mockOps(page, true);
  await page.route("**/api/v2/products?*", (route) => route.fulfill({ json: { items: [], total: 0, page: 1, page_size: 20 } }));
  await page.goto("/products");
  await expect(page.locator("[data-global-agent-launcher]")).toBeVisible();
});

test("merchant-owning operator Dock unmounts when entering operations", async ({ page }) => {
  await mockOps(page);
  await page.route("**/api/auth/session", (route) => route.fulfill({ json: { authenticated: true, preferences: { locale: "zh-CN", theme: "system" }, access_required: true, user: { id: "operator", email: "ops@example.com", display_name: "Operator", is_operator: true }, merchant: merchants[0] } }));
  await page.goto("/account");
  await expect(page.locator("[data-global-agent-launcher]")).toBeVisible();
  await page.locator('nav a[href="/ops"]').first().click();
  await expect(page).toHaveURL(/\/ops$/);
  await expect(page.locator("[data-global-agent-dock]")).toHaveCount(0);
  await page.getByRole("link", { name: merchants[0].name, exact: true }).click();
  await expect(page).toHaveURL(/\/ops\/merchants\/merchant-a$/);
  await expect(page.locator("[data-global-agent-dock]")).toHaveCount(0);
  await page.getByRole("link", { name: /Lamp A/ }).click();
  await expect(page.getByRole("tab", { name: "商品图片", exact: true })).toBeVisible();
  await expect(page.locator("[data-global-agent-dock]")).toHaveCount(0);
});
