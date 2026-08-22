import { expect, test, type Page } from "@playwright/test";
import path from "node:path";

import {
  REFERENCE_PRODUCT_IMAGE,
  assertLiveBrowserGraphEnabled,
  loginAsAdmin,
  requiredEnv,
} from "./liveGraph";

const SCRATCH_ACTIONS = "/tmp/grok-goal-561ad5722a1f/implementer/e2e-actions";

const NODE_TYPES = [
  { type: "product_source", label: "商品资料" },
  { type: "image_asset", label: "图片素材" },
  { type: "creative_brief", label: "创作要求" },
  { type: "visual_system", label: "视觉规范" },
  { type: "prompt_generation", label: "提示词生成" },
  { type: "image_generation", label: "图片生成" },
] as const;

type Theme = "light" | "dark";

const PRESETS: Array<{ name: string; width: number; height: number; theme: Theme }> = [
  { name: "1440-light", width: 1440, height: 900, theme: "light" },
  { name: "1440-dark", width: 1440, height: 900, theme: "dark" },
  { name: "1024-light", width: 1024, height: 768, theme: "light" },
  { name: "1024-dark", width: 1024, height: 768, theme: "dark" },
  { name: "390-light", width: 390, height: 844, theme: "light" },
  { name: "390-dark", width: 390, height: 844, theme: "dark" },
];

interface GraphNodePayload {
  id: string;
  node_type: string;
  title: string;
  unused: boolean;
  bound_asset_id: string | null;
  group_id: string | null;
  incoming: Array<{ node_id: string; role: string }>;
  outgoing: Array<{ node_id: string; role: string }>;
}

interface GraphPayload {
  id: string;
  revision: number;
  can_undo: boolean;
  can_redo: boolean;
  nodes: GraphNodePayload[];
  edges: Array<{ id: string; source_node_id: string; target_node_id: string; role: string }>;
  groups: Array<{ id: string; title: string; member_ids: string[] }>;
}

test.use({
  trace: "on",
  screenshot: "on",
});

function productIdFrom(page: Page): string {
  const productId = new URL(page.url()).pathname.split("/")[2] ?? "";
  expect(productId).toBeTruthy();
  return productId;
}

function isWorkbenchApiFailure(url: string, status: number): boolean {
  if (status < 400) return false;
  if (/\/api\/v3\/.*\/(workflows|recipes)(\/|$|\?)/.test(url) || /\/api\/v3\/.*\/runs(\/|$|\?)/.test(url)) {
    return true;
  }
  return status === 409 && /\/agent-conversations\/.*\/turns\//.test(url);
}

function attachWorkbenchGuards(page: Page): () => void {
  const consoleErrors: string[] = [];
  const failedRequests: string[] = [];
  page.on("pageerror", (error) => {
    consoleErrors.push(error.message);
  });
  page.on("console", (message) => {
    if (message.type() !== "error") return;
    const text = message.text();
    if (text.includes("favicon") || text.includes("Download the React DevTools")) return;
    // Agent process 502/503 is a separate availability issue; 409 must stay visible.
    if (/Failed to load resource: the server responded with a status of (502|503)/.test(text)) return;
    consoleErrors.push(text);
  });
  page.on("response", (response) => {
    if (!isWorkbenchApiFailure(response.url(), response.status())) return;
    failedRequests.push(`${response.status()} ${response.url()}`);
  });
  return () => {
    expect(consoleErrors, consoleErrors.join("\n")).toEqual([]);
    expect(failedRequests, failedRequests.join("\n")).toEqual([]);
  };
}

async function preparePage(page: Page, theme: Theme): Promise<void> {
  await page.addInitScript((nextTheme: Theme) => {
    window.localStorage.setItem("productflow.locale", "zh-CN");
    window.localStorage.setItem("productflow.theme", nextTheme);
  }, theme);
  await page.emulateMedia({ reducedMotion: "reduce", colorScheme: theme });
}

async function openDirectCreateWorkbench(page: Page, name: string): Promise<void> {
  await page.goto("/products/new");
  await expect(page.locator("[data-image-type='detail']")).toBeVisible();
  await page.locator("#agent-product-name").fill(name);
  await page.locator("#agent-product-brief").fill("电商细节图，保留真实材质。");
  await page.locator('[data-image-type="detail"]').click();
  await expect(page.locator('[data-image-type="detail"] input[type="checkbox"]')).toBeChecked();
  await page.locator('[data-image-type="detail"] input[type="number"]').fill("1");
  await expect(page.locator('[data-image-type="detail"] input[type="number"]')).toHaveValue("1");
  await page.locator("[data-agent-product-intake-form] input[type='file']").setInputFiles(
    REFERENCE_PRODUCT_IMAGE,
  );
  const submit = page.getByRole("button", { name: "直接创建进入工作台" });
  await expect(submit).toBeEnabled();
  await submit.scrollIntoViewIfNeeded();
  await Promise.all([
    page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/, { timeout: 60_000 }),
    submit.click(),
  ]);
  await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme", /light|dark/);
}

async function currentGraph(page: Page): Promise<GraphPayload> {
  const response = await page.request.get(
    `/api/v3/products/${encodeURIComponent(productIdFrom(page))}/workflows/current`,
  );
  expect(response.ok(), await response.text()).toBeTruthy();
  return await response.json() as GraphPayload;
}

async function openAddPanel(page: Page) {
  await page.locator('[data-sidebar-tool="add"]').click();
  const panel = page.locator("[data-graph-add-node-panel]");
  await expect(panel).toBeVisible();
  return panel;
}

async function addPaletteNode(page: Page, label: string): Promise<GraphPayload> {
  const before = await currentGraph(page);
  const panel = await openAddPanel(page);
  await panel.getByRole("button", { name: new RegExp(label) }).click();
  await expect.poll(async () => (await currentGraph(page)).nodes.length).toBe(before.nodes.length + 1);
  return currentGraph(page);
}

async function selectNode(page: Page, nodeId: string, toggle = false): Promise<void> {
  const card = page.locator(`[data-workflow-node-id="${nodeId}"]`);
  await expect(card).toBeVisible();
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

async function enableMultiSelect(page: Page): Promise<void> {
  const viewport = page.viewportSize();
  if (viewport && viewport.width <= 1023) {
    await page.getByRole("button", { name: "选择模式：点按节点加入或移出多选" }).click();
  }
}

async function shiftSelectNode(page: Page, nodeId: string): Promise<void> {
  await selectNode(page, nodeId, true);
}

async function fitCanvas(page: Page): Promise<void> {
  const fit = page.getByRole("button", { name: "Fit View" });
  if (await fit.isVisible()) {
    await fit.click({ force: true });
  }
}

for (const preset of PRESETS) {
  test.describe(`v3 workbench actions ${preset.name}`, () => {
    test.use({
      viewport: { width: preset.width, height: preset.height },
      colorScheme: preset.theme,
      reducedMotion: "reduce",
    });

    test.beforeEach(async ({ page }) => {
      assertLiveBrowserGraphEnabled();
      await preparePage(page, preset.theme);
    });

    test("adds six node types and a scene with persisted nodes and edges", async ({ page }) => {
      const assertClean = attachWorkbenchGuards(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      await openDirectCreateWorkbench(page, `e2e-actions-assemble ${preset.name} ${Date.now()}`);
      await expect(page.locator("html")).toHaveAttribute("data-theme", preset.theme);

      const before = await currentGraph(page);
      for (const item of NODE_TYPES) {
        expect(before.nodes.some((node) => node.node_type === item.type), item.type).toBeTruthy();
      }

      for (const item of NODE_TYPES) {
        const snapshot = await addPaletteNode(page, item.label);
        expect(snapshot.nodes.filter((node) => node.node_type === item.type).length).toBeGreaterThan(
          before.nodes.filter((node) => node.node_type === item.type).length,
        );
      }

      const addedImage = (await currentGraph(page)).nodes.find((node) => {
        return node.node_type === "image_generation"
          && !before.nodes.some((existing) => existing.id === node.id);
      });
      expect(addedImage).toBeTruthy();
      await selectNode(page, addedImage!.id);
      await page.locator('[data-sidebar-tool="details"]').click();
      await expect(page.locator("[data-graph-node-inspector]")).toContainText("先连上提示词节点，才能运行");

      const preScene = await currentGraph(page);
      const panel = await openAddPanel(page);
      await panel.locator("[data-add-shot]").getByRole("button", { name: "添加" }).click();
      await expect.poll(async () => (await currentGraph(page)).groups.length).toBe(preScene.groups.length + 1);
      const afterScene = await currentGraph(page);
      const newGroups = afterScene.groups.filter((group) => !preScene.groups.some((item) => item.id === group.id));
      const newPrompts = afterScene.nodes.filter((node) => {
        return node.node_type === "prompt_generation"
          && !preScene.nodes.some((item) => item.id === node.id);
      });
      const newImages = afterScene.nodes.filter((node) => {
        return node.node_type === "image_generation"
          && !preScene.nodes.some((item) => item.id === node.id);
      });
      expect(newGroups).toHaveLength(1);
      expect(newPrompts).toHaveLength(1);
      expect(newImages).toHaveLength(1);
      const source = afterScene.nodes.find((node) => node.node_type === "product_source");
      const visual = afterScene.nodes.find((node) => node.node_type === "visual_system");
      const brief = afterScene.nodes.find((node) => node.node_type === "creative_brief");
      expect(source && newPrompts[0].incoming.some((edge) => edge.node_id === source.id)).toBeTruthy();
      expect(visual && newPrompts[0].incoming.some((edge) => edge.node_id === visual.id)).toBeTruthy();
      expect(brief && newPrompts[0].incoming.some((edge) => edge.node_id === brief.id)).toBeTruthy();
      expect(newImages[0].incoming.some((edge) => edge.node_id === newPrompts[0].id)).toBeTruthy();
      await expect(page.locator(`[data-workflow-node-id="${newPrompts[0].id}"]`)).toBeVisible();
      await expect(page.locator(`[data-workflow-node-id="${newImages[0].id}"]`)).toBeVisible();
      assertClean();
    });

    test("deleting an edge removes it from the persisted graph", async ({ page }) => {
      const assertClean = attachWorkbenchGuards(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      await openDirectCreateWorkbench(page, `e2e-actions-edge ${preset.name} ${Date.now()}`);
      const before = await currentGraph(page);
      expect(before.edges.length).toBeGreaterThan(0);
      const beforeIds = new Set(before.edges.map((edge) => edge.id));
      const edgeHit = page.locator("[data-edge-emphasis]").last();
      await edgeHit.click({ force: true });
      const deleteEdge = page.getByRole("button", { name: /删除连线/ }).first();
      await expect(deleteEdge).toBeVisible();
      await deleteEdge.click({ force: true });
      await expect.poll(async () => (await currentGraph(page)).edges.length).toBe(before.edges.length - 1);
      const after = await currentGraph(page);
      const deleted = [...beforeIds].filter((id) => !after.edges.some((edge) => edge.id === id));
      expect(deleted).toHaveLength(1);
      assertClean();
    });

    test("binding an asset without connecting shows unused", async ({ page }) => {
      const assertClean = attachWorkbenchGuards(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      await openDirectCreateWorkbench(page, `e2e-actions-unused ${preset.name} ${Date.now()}`);
      const afterAdd = await addPaletteNode(page, "图片素材");
      const created = afterAdd.nodes.find((node) => {
        return node.node_type === "image_asset" && node.bound_asset_id == null;
      });
      expect(created).toBeTruthy();
      await selectNode(page, created!.id);
      await page.locator('[data-sidebar-tool="details"]').click();
      await expect(page.locator("[data-graph-node-inspector]")).toBeVisible();
      await page.locator("[data-graph-node-inspector]").getByRole("button", { name: "选图" }).click();
      await expect(page.locator("[data-graph-library-panel]")).toBeVisible();
      await page.locator("[data-gallery-asset-id]").first().getByLabel("更多操作").click();
      await page.getByRole("button", { name: "再次引用" }).click();
      await expect.poll(async () => {
        const graph = await currentGraph(page);
        const node = graph.nodes.find((item) => item.id === created!.id);
        return Boolean(node?.unused && node.bound_asset_id);
      }).toBeTruthy();
      await selectNode(page, created!.id);
      await expect(page.locator(`[data-workflow-node-id="${created!.id}"]`)).toContainText("已选图，还没被用到");
      await page.locator('[data-sidebar-tool="details"]').click();
      await expect(page.locator("[data-graph-node-inspector]")).toContainText("已选图，还没被用到");
      const persisted = (await currentGraph(page)).nodes.find((node) => node.id === created!.id);
      expect(persisted?.outgoing ?? []).toEqual([]);
      assertClean();
    });

    test("dirty inspector flushes before switching nodes", async ({ page }) => {
      const assertClean = attachWorkbenchGuards(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      await openDirectCreateWorkbench(page, `e2e-actions-flush ${preset.name} ${Date.now()}`);
      const graph = await currentGraph(page);
      const brief = graph.nodes.find((node) => node.node_type === "creative_brief");
      const visual = graph.nodes.find((node) => node.node_type === "visual_system");
      expect(brief && visual).toBeTruthy();
      await selectNode(page, brief!.id);
      await page.locator('[data-sidebar-tool="details"]').click();
      await expect(page.locator("[data-graph-node-inspector]")).toContainText("创作要求");
      const title = `冲洗标题 ${Date.now()}`;
      await page.locator("[data-graph-node-inspector]").getByLabel("标题").fill(title);
      await expect(
        page.locator("[data-graph-node-inspector]").getByRole("button", { name: "保存", exact: true }),
      ).toBeVisible();
      await selectNode(page, visual!.id);
      await expect.poll(async () => {
        const next = await currentGraph(page);
        return next.nodes.find((node) => node.id === brief!.id)?.title ?? "";
      }).toBe(title);
      await selectNode(page, brief!.id);
      await expect(page.locator("[data-graph-node-inspector]")).toContainText(title);
      await expect(page.locator("[data-graph-node-inspector]").getByLabel("标题")).toHaveValue(title);
      assertClean();
    });

    test("failed node run appears on the card, in details, and can retry", async ({ page }) => {
      const assertClean = attachWorkbenchGuards(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      await openDirectCreateWorkbench(page, `e2e-actions-fail ${preset.name} ${Date.now()}`);
      const graph = await currentGraph(page);
      const image = graph.nodes.find((node) => node.node_type === "image_generation");
      expect(image).toBeTruthy();
      await selectNode(page, image!.id);
      await page.locator('[data-sidebar-tool="details"]').click();
      await page.locator("[data-graph-node-inspector]").getByRole("button", { name: "运行该节点" }).click();
      const card = page.locator(`[data-workflow-node-id="${image!.id}"]`);
      await expect(card).toContainText("失败", { timeout: 60_000 });
      await expect(card).toContainText("上游提示词尚未生成");
      await expect(page.locator("[data-graph-node-inspector]")).toContainText("上游提示词尚未生成");
      await expect(page.getByRole("button", { name: "重试" })).toBeVisible();
      const runs = await page.request.get(
        `/api/v3/products/${encodeURIComponent(productIdFrom(page))}/workflows/${encodeURIComponent(graph.id)}/runs`,
      );
      expect(runs.ok(), await runs.text()).toBeTruthy();
      const payload = await runs.json() as { items: Array<{ status: string; failure_reason: string | null }> };
      expect(payload.items[0]?.status).toBe("failed");
      expect(payload.items[0]?.failure_reason).toContain("上游提示词尚未生成");
      assertClean();
    });

    test("copy-paste selects the clones and keeps internal edges", async ({ page }) => {
      const assertClean = attachWorkbenchGuards(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      await openDirectCreateWorkbench(page, `e2e-actions-paste ${preset.name} ${Date.now()}`);
      await enableMultiSelect(page);
      const before = await currentGraph(page);
      const prompt = before.nodes.find((node) => node.node_type === "prompt_generation");
      const image = before.nodes.find((node) => {
        return node.node_type === "image_generation" && node.incoming.some((edge) => edge.node_id === prompt?.id);
      });
      expect(prompt && image).toBeTruthy();
      await selectNode(page, prompt!.id);
      await shiftSelectNode(page, image!.id);
      const duplicatePanel = await openAddPanel(page);
      const duplicate = duplicatePanel.getByRole("button", { name: "复制 复制选中的节点，以及它们之间的连线" });
      await expect(duplicate).toBeVisible();
      await duplicate.click({ force: true });
      await expect.poll(async () => (await currentGraph(page)).nodes.length).toBe(before.nodes.length + 2);
      const after = await currentGraph(page);
      const created = after.nodes.filter((node) => !before.nodes.some((item) => item.id === node.id));
      expect(created).toHaveLength(2);
      const createdPrompt = created.find((node) => node.node_type === "prompt_generation");
      const createdImage = created.find((node) => node.node_type === "image_generation");
      expect(createdPrompt && createdImage).toBeTruthy();
      expect(after.edges.some((edge) => {
        return edge.source_node_id === createdPrompt!.id && edge.target_node_id === createdImage!.id;
      })).toBeTruthy();
      await expect(page.locator(`[data-workflow-node-id="${createdPrompt!.id}"]`)).toBeVisible();
      await expect(page.locator(`[data-workflow-node-id="${createdImage!.id}"]`)).toBeVisible();
      const panel = await openAddPanel(page);
      await expect(panel.getByRole("button", { name: "复制 复制选中的节点，以及它们之间的连线" })).toBeEnabled();
      assertClean();
    });

    test("group rename, enter, and exit restore the full graph", async ({ page }) => {
      const assertClean = attachWorkbenchGuards(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      await openDirectCreateWorkbench(page, `e2e-actions-group ${preset.name} ${Date.now()}`);
      await enableMultiSelect(page);
      const before = await currentGraph(page);
      const prompt = before.nodes.find((node) => node.node_type === "prompt_generation");
      const image = before.nodes.find((node) => node.node_type === "image_generation");
      const source = before.nodes.find((node) => node.node_type === "product_source");
      expect(prompt && image && source).toBeTruthy();
      await selectNode(page, prompt!.id);
      await shiftSelectNode(page, image!.id);
      await expect(page.locator(".react-flow__node.selected")).toHaveCount(2);
      const panel = await openAddPanel(page);
      await panel.getByRole("button", { name: "编组 把选中的节点放进一个分组" }).click();
      await expect.poll(async () => (await currentGraph(page)).groups.length).toBeGreaterThan(before.groups.length);
      const grouped = await currentGraph(page);
      const createdGroup = grouped.groups.find((group) => !before.groups.some((item) => item.id === group.id));
      expect(createdGroup).toBeTruthy();
      await fitCanvas(page);
      const groupCard = page.locator(`[data-graph-group-id="${createdGroup!.id}"]`);
      await expect(groupCard).toBeVisible();
      await selectNode(page, source!.id);
      await groupCard.getByLabel("重命名").evaluate((element: HTMLElement) => element.click());
      await expect(groupCard.locator("input")).toBeVisible();
      await groupCard.locator("input").fill("验收分组");
      await groupCard.locator("input").press("Enter");
      await expect.poll(async () => {
        const graph = await currentGraph(page);
        return graph.groups.find((group) => group.id === createdGroup!.id)?.title ?? "";
      }).toBe("验收分组");
      await groupCard.getByLabel("进入").evaluate((element: HTMLElement) => element.click());
      await expect(page.locator('[aria-label="工作流画布"]')).toHaveAttribute(
        "data-graph-entered-group",
        createdGroup!.id,
      );
      await expect(page.locator("[data-graph-group-breadcrumb]")).toContainText("验收分组");
      await expect(page.locator(`[data-workflow-node-id="${source!.id}"]`)).toHaveCount(0);
      await page.getByLabel("返回全局画布").click();
      await expect(page.locator('[aria-label="工作流画布"]')).not.toHaveAttribute("data-graph-entered-group");
      await expect(page.locator(`[data-workflow-node-id="${source!.id}"]`)).toBeVisible();
      assertClean();
    });

    test("undo reverts the last operation group and redo reapplies only that group", async ({ page }) => {
      const assertClean = attachWorkbenchGuards(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      await openDirectCreateWorkbench(page, `e2e-actions-undo ${preset.name} ${Date.now()}`);
      const before = await currentGraph(page);
      const panel = await openAddPanel(page);
      await panel.locator("[data-add-shot]").getByRole("button", { name: "添加" }).click();
      await expect.poll(async () => (await currentGraph(page)).groups.length).toBe(before.groups.length + 1);
      const added = await currentGraph(page);
      expect(added.can_undo).toBeTruthy();
      const undo = page.getByLabel("撤销");
      await expect(undo).toBeEnabled();
      await undo.click();
      await expect.poll(async () => (await currentGraph(page)).groups.length).toBe(before.groups.length);
      const undone = await currentGraph(page);
      expect(undone.nodes.map((node) => node.id).sort()).toEqual(before.nodes.map((node) => node.id).sort());
      expect(undone.can_redo).toBeTruthy();
      const redo = page.getByLabel("重做");
      await expect(redo).toBeEnabled();
      await redo.click();
      await expect.poll(async () => (await currentGraph(page)).groups.length).toBe(added.groups.length);
      const redone = await currentGraph(page);
      expect(redone.nodes.length).toBe(added.nodes.length);
      expect(redone.can_redo).toBeFalsy();
      await expect(redo).toBeDisabled();
      assertClean();
    });

    test("maximize hides the top bar and leaves the canvas filling the main area", async ({ page }) => {
      const assertClean = attachWorkbenchGuards(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      await openDirectCreateWorkbench(page, `e2e-actions-max ${preset.name} ${Date.now()}`);
      await expect(page.getByText(/Agent 工作台/)).toBeVisible();
      await page.getByLabel("最大化画布").click();
      await expect(page.getByText(/Agent 工作台/)).toHaveCount(0);
      await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
      await expect(page.locator("[data-workflow-node-id]").first()).toBeVisible();
      await expect(page.getByLabel("还原画布布局")).toBeVisible();
      await page.getByLabel("还原画布布局").click();
      await expect(page.getByText(/Agent 工作台/)).toBeVisible();
      assertClean();
    });

    test("390 drawer leaves canvas nodes visible with the inspector open", async ({ page }) => {
      const assertClean = attachWorkbenchGuards(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
      await openDirectCreateWorkbench(page, `e2e-actions-drawer ${preset.name} ${Date.now()}`);
      await openAddPanel(page);
      const canvas = page.locator("[data-agent-workbench-canvas-slot]");
      const inspector = page.locator("[data-product-workbench-inspector]");
      await expect(canvas).toBeVisible();
      await expect(page.locator("[data-workflow-node-id]").first()).toBeVisible();
      if (preset.width <= 1023) {
        await expect(inspector).toHaveAttribute("data-inspector-layout", "drawer");
        const canvasBox = await canvas.boundingBox();
        const inspectorBox = await inspector.boundingBox();
        expect(canvasBox).toBeTruthy();
        expect(inspectorBox).toBeTruthy();
        expect(inspectorBox!.height).toBeLessThan(canvasBox!.height);
        expect(canvasBox!.y).toBeLessThan(inspectorBox!.y);
        expect(canvasBox!.height).toBeGreaterThan(120);
      }
      if (preset.name === "390-light") {
        await page.screenshot({
          path: path.join(SCRATCH_ACTIONS, "390-drawer.png"),
          fullPage: false,
        });
      }
      assertClean();
    });
  });
}
