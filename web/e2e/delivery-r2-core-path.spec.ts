import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

import type {
  DeliveryAdoptionVersion,
  GraphProjection,
  GraphRun,
  WorkflowRecipeSummary,
} from "../src/lib/types";
import {
  lockLocale,
  loginAsAdmin,
  REFERENCE_PRODUCT_IMAGE,
  requiredEnv,
  withMockDocumentProviders,
} from "./liveGraph";
import {
  createWorkflow,
  selectWorkflowNode,
  waitForWorkflowRun,
  workflowGraph,
  workflowProductId,
  workflowRunsPath,
} from "./canvasWorkflow";

const EVIDENCE_ROOT = path.resolve(
  process.env.PRODUCTFLOW_R2_EVIDENCE_DIR?.trim()
    || path.join(process.cwd(), "..", "storage-dev", "audits", "delivery-r2-core-path-gate"),
);

test.use({ trace: "on", screenshot: "on" });

test.describe("delivery R2 core path gate", () => {
  test.skip(
    process.env.PRODUCTFLOW_RUN_CANVAS_WORKFLOW !== "1",
    "set PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 with a mock-capable stack",
  );
  test.setTimeout(240_000);

  test.beforeEach(async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  });

  test("create → results → adopt → export → rerun preserves → recipe clears identity", async ({ page }, info) => {
    await mkdir(EVIDENCE_ROOT, { recursive: true });
    const pathTable: Array<Record<string, string>> = [];

    await withMockDocumentProviders(page.request, async () => {
      await page.setViewportSize({ width: 1440, height: 960 });
      let graph = await createWorkflow(page);
      const productId = workflowProductId(page);
      const image = graph.nodes.find((node) => node.node_type === "image_generation")!;
      pathTable.push({ step: "create_workflow", product_id: productId, graph_id: graph.id, image_node: image.id });

      await selectWorkflowNode(page, image.id);
      const exportSettings = page.locator("[data-export-settings] > summary");
      if (await exportSettings.count()) {
        await exportSettings.click();
      }
      const preset = page.locator('[data-delivery-preset-key="detail_portrait"]');
      await expect(preset).toBeVisible();
      await preset.scrollIntoViewIfNeeded();
      await preset.click();
      await expect.poll(async () => (
        (await workflowGraph(page)).nodes.find((node) => node.id === image.id)?.config.delivery_spec
      )).toMatchObject({ width: 1200, height: 1600, format: "png" });

      const adoptedAssetId = await runImage(page, graph, image.id);
      pathTable.push({ step: "generate_image", source_asset_id: adoptedAssetId });

      await page.locator('[data-graph-main-view="results"]').click();
      await expect(page.locator("[data-graph-results-view]")).toBeVisible();
      const resultItem = page.locator(`[data-graph-result-item="${image.id}"]`);
      await expect(resultItem).toBeVisible();
      await expect(resultItem).toHaveAttribute("data-graph-result-status", /succeeded|ready|idle|pending|failed|running|unknown/);
      await page.screenshot({ path: path.join(EVIDENCE_ROOT, "desktop-1440x960-results.png"), fullPage: false });
      await info.attach("desktop-results", {
        path: path.join(EVIDENCE_ROOT, "desktop-1440x960-results.png"),
        contentType: "image/png",
      });

      await expect(resultItem.locator("[data-graph-result-adopt]")).toBeVisible();
      await resultItem.locator("[data-graph-result-adopt]").click();
      await expect.poll(async () => {
        const current = await currentAdoption(page, productId);
        return current?.slots.find((slot) => slot.slot_key === image.id)?.source_asset_id ?? null;
      }).toBe(adoptedAssetId);
      await expect(resultItem).toHaveAttribute("data-graph-result-delivery-adopted", "true");
      const adoption = (await currentAdoption(page, productId))!;
      expect(adoption.version).toBe(1);
      pathTable.push({
        step: "adopt",
        adoption_version_id: adoption.id,
        adopted_asset_id: adoptedAssetId,
        slot_key: image.id,
      });

      await waitForAdoptionExportReady(page, productId, adoption.id);
      const exportButton = page.locator("[data-graph-results-export-adoption]");
      await expect(exportButton).toBeVisible();
      await expect(exportButton).toBeEnabled();
      // 成果头内联操作区；不得被画布右上浮动工具条拦截（禁止 force）。
      await expect(page.locator("[data-graph-canvas-toolbar]")).toHaveCount(0);
      const downloadPromise = page.waitForEvent("download");
      const exportResponsePromise = page.waitForResponse((response) => (
        response.request().method() === "POST"
        && new URL(response.url()).pathname
          === `/api/v3/products/${productId}/delivery-adoptions/${adoption.id}/export`
      ));
      await exportButton.click();
      const exportResponse = await exportResponsePromise;
      expect(exportResponse.ok(), await exportResponse.text()).toBeTruthy();
      const download = await downloadPromise;
      const zipPath = path.join(EVIDENCE_ROOT, "adoption-export.zip");
      await download.saveAs(zipPath);
      const manifest = JSON.parse(execFileSync("unzip", ["-p", zipPath, "manifest.json"], { encoding: "utf8" })) as {
        kind: string;
        items: Array<{
          filename: string;
          source_asset: { id: string };
          measured?: { sha256?: string };
        }>;
      };
      expect(manifest.kind).toMatch(/delivery/);
      expect(manifest.items.map((item) => item.source_asset.id)).toEqual([adoptedAssetId]);
      for (const item of manifest.items) {
        const bytes = execFileSync("unzip", ["-p", zipPath, item.filename]);
        const hash = sha256(bytes);
        if (item.measured?.sha256) expect(hash).toBe(item.measured.sha256);
        pathTable.push({
          step: "export_item",
          filename: item.filename,
          source_asset_id: item.source_asset.id,
          sha256: hash,
        });
      }

      await page.setViewportSize({ width: 390, height: 844 });
      await expect(page.locator("[data-graph-results-view]")).toBeVisible();
      const mobileExport = page.locator("[data-graph-results-export-adoption]");
      await expect(mobileExport).toBeVisible();
      await expect(mobileExport).toBeEnabled();
      await expect(page.locator("[data-graph-canvas-toolbar]")).toHaveCount(0);
      const mobileDownloadPromise = page.waitForEvent("download");
      const mobileExportResponse = page.waitForResponse((response) => (
        response.request().method() === "POST"
        && new URL(response.url()).pathname
          === `/api/v3/products/${productId}/delivery-adoptions/${adoption.id}/export`
      ));
      await mobileExport.click();
      expect((await mobileExportResponse).ok()).toBeTruthy();
      await mobileDownloadPromise;
      await page.screenshot({ path: path.join(EVIDENCE_ROOT, "mobile-390x844-export-reachable.png"), fullPage: false });
      await page.setViewportSize({ width: 1440, height: 960 });

      graph = await workflowGraph(page);
      await page.locator('[data-graph-main-view="flow"]').click();
      await expect(page.locator('[data-graph-main-view-panel="flow"]')).toBeVisible();
      await expect(page.locator("[data-graph-canvas-toolbar]")).toBeVisible();
      // 再跑一次节点：即便 mock digest 复用同一资产 ID，采用快照仍须钉住原 ID。
      const rerunAssetId = await runImage(page, graph, image.id);
      // mock 同 digest 可能复用资产；只要采用快照仍钉住原 ID 即满足合同。
      const afterRerun = (await currentAdoption(page, productId))!;
      expect(afterRerun.id).toBe(adoption.id);
      expect(afterRerun.slots.find((slot) => slot.slot_key === image.id)?.source_asset_id).toBe(adoptedAssetId);
      const immutable = await page.request.get(
        `/api/v3/products/${productId}/delivery-adoptions/${adoption.id}`,
      );
      expect(immutable.ok(), await immutable.text()).toBeTruthy();
      const immutableBody = (await immutable.json()) as DeliveryAdoptionVersion;
      expect(immutableBody.slots[0]?.source_asset_id).toBe(adoptedAssetId);
      if (rerunAssetId === adoptedAssetId) {
        pathTable.push({
          step: "rerun_same_digest_asset",
          note: "mock reused asset id; adoption pointer still immutable",
          adoption_still_asset_id: adoptedAssetId,
        });
      } else {
        pathTable.push({
          step: "rerun_preserves_adoption",
          rerun_asset_id: rerunAssetId,
          adoption_still_asset_id: adoptedAssetId,
        });
      }

      const visual = await page.request.post("/api/v3/visual-systems", {
        data: {
          name: `r2-visual-${Date.now()}`,
          payload: { style: "clean ceramic", colors: ["#E8E4DC", "#2F2A24"] },
        },
      });
      expect(visual.ok(), await visual.text()).toBeTruthy();
      const visualBody = await visual.json() as {
        id: string;
        current_version: { id: string };
      };
      const select = await page.request.put(`/api/v3/products/${productId}/visual-selection`, {
        data: { visual_system_version_id: visualBody.current_version.id },
      });
      expect(select.ok(), await select.text()).toBeTruthy();
      pathTable.push({
        step: "visual_select",
        visual_system_id: visualBody.id,
        visual_version_id: visualBody.current_version.id,
      });

      await selectWorkflowNode(page, image.id);
      const recipe = await saveFullRecipe(page, `r2-core-recipe ${Date.now()}`);
      expect(JSON.stringify(recipe.current_version.payload)).not.toContain(productId);
      expect(JSON.stringify(recipe.current_version.payload)).not.toContain(adoptedAssetId);
      expect(JSON.stringify(recipe.current_version.payload)).not.toContain(rerunAssetId);

      await page.setViewportSize({ width: 390, height: 844 });
      await page.goto("/products/new");
      await page.locator('[data-create-mode="recipe"]').click();
      const secondName = `r2 second ${Date.now()}`;
      await page.locator("#recipe-product-name").fill(secondName);
      await page.locator("#create-recipe").selectOption(recipe.id);
      await page.locator('[data-create-recipe-form] input[type="file"]').setInputFiles(REFERENCE_PRODUCT_IMAGE);
      await page.locator("#recipe-source-note").fill("第二商品新身份，不继承旧商品事实");
      await page.getByRole("button", { name: "预览配方", exact: true }).click();
      const dialog = page.getByRole("dialog");
      await expect(dialog.locator("[data-recipe-preview]")).toBeVisible();
      const previewText = await dialog.innerText();
      expect(previewText).not.toContain(productId);
      expect(previewText).not.toContain(adoptedAssetId);
      await page.screenshot({ path: path.join(EVIDENCE_ROOT, "mobile-390x844-recipe-preview.png"), fullPage: false });

      const confirmed = page.waitForResponse((response) => (
        response.request().method() === "POST"
        && new URL(response.url()).pathname === "/api/v3/products/from-recipe"
      ));
      await dialog.getByRole("button", { name: "确认并创建商品", exact: true }).click();
      const created = await confirmed;
      expect(created.status(), await created.text()).toBe(201);
      const result = await created.json() as { product: { id: string; name: string } };
      expect(result.product.id).not.toBe(productId);
      await page.waitForURL(`**/products/${result.product.id}`);
      await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
      const secondGraph = await workflowGraph(page);
      expect(secondGraph.product_id).toBe(result.product.id);
      expect(JSON.stringify(secondGraph)).not.toContain(productId);
      expect(JSON.stringify(secondGraph)).not.toContain(adoptedAssetId);
      expect(JSON.stringify(secondGraph)).not.toContain(rerunAssetId);
      for (const node of secondGraph.nodes) {
        expect(node.bound_asset_id).toBeNull();
        expect(node.preview_asset_id).toBeNull();
        expect(node.current_artifact_id).toBeNull();
        if (node.node_type === "product_source") {
          expect(node.config.source_product_id).toBe(result.product.id);
        }
      }
      pathTable.push({
        step: "recipe_second_product",
        second_product_id: result.product.id,
        second_graph_id: secondGraph.id,
      });

      await page.locator('[data-graph-main-view="results"]').click();
      await expect(page.locator("[data-graph-results-view]")).toBeVisible();
      await expect(page.locator("[data-graph-result-item]").first()).toBeVisible();
      await page.screenshot({ path: path.join(EVIDENCE_ROOT, "mobile-390x844-results.png"), fullPage: false });

      await page.setViewportSize({ width: 1440, height: 960 });
      await page.goto(`/products/${productId}`);
      await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
      await page.locator('[data-graph-main-view="results"]').click();
      await expect(page.locator(`[data-graph-result-item="${image.id}"]`)).toHaveAttribute(
        "data-graph-result-delivery-adopted",
        "true",
      );
      await expect(page.locator("[data-graph-results-export-adoption]")).toBeVisible();
      await page.screenshot({ path: path.join(EVIDENCE_ROOT, "desktop-1440x960-adopted-export.png"), fullPage: false });
    });

    await writeFile(
      path.join(EVIDENCE_ROOT, "path-table.json"),
      `${JSON.stringify({ captured_at: new Date().toISOString(), path_table: pathTable }, null, 2)}\n`,
      "utf8",
    );
    await info.attach("path-table", {
      path: path.join(EVIDENCE_ROOT, "path-table.json"),
      contentType: "application/json",
    });
  });

  test("mobile create and results controls remain reachable", async ({ page }) => {
    await withMockDocumentProviders(page.request, async () => {
      await page.setViewportSize({ width: 390, height: 844 });
      await page.goto("/products/new");
      await expect(page.locator("#agent-product-name")).toBeVisible();
      await expect(page.locator("[data-create-direct]")).toBeVisible();
      const graph = await createWorkflow(page);
      const image = graph.nodes.find((node) => node.node_type === "image_generation")!;
      await page.locator('[data-graph-main-view="results"]').click();
      await expect(page.locator("[data-graph-results-view]")).toBeVisible();
      const item = page.locator(`[data-graph-result-item="${image.id}"]`);
      await expect(item).toBeVisible();
      await expect(item.locator("[data-graph-result-run]")).toBeVisible();
      await expect(item.locator("[data-graph-result-locate]")).toBeVisible();
      await mkdir(EVIDENCE_ROOT, { recursive: true });
      await page.screenshot({ path: path.join(EVIDENCE_ROOT, "mobile-390x844-create-results-reachability.png"), fullPage: false });
    });
  });
});

function sha256(bytes: Buffer): string {
  return createHash("sha256").update(bytes).digest("hex");
}

async function runImage(page: Page, graph: GraphProjection, nodeId: string): Promise<string> {
  await selectWorkflowNode(page, nodeId);
  const responsePromise = page.waitForResponse((response) => (
    response.request().method() === "POST"
    && new URL(response.url()).pathname === workflowRunsPath(page, graph)
  ));
  await page.locator("[data-graph-inspector-run-node]").click();
  const response = await responsePromise;
  expect(response.ok(), await response.text()).toBeTruthy();
  const run = (await response.json()) as GraphRun;
  await waitForWorkflowRun(page, graph, run.id, "succeeded");
  const assetId = (await workflowGraph(page)).nodes.find((node) => node.id === nodeId)?.preview_asset_id;
  expect(assetId).toBeTruthy();
  return assetId!;
}

async function currentAdoption(page: Page, productId: string): Promise<DeliveryAdoptionVersion | null> {
  const response = await page.request.get(`/api/v3/products/${productId}/delivery-adoptions/current`);
  if (response.status() === 404) return null;
  expect(response.ok(), await response.text()).toBeTruthy();
  return response.json();
}

async function waitForAdoptionExportReady(page: Page, productId: string, versionId: string): Promise<void> {
  const ensure = await page.request.post(
    `/api/v3/products/${productId}/delivery-adoptions/${versionId}/renditions`,
    { data: {} },
  );
  expect(ensure.ok() || ensure.status() === 202, await ensure.text()).toBeTruthy();
  await expect.poll(async () => {
    const preview = await page.request.post(
      `/api/v3/products/${productId}/delivery-adoptions/${versionId}/preview`,
      { data: {} },
    );
    if (!preview.ok()) return `http ${preview.status()}`;
    const body = await preview.json() as { export_ready?: boolean };
    return body.export_ready ? "ready" : "pending";
  }, { timeout: 90_000 }).toBe("ready");
}

async function saveFullRecipe(page: Page, title: string): Promise<WorkflowRecipeSummary> {
  await page.locator('[data-sidebar-tool="add"]').filter({ visible: true }).click();
  await page.getByRole("button", { name: /保存完整工作流预设/ }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("预设名称").fill(title);
  const responsePromise = page.waitForResponse((response) => (
    response.request().method() === "POST"
    && /\/workflows\/[^/]+\/recipes$/.test(new URL(response.url()).pathname)
  ));
  await dialog.getByRole("button", { name: "保存预设", exact: true }).click();
  const response = await responsePromise;
  expect(response.ok(), await response.text()).toBeTruthy();
  await expect(dialog).not.toBeVisible();
  return response.json();
}
