import { expect, test, type Page } from "@playwright/test";

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

  for (const width of [1440, 1024, 390]) {
    test(`full recipe creation cancels without writes and confirms once at ${width}px`, async ({ page }) => {
      const source = await createWorkflow(page);
      const sourceProductId = workflowProductId(page);
      const sourceAssetId = await runImage(page, source, source.nodes.find((node) => node.node_type === "image_generation")!.id);
      const recipe = await saveRecipe(page, "保存完整工作流预设");
      const recipeNodes = recipe.current_version.payload.nodes;
      await page.setViewportSize({ width, height: 900 });
      await page.goto("/products/new");
      await page.locator('[data-create-mode="recipe"]').click();
      const name = `recipe target ${width} ${Date.now()}`;
      await page.locator("#recipe-product-name").fill(name);
      await page.locator("#create-recipe").selectOption(recipe.id);
      await page.locator('[data-create-recipe-form] input[type="file"]').setInputFiles(REFERENCE_PRODUCT_IMAGE);
      await page.locator("#recipe-source-note").fill("这是新商品的资料，不复用原商品身份");
      const writes: string[] = [];
      page.on("request", (request) => {
        if (request.method() === "POST" && new URL(request.url()).pathname === "/api/v3/products/from-recipe") writes.push(request.url());
      });
      await page.getByRole("button", { name: "预览配方", exact: true }).click();
      const dialog = page.getByRole("dialog");
      await expect(dialog.locator("[data-recipe-preview]")).toHaveAttribute("data-recipe-preview-mode", "create");
      await expect(dialog.locator("[data-recipe-preview-nodes] li")).toHaveCount(recipeNodes.length);
      await dialog.getByRole("button", { name: "取消", exact: true }).click();
      expect(writes).toHaveLength(0);
      const products = await page.request.get(`/api/v2/products?q=${encodeURIComponent(name)}`);
      expect((await products.json()).total).toBe(0);
      await page.getByRole("button", { name: "预览配方", exact: true }).click();
      await expect(dialog.getByRole("button", { name: "确认并创建商品", exact: true })).toBeEnabled();
      let firstProductId: string | undefined;
      if (width === 1440) {
        // Drop the response at fetch's application boundary; CDP interception omits upload bytes.
        await page.evaluate(() => {
          const fetch = window.fetch.bind(window);
          let dropped = false;
          window.fetch = async (input, init) => {
            const response = await fetch(input, init);
            if (!dropped && typeof input === "string" && input.endsWith("/api/v3/products/from-recipe") && response.ok) {
              dropped = true;
              throw new TypeError("Injected response loss after commit");
            }
            return response;
          };
        });
        const firstConfirmation = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/v3/products/from-recipe");
        await dialog.getByRole("button", { name: "确认并创建商品", exact: true }).click();
        const applied = await firstConfirmation;
        expect(applied.status(), await applied.text()).toBe(201);
        firstProductId = (await applied.json()).product.id;
        await expect(dialog.getByRole("alert")).toBeVisible();
      }
      const confirmed = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/v3/products/from-recipe");
      await dialog.getByRole("button", { name: "确认并创建商品", exact: true }).click();
      const response = await confirmed;
      expect(response.status(), await response.text()).toBe(width === 1440 ? 200 : 201);
      const result = await response.json();
      if (width === 1440) expect(result.product.id).toBe(firstProductId);
      await page.waitForURL(`**/products/${result.product.id}`);
      await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
      expect(writes).toHaveLength(width === 1440 ? 2 : 1);
      const graph = await workflowGraph(page);
      expect(graph.product_id).toBe(result.product.id);
      expect(graph.id).not.toBe(source.id);
      expect(graph.nodes).toHaveLength(recipeNodes.length);
      expect(graph.edges).toHaveLength(source.edges.length);
      expect(graph.groups).toHaveLength(source.groups.length);
      for (const node of graph.nodes) {
        expect(source.nodes.some((old) => old.id === node.id)).toBe(false);
        expect(node.bound_asset_id).toBeNull();
        expect(node.current_artifact_id).toBeNull();
        expect(node.preview_asset_id).toBeNull();
        if (node.node_type === "product_source") {
          expect(node.config.source_product_id).toBe(result.product.id);
          expect(node.source_product?.name).toBe(name);
        }
      }
      for (const edge of graph.edges) expect(source.edges.some((old) => old.id === edge.id)).toBe(false);
      expect(JSON.stringify(graph)).not.toContain(sourceProductId);
      expect(JSON.stringify(graph)).not.toContain(sourceAssetId);
      await page.reload();
      await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
      expect((await workflowGraph(page)).nodes).toEqual(graph.nodes);
      const after = await page.request.get(`/api/v2/products?q=${encodeURIComponent(name)}`);
      expect((await after.json()).total).toBe(1);
    });
  }

  test("recipe creation layout supports all locales and themes", async ({ page, browser }, testInfo) => {
    const source = await createWorkflow(page);
    const recipe = await saveRecipe(page, "保存完整工作流预设", `完整配方-${"workflow".repeat(30)}`);
    const storageState = await page.context().storageState();
    for (const width of [1440, 1024, 390]) {
      for (const theme of ["light", "dark"] as const) {
        for (const locale of ["zh-CN", "en-US", "ja-JP", "vi-VN"]) {
          const context = await browser.newContext({ storageState, viewport: { width, height: 900 }, reducedMotion: "reduce", colorScheme: theme });
          try {
            const p = await context.newPage();
            const errors: string[] = [];
            p.on("pageerror", (error) => errors.push(error.message));
            p.on("console", (message) => { if (message.type() === "error") errors.push(message.text()); });
            p.on("response", (response) => { if (response.status() >= 400) errors.push(`${response.status()} ${response.url()}`); });
            await p.addInitScript(({ locale, theme }) => { localStorage.setItem("productflow.locale", locale); localStorage.setItem("productflow.theme", theme); }, { locale, theme });
            await p.goto(`${new URL(page.url()).origin}/products/new`);
            await p.locator('[data-create-mode="recipe"]').click();
            await p.locator("#recipe-product-name").fill("商品 Product サンプル Sản phẩm");
            await p.locator("#create-recipe").selectOption(recipe.id);
            await p.locator('[data-create-recipe-form] input[type="file"]').setInputFiles(REFERENCE_PRODUCT_IMAGE);
            await expect(p.locator('[data-create-recipe-form] img')).toBeVisible();
            expect(await p.locator('[data-create-recipe-form] img').evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
            const dimensions = await p.evaluate(() => ({ viewport: innerWidth, client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
            expect(dimensions.viewport).toBe(width);
            expect(dimensions.client).toBe(width);
            expect(dimensions.scroll).toBeLessThanOrEqual(dimensions.client);
            const submit = p.locator('[data-create-recipe-form] button[type="submit"]');
            await submit.scrollIntoViewIfNeeded();
            await p.screenshot({ path: testInfo.outputPath(`form-${width}-${theme}-${locale}.png`) });
            await submit.click();
            const dialog = p.getByRole("dialog");
            await expect(dialog.locator("[data-recipe-preview]")).toBeVisible();
            for (const button of await dialog.getByRole("button").all()) {
              const bounds = await button.boundingBox();
              expect(bounds).not.toBeNull();
              expect(bounds!.x).toBeGreaterThanOrEqual(0);
              expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
              expect(bounds!.y).toBeGreaterThanOrEqual(0);
              expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(900);
              expect(await button.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
            }
            await p.screenshot({ path: testInfo.outputPath(`preview-${width}-${theme}-${locale}.png`) });
            expect(errors).toEqual([]);
          } finally { await context.close(); }
        }
      }
    }
    expect((await workflowGraph(page)).id).toBe(source.id);
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

async function saveRecipe(page: Page, label: string, title = `asset-recipe ${Date.now()}`): Promise<WorkflowRecipeSummary> {
  await page.locator('[data-sidebar-tool="add"]').filter({ visible: true }).click();
  await page.getByRole("button", { name: new RegExp(label) }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("预设名称").fill(title);
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
