import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";

import type { DeliveryRenditionJob, GraphProjection, GraphRun } from "../src/lib/types";
import { assertMockImageProviders, lockLocale, loginAsAdmin, requiredEnv } from "./liveGraph";
import { createWorkflow, selectWorkflowNode, waitForWorkflowRun, workflowGraph, workflowRunsPath } from "./canvasWorkflow";

test.describe("canvas delivery", () => {
  test.skip(process.env.PRODUCTFLOW_RUN_CANVAS_WORKFLOW !== "1", "set PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 with an isolated mock stack");
  test.setTimeout(120_000);

  test.beforeEach(async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await assertMockImageProviders(page.request);
  });

  test("delivery presets preserve the original and download verified image and manifest bytes", async ({ page }) => {
    const { graph, imageId, sourceId, runId } = await generatedSource(page);
    const source = await page.request.get(`/api/v2/product-image-assets/${sourceId}/download`);
    expect(source.ok(), await source.text()).toBeTruthy();
    const sourceHash = sha256(await source.body());
    const jobs = [];
    for (const [key, width, height] of [["detail_portrait", 1200, 1600], ["scene_landscape", 1600, 1200]] as const) {
      const job = await createRendition(page, graph, imageId, sourceId, key, width, height);
      expect(job.delivery_spec).toMatchObject({ width, height, format: "png" });
      expect(job.source_asset_id).toBe(sourceId);
      expect(job.result_asset?.parent_asset_id).toBe(sourceId);
      expect((await workflowGraph(page)).nodes.find((node) => node.id === imageId)?.preview_asset_id).toBe(sourceId);
      jobs.push(job);
    }
    const panel = page.locator("[data-delivery-rendition-panel]");
    await panel.getByRole("button", { name: "预览", exact: true }).first().click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect.poll(() => dialog.locator("img").first().evaluate((element: HTMLImageElement) => element.naturalWidth > 0)).toBeTruthy();
    await dialog.getByRole("button", { name: "关闭预览", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    const downloadPromise = page.waitForEvent("download");
    await panel.locator(`a[href$="${jobs[0].result_asset!.download_url}"]`).click();
    const downloaded = await downloadPromise;
    expect(await downloaded.failure()).toBeNull();
    const imagePath = test.info().outputPath("delivery.png");
    await downloaded.saveAs(imagePath);
    const png = await readFile(imagePath);
    expect(png.subarray(0, 8).toString("hex")).toBe("89504e470d0a1a0a");
    expect(png.readUInt32BE(16)).toBe(1200);
    expect(png.readUInt32BE(20)).toBe(1600);

    await page.locator('[data-sidebar-tool="library"]').filter({ visible: true }).click();
    for (const job of jobs) {
      await page.locator(`[data-gallery-asset-id="${job.result_asset!.id}"] button[aria-pressed]`).click();
    }
    const exportButton = page.getByTestId("delivery-export-button");
    await expect(exportButton).toBeEnabled();
    const zipPromise = page.waitForEvent("download");
    await exportButton.click();
    const zip = await zipPromise;
    expect(await zip.failure()).toBeNull();
    const zipPath = test.info().outputPath("delivery-export.zip");
    await zip.saveAs(zipPath);
    const manifest = JSON.parse(execFileSync("unzip", ["-p", zipPath, "manifest.json"], { encoding: "utf8" })) as {
      kind: string; complete: boolean; missing_items: unknown[];
      items: Array<{ filename: string; source_asset: { id: string }; result_asset: { id: string; parent_asset_id: string }; rendition_job: { id: string }; run: { id: string }; measured: { sha256: string } }>;
    };
    expect(manifest.kind).toBe("productflow.delivery_export");
    expect(manifest.complete).toBe(true);
    expect(manifest.missing_items).toEqual([]);
    expect(manifest.items.map((item) => item.rendition_job.id).sort()).toEqual(jobs.map((job) => job.id).sort());
    for (const item of manifest.items) {
      expect(item.source_asset.id).toBe(sourceId);
      expect(item.result_asset.parent_asset_id).toBe(sourceId);
      expect(item.run.id).toBe(runId);
      expect(sha256(execFileSync("unzip", ["-p", zipPath, item.filename]))).toBe(item.measured.sha256);
    }
    const unchanged = await page.request.get(`/api/v2/product-image-assets/${sourceId}/download`);
    expect(sha256(await unchanged.body())).toBe(sourceHash);
    const runs = await page.request.get(workflowRunsPath(page, graph));
    expect((await runs.json()).items).toHaveLength(1);
    await page.locator(`[data-gallery-asset-id="${sourceId}"]`).getByLabel("更多操作").click();
    const originalPromise = page.waitForEvent("download");
    await page.getByRole("menu").getByRole("link", { name: "下载原图", exact: true }).click();
    const original = await originalPromise;
    expect(await original.failure()).toBeNull();
    const originalPath = test.info().outputPath("original.png");
    await original.saveAs(originalPath);
    expect(sha256(await readFile(originalPath))).toBe(sourceHash);
    await page.keyboard.press("Escape");

    await page.locator(`[data-gallery-asset-id="${sourceId}"] button[aria-pressed]`).click();
    await expect(exportButton).toBeDisabled();
    await expect(page.getByTestId("delivery-export-reason")).toContainText("原图或非交付图");
    await page.locator(`[data-gallery-asset-id="${sourceId}"] button[aria-pressed]`).click();
    await page.route("**/delivery-exports", (route) => route.fulfill({ status: 503, json: { detail: "交付包测试失败" } }));
    let downloads = 0;
    page.on("download", () => downloads++);
    await exportButton.click();
    await expect(page.getByText("交付包测试失败", { exact: true })).toBeVisible();
    expect(downloads).toBe(0);
  });

  test("rendition submission failure stays visible without replacing the source", async ({ page }) => {
    const { graph, imageId, sourceId } = await generatedSource(page);
    await page.locator('[data-delivery-preset-key="scene_landscape"]').click();
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === imageId)?.config.delivery_spec).toMatchObject({ width: 1600, height: 1200 });
    await page.route(`**/product-image-assets/${sourceId}/renditions`, async (route) => {
      if (route.request().method() === "POST") await route.fulfill({ status: 503, json: { detail: "交付图提交测试失败" } });
      else await route.continue();
    });
    await page.getByRole("button", { name: "生成交付图", exact: true }).click();
    await expect(page.locator("[data-delivery-rendition-panel]").getByRole("alert")).toContainText("交付图提交测试失败");
    expect((await workflowGraph(page)).nodes.find((node) => node.id === imageId)?.preview_asset_id).toBe(sourceId);
    const runs = await page.request.get(workflowRunsPath(page, graph));
    expect((await runs.json()).items).toHaveLength(1);
  });
});

function sha256(bytes: Buffer): string {
  return createHash("sha256").update(bytes).digest("hex");
}

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
  const sourceId = (await workflowGraph(page)).nodes.find((node) => node.id === imageId)!.preview_asset_id!;
  expect(sourceId).toBeTruthy();
  return { graph, imageId, sourceId, runId: run.id };
}

async function createRendition(page: Page, graph: GraphProjection, imageId: string, sourceId: string, key: string, width: number, height: number): Promise<DeliveryRenditionJob> {
  await page.locator(`[data-delivery-preset-key="${key}"]`).click();
  await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === imageId)?.config.delivery_spec).toMatchObject({ width, height, format: "png" });
  const responsePromise = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/v2/product-image-assets/${sourceId}/renditions`);
  await page.getByRole("button", { name: "生成交付图", exact: true }).click();
  const response = await responsePromise;
  expect(response.ok(), await response.text()).toBeTruthy();
  const job: DeliveryRenditionJob = await response.json();
  await expect.poll(async () => {
    const reply = await page.request.get(`/api/v2/delivery-rendition-jobs/${job.id}`);
    expect(reply.ok(), await reply.text()).toBeTruthy();
    return (await reply.json()).status;
  }, { timeout: 30_000 }).toBe("succeeded");
  const result = await page.request.get(`/api/v2/delivery-rendition-jobs/${job.id}`);
  const current = await workflowGraph(page);
  expect(current.id).toBe(graph.id);
  expect(current.nodes.find((node) => node.id === imageId)?.preview_asset_id).toBe(sourceId);
  return result.json();
}
