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
const PRODUCT_TWO = "policy-product-2";
const NODE_ID = "quality-image";
const ASSET_ID = "quality-asset";
const GRAPH_ID = "quality-graph";

const DELIVERY_SPEC = {
  width: 800,
  height: 800,
  format: "png" as const,
  fit: "contain" as const,
};

const QUALITY_LABELS: Record<Locale, string> = {
  "zh-CN": "有限检查未通过",
  "en-US": "Finite checks failed",
  "ja-JP": "有限チェックに不合格",
  "vi-VN": "Kiểm tra giới hạn không đạt",
};

const SESSION: SessionState = {
  authenticated: true,
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
      const version = adoption(productId, body);
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

  return state;
}

async function openResults(page: Page, productId: string, locale: Locale, theme: Theme, width: number): Promise<void> {
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

test("quality confirmation keeps the frozen product body and exports confirmed nonqualified output", async ({ page }) => {
  const mock = await installPolicyMock(page);
  await openResults(page, PRODUCT_ONE, "zh-CN", "light", 1440);

  const card = page.locator(`[data-graph-result-item="${NODE_ID}"]`);
  const adoptButton = card.locator("[data-graph-result-adopt]");
  await expect(adoptButton).toBeEnabled();
  await adoptButton.click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("文字追溯检查未通过");
  expect(mock.adoptionRequests).toHaveLength(1);
  expect(mock.adoptionRequests[0].body.acknowledge_quality_warnings).toBe(false);

  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toBeHidden();
  expect(mock.adoptionRequests).toHaveLength(1);

  mock.ordinary409Next = true;
  await adoptButton.click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("alert")).toContainText("普通采用冲突");
  expect(mock.adoptionRequests).toHaveLength(2);

  await adoptButton.click();
  await expect(dialog).toBeVisible();
  expect(mock.adoptionRequests).toHaveLength(3);
  const frozenBody = mock.adoptionRequests[2].body;

  await page.goto(`/products/${PRODUCT_TWO}`);
  await expect(page.locator("[data-graph-results-view]")).toBeVisible();
  await expect(dialog).toBeHidden();
  expect(mock.adoptionRequests).toHaveLength(3);

  await openResults(page, PRODUCT_ONE, "zh-CN", "light", 1440);
  const restoredCard = page.locator(`[data-graph-result-item="${NODE_ID}"]`);
  await restoredCard.locator("[data-graph-result-adopt]").click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByRole("dialog").getByRole("button", { name: "确认采用", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeHidden();
  expect(mock.adoptionRequests).toHaveLength(5);
  expect(mock.adoptionRequests[3]).toEqual({ productId: PRODUCT_ONE, body: frozenBody });
  expect(mock.adoptionRequests[4]).toEqual({
    productId: PRODUCT_ONE,
    body: { ...frozenBody, acknowledge_quality_warnings: true },
  });

  await expect(restoredCard).toHaveAttribute("data-graph-result-delivery-adopted", "true");
  await expect(restoredCard.locator("[data-graph-result-quality-status='fail']")).toContainText("有限检查未通过");
  await expect(restoredCard).toContainText("服务端保留：文字追溯检查未通过");

  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "导出已采用交付", exact: true }).click();
  await download;
  expect(mock.ensureBodies).toEqual([{}]);
  expect(mock.previewBodies).toEqual([{}]);
  expect(mock.exportBodies).toEqual([{}]);
});

const MATRIX: ReadonlyArray<{ locale: Locale; theme: Theme; width: number }> = [
  ...(["zh-CN", "en-US", "ja-JP", "vi-VN"] as const).flatMap((locale) => (
    (["light", "dark"] as const).flatMap((theme) => [
      { locale, theme, width: 390 },
      { locale, theme, width: 1440 },
    ])
  )),
];

for (const preset of MATRIX) {
  test(`quality status stays honest at ${preset.width}px in ${preset.locale}/${preset.theme}`, async ({ page }, testInfo) => {
    await installPolicyMock(page);
    await openResults(page, PRODUCT_ONE, preset.locale, preset.theme, preset.width);
    await expect(page.locator("html")).toHaveAttribute("lang", preset.locale);
    await expect(page.locator("html")).toHaveAttribute("data-theme", preset.theme);
    const quality = page.locator(`[data-graph-result-item="${NODE_ID}"] [data-graph-result-quality-status="fail"]`);
    await expect(quality).toContainText(QUALITY_LABELS[preset.locale]);
    await page.locator(`[data-graph-result-item="${NODE_ID}"] [data-graph-result-adopt]`).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    const geometry = await dialog.evaluate((element) => {
      const rect = element.getBoundingClientRect();
      return { left: rect.left, right: rect.right, inner: window.innerWidth, client: document.documentElement.clientWidth, scroll: element.scrollWidth, width: element.clientWidth };
    });
    expect(geometry.inner).toBe(preset.width);
    expect(geometry.left).toBeGreaterThanOrEqual(0);
    expect(geometry.right).toBeLessThanOrEqual(geometry.client);
    expect(geometry.scroll).toBeLessThanOrEqual(geometry.width);
    await page.screenshot({ path: testInfo.outputPath(`quality-${preset.width}-${preset.locale}-${preset.theme}.png`) });
  });
}
