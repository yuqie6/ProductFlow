import { expect, type Page } from "@playwright/test";

import type { GraphChangeSet, GraphProjection, GraphRun } from "../src/lib/types";
import { REFERENCE_PRODUCT_IMAGE, selectCreateImageType } from "./liveGraph";

export function workflowProductId(page: Page): string {
  return new URL(page.url()).pathname.split("/")[2];
}

export async function workflowGraph(page: Page): Promise<GraphProjection> {
  const response = await page.request.get(`/api/v3/products/${workflowProductId(page)}/workflows/current`);
  expect(response.ok(), await response.text()).toBeTruthy();
  return response.json();
}

export function workflowRunsPath(page: Page, graph: GraphProjection): string {
  return `/api/v3/products/${workflowProductId(page)}/workflows/${graph.id}/runs`;
}

export async function workflowRun(page: Page, graph: GraphProjection, runId: string): Promise<GraphRun> {
  const response = await page.request.get(`${workflowRunsPath(page, graph)}/${runId}`);
  expect(response.ok(), await response.text()).toBeTruthy();
  return response.json();
}

export async function waitForWorkflowRun(page: Page, graph: GraphProjection, runId: string, status: string): Promise<GraphRun> {
  await expect.poll(async () => (await workflowRun(page, graph, runId)).status, { timeout: 60_000 }).toBe(status);
  return workflowRun(page, graph, runId);
}

export async function createWorkflow(page: Page): Promise<GraphProjection> {
  await page.goto("/products/new");
  await page.locator("#agent-product-name").fill(`canvas-workflow ${Date.now()}`);
  await page.locator("#agent-product-brief").fill("陶瓷杯，保留杯身材质和把手结构。");
  for (const [kind, count] of [["detail", "2"], ["hero", "1"]]) {
    await selectCreateImageType(page, kind);
    await page.locator(`[data-image-type="${kind}"] input[type="number"]`).fill(count);
  }
  await page.locator("[data-agent-product-intake-form] input[type=file]").setInputFiles(REFERENCE_PRODUCT_IMAGE);
  await expect(page.locator("[data-create-direct]")).toHaveAttribute("data-create-direct-ready", "true");
  await Promise.all([
    page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/),
    page.locator("[data-create-direct]").click(),
  ]);
  await expect(page.locator("[data-graph-shot-filmstrip]")).toBeVisible();
  return workflowGraph(page);
}

export async function selectWorkflowNode(page: Page, nodeId: string): Promise<void> {
  await page.locator('[data-sidebar-tool="details"]').filter({ visible: true }).click();
  const node = page.locator(`[data-workflow-node-id="${nodeId}"]`);
  await expect(node).toBeVisible();
  await node.evaluate((element) => element.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, view: window })));
  await expect(page.locator("[data-graph-node-inspector]")).toBeVisible();
}

export async function authorWorkflowPrompt(page: Page, nodeId: string, goal: string): Promise<void> {
  await selectWorkflowNode(page, nodeId);
  await page.getByLabel("设计目标").fill(goal);
  await page.getByLabel("布局").fill("左侧保留留白");
  await expect.poll(async () => {
    const node = (await workflowGraph(page)).nodes.find((item) => item.id === nodeId);
    return { origin: node?.document_origin, prompt: node?.config.prompt };
  }).toMatchObject({ origin: "authored", prompt: { design_goal: goal, composition: { layout: "左侧保留留白" } } });
}

export async function applyWorkflowFixture(page: Page, operations: GraphChangeSet["operations"]): Promise<GraphProjection> {
  const graph = await workflowGraph(page);
  const response = await page.request.post(`/api/v3/products/${workflowProductId(page)}/workflows/${graph.id}/changesets`, {
    data: { base_graph_revision: graph.revision, summary: "canvas workflow fixture", operations },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  await page.reload();
  await expect(page.locator("[data-graph-shot-filmstrip]")).toBeVisible();
  return workflowGraph(page);
}

export async function bindWorkflowReference(page: Page, nodeId: string, assetId: string): Promise<void> {
  await selectWorkflowNode(page, nodeId);
  await page.locator("[data-graph-node-inspector]").getByRole("button", { name: "选图" }).click();
  await page.locator(`[data-gallery-asset-id="${assetId}"]`).getByLabel("更多操作").click();
  await page.getByRole("menuitem", { name: "再次引用" }).click();
  await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === nodeId)?.bound_asset_id).toBe(assetId);
}
