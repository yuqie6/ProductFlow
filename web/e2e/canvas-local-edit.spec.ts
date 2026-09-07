import { expect, test, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

import type { GraphProjection, GraphRun, LocalImageEditTask } from "../src/lib/types";
import { assertMockImageProviders, lockLocale, loginAsAdmin, requiredEnv } from "./liveGraph";
import { createWorkflow, selectWorkflowNode, waitForWorkflowRun, workflowGraph, workflowProductId, workflowRunsPath } from "./canvasWorkflow";

const EVIDENCE_ROOT =
  process.env.PRODUCTFLOW_LOCAL_EDIT_EVIDENCE_DIR?.trim() ||
  path.join(process.cwd(), "..", "storage-dev", "audits", "delivery-r2-local-edit-retest");

test.describe("canvas local edit", () => {
  test.skip(process.env.PRODUCTFLOW_RUN_CANVAS_WORKFLOW !== "1", "set PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 with an isolated mock stack");
  test.setTimeout(120_000);

  test.beforeEach(async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await assertMockImageProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"));
  });

  test("inspector local edit creates a lineage asset, adopts, and reverts", async ({ page }) => {
    fs.mkdirSync(EVIDENCE_ROOT, { recursive: true });
    const { imageId, sourceId } = await generatedSource(page);
    const capability = await page.request.get("/api/v3/local-image-edits/capability");
    expect(capability.ok(), await capability.text()).toBeTruthy();
    expect(await capability.json()).toMatchObject({ supported: true, operations: expect.arrayContaining(["inpaint"]) });

    await openInspectorLocalEdit(page, imageId);
    await completeLocalEdit(page, "去掉选区杂物");
    await expect(page.locator("[data-local-edit-result]")).toBeVisible();
    await expect(page.locator("[data-local-edit-adopted]")).toHaveAttribute("data-local-edit-adopted", "false");
    await expect(page.locator('[data-local-edit-result-tab="result"]')).toBeVisible();
    await expect(page.getByRole("button", { name: "采用为当前结果", exact: true })).toBeVisible();

    await page.setViewportSize({ width: 1440, height: 960 });
    await expect(page.locator("[data-local-edit-result]")).toBeVisible();
    await page.screenshot({
      path: path.join(EVIDENCE_ROOT, "desktop-1440x960-local-edit-result.png"),
      fullPage: false,
    });

    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator("[data-local-edit-result]")).toBeVisible();
    await expect(page.getByRole("button", { name: "采用为当前结果", exact: true })).toBeVisible();
    const mobileGeometry = await page.evaluate(() => ({
      viewport: window.innerWidth,
      client: document.documentElement.clientWidth,
      resultVisible: Boolean(document.querySelector("[data-local-edit-result]")),
    }));
    expect(mobileGeometry.viewport).toBe(390);
    expect(mobileGeometry.resultVisible).toBe(true);
    await page.screenshot({
      path: path.join(EVIDENCE_ROOT, "mobile-390x844-local-edit-result.png"),
      fullPage: false,
    });
    await page.setViewportSize({ width: 1440, height: 960 });

    const task = await latestLocalEdit(page);
    expect(task.status).toBe("succeeded");
    expect(task.source_asset.id).toBe(sourceId);
    expect(task.result_asset?.id).toBeTruthy();
    expect(task.result_asset?.id).not.toBe(sourceId);
    expect(task.result_asset?.origin_type).toBe("local_edit");
    expect(task.result_asset?.parent_asset_id).toBe(sourceId);
    const resultId = task.result_asset!.id;
    expect((await workflowGraph(page)).nodes.find((node) => node.id === imageId)?.preview_asset_id).toBe(sourceId);

    const sourceBytes = await downloadAsset(page, sourceId);
    await page.getByRole("button", { name: "采用为当前结果", exact: true }).click();
    await expect(page.locator("[data-local-edit-adopted]")).toHaveAttribute("data-local-edit-adopted", "true");
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === imageId)?.preview_asset_id).toBe(resultId);
    await page.screenshot({
      path: path.join(EVIDENCE_ROOT, "desktop-1440x960-local-edit-adopted.png"),
      fullPage: false,
    });

    await page.getByRole("button", { name: "撤销采用", exact: true }).click();
    await expect(page.locator("[data-local-edit-adopted]")).toHaveAttribute("data-local-edit-adopted", "false");
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === imageId)?.preview_asset_id).toBe(sourceId);
    expect((await downloadAsset(page, sourceId)).equals(sourceBytes)).toBeTruthy();

    fs.writeFileSync(
      path.join(EVIDENCE_ROOT, "path-table.json"),
      JSON.stringify(
        {
          source_asset_id: sourceId,
          result_asset_id: resultId,
          origin_type: "local_edit",
          parent_asset_id: sourceId,
          image_node_id: imageId,
          product_id: workflowProductId(page),
          screenshots: [
            "desktop-1440x960-local-edit-result.png",
            "mobile-390x844-local-edit-result.png",
            "desktop-1440x960-local-edit-adopted.png",
          ],
        },
        null,
        2,
      ),
    );
  });

  test("submit failure keeps the node current image", async ({ page }) => {
    const { imageId, sourceId } = await generatedSource(page);
    await openInspectorLocalEdit(page, imageId);
    await page.route("**/image-edits/**/submit", async (route) => {
      if (route.request().method() === "POST") {
        await route.fulfill({ status: 503, json: { detail: "局部编辑提交测试失败" } });
        return;
      }
      await route.continue();
    });
    await fillLocalEdit(page, "去掉选区杂物");
    await page.getByRole("button", { name: "提交局部编辑", exact: true }).click();
    await expect(page.locator("[data-local-edit-error]")).toContainText("局部编辑提交测试失败");
    expect((await workflowGraph(page)).nodes.find((node) => node.id === imageId)?.preview_asset_id).toBe(sourceId);
  });
});

async function generatedSource(page: Page) {
  const graph = await createWorkflow(page);
  const imageId = graph.nodes.find((node) => node.node_type === "image_generation")!.id;
  await selectWorkflowNode(page, imageId);
  const responsePromise = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === workflowRunsPath(page, graph));
  await page.locator("[data-graph-inspector-run-node]").click();
  const response = await responsePromise;
  expect(response.ok(), await response.text()).toBeTruthy();
  const run: GraphRun = await response.json();
  await waitForWorkflowRun(page, graph, run.id, "succeeded");
  const next: GraphProjection = await workflowGraph(page);
  const sourceId = next.nodes.find((node) => node.id === imageId)!.preview_asset_id!;
  expect(sourceId).toBeTruthy();
  return { graph: next, imageId, sourceId };
}

async function openInspectorLocalEdit(page: Page, imageId: string) {
  await selectWorkflowNode(page, imageId);
  await page.locator("[data-graph-node-local-edit]").click();
  await expect(page.locator("[data-local-edit-dialog]")).toBeVisible();
  await expect(page.locator("[data-local-edit-operation-unavailable]")).toHaveCount(0);
  await expect(page.locator("[data-local-edit-target-impact]")).toBeVisible();
}

async function fillLocalEdit(page: Page, instruction: string) {
  await expect(page.locator("[data-local-edit-error]")).toHaveCount(0);
  const canvas = page.locator("[data-local-edit-mask-canvas]");
  await expect(canvas).toBeVisible();
  await expect(canvas).not.toHaveAttribute("aria-disabled", "true");
  await page.getByLabel("编辑要求").fill(instruction);
  const box = await canvas.boundingBox();
  expect(box).toBeTruthy();
  await page.mouse.move(box!.x + box!.width * 0.25, box!.y + box!.height * 0.25);
  await page.mouse.down();
  await page.mouse.move(box!.x + box!.width * 0.45, box!.y + box!.height * 0.45);
  await page.mouse.up();
  await expect.poll(async () => Number(await page.locator("[data-local-edit-mask-summary]").getAttribute("data-edit-pixels"))).toBeGreaterThan(0);
  await expect.poll(async () => Number(await page.locator("[data-local-edit-mask-summary]").getAttribute("data-protected-pixels"))).toBeGreaterThan(0);
}

async function completeLocalEdit(page: Page, instruction: string) {
  await fillLocalEdit(page, instruction);
  await page.getByRole("button", { name: "提交局部编辑", exact: true }).click();
  await expect(page.locator("[data-local-edit-task-status='succeeded']")).toBeVisible({ timeout: 60_000 });
}

async function latestLocalEdit(page: Page): Promise<LocalImageEditTask> {
  const response = await page.request.get(`/api/v3/products/${workflowProductId(page)}/image-edits?limit=50`);
  expect(response.ok(), await response.text()).toBeTruthy();
  const body = await response.json() as { items: LocalImageEditTask[] };
  expect(body.items.length).toBeGreaterThan(0);
  return body.items[0];
}

async function downloadAsset(page: Page, assetId: string): Promise<Buffer> {
  const response = await page.request.get(`/api/v2/product-image-assets/${assetId}/download`);
  expect(response.ok(), await response.text()).toBeTruthy();
  return response.body();
}
