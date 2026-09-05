import { expect, test, type Page } from "@playwright/test";
import { readFile } from "node:fs/promises";

import type { GraphProjection, GraphRun, WorkflowRecipePreview, WorkflowRecipeSummary } from "../src/lib/types";
import { assertMockImageProviders, lockLocale, loginAsAdmin, REFERENCE_PRODUCT_IMAGE, requiredEnv } from "./liveGraph";
import { authorWorkflowPrompt, createWorkflow, selectWorkflowNode, waitForWorkflowRun, workflowGraph, workflowProductId, workflowRunsPath } from "./canvasWorkflow";

test.use({ trace: "on", screenshot: "on" });

test.describe("canvas asset and recipe identity", () => {
  test.skip(process.env.PRODUCTFLOW_RUN_CANVAS_WORKFLOW !== "1", "requires an isolated mock stack");
  test.setTimeout(120_000);

  test.beforeEach(async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await assertMockImageProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"));
  });

  test("pin preserves the chosen asset after another generation and a gallery drop adds a reference", async ({ page }) => {
    const graph = await createWorkflow(page);
    const image = graph.nodes.find((node) => node.node_type === "image_generation")!;
    const sourceId = await runImage(page, graph, image.id);
    await selectWorkflowNode(page, image.id);
    await page.locator("[data-graph-pin-asset]").click();
    await expect.poll(async () => (await workflowGraph(page)).nodes.length).toBe(graph.nodes.length + 1);
    const pinnedGraph = await workflowGraph(page);
    const pinned = pinnedGraph.nodes.find((node) => !graph.nodes.some((old) => old.id === node.id))!;
    expect(pinned).toMatchObject({ node_type: "image_asset", bound_asset_id: sourceId, config: {} });
    expect(pinnedGraph.edges).toEqual(graph.edges);
    const prompt = graph.nodes.find((node) => node.node_type === "image_prompt" && image.incoming.some((edge) => edge.node_id === node.id))!;
    await authorWorkflowPrompt(page, prompt.id, "增加杯口细节，保留已固定的原结果");
    const nextSourceId = await runImage(page, graph, image.id);
    expect(nextSourceId).not.toBe(sourceId);
    expect((await workflowGraph(page)).nodes.find((node) => node.id === pinned.id)?.bound_asset_id).toBe(sourceId);

    await page.locator('[data-sidebar-tool="library"]').filter({ visible: true }).click();
    await page.getByRole("button", { name: "适配全图", exact: true }).click();
    const card = page.locator(`[data-gallery-asset-id="${nextSourceId}"]`);
    const target = page.locator(`[data-id="${prompt.id}"] [data-handleid="reference"]`);
    await expect(card).toBeVisible();
    await expect(target).toBeVisible();
    await card.dragTo(target);
    await expect.poll(async () => (await workflowGraph(page)).nodes.length).toBe(pinnedGraph.nodes.length + 1);
    const dropped = await workflowGraph(page);
    const reference = dropped.nodes.find((node) => node.node_type === "image_asset" && node.bound_asset_id === nextSourceId)!;
    expect(reference).toBeTruthy();
    expect(dropped.edges.filter((edge) => edge.source_node_id === reference.id)).toMatchObject([
      { target_node_id: prompt.id, role: "reference" },
    ]);
    expect(dropped.nodes.find((node) => node.id === pinned.id)?.bound_asset_id).toBe(sourceId);
    await page.reload();
    await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
    await expect(page.locator(`[data-workflow-node-id="${reference.id}"]`)).toBeVisible();
    expect((await workflowGraph(page)).nodes.find((node) => node.id === reference.id)?.bound_asset_id).toBe(nextSourceId);
  });

  test("fragment confirmation preserves the target graph and strips source identities and outputs", async ({ page }) => {
    const source = await createWorkflow(page);
    const image = source.nodes.find((node) => node.node_type === "image_generation")!;
    const assetId = await runImage(page, source, image.id);
    await selectWorkflowNode(page, image.id);
    await page.locator("[data-graph-pin-asset]").click();
    await expect.poll(async () => (await workflowGraph(page)).nodes.length).toBe(source.nodes.length + 1);
    const sourceGraph = await workflowGraph(page);
    const sourceProductId = workflowProductId(page);
    await selectWorkflowNode(page, image.id);
    const recipe = await saveRecipe(page, "保存当前分组为预设");
    expect(recipe.kind).toBe("recipe_fragment");
    const payload = recipe.current_version.payload;
    expect(payload.nodes.length).toBeGreaterThan(2);
    expect(payload.edges.length).toBeGreaterThan(0);
    for (const id of [sourceProductId, sourceGraph.id, assetId]) {
      expect(JSON.stringify(payload)).not.toContain(id);
    }
    const target = await createWorkflow(page);
    const preview = await openRecipe(page, recipe);
    const dialog = page.getByRole("dialog");
    await expect(dialog.locator("[data-recipe-preview]")).toHaveAttribute("data-recipe-preview-mode", "merge");
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    expect((await workflowGraph(page)).revision).toBe(target.revision);
    expect((await workflowGraph(page)).nodes).toEqual(target.nodes);
    await openRecipe(page, recipe);
    await confirmRecipe(page, recipe);
    const applied = await workflowGraph(page);
    expect(applied.id).toBe(target.id);
    expect(applied.revision).toBe(target.revision + 1);
    for (const node of target.nodes) {
      expect(applied.nodes.find((item) => item.id === node.id)).toMatchObject({
        id: node.id, node_type: node.node_type, title: node.title, config: node.config,
        bound_asset_id: node.bound_asset_id, group_id: node.group_id,
        position_x: node.position_x, position_y: node.position_y,
      });
    }
    expect(applied.edges.filter((edge) => target.edges.some((old) => old.id === edge.id))).toEqual(target.edges);
    const added = applied.nodes.filter((node) => !target.nodes.some((old) => old.id === node.id));
    expect(added).toHaveLength(payload.nodes.length);
    expect(applied.edges.length).toBe(target.edges.length + preview.edges.length);
    expect(applied.groups.length).toBe(target.groups.length + payload.groups.length);
    for (const node of added) {
      expect(sourceGraph.nodes.some((sourceNode) => sourceNode.id === node.id)).toBe(false);
      expect(node.bound_asset_id).toBeNull();
      expect(node.preview_asset_id).toBeNull();
      expect(node.current_artifact_id).toBeNull();
    }
    const addedIds = new Set(added.map((node) => node.id));
    const newEdges = applied.edges.filter((edge) => !target.edges.some((old) => old.id === edge.id));
    const internal = newEdges.filter((edge) => addedIds.has(edge.source_node_id));
    expect(internal).toHaveLength(payload.edges.length);
    for (const edge of newEdges) {
      expect(addedIds.has(edge.target_node_id)).toBe(true);
      if (!addedIds.has(edge.source_node_id)) {
        expect(target.nodes.some((node) => node.id === edge.source_node_id)).toBe(true);
      }
    }
    for (const edge of payload.edges) {
      const sourceNode = payload.nodes.find((node) => node.key === edge.source_node_key)!;
      const targetNode = payload.nodes.find((node) => node.key === edge.target_node_key)!;
      expect(internal).toContainEqual(expect.objectContaining({
        source_node_id: added.find((node) => node.title === sourceNode.title)!.id,
        target_node_id: added.find((node) => node.title === targetNode.title)!.id,
        role: edge.role, order: edge.order,
      }));
    }
    await page.reload();
    await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
    await expect(page.locator("[data-workflow-node-id]")).toHaveCount(applied.nodes.length);
    expect((await workflowGraph(page)).nodes).toEqual(applied.nodes);
  });

  test("full recipe rejects an existing graph without writing it", async ({ page }) => {
    const source = await createWorkflow(page);
    const recipe = await saveRecipe(page, "保存完整工作流预设");
    await openRecipe(page, recipe);
    const dialog = page.getByRole("dialog");
    await expect(dialog).toContainText("完整配方不能合并");
    await expect(dialog.getByRole("button", { name: "确认应用", exact: true })).toBeDisabled();
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    expect((await workflowGraph(page)).revision).toBe(source.revision);

  });

  test("graphless product can reach the recipe library @known-gap", async ({ page }) => {
    test.skip(process.env.PRODUCTFLOW_PROBE_FULL_RECIPE_ENTRY !== "1", "canvas-full-recipe-entry: opt-in reproduction of the unresolved entry gap");
    const created = await page.request.post("/api/v2/products", { multipart: {
      name: `recipe graphless ${Date.now()}`,
      images: { name: "reference.png", mimeType: "image/png", buffer: await readFile(REFERENCE_PRODUCT_IMAGE) },
    } });
    expect(created.ok(), await created.text()).toBeTruthy();
    const { product } = await created.json();
    await page.goto(`/products/${product.id}`);
    await expect(page.getByRole("alert")).toHaveText("商品还没有 Agent 工作区");
    expect((await page.request.get(`/api/v3/products/${product.id}/workflows/current`)).status()).toBe(404);
    await expect(page.locator('[data-sidebar-tool="recipes"]').filter({ visible: true })).toHaveCount(1, { timeout: 1000 });
  });
});

async function runImage(page: Page, graph: GraphProjection, nodeId: string): Promise<string> {
  await selectWorkflowNode(page, nodeId);
  const responsePromise = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === workflowRunsPath(page, graph));
  await page.locator("[data-graph-inspector-run-node]").click();
  const response = await responsePromise;
  expect(response.ok(), await response.text()).toBeTruthy();
  const run: GraphRun = await response.json();
  await waitForWorkflowRun(page, graph, run.id, "succeeded");
  const assetId = (await workflowGraph(page)).nodes.find((node) => node.id === nodeId)!.preview_asset_id;
  expect(assetId).toBeTruthy();
  return assetId!;
}

async function saveRecipe(page: Page, label: string): Promise<WorkflowRecipeSummary> {
  await page.locator('[data-sidebar-tool="add"]').filter({ visible: true }).click();
  await page.getByRole("button", { name: new RegExp(label) }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("预设名称").fill(`asset-recipe ${Date.now()}`);
  const responsePromise = page.waitForResponse((response) => response.request().method() === "POST" && /\/workflows\/[^/]+\/recipes$/.test(new URL(response.url()).pathname));
  await dialog.getByRole("button", { name: "保存预设", exact: true }).click();
  const response = await responsePromise;
  expect(response.ok(), await response.text()).toBeTruthy();
  await expect(dialog).not.toBeVisible();
  return response.json();
}

async function openRecipe(page: Page, recipe: WorkflowRecipeSummary): Promise<WorkflowRecipePreview> {
  await page.locator('[data-sidebar-tool="recipes"]').filter({ visible: true }).click();
  const responsePromise = page.waitForResponse((response) => response.request().method() === "POST" && response.url().endsWith(`/workflow-recipes/${recipe.id}/preview`));
  await page.locator("[data-graph-recipe-panel] article").filter({ has: page.getByRole("heading", { name: recipe.current_version.title, exact: true }) }).getByRole("button", { name: "应用", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  return (await responsePromise).json();
}

async function confirmRecipe(page: Page, recipe: WorkflowRecipeSummary): Promise<void> {
  const responsePromise = page.waitForResponse((response) => response.request().method() === "POST" && response.url().endsWith(`/workflow-recipes/${recipe.id}/apply`));
  await page.getByRole("dialog").getByRole("button", { name: "确认应用", exact: true }).click();
  const response = await responsePromise;
  expect(response.ok(), await response.text()).toBeTruthy();
  await expect(page.getByRole("dialog")).not.toBeVisible();
}
