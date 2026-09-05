import { expect, test } from "@playwright/test";
import { createWorkflow, selectWorkflowNode, workflowGraph, workflowProductId, workflowRunsPath, waitForWorkflowRun } from "./canvasWorkflow";
import { lockLocale, loginAsAdmin, requiredEnv, withMockDocumentProviders } from "./liveGraph";
import type { GraphRun, ProductFactsResponse } from "../src/lib/types";

test.describe("node detail redesign", () => {
  test.skip(process.env.PRODUCTFLOW_RUN_NODE_DETAIL !== "1", "requires an isolated mock stack");
  test("sparse picture changes survive reload, follow shared edits, and restore inheritance", async ({ page }, info) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await withMockDocumentProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"), async () => {
      const graph = await createWorkflow(page);
      const images = graph.nodes.filter((node) => node.node_type === "image_generation");
      const second = images[1];
      const planId = second.incoming.find((edge) => edge.role === "prompt")!.node_id;
      const inspector = page.locator("[data-graph-node-inspector]");
      await selectWorkflowNode(page, planId);
      await inspector.getByLabel("背景").fill("共享棚拍背景");
      await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === planId)?.config.prompt).toMatchObject({ content: { background: "共享棚拍背景" } });
      await selectWorkflowNode(page, second.id);
      const scene = inspector.locator('[data-override-field="content.background"]');
      await expect(scene).toContainText("共享棚拍背景");
      await scene.getByRole("button", { name: "修改本图", exact: true }).click();
      await scene.getByLabel("背景").fill("本图厨房背景");
      await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === second.id)?.config.prompt_overrides).toEqual({ content: { background: "本图厨房背景" } });
      await page.reload();
      await selectWorkflowNode(page, second.id);
      await expect(scene.getByLabel("背景")).toHaveValue("本图厨房背景");
      const text = inspector.locator("section").filter({ has: page.getByRole("heading", { name: "画面文字", exact: true }) });
      await text.getByRole("button", { name: "修改本图", exact: true }).click();
      await text.getByRole("button", { name: "带文字", exact: true }).click();
      await text.getByLabel("主标题", { exact: true }).fill("仅第二张的标题");
      await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === second.id)?.config.text_override).toMatchObject({ policy: "required", headline: "仅第二张的标题" });
      expect((await workflowGraph(page)).nodes.find((node) => node.id === images[0].id)?.config).not.toHaveProperty("text_override");

      await selectWorkflowNode(page, planId);
      await inspector.getByLabel("背景").fill("更新后的共享场景");
      await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === planId)?.config.prompt).toMatchObject({ content: { background: "更新后的共享场景" } });
      await selectWorkflowNode(page, second.id);
      await expect(scene.getByLabel("背景")).toHaveValue("本图厨房背景");
      await scene.getByRole("button", { name: "恢复继承", exact: true }).click();
      await expect(scene).toContainText("更新后的共享场景");
      await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === second.id)?.image_input?.prompt.content).toMatchObject({ background: "更新后的共享场景" });

      const runResponse = await page.request.post(workflowRunsPath(page, graph), { data: { scope: "selection", node_ids: [second.id] } });
      expect(runResponse.ok(), await runResponse.text()).toBeTruthy();
      const submitted = await runResponse.json() as GraphRun;
      const run = await waitForWorkflowRun(page, graph, submitted.id, "succeeded");
      expect(run.node_runs.filter((node) => images.some((image) => image.id === node.node_id)).map((node) => node.node_id)).toEqual([second.id]);
      await page.reload();
      await selectWorkflowNode(page, second.id);
      await expect(inspector.locator("[data-graph-node-local-edit]")).toBeVisible();

      for (const size of [{ width: 1440, height: 900 }, { width: 1024, height: 768 }, { width: 390, height: 844 }]) {
        await page.setViewportSize(size);
        await selectWorkflowNode(page, second.id);
        const bounds = await page.evaluate(() => ({ inner: innerWidth, client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
        expect(bounds.inner).toBe(size.width);
        expect(bounds.scroll).toBeLessThanOrEqual(bounds.client);
        await page.screenshot({ path: info.outputPath(`picture-${size.width}.png`), fullPage: true });
      }
      await page.setViewportSize({ width: 1440, height: 900 });
      for (const kind of ["product_source", "image_asset", "creative_brief", "visual_system", "image_prompt"] as const) {
        const node = graph.nodes.find((item) => item.node_type === kind)!;
        await selectWorkflowNode(page, node.id);
        await page.screenshot({ path: info.outputPath(`${kind}.png`), fullPage: true });
      }
      expect(errors).toEqual([]);
      console.log(`verified product ${workflowProductId(page)}`);
    });
  });

  test("facts confirmation, palette editing and six detail forms across locales", async ({ page, browser }, info) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    const graph = await createWorkflow(page);
    const source = graph.nodes.find((node) => node.node_type === "product_source")!;
    const visual = graph.nodes.find((node) => node.node_type === "visual_system")!;
    const factsPath = `/api/v3/products/${graph.product_id}/facts`;
    const original = await (await page.request.get(factsPath)).json() as ProductFactsResponse;
    const seeded = await page.request.put(factsPath, { data: {
      expected_fact_version: original.fact_set?.version ?? null,
      facts: [{ key: "容量", value: "500 ml", status: "observed", requires_confirmation: true }],
    } });
    expect(seeded.ok(), await seeded.text()).toBeTruthy();
    await page.reload();
    await selectWorkflowNode(page, source.id);
    const inspector = page.locator("[data-graph-node-inspector]");
    await expect(inspector.getByText("待确认信息", { exact: true })).toBeVisible();
    await inspector.getByRole("button", { name: "确认事实", exact: true }).click();
    await expect(inspector.getByText("待确认信息", { exact: true })).toHaveCount(0);
    await inspector.getByRole("button", { name: "保存商品资料", exact: true }).click();
    await expect.poll(async () => (await (await page.request.get(factsPath)).json() as ProductFactsResponse).fact_set?.facts).toMatchObject([{ key: "容量", status: "confirmed", requires_confirmation: false }]);
    await inspector.getByLabel("商品名称", { exact: true }).fill("未保存的商品名称");
    const current = await (await page.request.get(factsPath)).json() as ProductFactsResponse;
    const concurrent = await page.request.put(factsPath, { data: { expected_fact_version: current.fact_set!.version, name: "另一个编辑者的名称" } });
    expect(concurrent.ok(), await concurrent.text()).toBeTruthy();
    const conflict = page.waitForResponse((response) => response.request().method() === "PUT" && new URL(response.url()).pathname === factsPath);
    await inspector.getByRole("button", { name: "保存商品资料", exact: true }).click();
    expect((await conflict).status()).toBe(409);
    await expect(inspector.getByLabel("商品名称", { exact: true })).toHaveValue("未保存的商品名称");

    await selectWorkflowNode(page, visual.id);
    await inspector.getByLabel("风格关键词", { exact: true }).fill("明亮侧光\n冷暖对比\n清晰材质");
    await inspector.getByRole("button", { name: "添加", exact: true }).click();
    await inspector.getByLabel("HEX", { exact: true }).fill("#287A65");
    await inspector.getByLabel("颜色说明", { exact: true }).fill("商品包装的主色");
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === visual.id)?.config.visual_overlay).toMatchObject({ colors: [{ value: "#287A65", label: "商品包装的主色" }] });
    const productURL = page.url();
    const storageState = await page.context().storageState();
    for (const width of [1440, 390]) {
      for (const locale of ["zh-CN", "en-US", "ja-JP", "vi-VN"]) {
        const theme = width === 390 ? "dark" : "light";
        const context = await browser.newContext({ storageState, viewport: { width, height: 900 }, hasTouch: width === 390, reducedMotion: "reduce", colorScheme: theme });
        try {
          const p = await context.newPage();
          const errors: string[] = [];
          p.on("pageerror", (error) => errors.push(error.message));
          p.on("response", (response) => {
            // Direct canvas creation has no Agent workspace; its optional probe returns 409.
            if (response.status() === 409 && new URL(response.url()).pathname === `/api/v2/products/${graph.product_id}/agent-workbench`) return;
            if (response.status() >= 400) errors.push(`${response.status()} ${response.url()}`);
          });
          await p.addInitScript(({ locale, theme }) => { localStorage.setItem("productflow.locale", locale); localStorage.setItem("productflow.theme", theme); }, { locale, theme });
          await p.goto(productURL);
          for (const kind of ["product_source", "image_asset", "creative_brief", "visual_system", "image_prompt", "image_generation"] as const) {
            await selectWorkflowNode(p, graph.nodes.find((node) => node.node_type === kind)!.id);
            const detail = p.locator("[data-graph-node-inspector]");
            const input = kind === "visual_system" ? detail.locator('input[type="color"]') : kind === "image_generation" ? detail.locator('[data-override-field="content.background"]') : detail.locator("input, textarea").first();
            await input.scrollIntoViewIfNeeded();
            const sizes = await detail.evaluate((element) => ({ viewport: innerWidth, client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth, detailClient: element.clientWidth, detailScroll: element.scrollWidth }));
            expect(sizes.viewport).toBe(width);
            expect(sizes.scroll).toBeLessThanOrEqual(sizes.client);
            expect(sizes.detailScroll).toBeLessThanOrEqual(sizes.detailClient);
            if (kind === "visual_system") await expect(detail.locator('input[type="color"]')).toHaveValue("#287a65");
            for (const image of await detail.locator("img").all()) expect(await image.evaluate((element: HTMLImageElement) => element.complete && element.naturalWidth > 0)).toBe(true);
            await p.screenshot({ path: info.outputPath(`${kind}-${width}-${locale}-${theme}.png`) });
          }
          expect(errors).toEqual([]);
        } finally { await context.close(); }
      }
    }
  });
});
