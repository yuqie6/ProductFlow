import { expect, test } from "@playwright/test";
import { applyWorkflowFixture, createWorkflow, selectWorkflowNode, workflowGraph, workflowProductId, workflowRunsPath, waitForWorkflowRun } from "./canvasWorkflow";
import { lockLocale, loginAsAdmin, requiredEnv, withMockDocumentProviders } from "./liveGraph";
import type { GraphRun, ProductFactsResponse } from "../src/lib/types";

test.describe("node detail redesign", () => {
  test.skip(process.env.PRODUCTFLOW_RUN_NODE_DETAIL !== "1", "requires an isolated mock stack");
  test("section generation, node image history and export preserve independent content", async ({ page }, info) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await withMockDocumentProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"), async () => {
      const graph = await createWorkflow(page);
      const plan = graph.nodes.find((node) => node.node_type === "image_prompt")!;
      await applyWorkflowFixture(page, [{ op: "update_node_config", node_ref: plan.id, config: { ...plan.config,
        prompt: { design_goal: "保留人工目标", composition: { layout: "人工构图", copy_regions: ["顶部"] }, content: { background: "原始背景", focus: ["保留杯柄"] } },
      } }]);
      await selectWorkflowNode(page, plan.id);
      const controls = page.locator("[data-section-generation]");
      await controls.getByRole("combobox").click();
      await page.getByRole("option", { name: "构图与场景", exact: true }).click();
      const response = page.waitForResponse((response) => response.request().method() === "POST" && response.url().endsWith("/runs"));
      await controls.getByRole("button", { name: "改写文稿", exact: true }).click();
      const submitted = await response;
      expect(submitted.request().postDataJSON()).toMatchObject({ document_section: "composition", document_action: "rewrite" });
      const run = await submitted.json() as GraphRun;
      await waitForWorkflowRun(page, graph, run.id, "succeeded");
      const candidate = page.locator("[data-graph-document-candidate]");
      await expect(candidate).toBeVisible();
      expect((await workflowGraph(page)).nodes.find((node) => node.id === plan.id)?.config.prompt).toMatchObject({ design_goal: "保留人工目标", content: { background: "原始背景" } });
      await candidate.getByRole("button", { name: "整份采用", exact: true }).click();
      await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === plan.id)?.config.prompt).toMatchObject({
        design_goal: "保留人工目标", content: { background: "干净背景", focus: ["保留杯柄"] }, composition: { copy_regions: ["顶部"] },
      });
      const imageId = graph.edges.find((edge) => edge.source_node_id === plan.id && edge.role === "prompt")!.target_node_id;
      for (let index = 0; index < 2; index++) {
        const response = await page.request.post(workflowRunsPath(page, graph), { data: { scope: "node", node_id: imageId, force: true } });
        expect(response.ok(), await response.text()).toBeTruthy();
        await waitForWorkflowRun(page, graph, (await response.json() as GraphRun).id, "succeeded");
      }
      await page.reload();
      await selectWorkflowNode(page, imageId);
      const history = page.locator("[data-node-image-history]");
      await history.getByRole("button", { name: "历史版本", exact: true }).click();
      await expect(history.locator("[data-history-asset]")).toHaveCount(2);
      const beforeRuns = await (await page.request.get(workflowRunsPath(page, graph))).json();
      await page.locator("[data-export-settings] > summary").click();
      const panel = page.locator("[data-delivery-rendition-panel]");
      await panel.locator('[data-delivery-preset-key="scene_landscape"]').click();
      await panel.getByRole("button", { name: "生成交付图", exact: true }).click();
      await expect(panel.getByRole("link").first()).toBeVisible();
      expect(await (await page.request.get(workflowRunsPath(page, graph))).json()).toEqual(beforeRuns);
      for (const width of [1440, 1024, 390]) {
        await page.setViewportSize({ width, height: 900 });
        await selectWorkflowNode(page, imageId);
        await history.locator("[data-history-asset]").first().scrollIntoViewIfNeeded();
        await expect(history.locator("img").first()).toBeVisible();
        expect(await history.locator("img").evaluateAll((images) => images.every((image) => image instanceof HTMLImageElement && image.naturalWidth > 0))).toBe(true);
        await page.screenshot({ path: info.outputPath(`history-export-${width}.png`) });
      }
    });
  });

  test("generation controls follow the returned adapter options", async ({ page }, info) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await page.route("**/api/v3/image-generation-options", (route) => route.fulfill({ json: { aspect_ratio: ["1:1", "2:3", "3:2"], quality_intent: ["draft", "standard", "high"] } }));
    const graph = await createWorkflow(page);
    const image = graph.nodes.find((node) => node.node_type === "image_generation")!;
    await selectWorkflowNode(page, image.id);
    const inspector = page.locator("[data-graph-node-inspector]");
    const ratios = inspector.locator("[data-image-aspect-ratio-picker]");
    await expect(ratios.getByRole("button")).toHaveCount(3);
    await expect(ratios.locator("input")).toHaveCount(0);
    await ratios.getByRole("button", { name: "2:3", exact: true }).click();
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === image.id)?.config.generation_spec).toMatchObject({ aspect_ratio: "2:3", resolution_tier: "high" });
    await inspector.getByRole("tab", { name: "高级", exact: true }).click();
    await expect(inspector.getByRole("combobox", { name: "质量", exact: true })).toBeVisible();
    await expect(inspector.getByRole("combobox", { name: "参考保真度", exact: true })).toHaveCount(0);
    await page.screenshot({ path: info.outputPath("adapter-options.png") });
  });
  test("picture style exposes series values, explicit clearing and independent restore", async ({ page }, info) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    const graph = await createWorkflow(page);
    const image = graph.nodes.find((node) => node.node_type === "image_generation")!;
    const visual = graph.nodes.find((node) => node.node_type === "visual_system")!;
    await applyWorkflowFixture(page, [{ op: "update_node_config", node_ref: visual.id, config: {
      ...visual.config, visual_overlay: { style: ["冷色棚拍"], colors: [{ role: "background", value: "#eeeeee", label: "背景" }] },
    } }]);
    await selectWorkflowNode(page, image.id);
    const details = page.locator("[data-image-style-details]");
    await details.locator(":scope > summary").click();
    const style = details.locator('[data-style-override="style"]');
    await expect(style).toContainText("继承系列设置");
    await expect(style).toContainText("冷色棚拍");
    await expect(style.getByRole("textbox")).toHaveCount(0);
    await style.getByRole("button", { name: "修改本图", exact: true }).click();
    await style.getByRole("textbox").fill("暖色近景");
    await expect(style).toContainText("替代以下系列设置");
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === image.id)?.config.visual_overlay).toEqual({ style: ["暖色近景"] });
    const colors = details.locator('[data-style-override="colors"]');
    await colors.getByRole("button", { name: "修改本图", exact: true }).click();
    await colors.getByRole("button", { name: "移除", exact: true }).click();
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === image.id)?.config.visual_overlay).toEqual({ style: ["暖色近景"], colors: [] });
    await page.reload();
    await selectWorkflowNode(page, image.id);
    await details.locator(":scope > summary").click();
    await expect(style.getByRole("textbox")).toHaveValue("暖色近景");
    await expect(colors).toContainText("替代以下系列设置");
    await style.getByRole("button", { name: "恢复继承", exact: true }).click();
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === image.id)?.config.visual_overlay).toEqual({ colors: [] });
    await expect(style).toContainText("冷色棚拍");
    await colors.getByRole("button", { name: "恢复继承", exact: true }).click();
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === image.id)?.config.visual_overlay).toEqual({});
    await expect(colors).toContainText("继承系列设置");
    await expect(colors).toContainText("#eeeeee");
    await page.screenshot({ path: info.outputPath("picture-style-inheritance.png") });
  });
  test("brief entries, read-only fact questions and shared plan sources", async ({ page }, info) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    const graph = await createWorkflow(page);
    const brief = graph.nodes.find((node) => node.node_type === "creative_brief")!;
    const plan = graph.nodes.find((node) => node.node_type === "image_prompt")!;
    const source = graph.nodes.find((node) => node.node_type === "product_source")!;
    await applyWorkflowFixture(page, [{ op: "update_node_config", node_ref: brief.id, config: {
      ...brief.config, required_elements: ["保留杯柄"], prohibitions: ["不得增加刻度"], fact_gaps: ["容量待确认"],
    } }]);
    const inspector = page.locator("[data-graph-node-inspector]");
    await selectWorkflowNode(page, brief.id);
    await expect(inspector.locator("[data-fact-gap-summary]")).toHaveText("容量待确认");
    await expect(inspector.getByRole("textbox", { name: "待确认信息" })).toHaveCount(0);
    const entries = inspector.locator('[data-brief-entries="必须包含"]');
    await entries.getByRole("button", { name: "添加 必须包含", exact: true }).click();
    const newEntry = entries.locator("[data-new-brief-entry]");
    await inspector.getByLabel("传播目标", { exact: true }).fill("用于展示陶瓷杯");
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === brief.id)?.config.goal).toBe("用于展示陶瓷杯");
    await expect(newEntry.getByRole("textbox")).toBeVisible();
    await newEntry.getByRole("textbox").fill("完整展示杯口");
    await newEntry.getByRole("button", { name: "添加", exact: true }).press("Enter");
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === brief.id)?.config.required_elements).toEqual(["保留杯柄", "完整展示杯口"]);
    await entries.getByRole("button", { name: "移除", exact: true }).last().click();
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === brief.id)?.config.required_elements).toEqual(["保留杯柄"]);
    await inspector.getByRole("button", { name: source.title, exact: true }).first().click();
    await expect(page.locator(`[data-inspector-node-id="${source.id}"]`)).toBeVisible();
    await selectWorkflowNode(page, plan.id);
    const shared = inspector.locator(`[data-shared-source="${brief.id}"]`);
    await expect(shared).toContainText("必须包含");
    await expect(shared).toContainText("保留杯柄");
    await expect(shared).toContainText("不得出现");
    await expect(shared).toContainText("不得增加刻度");
    await expect(shared.getByRole("textbox")).toHaveCount(0);
    await expect(inspector.getByLabel("商品近似占比（%）")).toBeHidden();
    await inspector.locator("[data-plan-composition-details] > summary").click();
    await inspector.getByLabel("商品近似占比（%）").fill("61");
    await expect.poll(async () => (await workflowGraph(page)).nodes.find((node) => node.id === plan.id)?.config.prompt).toMatchObject({ composition: { product_share_percent: 61 } });
    for (const width of [1440, 1024, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await selectWorkflowNode(page, plan.id);
      const bounds = await page.evaluate(() => ({ inner: innerWidth, client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
      expect(bounds.inner).toBe(width);
      expect(bounds.scroll).toBeLessThanOrEqual(bounds.client);
      await page.screenshot({ path: info.outputPath(`plan-sources-${width}.png`) });
    }
    await shared.getByRole("button", { name: brief.title, exact: true }).click();
    await expect(page.locator(`[data-inspector-node-id="${brief.id}"]`)).toBeVisible();
  });
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
      const text = inspector.locator("fieldset").filter({ has: page.locator("legend", { hasText: "画面文字" }) });
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
