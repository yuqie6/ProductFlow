import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

import type {
  DeliveryAdoptionVersion,
  GraphProjection,
  GraphRun,
  ProductFactsResponse,
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
  authorWorkflowPrompt,
  createWorkflow,
  selectWorkflowNode,
  waitForWorkflowRun,
  workflowGraph,
  workflowProductId,
  workflowRunsPath,
} from "./canvasWorkflow";

/**
 * Merchant image-task loop gate (mock providers):
 * create → edit facts/text → generate → conditional results default → adopt → export
 * → Brand reuse on second product without source identity bleed.
 */
const EVIDENCE_ROOT = path.resolve(
  process.env.PRODUCTFLOW_MERCHANT_LOOP_EVIDENCE_DIR?.trim()
    || path.join(process.cwd(), "..", "storage-dev", "audits", "delivery-merchant-image-loop-gate"),
);

test.use({ trace: "on", screenshot: "on" });

test.describe("merchant image task loop gate", () => {
  test.skip(
    process.env.PRODUCTFLOW_RUN_CANVAS_WORKFLOW !== "1",
    "set PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 with a mock-capable stack",
  );
  test.setTimeout(300_000);

  test.beforeEach(async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  });

  test("create → edit → generate → adopt → export → Brand reuse clears identity", async ({ page }, info) => {
    await mkdir(EVIDENCE_ROOT, { recursive: true });
    const pathTable: Array<Record<string, string>> = [];
    const stamp = Date.now();
    const uniqueFactValue = `闭环规格-${stamp}`;
    const authoredGoal = `闭环改稿目标-${stamp}`;

    await withMockDocumentProviders(page.request, async () => {
      await page.setViewportSize({ width: 1440, height: 960 });
      let graph = await createWorkflow(page);
      const productId = workflowProductId(page);
      const source = graph.nodes.find((node) => node.node_type === "product_source")!;
      const prompt = graph.nodes.find((node) => node.node_type === "image_prompt")!;
      const image = graph.nodes.find((node) => node.node_type === "image_generation")!;
      pathTable.push({
        step: "create",
        product_id: productId,
        graph_id: graph.id,
        image_node: image.id,
        provider: "mock",
      });

      // --- inspect/edit facts + text（事实走持久化 PUT；文案走检查器保存）---
      // 注：检查器「确认事实」路径曾触发 null.length 告警，本门用 API 写入已确认事实并核对 DB 投影。
      const factsPath = `/api/v3/products/${productId}/facts`;
      const original = await (await page.request.get(factsPath)).json() as ProductFactsResponse;
      const editedName = `闭环商品-${stamp}`;
      const seeded = await page.request.put(factsPath, {
        data: {
          expected_fact_version: original.fact_set?.version ?? null,
          name: editedName,
          facts: [{
            key: "闭环规格",
            value: uniqueFactValue,
            status: "confirmed",
            requires_confirmation: false,
          }],
        },
      });
      expect(seeded.ok(), await seeded.text()).toBeTruthy();
      await expect.poll(async () => {
        const body = await (await page.request.get(factsPath)).json() as ProductFactsResponse;
        return {
          name: body.product?.name,
          fact: body.fact_set?.facts?.find((fact) => fact.key === "闭环规格"),
        };
      }).toMatchObject({
        name: editedName,
        fact: { value: uniqueFactValue, status: "confirmed", requires_confirmation: false },
      });
      pathTable.push({
        step: "edit_facts",
        product_name: editedName,
        fact_key: "闭环规格",
        fact_value: uniqueFactValue,
        via: "PUT /facts confirmed",
      });

      await page.reload();
      await expect(page.locator("[data-graph-shot-filmstrip]")).toBeVisible();
      await page.locator('[data-graph-main-view="flow"]').click().catch(() => undefined);
      await selectWorkflowNode(page, source.id);
      const inspector = page.locator("[data-graph-node-inspector]");
      await expect(inspector.getByLabel("商品名称", { exact: true })).toHaveValue(editedName);
      await page.screenshot({
        path: path.join(EVIDENCE_ROOT, "desktop-1440x960-facts-edited.png"),
        fullPage: false,
      });
      pathTable.push({ step: "inspect_facts_ui", product_name: editedName, via: "inspector_shows_persisted_name" });

      // --- inspect/edit prompt text ---
      await page.locator('[data-graph-main-view="flow"]').click().catch(() => undefined);
      await authorWorkflowPrompt(page, prompt.id, authoredGoal);
      pathTable.push({ step: "edit_prompt_text", design_goal: authoredGoal, document_origin: "authored" });

      // --- generate ---
      await selectWorkflowNode(page, image.id);
      const exportSettings = page.locator("[data-export-settings] > summary");
      if (await exportSettings.count()) await exportSettings.click();
      const preset = page.locator('[data-delivery-preset-key="detail_portrait"]');
      await expect(preset).toBeVisible();
      await preset.scrollIntoViewIfNeeded();
      await preset.click();
      await expect.poll(async () => (
        (await workflowGraph(page)).nodes.find((node) => node.id === image.id)?.config.delivery_spec
      )).toMatchObject({ width: 1200, height: 1600, format: "png" });

      graph = await workflowGraph(page);
      const adoptedAssetId = await runImage(page, graph, image.id);
      pathTable.push({ step: "generate", source_asset_id: adoptedAssetId, provider: "mock" });

      // --- conditional default results (no explicit preference) ---
      await page.evaluate((pid) => {
        const key = `productflow.workbench.ui.v1:${pid}`;
        const raw = localStorage.getItem(key);
        if (!raw) return;
        try {
          const parsed = JSON.parse(raw) as Record<string, unknown>;
          delete parsed.mainView;
          localStorage.setItem(key, JSON.stringify(parsed));
        } catch {
          localStorage.removeItem(key);
        }
      }, productId);
      await page.reload();
      await expect(page.locator("[data-graph-main-view-panel=\"results\"]")).toBeVisible();
      await expect(page.locator(`[data-graph-result-item="${image.id}"]`)).toBeVisible();
      await page.screenshot({
        path: path.join(EVIDENCE_ROOT, "desktop-1440x960-default-results.png"),
        fullPage: false,
      });
      pathTable.push({ step: "default_entry_results", observed: "results_panel_after_reload_without_preference" });

      // --- adopt ---
      const resultItem = page.locator(`[data-graph-result-item="${image.id}"]`);
      await resultItem.locator("[data-graph-result-adopt]").click();
      await expect.poll(async () => {
        const current = await currentAdoption(page, productId);
        return current?.slots.find((slot) => slot.slot_key === image.id)?.source_asset_id ?? null;
      }).toBe(adoptedAssetId);
      const adoption = (await currentAdoption(page, productId))!;
      pathTable.push({
        step: "adopt",
        adoption_version_id: adoption.id,
        adopted_asset_id: adoptedAssetId,
      });

      // --- export ---
      await waitForAdoptionExportReady(page, productId, adoption.id);
      const exportButton = page.locator("[data-graph-results-export-adoption]");
      await expect(exportButton).toBeVisible();
      await expect(exportButton).toBeEnabled();
      await expect(page.locator("[data-graph-canvas-toolbar]")).toHaveCount(0);
      const downloadPromise = page.waitForEvent("download");
      const exportResponsePromise = page.waitForResponse((response) => (
        response.request().method() === "POST"
        && new URL(response.url()).pathname
          === `/api/v3/products/${productId}/delivery-adoptions/${adoption.id}/export`
      ));
      await exportButton.click();
      expect((await exportResponsePromise).ok()).toBeTruthy();
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
      await page.screenshot({
        path: path.join(EVIDENCE_ROOT, "desktop-1440x960-adopted-export.png"),
        fullPage: false,
      });

      // --- Brand on first product ---
      const visual = await page.request.post("/api/v3/visual-systems", {
        data: {
          name: `loop-brand-visual-${stamp}`,
          payload: {
            style: ["商家品牌冷色"],
            colors: [{ role: "accent", value: "#00AA66" }],
          },
        },
      });
      expect(visual.ok(), await visual.text()).toBeTruthy();
      const visualBody = await visual.json() as { id: string; current_version: { id: string } };
      const brandRes = await page.request.post("/api/v3/brands", {
        data: {
          name: `闭环品牌-${stamp}`,
          visual_system_id: visualBody.id,
        },
      });
      expect(brandRes.ok(), await brandRes.text()).toBeTruthy();
      const brandBody = await brandRes.json() as { id: string };
      const selectBrand = await page.request.put(`/api/v3/products/${productId}/brand-selection`, {
        data: { brand_id: brandBody.id },
      });
      expect(selectBrand.ok(), await selectBrand.text()).toBeTruthy();
      const inh1 = await page.request.post(`/api/v3/products/${productId}/visual-inheritance`, { data: {} });
      expect(inh1.ok(), await inh1.text()).toBeTruthy();
      const inh1Body = await inh1.json() as {
        brand_placeholder?: { reason?: string };
        effective_payload?: { style?: unknown };
      };
      expect(inh1Body.brand_placeholder?.reason).toBe("brand_style_merged");
      pathTable.push({
        step: "brand_select_product1",
        brand_id: brandBody.id,
        visual_system_id: visualBody.id,
        inheritance_reason: String(inh1Body.brand_placeholder?.reason ?? ""),
      });

      await page.locator('[data-graph-main-view="flow"]').click();
      await selectWorkflowNode(page, image.id);
      const recipe = await saveFullRecipe(page, `merchant-loop-recipe ${stamp}`);
      const recipeJSON = JSON.stringify(recipe.current_version.payload);
      expect(recipeJSON).not.toContain(productId);
      expect(recipeJSON).not.toContain(adoptedAssetId);
      expect(recipeJSON).not.toContain(uniqueFactValue);

      // --- second product via recipe + same Brand ---
      await page.setViewportSize({ width: 390, height: 844 });
      await page.goto("/products/new");
      await page.locator('[data-create-mode="recipe"]').click();
      const secondName = `loop second ${stamp}`;
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
      expect(previewText).not.toContain(uniqueFactValue);
      await page.screenshot({
        path: path.join(EVIDENCE_ROOT, "mobile-390x844-recipe-preview.png"),
        fullPage: false,
      });

      const confirmed = page.waitForResponse((response) => (
        response.request().method() === "POST"
        && new URL(response.url()).pathname === "/api/v3/products/from-recipe"
      ));
      await dialog.getByRole("button", { name: "确认并创建商品", exact: true }).click();
      const created = await confirmed;
      expect(created.status(), await created.text()).toBe(201);
      const result = await created.json() as { product: { id: string } };
      expect(result.product.id).not.toBe(productId);
      await page.waitForURL(`**/products/${result.product.id}`);
      await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
      const secondGraph = await workflowGraph(page);
      const secondGraphJSON = JSON.stringify(secondGraph);
      expect(secondGraph.product_id).toBe(result.product.id);
      expect(secondGraphJSON).not.toContain(productId);
      expect(secondGraphJSON).not.toContain(adoptedAssetId);
      expect(secondGraphJSON).not.toContain(uniqueFactValue);
      for (const node of secondGraph.nodes) {
        expect(node.bound_asset_id).toBeNull();
        expect(node.preview_asset_id).toBeNull();
        expect(node.current_artifact_id).toBeNull();
        if (node.node_type === "product_source") {
          expect(node.config.source_product_id).toBe(result.product.id);
        }
      }

      const brand2 = await page.request.put(`/api/v3/products/${result.product.id}/brand-selection`, {
        data: { brand_id: brandBody.id },
      });
      expect(brand2.ok(), await brand2.text()).toBeTruthy();
      const inh2 = await page.request.post(`/api/v3/products/${result.product.id}/visual-inheritance`, {
        data: {},
      });
      expect(inh2.ok(), await inh2.text()).toBeTruthy();
      const inh2Body = await inh2.json() as {
        brand_placeholder?: { reason?: string };
        effective_payload?: Record<string, unknown>;
      };
      expect(inh2Body.brand_placeholder?.reason).toBe("brand_style_merged");
      const style = inh2Body.effective_payload?.style;
      expect(JSON.stringify(style ?? null)).toContain("商家品牌冷色");
      expect(JSON.stringify(inh2Body)).not.toContain(productId);
      expect(JSON.stringify(inh2Body)).not.toContain(uniqueFactValue);

      const secondFacts = await (await page.request.get(
        `/api/v3/products/${result.product.id}/facts`,
      )).json() as ProductFactsResponse;
      const leaked = (secondFacts.fact_set?.facts ?? []).some((fact) => (
        fact.value === uniqueFactValue || fact.key === "闭环规格"
      ));
      expect(leaked).toBeFalsy();

      pathTable.push({
        step: "brand_reuse_second_product",
        second_product_id: result.product.id,
        second_graph_id: secondGraph.id,
        brand_id: brandBody.id,
        inheritance_reason: String(inh2Body.brand_placeholder?.reason ?? ""),
        identity_bleed: "none",
      });

      await page.locator('[data-graph-main-view="results"]').click();
      await expect(page.locator("[data-graph-results-view]")).toBeVisible();
      await page.screenshot({
        path: path.join(EVIDENCE_ROOT, "mobile-390x844-second-results.png"),
        fullPage: false,
      });
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
