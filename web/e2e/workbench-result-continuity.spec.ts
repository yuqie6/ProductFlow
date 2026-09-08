import { expect, test, type Page, type Route } from "@playwright/test";
import { readFileSync } from "node:fs";

import type {
  CanonicalProductDetail,
  DeliveryAdoptionCreateInput,
  DeliveryAdoptionPreview,
  DeliveryAdoptionSlot,
  DeliveryAdoptionVersion,
  GraphNodeCatalog,
  GraphProjection,
  SessionState,
} from "../src/lib/types";
import { REFERENCE_PRODUCT_IMAGE } from "./liveGraph";

type Locale = "zh-CN" | "en-US" | "ja-JP" | "vi-VN";
type Theme = "light" | "dark";

const PRODUCT_ONE = "policy-product-1";
const NODE_ID = "quality-image";
const ASSET_ID = "quality-asset";
const GRAPH_ID = "quality-graph";

const DELIVERY_SPEC = {
  width: 800,
  height: 800,
  format: "png" as const,
  fit: "contain" as const,
};

const PREVIEW_LABELS: Record<Locale, string> = {
  "zh-CN": "预览图片",
  "en-US": "Preview image",
  "ja-JP": "画像をプレビュー",
  "vi-VN": "Xem trước ảnh",
};

const SESSION: SessionState = {
  authenticated: true, preferences: { locale: "zh-CN", theme: "system" },
  access_required: false,
  needs_bootstrap: false,
  user: {
    id: "policy-user",
    email: "policy@example.com",
    display_name: "Policy test",
    is_operator: true,
  },
  merchant: { id: "policy-merchant", name: "Policy merchant", status: "active" },
};

const CATALOG: GraphNodeCatalog = {
  version: 1,
  nodes: [{
    node_type: "image_generation",
    output_data_type: "image_asset",
    kind: "effect",
    accepts: [],
  }],
};

interface AdoptionRequest {
  productId: string;
  body: DeliveryAdoptionCreateInput;
}

interface PolicyMockState {
  adoptionRequests: AdoptionRequest[];
  currentByProduct: Map<string, DeliveryAdoptionVersion>;
  ordinary409Next: boolean;
  writes: string[];
  exportVersions: string[];
  ensureBodies: Array<Record<string, unknown>>;
  previewBodies: Array<Record<string, unknown>>;
  exportBodies: Array<Record<string, unknown>>;
}

function product(productId: string): CanonicalProductDetail {
  return {
    id: productId,
    name: `策略测试商品 ${productId.slice(-1)}`,
    category: "商品",
    price: null,
    source_note: "浏览器策略 mock",
    cover_image_asset_id: null,
    intake: null,
    created_at: "2026-09-08T00:00:00Z",
    updated_at: "2026-09-08T00:00:00Z",
  };
}

function graph(productId: string): GraphProjection {
  return {
    id: GRAPH_ID,
    product_id: productId,
    title: "策略质量工作流",
    schema_version: 3,
    revision: 7,
    last_operation_group_id: null,
    can_undo: false,
    can_redo: false,
    nodes: [{
      id: NODE_ID,
      node_type: "image_generation",
      title: "质量提示主图",
      position_x: 100,
      position_y: 100,
      config: {
        image_type_key: "hero",
        delivery_spec: DELIVERY_SPEC,
      },
      bound_asset_id: null,
      group_id: null,
      preview_asset_id: ASSET_ID,
      config_status: "ready",
      unused: false,
      current_artifact_payload: {
        text_trace: { text_qualified: false },
        produce_route: { route_qualified: true },
      },
      incoming: [],
      outgoing: [],
    }],
    edges: [],
    groups: [],
  };
}

function adoption(productId: string, body: DeliveryAdoptionCreateInput): DeliveryAdoptionVersion {
  const input = body.slots[0];
  const slot: DeliveryAdoptionSlot = {
    id: "adopted-slot",
    slot_key: input.slot_key,
    sort_order: input.sort_order,
    image_type_key: input.image_type_key ?? null,
    source_asset_id: input.source_asset_id,
    source_node_id: input.source_node_id ?? null,
    delivery_spec: input.delivery_spec,
    delivery_spec_hash: "quality-spec-hash",
    quality_status: "fail",
    quality_detail: "服务端保留：文字追溯检查未通过",
    text_overflow: true,
    qualified: false,
  };
  return {
    id: "adoption-version-1",
    product_id: productId,
    version: 1,
    is_current: true,
    graph_id: body.graph_id ?? null,
    graph_revision: body.graph_revision ?? null,
    fact_set_version_id: null,
    visual_system_version_id: null,
    notes: null,
    slots: [slot],
    created_at: "2026-09-08T00:00:00Z",
  };
}

function preview(productId: string, versionId: string): DeliveryAdoptionPreview {
  return {
    version_id: versionId,
    product_id: productId,
    complete: true,
    issues: [],
    items: [{
      slot_key: NODE_ID,
      sort_order: 0,
      filename: "quality-image.png",
      source_asset_id: ASSET_ID,
      delivery_spec_hash: "quality-spec-hash",
      qualified: false,
      rendition_job_id: "rendition-1",
      rendition_status: "succeeded",
      result_asset_id: ASSET_ID,
    }],
    export_ready: true,
    allow_partial: false,
  };
}

async function json(route: Route, status: number, payload: unknown): Promise<void> {
  await route.fulfill({ status, contentType: "application/json", body: JSON.stringify(payload) });
}

function productIdFromPath(pathname: string): string | null {
  const match = pathname.match(/^\/api\/v[23]\/products\/([^/]+)/);
  return match ? decodeURIComponent(match[1]) : null;
}

async function installPolicyMock(page: Page): Promise<PolicyMockState> {
  const state: PolicyMockState = {
    adoptionRequests: [],
    writes: [],
    exportVersions: [],
    currentByProduct: new Map(),
    ordinary409Next: false,
    ensureBodies: [],
    previewBodies: [],
    exportBodies: [],
  };
  const imageBody = readFileSync(REFERENCE_PRODUCT_IMAGE);

  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;
    const productId = productIdFromPath(pathname);
    if (request.method() !== "GET") state.writes.push(pathname);

    if (pathname === "/api/auth/session") {
      await json(route, 200, SESSION);
      return;
    }
    if (request.method() === "GET" && pathname === "/api/v3/node-catalog") {
      await json(route, 200, CATALOG);
      return;
    }
    if (request.method() === "GET" && pathname.endsWith("/agent-workbench")) {
      await json(route, 409, { detail: "此测试商品没有 Agent 对话" });
      return;
    }
    if (request.method() === "GET" && pathname.endsWith("/workflows/current") && productId) {
      await json(route, 200, graph(productId));
      return;
    }
    if (request.method() === "GET" && pathname.includes("/workflows/") && pathname.endsWith("/runs")) {
      await json(route, 200, { items: [] });
      return;
    }
    if (request.method() === "GET" && pathname.startsWith("/api/v2/products/") && productId) {
      await json(route, 200, product(productId));
      return;
    }
    if (request.method() === "GET" && pathname.endsWith("/delivery-adoptions/current") && productId) {
      const current = state.currentByProduct.get(productId);
      if (current) {
        await json(route, 200, current);
      } else {
        await json(route, 404, { detail: "没有交付采用版本" });
      }
      return;
    }
    if (request.method() === "POST" && pathname.match(/\/delivery-adoptions$/) && productId) {
      const body = request.postDataJSON() as DeliveryAdoptionCreateInput;
      state.adoptionRequests.push({ productId, body });
      if (state.ordinary409Next) {
        state.ordinary409Next = false;
        await json(route, 409, { code: "adoption_graph_revision_conflict", detail: "普通采用冲突" });
        return;
      }
      if (!body.acknowledge_quality_warnings) {
        await json(route, 409, {
          code: "adoption_quality_confirmation_required",
          detail: "图位 quality-image：文字追溯检查未通过",
        });
        return;
      }
      const nextVersion = (state.currentByProduct.get(productId)?.version ?? 0) + 1;
      const version = { ...adoption(productId, body), id: `adoption-version-${nextVersion}`, version: nextVersion };
      state.currentByProduct.set(productId, version);
      await json(route, 201, version);
      return;
    }
    if (request.method() === "POST" && pathname.endsWith("/renditions") && productId) {
      state.ensureBodies.push((request.postDataJSON() ?? {}) as Record<string, unknown>);
      const versionId = pathname.split("/").at(-2) ?? "adoption-version-1";
      await json(route, 202, { preview: preview(productId, versionId) });
      return;
    }
    if (request.method() === "POST" && pathname.endsWith("/preview") && productId) {
      state.previewBodies.push((request.postDataJSON() ?? {}) as Record<string, unknown>);
      const versionId = pathname.split("/").at(-2) ?? "adoption-version-1";
      await json(route, 200, preview(productId, versionId));
      return;
    }
    if (request.method() === "POST" && pathname.endsWith("/export") && productId) {
      state.exportVersions.push(pathname.split("/").at(-2)!);
      state.exportBodies.push((request.postDataJSON() ?? {}) as Record<string, unknown>);
      await route.fulfill({
        status: 200,
        contentType: "application/zip",
        headers: { "Content-Disposition": "attachment; filename=delivery-adoption.zip" },
        body: Buffer.from("PK\\x03\\x04policy-mock"),
      });
      return;
    }
    if (pathname.includes("/product-image-assets/") && pathname.endsWith("/download")) {
      await route.fulfill({ status: 200, contentType: "image/png", body: imageBody });
      return;
    }
    if (pathname.startsWith("/api/v2/agent-sessions") || pathname.startsWith("/api/v2/agent-tasks")) {
      await json(route, 200, { items: [], next_cursor: null });
      return;
    }

    // Keep unrelated lazy workbench queries quiet while the policy flow is under test.
    await json(route, 200, { items: [], next_cursor: null });
  });

  state.currentByProduct.set(PRODUCT_ONE, adoption(PRODUCT_ONE, {
    graph_id: GRAPH_ID, graph_revision: 1, acknowledge_quality_warnings: true,
    slots: [{ slot_key: NODE_ID, source_node_id: NODE_ID, source_asset_id: "older-asset", sort_order: 0, delivery_spec: DELIVERY_SPEC }],
  }));
  return state;
}

async function openResults(page: Page, productId: string, locale: Locale, theme: Theme, width: number): Promise<void> {
  await page.route("**/api/auth/session", (route) => route.fulfill({ json: { ...SESSION, preferences: { locale, theme } } }));
  await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
  await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
  await page.addInitScript(({ nextLocale, nextTheme }: { nextLocale: Locale; nextTheme: Theme }) => {
    window.localStorage.setItem("productflow.locale", nextLocale);
    window.localStorage.setItem("productflow.theme", nextTheme);
  }, { nextLocale: locale, nextTheme: theme });
  await page.goto(`/products/${productId}`);
  await expect(page.locator("[data-graph-results-view]")).toBeVisible();
  await expect(page.locator(`[data-graph-result-item="${NODE_ID}"]`)).toBeVisible();
}


for (const locale of ["zh-CN", "en-US", "ja-JP", "vi-VN"] as const) {
  for (const width of [1440, 390]) {
    test(`preview and adopted export ${locale} ${width}`, async ({ browser }, testInfo) => {
      const context = await browser.newContext({ viewport: { width, height: 900 }, hasTouch: width === 390 });
      const page = await context.newPage();
      const errors: string[] = [];
      page.on("pageerror", error => errors.push(error.message));
      const mock = await installPolicyMock(page);
      await openResults(page, PRODUCT_ONE, locale, width === 390 ? "dark" : "light", width);
      const card = page.locator(`[data-graph-result-item="${NODE_ID}"]`);
      const warning = page.locator("[data-graph-results-adoption-stale]");
      await expect(warning).toBeVisible();
      await expect(card).toHaveAttribute("data-graph-result-delivery-adopted", "false");
      const selectionBeforePreview = await card.getAttribute("class");
      const previewButton = card.getByRole("button", { name: PREVIEW_LABELS[locale], exact: true });
      if (width === 390) await previewButton.tap();
      else { await previewButton.focus(); await page.keyboard.press("Enter"); }
      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible();
      await expect(dialog.locator("img")).toHaveAttribute("src", new RegExp(ASSET_ID));
      await expect(dialog.locator("a")).toHaveAttribute("href", new RegExp(ASSET_ID));
      expect(mock.writes).toEqual([]);
      expect(await card.getAttribute("class")).toBe(selectionBeforePreview);
      await page.screenshot({ path: testInfo.outputPath("preview.png") });
      await dialog.locator("button").first().focus();
      await page.keyboard.press("Enter");
      await expect(dialog).toBeHidden();
      await card.locator("button").first().click();
      await expect(card).toHaveClass(/border-accent/);
      await expect(dialog).toBeHidden();
      const geometry = await warning.evaluate(element => {
        const r = element.getBoundingClientRect();
        return { inner: innerWidth, client: document.documentElement.clientWidth, left: r.left, right: r.right, scroll: element.scrollWidth, width: element.clientWidth };
      });
      expect(geometry.inner).toBe(width);
      expect(geometry.left).toBeGreaterThanOrEqual(0);
      expect(geometry.right).toBeLessThanOrEqual(geometry.client);
      expect(geometry.scroll).toBeLessThanOrEqual(geometry.width);
      const cardBounds = await card.boundingBox();
      for (const button of await card.locator("button").all()) {
        const bounds = await button.boundingBox();
        if (bounds && cardBounds) {
          expect(bounds.x).toBeGreaterThanOrEqual(cardBounds.x);
          expect(bounds.x + bounds.width).toBeLessThanOrEqual(cardBounds.x + cardBounds.width);
        }
      }
      await page.screenshot({ path: testInfo.outputPath("results.png") });
      const download = page.waitForEvent("download");
      await page.locator("[data-graph-results-export-adoption]").click();
      await download;
      expect(mock.exportVersions).toEqual(["adoption-version-1"]);
      expect(mock.adoptionRequests).toEqual([]);
      await expect(dialog).toBeHidden();
      await card.locator("[data-graph-result-adopt]").click();
      await expect(dialog).toBeVisible();
      await dialog.locator("button").last().click();
      await expect(dialog).toBeHidden();
      await expect(warning).toBeHidden();
      expect(mock.adoptionRequests.at(-1)?.body.slots[0].source_asset_id).toBe(ASSET_ID);
      await expect(card).toHaveAttribute("data-graph-result-delivery-adopted", "true");
      const newDownload = page.waitForEvent("download");
      await page.locator("[data-graph-results-export-adoption]").click();
      await newDownload;
      expect(mock.exportVersions).toEqual(["adoption-version-1", "adoption-version-2"]);
      expect(errors).toEqual([]);
      await context.close();
    });
  }
}

test("32 changed slots keep narrow-screen results usable", async ({ browser }, testInfo) => {
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true });
  const page = await context.newPage();
  const mock = await installPolicyMock(page);
  const base = graph(PRODUCT_ONE);
  const nodes = Array.from({ length: 32 }, (_, index) => ({ ...base.nodes[0], id: index === 0 ? NODE_ID : `image-${index}`, title: `商品图片 ${index + 1}` }));
  await page.route("**/workflows/current", route => json(route, 200, { ...base, nodes }));
  const previous = mock.currentByProduct.get(PRODUCT_ONE)!;
  mock.currentByProduct.set(PRODUCT_ONE, { ...previous, slots: nodes.map((node, index) => ({ ...previous.slots[0], id: `slot-${index}`, slot_key: node.id, source_node_id: node.id, sort_order: index })) });
  await openResults(page, PRODUCT_ONE, "zh-CN", "dark", 390);
  const warning = page.locator("[data-graph-results-adoption-stale]");
  const issues = warning.locator("ul");
  await expect(issues.locator("li")).toHaveCount(32);
  const bounds = await issues.evaluate(element => ({ height: element.clientHeight, scroll: element.scrollHeight, inner: innerWidth, client: document.documentElement.clientWidth }));
  expect(bounds.inner).toBe(390);
  expect(bounds.client).toBe(390);
  expect(bounds.height).toBeLessThanOrEqual(96);
  expect(bounds.scroll).toBeGreaterThan(bounds.height);
  await issues.focus();
  await page.keyboard.press("End");
  await expect.poll(() => issues.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
  const preview = page.locator(`[data-graph-result-item="${NODE_ID}"] [data-graph-result-preview]`);
  await expect(preview).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath("32-slots.png") });
  await preview.tap();
  await expect(page.getByRole("dialog")).toBeVisible();
  expect(mock.writes).toEqual([]);
  await context.close();
});

test("unlinked adopted slot reports uncertainty and remains exportable", async ({ page }, testInfo) => {
  const mock = await installPolicyMock(page);
  const previous = mock.currentByProduct.get(PRODUCT_ONE)!;
  mock.currentByProduct.set(PRODUCT_ONE, { ...previous, slots: [{ ...previous.slots[0], slot_key: "custom-hero", source_node_id: null }] });
  await openResults(page, PRODUCT_ONE, "zh-CN", "light", 1440);
  await expect(page.locator('[data-graph-results-adoption-issue="unlinked_node"]')).toContainText("无法关联当前图位");
  await expect(page.locator('[data-graph-results-adoption-issue="deleted_node"]')).toHaveCount(0);
  const download = page.waitForEvent("download");
  await page.locator("[data-graph-results-export-adoption]").click();
  await download;
  expect(mock.exportVersions).toEqual(["adoption-version-1"]);
  expect(mock.adoptionRequests).toEqual([]);
  await page.screenshot({ path: testInfo.outputPath("unlinked-slot.png") });
});
