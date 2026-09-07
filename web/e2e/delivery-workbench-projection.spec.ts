import { expect, test, type Page } from "@playwright/test";

import type { GraphProjection } from "../src/lib/types";
import { lockLocale, loginAsAdmin, requiredEnv } from "./liveGraph";
import {
  applyWorkflowFixture,
  createWorkflow,
  workflowRunsPath,
} from "./canvasWorkflow";

test.describe("delivery workbench projection", () => {
  test.skip(process.env.PRODUCTFLOW_RUN_CANVAS_WORKFLOW !== "1", "set PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 with an isolated mock stack");
  test.setTimeout(120_000);

  test.beforeEach(async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  });

  test("results view lists every image node once and reuses run/locate without create or agent", async ({ page }) => {
    let graph = await createWorkflow(page);
    graph = await ensureUngroupedAndEvidence(page, graph);

    const generationIds = graph.nodes
      .filter((node) => node.node_type === "image_generation")
      .map((node) => node.id)
      .sort();
    const evidenceIds = graph.nodes
      .filter((node) => node.node_type === "image_asset" && node.config.role === "evidence")
      .map((node) => node.id);
    expect(generationIds.length).toBeGreaterThanOrEqual(3);
    expect(evidenceIds.length).toBeGreaterThanOrEqual(1);

    await expect(page.locator('[data-graph-main-view-panel="flow"]')).toBeVisible();
    await expect(page.locator("[data-graph-shot-filmstrip]")).toBeVisible();

    const createRequests: string[] = [];
    const agentTurnRequests: string[] = [];
    const runBodies: unknown[] = [];
    page.on("request", (request) => {
      const url = new URL(request.url());
      const method = request.method();
      if (method === "POST" && /\/products\/?$/.test(url.pathname)) createRequests.push(url.pathname);
      if (method === "POST" && url.pathname.includes("/agent-conversations") && url.pathname.endsWith("/turns")) {
        agentTurnRequests.push(url.pathname);
      }
      if (method === "POST" && url.pathname === workflowRunsPath(page, graph)) {
        runBodies.push(request.postDataJSON());
      }
    });

    await page.locator('[data-graph-main-view="results"]').click();
    await expect(page.locator("[data-graph-results-view]")).toBeVisible();
    await expect(page.locator('[data-graph-main-view-panel="results"]')).toBeVisible();
    await expect(page.locator("[data-graph-shot-filmstrip]")).toHaveCount(0);

    const resultItems = page.locator("[data-graph-result-item]");
    await expect(resultItems).toHaveCount(generationIds.length + evidenceIds.length);
    const renderedIds = await resultItems.evaluateAll((nodes) => (
      nodes.map((node) => node.getAttribute("data-graph-result-item")).filter(Boolean).sort()
    ));
    expect(renderedIds).toEqual([...generationIds, ...evidenceIds].sort());
    expect(new Set(renderedIds).size).toBe(renderedIds.length);

    const missing = graph.nodes.find((node) => (
      node.node_type === "image_generation" && !node.preview_asset_id
    ));
    expect(missing).toBeTruthy();
    await page.locator(`[data-graph-result-item="${missing!.id}"] [data-graph-result-select]`).click();
    await expect(page.locator(`[data-inspector-node-id="${missing!.id}"]`)).toBeVisible();

    const failedOrIdle = missing!;
    await page.locator(`[data-graph-result-item="${failedOrIdle.id}"] [data-graph-result-run]`).click();
    await expect.poll(() => runBodies.length).toBe(1);
    expect(runBodies[0]).toEqual({ scope: "node", node_id: failedOrIdle.id });

    await page.locator(`[data-graph-result-item="${failedOrIdle.id}"] [data-graph-result-locate]`).click();
    await expect(page.locator('[data-graph-main-view-panel="flow"]')).toBeVisible();
    await expect(page.locator(`[data-workflow-node-id="${failedOrIdle.id}"]`)).toBeVisible();

    expect(createRequests).toEqual([]);
    expect(agentTurnRequests).toEqual([]);

    await page.setViewportSize({ width: 1440, height: 960 });
    await page.locator('[data-graph-main-view="results"]').click();
    await expect(page.locator("[data-graph-results-view]")).toBeVisible();
    await page.setViewportSize({ width: 1280, height: 800 });
    await expect(page.locator("[data-graph-results-scroll]")).toBeVisible();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator("[data-graph-results-view]")).toBeVisible();
    await expect(page.locator(`[data-graph-result-item="${failedOrIdle.id}"] [data-graph-result-run]`)).toBeVisible();
  });

  test("save conflict before result run does not submit", async ({ page }) => {
    const graph = await createWorkflow(page);
    const image = graph.nodes.find((node) => node.node_type === "image_generation")!;
    const prompt = graph.nodes.find((node) => (
      node.node_type === "image_prompt" && node.group_id === image.group_id
    ))!;

    await page.locator(`[data-workflow-node-id="${prompt.id}"]`).click();
    await expect(page.locator(`[data-inspector-node-id="${prompt.id}"]`)).toBeVisible();

    let saves = 0;
    let submits = 0;
    page.on("request", (request) => {
      if (request.method() === "POST" && new URL(request.url()).pathname === workflowRunsPath(page, graph)) submits += 1;
    });
    await page.route("**/changesets", async (route) => {
      saves += 1;
      await route.fulfill({ status: 409, json: { detail: "画布版本冲突" } });
    });

    await page.getByLabel("设计目标").fill("成果视图冲突草稿");
    await page.locator('[data-graph-main-view="results"]').click();
    await expect(page.locator("[data-graph-results-view]")).toBeVisible();
    await page.locator(`[data-graph-result-item="${image.id}"] [data-graph-result-run]`).click();
    await expect.poll(() => saves).toBe(1);
    await page.waitForTimeout(800);
    expect(submits).toBe(0);
  });
});

async function ensureUngroupedAndEvidence(page: Page, graph: GraphProjection): Promise<GraphProjection> {
  const hasUngrouped = graph.nodes.some((node) => node.node_type === "image_generation" && !node.group_id);
  const hasEvidence = graph.nodes.some((node) => node.node_type === "image_asset" && node.config.role === "evidence");
  if (hasUngrouped && hasEvidence) return graph;

  const operations: Array<Record<string, unknown>> = [];
  if (!hasUngrouped) {
    operations.push({
      op: "create_node",
      client_ref: "ungrouped-image",
      node_type: "image_generation",
      title: "未分组生图",
      position_x: 80,
      position_y: 720,
      config: { image_type_key: "detail", generation_spec: { aspect_ratio: "1:1" } },
    });
  }
  if (!hasEvidence) {
    operations.push({
      op: "create_node",
      client_ref: "evidence-slot",
      node_type: "image_asset",
      title: "认证材料",
      position_x: 80,
      position_y: 900,
      config: { role: "evidence" },
    });
  }
  return applyWorkflowFixture(page, operations as never);
}
