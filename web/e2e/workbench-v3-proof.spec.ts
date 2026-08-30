import { expect, test, type Page } from "@playwright/test";

import {
  REFERENCE_PRODUCT_IMAGE,
  assertLiveBrowserGraphEnabled,
  lockLocale,
  loginAsAdmin,
  requiredEnv,
  selectCreateImageType,
} from "./liveGraph";

const SCREENSHOT_PREFIX = "/tmp/productflow-workbench-v3-proof";

const PRESETS = [
  { name: "1440", width: 1440, height: 900 },
  { name: "1024", width: 1024, height: 768 },
  { name: "390", width: 390, height: 844 },
] as const;

const SHOT_LIST_PRESETS = [
  { name: "1440", width: 1440, height: 900 },
  { name: "390", width: 390, height: 844 },
] as const;

const RECOMMENDED_IMAGE_TYPES = ["hero", "detail", "scene", "selling_point"] as const;

function attachBrowserGuards(page: Page): () => void {
  const consoleErrors: string[] = [];
  const failedRequests: string[] = [];
  page.on("pageerror", (error) => {
    consoleErrors.push(`pageerror: ${error.message}`);
  });
  page.on("console", (message) => {
    if (message.type() !== "error") return;
    const text = message.text();
    if (text.includes("favicon") || text.includes("Download the React DevTools")) return;
    if (text.includes("Failed to load resource") && (text.includes("409") || text.includes("404"))) return;
    consoleErrors.push(`console: ${text}`);
  });
  page.on("requestfailed", (request) => {
    if (request.failure()?.errorText === "net::ERR_ABORTED") return;
    failedRequests.push(`request: ${request.url()} ${request.failure()?.errorText ?? "failed"}`);
  });
  page.on("response", (response) => {
    if (response.status() < 400 || /\/favicon\.ico(?:\?|$)/.test(response.url())) return;
    if (response.status() === 409) return;
    if (response.status() === 404 && /\/workflows\/current(\?|$)/.test(response.url())) return;
    failedRequests.push(`response: ${response.status()} ${response.url()}`);
  });
  return () => {
    expect(consoleErrors, consoleErrors.join("\n")).toEqual([]);
    expect(failedRequests, failedRequests.join("\n")).toEqual([]);
  };
}

async function openDirectCreateWorkbench(page: Page, name: string): Promise<void> {
  await page.goto("/products/new");
  await expect(page.locator("[data-image-type='detail']")).toBeVisible();
  await page.locator("#agent-product-name").fill(name);
  await page.locator("#agent-product-brief").fill("电商细节图，保留真实材质。此次只验证工作台交互。");
  await selectCreateImageType(page, "detail");
  await page.locator('[data-image-type="detail"] input[type="number"]').fill("1");
  await page.locator("[data-agent-product-intake-form] input[type='file']").setInputFiles(
    REFERENCE_PRODUCT_IMAGE,
  );
  const submit = page.getByRole("button", { name: "只建画布" });
  await expect(submit).toBeEnabled();
  await Promise.all([
    page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/, {
      timeout: 60_000,
      waitUntil: "commit",
    }),
    submit.click(),
  ]);
  await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
  const shotsTab = page.locator('[data-graph-view="shots"]');
  const canvasTab = page.locator('[data-graph-view="canvas"]');
  await expect(shotsTab).toBeVisible();
  await expect(canvasTab).toBeVisible();
  await expect(shotsTab).toBeEnabled();
  await expect(shotsTab).toHaveAttribute("aria-selected", "true");
  await expect(page.locator("[data-graph-shot-list]")).toBeVisible();
  await canvasTab.click({ force: true });
  await expect(canvasTab).toHaveAttribute("aria-selected", "true");
  await expect(page.locator('[aria-hidden="false"] [aria-label="工作流画布"]')).toBeVisible();
}

async function openRecommendedSetWorkbench(page: Page, name: string): Promise<void> {
  await page.goto("/products/new");
  await expect(page.locator("[data-image-type='detail']")).toBeVisible();
  await page.locator("#agent-product-name").fill(name);
  await page.locator("#agent-product-brief").fill("电商细节图，保留真实材质。此次只验证镜头列表交互。");

  await selectCreateImageType(page, "detail");
  const existingChoice = page.locator('[data-image-type="detail"] input[type="checkbox"]');
  await expect(existingChoice).toBeChecked();

  const recommendedSet = page.locator("[data-agent-apply-recommended-set]");
  await expect(recommendedSet).toBeVisible();
  await expect(recommendedSet).toBeEnabled();
  await recommendedSet.click({ force: true });
  for (const imageType of RECOMMENDED_IMAGE_TYPES) {
    await expect(page.locator(`[data-image-type="${imageType}"] input[type="checkbox"]`)).toBeChecked();
  }
  await expect(existingChoice).toBeChecked();

  await page.locator("[data-agent-product-intake-form] input[type='file']").setInputFiles(
    REFERENCE_PRODUCT_IMAGE,
  );
  const submit = page.getByRole("button", { name: "只建画布" });
  await expect(submit).toBeEnabled();
  await submit.scrollIntoViewIfNeeded();
  await Promise.all([
    page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/, {
      timeout: 60_000,
      waitUntil: "commit",
    }),
    submit.click(),
  ]);
  await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
  const shotsTab = page.locator('[data-graph-view="shots"]');
  const canvasTab = page.locator('[data-graph-view="canvas"]');
  await expect(shotsTab).toBeVisible();
  await expect(canvasTab).toBeVisible();
  await expect(shotsTab).toBeEnabled();
  await expect(shotsTab).toHaveAttribute("aria-selected", "true");
  await expect(page.locator("[data-graph-shot-list]")).toBeVisible();
}

function productIdFrom(page: Page): string {
  const productId = new URL(page.url()).pathname.split("/")[2] ?? "";
  expect(productId).toBeTruthy();
  return productId;
}

async function currentGraphGroupIds(page: Page): Promise<string[]> {
  const response = await page.request.get(
    `/api/v3/products/${encodeURIComponent(productIdFrom(page))}/workflows/current`,
  );
  expect(response.ok(), await response.text()).toBeTruthy();
  const payload = await response.json() as { groups: Array<{ id: string }> };
  return payload.groups.map((group) => group.id);
}

async function assertShotListLayout(page: Page): Promise<string[]> {
  const shotList = page.locator("[data-graph-shot-list]");
  await expect(shotList).toBeVisible();
  await expect.poll(async () => shotList.locator("[data-graph-shot-id]").count()).toBe(4);
  const groupIds = await shotList.locator("[data-graph-shot-id]").evaluateAll((rows) => (
    rows.map((row) => row.getAttribute("data-graph-shot-id")).filter((id): id is string => Boolean(id))
  ));
  expect(groupIds).toHaveLength(4);
  await expect(page.locator('[data-graph-shot-run-all]')).toBeEnabled();

  const layout = await page.evaluate(() => {
    const controls = [
      ...document.querySelectorAll<HTMLElement>(
        '[data-graph-view-switcher], [data-graph-shot-run-all], [data-graph-shot-open-node], [data-graph-shot-run], [data-graph-shot-preview]',
      ),
    ].filter((element) => {
      const style = window.getComputedStyle(element);
      return style.display !== "none" && style.visibility !== "hidden";
    });
    const boxes = controls.map((element) => {
      const box = element.getBoundingClientRect();
      return { x: box.x, y: box.y, right: box.right, bottom: box.bottom, width: box.width, height: box.height };
    });
    const overlapPairs: Array<[number, number]> = [];
    for (let left = 0; left < boxes.length; left += 1) {
      for (let right = left + 1; right < boxes.length; right += 1) {
        const width = Math.max(0, Math.min(boxes[left].right, boxes[right].right) - Math.max(boxes[left].x, boxes[right].x));
        const height = Math.max(0, Math.min(boxes[left].bottom, boxes[right].bottom) - Math.max(boxes[left].y, boxes[right].y));
        if (width * height > 1) overlapPairs.push([left, right]);
      }
    }
    const shotList = document.querySelector<HTMLElement>("[data-graph-shot-list]");
    return {
      viewportWidth: window.innerWidth,
      documentWidth: document.documentElement.scrollWidth,
      bodyWidth: document.body.scrollWidth,
      shotListWidth: shotList?.clientWidth ?? 0,
      shotListScrollWidth: shotList?.scrollWidth ?? 0,
      boxes,
      overlapPairs,
    };
  });
  expect(layout.documentWidth).toBeLessThanOrEqual(layout.viewportWidth);
  expect(layout.bodyWidth).toBeLessThanOrEqual(layout.viewportWidth);
  expect(layout.shotListWidth).toBeGreaterThan(0);
  expect(layout.shotListScrollWidth).toBeLessThanOrEqual(layout.shotListWidth);
  expect(layout.boxes.every((box) => (
    Number.isFinite(box.x)
    && Number.isFinite(box.y)
    && box.width > 0
    && box.height > 0
    && box.x >= -1
    && box.right <= layout.viewportWidth + 1
  ))).toBeTruthy();
  expect(layout.overlapPairs).toEqual([]);
  return groupIds;
}

async function fitCanvas(page: Page): Promise<void> {
  const fit = page.getByRole("button", { name: "适配全图" });
  if (await fit.isVisible()) await fit.click({ force: true });
}

async function selectNode(page: Page, nodeId: string, toggle = false): Promise<void> {
  await fitCanvas(page);
  if ((page.viewportSize()?.width ?? 1440) <= 1023) {
    const mode = page.getByRole("button", {
      name: toggle ? "选择模式：点按节点加入或移出多选" : "编辑模式：拖动节点、连接节点",
    });
    if (await mode.isVisible() && await mode.getAttribute("aria-pressed") !== "true") {
      await mode.click({ force: true });
    }
  }
  const card = page.locator(`[data-workflow-node-id="${nodeId}"]`);
  await expect(card).toBeAttached();
  await card.evaluate((element: HTMLElement, useToggle: boolean) => {
    element.dispatchEvent(new MouseEvent("click", {
      bubbles: true,
      cancelable: true,
      view: window,
      shiftKey: useToggle,
      ctrlKey: useToggle,
      metaKey: useToggle,
      buttons: 1,
    }));
  }, toggle);
}

async function openAddPanel(page: Page) {
  await page.locator('[data-sidebar-tool="add"]').click({ force: true });
  const panel = page.locator("[data-graph-add-node-panel]");
  await expect(panel).toBeVisible();
  return panel;
}

async function nodeIds(page: Page): Promise<string[]> {
  return page.locator("[data-workflow-node-id]").evaluateAll((nodes) => (
    nodes.map((node) => node.getAttribute("data-workflow-node-id")).filter((id): id is string => Boolean(id))
  ));
}

async function addPaletteNode(page: Page, label: string): Promise<string> {
  const beforeIds = await nodeIds(page);
  const panel = await openAddPanel(page);
  await panel.getByRole("button", { name: new RegExp(label) }).click({ force: true });
  await expect.poll(async () => (await nodeIds(page)).length).toBe(beforeIds.length + 1);
  const created = (await nodeIds(page)).find((id) => !beforeIds.includes(id));
  expect(created).toBeTruthy();
  return created!;
}

async function assertCanvasEvidence(page: Page, presetName: string): Promise<void> {
  const canvas = page.locator('[aria-hidden="false"] [aria-label="工作流画布"]');
  await expect(canvas).toBeVisible();
  await fitCanvas(page);
  const metrics = await canvas.evaluate((element) => {
    const surface = element.getBoundingClientRect();
    const nodeBoxes = [...element.querySelectorAll<HTMLElement>("[data-workflow-node-id]")]
      .map((node) => {
        const box = node.getBoundingClientRect();
        return { x: box.x, y: box.y, width: box.width, height: box.height };
      });
    const edgeCount = element.querySelectorAll(".react-flow__edges path").length;
    const finiteBoxes = nodeBoxes.filter((box) => (
      Number.isFinite(box.x)
      && Number.isFinite(box.y)
      && box.width > 0
      && box.height > 0
    ));
    return {
      width: surface.width,
      height: surface.height,
      clientWidth: element.clientWidth,
      clientHeight: element.clientHeight,
      nodeCount: nodeBoxes.length,
      finiteNodeCount: finiteBoxes.length,
      edgeCount,
    };
  });
  expect(metrics.width).toBeGreaterThan(0);
  expect(metrics.height).toBeGreaterThan(0);
  expect(metrics.clientWidth).toBeGreaterThan(0);
  expect(metrics.clientHeight).toBeGreaterThan(0);
  expect(metrics.nodeCount).toBeGreaterThan(0);
  expect(metrics.finiteNodeCount).toBe(metrics.nodeCount);
  expect(metrics.edgeCount).toBeGreaterThan(0);

  const screenshotPath = `${SCREENSHOT_PREFIX}-${presetName}.png`;
  const screenshot = await canvas.screenshot({ path: screenshotPath });
  expect(screenshot.subarray(0, 8)).toEqual(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]));
  expect(screenshot.byteLength).toBeGreaterThan(2_000);
  const pixelEvidence = await page.evaluate(async (encoded) => {
    const image = new Image();
    image.src = `data:image/png;base64,${encoded}`;
    await image.decode();
    const bitmap = document.createElement("canvas");
    bitmap.width = image.naturalWidth;
    bitmap.height = image.naturalHeight;
    const context = bitmap.getContext("2d");
    if (!context) return { width: 0, height: 0, changedPixels: 0 };
    context.drawImage(image, 0, 0);
    const pixels = context.getImageData(0, 0, bitmap.width, bitmap.height).data;
    const background = pixels.slice(0, 4);
    let changedPixels = 0;
    for (let index = 0; index < pixels.length; index += 4) {
      if (
        Math.abs(pixels[index] - background[0]) > 8
        || Math.abs(pixels[index + 1] - background[1]) > 8
        || Math.abs(pixels[index + 2] - background[2]) > 8
        || pixels[index + 3] !== background[3]
      ) {
        changedPixels += 1;
      }
    }
    return { width: bitmap.width, height: bitmap.height, changedPixels };
  }, screenshot.toString("base64"));
  expect(pixelEvidence.width).toBeGreaterThan(0);
  expect(pixelEvidence.height).toBeGreaterThan(0);
  expect(pixelEvidence.changedPixels).toBeGreaterThan(20);
}

async function assertInspectorDoesNotCoverNode(page: Page, nodeId: string): Promise<void> {
  await selectNode(page, nodeId);
  await page.locator('[data-sidebar-tool="details"]').click({ force: true });
  const inspector = page.locator("[data-product-workbench-inspector]");
  const canvas = page.locator("[data-agent-workbench-canvas-slot]");
  const node = page.locator(`[data-workflow-node-id="${nodeId}"]`);
  await expect(inspector).toBeVisible();
  await expect(node).toBeVisible();
  const boxes = await Promise.all([canvas.boundingBox(), inspector.boundingBox(), node.boundingBox()]);
  expect(boxes[0]).toBeTruthy();
  expect(boxes[1]).toBeTruthy();
  expect(boxes[2]).toBeTruthy();
  const [canvasBox, inspectorBox, nodeBox] = boxes;
  expect(canvasBox!.width).toBeGreaterThan(0);
  expect(canvasBox!.height).toBeGreaterThan(0);
  expect(nodeBox!.width).toBeGreaterThan(0);
  expect(nodeBox!.height).toBeGreaterThan(0);
  const viewport = page.viewportSize();
  if (viewport && viewport.width <= 1023) {
    await expect(inspector).toHaveAttribute("data-inspector-layout", "drawer");
    expect(inspectorBox!.y).toBeGreaterThan(canvasBox!.y);
    expect(inspectorBox!.height).toBeLessThan(canvasBox!.height);
    expect(canvasBox!.height).toBeGreaterThan(120);
    await expect(page.locator("[data-workflow-node-id]").first()).toBeVisible();
    return;
  }
  const overlapWidth = Math.max(
    0,
    Math.min(nodeBox!.x + nodeBox!.width, inspectorBox!.x + inspectorBox!.width)
      - Math.max(nodeBox!.x, inspectorBox!.x),
  );
  const overlapHeight = Math.max(
    0,
    Math.min(nodeBox!.y + nodeBox!.height, inspectorBox!.y + inspectorBox!.height)
      - Math.max(nodeBox!.y, inspectorBox!.y),
  );
  expect(overlapWidth * overlapHeight).toBe(0);
}

for (const preset of PRESETS) {
  test.describe(`workbench v3 proof ${preset.name}px`, () => {
    test.use({
      viewport: { width: preset.width, height: preset.height },
      colorScheme: "light",
      contextOptions: { reducedMotion: "reduce" },
    });

    test("keeps one no-cost workbench flow observable from canvas to recipe preview", async ({ page }) => {
      assertLiveBrowserGraphEnabled();
      await lockLocale(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      const assertClean = attachBrowserGuards(page);
      await openDirectCreateWorkbench(page, `e2e-v3-proof-${preset.name}-${Date.now()}`);

      await assertCanvasEvidence(page, preset.name);
      const imageId = await addPaletteNode(page, "图片生成");

      await selectNode(page, imageId);
      await page.locator('[data-sidebar-tool="details"]').click({ force: true });
      const inspector = page.locator("[data-graph-node-inspector]");
      await expect(inspector).toBeVisible();
      const imageCard = page.locator(`[data-workflow-node-id="${imageId}"]`);
      await expect(inspector).toContainText("先连上参考图，才能生成");
      await expect(inspector).toContainText("先连上提示词节点，才能运行");
      await expect(imageCard).toContainText("还缺提示词，先连上再运行");
      await expect(inspector.getByRole("button", { name: "运行该节点" })).toBeDisabled();

      const beforeDuplicateIds = await nodeIds(page);
      const add = await openAddPanel(page);
      await add.getByRole("button", { name: "复制 复制选中的节点，以及它们之间的连线" }).click({ force: true });
      await expect.poll(async () => (await nodeIds(page)).length).toBe(beforeDuplicateIds.length + 1);
      const duplicatedId = (await nodeIds(page)).find((id) => !beforeDuplicateIds.includes(id));
      expect(duplicatedId).toBeTruthy();
      await expect(page.locator(`[data-workflow-node-id="${duplicatedId}"]`)).toBeVisible();

      const recipeDialogName = `v3 proof recipe ${preset.name} ${Date.now()}`;
      await selectNode(page, duplicatedId!);
      const recipeAdd = await openAddPanel(page);
      const saveSelection = recipeAdd.getByRole("button", { name: /保存选中节点为预设/ });
      await expect(saveSelection).toBeEnabled();
      await saveSelection.click({ force: true });
      const saveDialog = page.getByRole("dialog");
      await expect(saveDialog).toBeVisible();
      await saveDialog.getByLabel("预设名称").fill(recipeDialogName);
      await saveDialog.getByRole("button", { name: "保存预设" }).click();
      await expect(saveDialog).toBeHidden();

      await page.locator('[data-sidebar-tool="recipes"]').click({ force: true });
      await expect(page.locator("[data-graph-recipe-panel]")).toBeVisible();
      const apply = page.getByRole("button", { name: "应用" }).last();
      await expect(apply).toBeVisible({ timeout: 30_000 });
      await apply.click({ force: true });
      const preview = page.getByRole("dialog");
      await expect(preview).toBeVisible();
      await expect(preview.getByRole("button", { name: "确认应用" })).toBeVisible();
      const previewBody = preview.locator("[data-recipe-preview]");
      if (await previewBody.count()) {
        await expect(previewBody).toHaveAttribute("data-recipe-preview-mode", /create|merge/);
        await expect(preview.locator("[data-recipe-preview-nodes]")).toBeVisible();
      } else {
        await expect(preview).toContainText(/不能合并|冲突|已有/);
      }
      await preview.getByRole("button", { name: "取消" }).click();

      await assertInspectorDoesNotCoverNode(page, duplicatedId!);
      await page.screenshot({ path: `${SCREENSHOT_PREFIX}-${preset.name}-final.png`, fullPage: false });
      assertClean();
    });
  });
}

for (const preset of SHOT_LIST_PRESETS) {
  test.describe(`recommended shot list proof ${preset.name}px`, () => {
    test.use({
      viewport: { width: preset.width, height: preset.height },
      colorScheme: "light",
      contextOptions: { reducedMotion: "reduce" },
    });

    test("keeps the recommended set visible across shot list and canvas without cost", async ({ page }) => {
      assertLiveBrowserGraphEnabled();
      await lockLocale(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      const assertClean = attachBrowserGuards(page);
      await openRecommendedSetWorkbench(page, `e2e-recommended-set-${preset.name}-${Date.now()}`);

      const shotGroupIds = await assertShotListLayout(page);
      const firstShot = page.locator("[data-graph-shot-id]").first();
      await firstShot.locator("[data-graph-shot-open-node]").click({ force: true });
      await expect(page.locator("[data-graph-node-inspector]")).toBeVisible();

      const shotsTab = page.locator('[data-graph-view="shots"]');
      const canvasTab = page.locator('[data-graph-view="canvas"]');
      await canvasTab.click({ force: true });
      await expect(canvasTab).toHaveAttribute("aria-selected", "true");
      const canvas = page.locator('[aria-hidden="false"] [aria-label="工作流画布"]');
      await expect(canvas).toBeVisible();
      const canvasGroupIds = await canvas.locator("[data-graph-group-id]").evaluateAll((groups) => (
        groups.map((group) => group.getAttribute("data-graph-group-id")).filter((id): id is string => Boolean(id))
      ));
      expect(canvasGroupIds.sort()).toEqual(shotGroupIds.sort());
      expect(canvasGroupIds.sort()).toEqual((await currentGraphGroupIds(page)).sort());

      await shotsTab.click({ force: true });
      await expect(shotsTab).toHaveAttribute("aria-selected", "true");
      await expect(page.locator("[data-graph-shot-list]")).toBeVisible();
      await expect(page.locator("[data-graph-shot-id]")).toHaveCount(4);
      assertClean();
    });
  });
}
