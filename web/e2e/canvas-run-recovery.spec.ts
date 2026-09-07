import { expect, test, type Page } from "@playwright/test";

import type { GraphProjection, GraphRun } from "../src/lib/types";
import { assertMockImageProviders, lockLocale, loginAsAdmin, requiredEnv } from "./liveGraph";
import {
  applyWorkflowFixture,
  authorWorkflowPrompt,
  bindWorkflowReference,
  createWorkflow,
  selectWorkflowNode,
  waitForWorkflowRun,
  workflowGraph,
  workflowRun,
  workflowRunsPath,
} from "./canvasWorkflow";

test.describe("canvas run recovery", () => {
  test.skip(process.env.PRODUCTFLOW_RUN_CANVAS_WORKFLOW !== "1", "set PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 with an isolated mock stack");
  test.setTimeout(120_000);

  test.beforeEach(async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await assertMockImageProviders(page.request);
  });

  test("scene run submits only its images and preserves authored and unrelated nodes", async ({ page }) => {
    const graph = await createWorkflow(page);
    const shot = detailShot(graph);
    const prompt = graph.nodes.find((node) => node.group_id === shot.id && node.node_type === "image_prompt")!;
    await authorWorkflowPrompt(page, prompt.id, "场景运行保留人工稿");
    const before = await workflowGraph(page);
    const requestBodies: unknown[] = [];
    page.on("request", (request) => {
      if (request.method() === "POST" && new URL(request.url()).pathname === workflowRunsPath(page, graph)) requestBodies.push(request.postDataJSON());
    });
    const run = await clickScene(page, graph, shot.id);
    const result = await waitForWorkflowRun(page, graph, run.id, "succeeded");
    const ids = before.nodes.filter((node) => node.group_id === shot.id && node.node_type === "image_generation").map((node) => node.id);
    expect(requestBodies).toHaveLength(1);
    expect(requestBodies[0]).toEqual({ scope: "selection", node_ids: expect.arrayContaining(ids) });
    expect(result.node_runs.map((node) => node.node_id).sort()).toEqual(ids.sort());
    const after = await workflowGraph(page);
    for (const node of before.nodes) {
      const current = after.nodes.find((item) => item.id === node.id)!;
      expect(current.config).toEqual(node.config);
      if (ids.includes(node.id)) expect(current.preview_asset_id).toBeTruthy();
      else expect(current.preview_asset_id).toBe(node.preview_asset_id);
    }
    expect(after.nodes.find((node) => node.id === prompt.id)?.document_origin).toBe("authored");
  });

  test("inspector retry uses repaired reference and retains the failed run", async ({ page }) => {
    const { graph, imageId, missingId, referenceId } = await missingReferenceFixture(page);
    await selectWorkflowNode(page, imageId);
    const responsePromise = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === workflowRunsPath(page, graph));
    await page.locator("[data-graph-inspector-run-node]").click();
    const response = await responsePromise;
    expect(response.ok(), await response.text()).toBeTruthy();
    const first: GraphRun = await response.json();
    await waitForWorkflowRun(page, graph, first.id, "failed");
    await expect(page.locator("[data-graph-node-inspector]")).toContainText("参考输入缺少已绑定的图片资产");
    await bindWorkflowReference(page, missingId, referenceId);
    await selectWorkflowNode(page, imageId);
    const retryPromise = page.waitForResponse((reply) => reply.request().method() === "POST" && new URL(reply.url()).pathname === `${workflowRunsPath(page, graph)}/${first.id}/retry`);
    await page.locator("[data-graph-node-inspector]").getByRole("button", { name: "重试", exact: true }).click();
    const retryResponse = await retryPromise;
    expect(retryResponse.ok(), await retryResponse.text()).toBeTruthy();
    const retried: GraphRun = await retryResponse.json();
    expect(retried.id).not.toBe(first.id);
    const result = await waitForWorkflowRun(page, graph, retried.id, "succeeded");
    expect(result.scope).toBe("node");
    expect(result.requested_node_id).toBe(imageId);
    expect(result.node_runs).toHaveLength(1);
    expect(result.node_runs[0].output?.product_image_asset_id).toBeTruthy();
    expect((await workflowRun(page, graph, first.id)).status).toBe("failed");
    expect((await workflowGraph(page)).nodes.find((node) => node.id === missingId)?.bound_asset_id).toBe(referenceId);
  });

  test("retry failed nodes excludes successful siblings and keeps their result", async ({ page }) => {
    const { graph, imageId, missingId, referenceId } = await missingReferenceFixture(page);
    const first = await clickScene(page, graph, detailShot(graph).id);
    const failed = await waitForWorkflowRun(page, graph, first.id, "failed");
    expect(failed.node_runs.filter((node) => node.status === "failed").map((node) => node.node_id)).toEqual([imageId]);
    expect(failed.node_runs.some((node) => node.status === "succeeded")).toBeTruthy();
    const before = await workflowGraph(page);
    await bindWorkflowReference(page, missingId, referenceId);
    await page.locator('[data-sidebar-tool="runs"]').filter({ visible: true }).click();
    const responsePromise = page.waitForResponse((reply) => reply.request().method() === "POST" && new URL(reply.url()).pathname === workflowRunsPath(page, graph));
    await page.locator(`[data-graph-run-id="${first.id}"]`).getByRole("button", { name: "重试失败节点", exact: true }).click();
    const response = await responsePromise;
    expect(response.request().postDataJSON()).toEqual({ scope: "selection", node_ids: [imageId] });
    expect(response.ok(), await response.text()).toBeTruthy();
    const submitted: GraphRun = await response.json();
    const result = await waitForWorkflowRun(page, graph, submitted.id, "succeeded");
    expect(result.node_runs.map((node) => node.node_id)).toEqual([imageId]);
    const after = await workflowGraph(page);
    for (const sibling of before.nodes.filter((node) => node.id !== imageId && node.node_type === "image_generation")) {
      expect(after.nodes.find((node) => node.id === sibling.id)?.preview_asset_id).toBe(sibling.preview_asset_id);
    }
    expect((await workflowRun(page, graph, first.id)).status).toBe("failed");
  });

  test("a rejected inspector save prevents scene submission and retains the draft", async ({ page }) => {
    const graph = await createWorkflow(page);
    const shot = detailShot(graph);
    const prompt = graph.nodes.find((node) => node.group_id === shot.id && node.node_type === "image_prompt")!;
    await selectWorkflowNode(page, prompt.id);
    let saves = 0;
    let submits = 0;
    page.on("request", (request) => {
      if (request.method() === "POST" && new URL(request.url()).pathname === workflowRunsPath(page, graph)) submits++;
    });
    await page.route("**/changesets", async (route) => {
      saves++;
      await route.fulfill({ status: 409, json: { detail: "画布版本冲突" } });
    });
    await page.getByLabel("设计目标").fill("冲突草稿应保留");
    await sceneButton(page, shot.id).click();
    await expect.poll(() => saves).toBe(1);
    await expect(page.locator('[data-graph-save-status="failed"]')).toBeVisible();
    await page.waitForTimeout(1000);
    expect(saves).toBe(1);
    expect(submits).toBe(0);
    await expect(page.getByLabel("设计目标")).toHaveValue("冲突草稿应保留");
    expect((await workflowGraph(page)).revision).toBe(graph.revision);
  });
});

function detailShot(graph: GraphProjection) {
  const image = graph.nodes.find((node) => node.node_type === "image_generation" && node.config.image_type_key === "detail")!;
  return graph.groups.find((group) => group.id === image.group_id)!;
}

function sceneButton(page: Page, groupId: string) {
  return page.locator(`[data-graph-shot-id="${groupId}"] [data-graph-shot-run]`);
}

async function clickScene(page: Page, graph: GraphProjection, groupId: string): Promise<GraphRun> {
  const responsePromise = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === workflowRunsPath(page, graph));
  await sceneButton(page, groupId).click();
  const response = await responsePromise;
  expect(response.ok(), await response.text()).toBeTruthy();
  return response.json();
}

async function missingReferenceFixture(page: Page) {
  let graph = await createWorkflow(page);
  const imageId = graph.nodes.find((node) => node.node_type === "image_generation" && node.group_id === detailShot(graph).id)!.id;
  const referenceId = graph.nodes.find((node) => node.node_type === "image_asset" && node.bound_asset_id)!.bound_asset_id!;
  graph = await applyWorkflowFixture(page, [
    { op: "create_node", client_ref: "missing", node_type: "image_asset", title: "待修复参考", position_x: 0, position_y: 0, config: { role: "product_identity" } },
    { op: "connect_nodes", client_ref: "missing-edge", source_ref: "missing", target_ref: imageId, order: 1 },
  ]);
  const missingId = graph.nodes.find((node) => node.title === "待修复参考")!.id;
  return { graph, imageId, missingId, referenceId };
}
