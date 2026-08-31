import { expect, test, type Page } from "@playwright/test";

import {
  REFERENCE_PRODUCT_IMAGE,
  loginAsAdmin,
  requiredEnv,
  selectCreateImageType,
} from "./liveGraph";

interface GraphNodePayload {
  id: string;
  node_type: string;
  pending_candidate_artifact_id?: string | null;
}

interface GraphPayload {
  id: string;
  revision: number;
  nodes: GraphNodePayload[];
}

async function createNoCostWorkbench(page: Page): Promise<string> {
  await page.goto("/products/new");
  await page.locator("#agent-product-name").fill(`candidate-review ${Date.now()}`);
  await page.locator("#agent-product-brief").fill("候选审阅布局验证，不执行模型生成。");
  await selectCreateImageType(page, "detail");
  await page.locator('[data-image-type="detail"] input[type="number"]').fill("1");
  await page.locator("[data-agent-product-intake-form] input[type='file']").setInputFiles(REFERENCE_PRODUCT_IMAGE);
  await Promise.all([
    page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/),
    page.locator("[data-create-direct]").click(),
  ]);
  return new URL(page.url()).pathname.split("/").filter(Boolean).at(-1) ?? "";
}

async function selectNode(page: Page, nodeID: string): Promise<void> {
  const card = page.locator(`[data-workflow-node-id="${nodeID}"]`);
  await expect(card).toBeAttached();
  await card.evaluate((element: HTMLElement) => {
    element.dispatchEvent(new MouseEvent("click", {
      bubbles: true,
      cancelable: true,
      view: window,
      buttons: 1,
    }));
  });
}

test("document candidate review stays bounded on desktop and mobile", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("productflow.locale", "zh-CN");
    window.localStorage.setItem("productflow.theme", "light");
  });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  const productID = await createNoCostWorkbench(page);
  const graphResponse = await page.request.get(`/api/v3/products/${encodeURIComponent(productID)}/workflows/current`);
  expect(graphResponse.ok(), await graphResponse.text()).toBeTruthy();
  const graph = await graphResponse.json() as GraphPayload;
  const prompt = graph.nodes.find((node) => node.node_type === "image_prompt");
  expect(prompt).toBeTruthy();

  await page.route(`**/api/v3/products/${productID}/workflows/current`, async (route) => {
    const response = await route.fetch();
    const payload = await response.json() as GraphPayload;
    payload.nodes = payload.nodes.map((node) => node.id === prompt!.id
      ? { ...node, pending_candidate_artifact_id: "candidate-review-1" }
      : node);
    await route.fulfill({ response, json: payload });
  });
  await page.route(`**/api/v3/products/${productID}/workflows/${graph.id}/nodes/${prompt!.id}/candidate`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      json: {
        artifact_id: "candidate-review-1",
        node_id: prompt!.id,
        document_action: "rewrite",
        status: "ready",
        base_document_hash: "a".repeat(64),
        input_digest: "b".repeat(64),
        current_document: { design_goal: "保留真实材质与产品结构" },
        candidate_document: { design_goal: "突出杯身纹理与便携卖点" },
        sections: [
          {
            key: "objective",
            changed: true,
            current: { design_goal: "保留真实材质与产品结构" },
            candidate: { design_goal: "突出杯身纹理与便携卖点" },
          },
          {
            key: "composition",
            changed: true,
            current: { composition: { layout: "居中", product_share_percent: 65 } },
            candidate: { composition: { layout: "左侧主体，右侧留白", product_share_percent: 58 } },
          },
        ],
        created_at: new Date().toISOString(),
      },
    });
  });

  await page.reload();
  await selectNode(page, prompt!.id);
  const review = page.locator("[data-graph-document-candidate]");
  await expect(review).toBeVisible();
  await expect(review).toContainText("AI 文稿建议");
  await expect(review).toContainText("设计目标");
  await expect(review).toContainText("布局");
  await expect(review).toContainText("商品占比");
  await expect(review).toContainText("保留真实材质与产品结构");
  await expect(review).toContainText("突出杯身纹理与便携卖点");
  await expect(review).toContainText("左侧主体，右侧留白");
  await expect(review).not.toContainText("design_goal");
  await expect(review).not.toContainText("product_share_percent");
  await expect(review).not.toContainText('"layout"');
  await expect(review.getByRole("button", { name: "目标" })).toHaveAttribute("aria-pressed", "true");
  await expect(review.getByRole("button", { name: "构图" })).toHaveAttribute("aria-pressed", "true");
  await expect(review.getByRole("button", { name: "应用所选" })).toBeEnabled();
  await expect(review.getByRole("button", { name: "整份采用" })).toBeEnabled();
  await page.screenshot({ path: "/tmp/productflow-document-candidate-desktop.png", fullPage: false });

  await page.setViewportSize({ width: 390, height: 844 });
  const drawerHandle = page.locator("[data-product-workbench-drawer-handle]");
  if (await drawerHandle.isVisible()) await drawerHandle.click({ force: true });
  await expect(review).toBeVisible();
  const geometry = await review.evaluate((element) => ({
    viewportWidth: document.documentElement.clientWidth,
    documentWidth: document.documentElement.scrollWidth,
    panelWidth: element.getBoundingClientRect().width,
    panelRight: element.getBoundingClientRect().right,
    panelScrollWidth: element.scrollWidth,
    panelClientWidth: element.clientWidth,
  }));
  expect(geometry.documentWidth).toBeLessThanOrEqual(geometry.viewportWidth);
  expect(geometry.panelRight).toBeLessThanOrEqual(geometry.viewportWidth + 1);
  expect(geometry.panelScrollWidth).toBeLessThanOrEqual(geometry.panelClientWidth + 1);
  expect(geometry.panelWidth).toBeGreaterThan(250);
  await page.screenshot({ path: "/tmp/productflow-document-candidate-mobile.png", fullPage: false });
});
