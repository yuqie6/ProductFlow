import { expect, test, type Page } from "@playwright/test";

import {
  CANVAS_DOCUMENT_SWITCH,
  REFERENCE_PRODUCT_IMAGE,
  assertMockImageProviders,
  lockLocale,
  loginAsAdmin,
  openCanvasView,
  requiredEnv,
  selectCreateImageType,
  waitForGraphRunSucceeded,
} from "./liveGraph";

interface GraphNodePayload {
  id: string;
  node_type: string;
  document_origin?: string | null;
  pending_candidate_artifact_id?: string | null;
  config: {
    prompt?: {
      design_goal?: string;
      composition?: { layout?: string; product_share_percent?: number };
    };
  };
}

interface GraphPayload {
  id: string;
  revision: number;
  nodes: GraphNodePayload[];
}

async function createWorkbench(page: Page): Promise<string> {
  await page.goto("/products/new");
  await page.locator("#agent-product-name").fill(`canvas-document ${Date.now()}`);
  await page.locator("#agent-product-brief").fill("文稿改写候选验证，使用 mock 供应商。");
  await selectCreateImageType(page, "detail");
  await page.locator('[data-image-type="detail"] input[type="number"]').fill("1");
  await page.locator("[data-agent-product-intake-form] input[type='file']").setInputFiles(REFERENCE_PRODUCT_IMAGE);
  await expect(page.locator("[data-create-direct]")).toHaveAttribute("data-create-direct-ready", "true");
  await Promise.all([
    page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/),
    page.locator("[data-create-direct]").click(),
  ]);
  await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
  await openCanvasView(page);
  return new URL(page.url()).pathname.split("/").filter(Boolean).at(-1) ?? "";
}

async function currentGraph(page: Page, productID: string): Promise<GraphPayload> {
  const response = await page.request.get(`/api/v3/products/${encodeURIComponent(productID)}/workflows/current`);
  expect(response.ok(), await response.text()).toBeTruthy();
  return await response.json() as GraphPayload;
}

async function authorPrompt(page: Page, productID: string): Promise<GraphNodePayload> {
  const graph = await currentGraph(page, productID);
  const prompt = graph.nodes.find((node) => node.node_type === "image_prompt");
  expect(prompt).toBeTruthy();
  const config = {
    ...prompt!.config,
    prompt: {
      ...(prompt!.config.prompt ?? {}),
      design_goal: "人工目标-不要被构图应用改掉",
      composition: {
        ...(prompt!.config.prompt?.composition ?? {}),
        layout: "人工左侧留白",
        product_share_percent: 55,
      },
    },
  };
  const patched = await page.request.post(
    `/api/v3/products/${encodeURIComponent(productID)}/workflows/${encodeURIComponent(graph.id)}/changesets`,
    {
      data: {
        base_graph_revision: graph.revision,
        summary: "手填提示词",
        operations: [{ op: "update_node_config", node_ref: prompt!.id, config }],
      },
    },
  );
  expect(patched.ok(), await patched.text()).toBeTruthy();
  const after = await currentGraph(page, productID);
  const authored = after.nodes.find((node) => node.id === prompt!.id);
  expect(authored?.document_origin).toBe("authored");
  return authored!;
}

async function selectNode(page: Page, nodeID: string): Promise<void> {
  const card = page.locator(`[data-workflow-node-id="${nodeID}"]`);
  await expect(card).toBeVisible();
  await card.evaluate((element: HTMLElement) => {
    element.dispatchEvent(new MouseEvent("click", {
      bubbles: true,
      cancelable: true,
      view: window,
      buttons: 1,
    }));
  });
}

test.describe("canvas document mock provider", () => {
  test.skip(process.env[CANVAS_DOCUMENT_SWITCH] !== "1", `set ${CANVAS_DOCUMENT_SWITCH}=1`);

  test("rewrite stages a candidate and section apply keeps unselected fields", async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await assertMockImageProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"));
    const productID = await createWorkbench(page);
    const prompt = await authorPrompt(page, productID);
    await page.reload();
    await openCanvasView(page);
    await selectNode(page, prompt.id);
    await expect(page.locator("[data-graph-inspector-rewrite]")).toBeEnabled();
    const graph = await currentGraph(page, productID);
    await page.locator("[data-graph-inspector-rewrite]").click();
    await waitForGraphRunSucceeded(page.request, productID, graph.id);
    await expect.poll(async () => {
      const latest = await currentGraph(page, productID);
      return latest.nodes.find((node) => node.id === prompt.id)?.pending_candidate_artifact_id ?? null;
    }).not.toBeNull();
    const liveBeforeApply = await currentGraph(page, productID);
    const beforeNode = liveBeforeApply.nodes.find((node) => node.id === prompt.id);
    expect(beforeNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
    expect(beforeNode?.config.prompt?.composition?.layout).toBe("人工左侧留白");

    await selectNode(page, prompt.id);
    const review = page.locator("[data-graph-document-candidate]");
    await expect(review).toBeVisible();
    const composition = review.getByRole("button", { name: "构图" });
    await expect(composition).toBeVisible();
    if ((await composition.getAttribute("aria-pressed")) !== "true") {
      await composition.click();
    }
    const objective = review.getByRole("button", { name: "目标" });
    if (await objective.isVisible() && (await objective.getAttribute("aria-pressed")) === "true") {
      await objective.click();
    }
    await review.getByRole("button", { name: "应用所选" }).click();
    await expect.poll(async () => {
      const latest = await currentGraph(page, productID);
      return latest.nodes.find((node) => node.id === prompt.id)?.pending_candidate_artifact_id ?? null;
    }).toBeNull();
    const applied = await currentGraph(page, productID);
    const appliedNode = applied.nodes.find((node) => node.id === prompt.id);
    expect(appliedNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
    expect(appliedNode?.config.prompt?.composition?.layout).not.toBe("人工左侧留白");
  });

  test("complete and replace stage candidates without writing live config", async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await assertMockImageProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"));
    const productID = await createWorkbench(page);
    const prompt = await authorPrompt(page, productID);
    await page.reload();
    await openCanvasView(page);
    await selectNode(page, prompt.id);

    await page.locator("[data-graph-inspector-complete]").click();
    const graph = await currentGraph(page, productID);
    await waitForGraphRunSucceeded(page.request, productID, graph.id);
    await expect.poll(async () => {
      const latest = await currentGraph(page, productID);
      return latest.nodes.find((node) => node.id === prompt.id)?.pending_candidate_artifact_id ?? null;
    }).not.toBeNull();
    const afterComplete = await currentGraph(page, productID);
    const completeNode = afterComplete.nodes.find((node) => node.id === prompt.id);
    expect(completeNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
    expect(completeNode?.config.prompt?.composition?.layout).toBe("人工左侧留白");

    await selectNode(page, prompt.id);
    await page.locator("[data-graph-document-candidate]").getByRole("button", { name: "放弃建议" }).click();
    await expect.poll(async () => {
      const latest = await currentGraph(page, productID);
      return latest.nodes.find((node) => node.id === prompt.id)?.pending_candidate_artifact_id ?? null;
    }).toBeNull();

    await selectNode(page, prompt.id);
    await page.locator("[data-graph-inspector-replace]").click();
    await page.getByRole("dialog").getByRole("button", { name: "重新生成" }).click();
    await waitForGraphRunSucceeded(page.request, productID, graph.id);
    await expect.poll(async () => {
      const latest = await currentGraph(page, productID);
      return latest.nodes.find((node) => node.id === prompt.id)?.pending_candidate_artifact_id ?? null;
    }).not.toBeNull();
    const afterReplace = await currentGraph(page, productID);
    const replaceNode = afterReplace.nodes.find((node) => node.id === prompt.id);
    expect(replaceNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
    expect(replaceNode?.config.prompt?.composition?.layout).toBe("人工左侧留白");
  });
});
