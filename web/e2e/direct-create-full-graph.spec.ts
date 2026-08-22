import { expect, test } from "@playwright/test";

import {
  REFERENCE_PRODUCT_IMAGE,
  assertGeneratedImageBytes,
  assertLiveBrowserGraphEnabled,
  assertRealImageProviders,
  lockLocale,
  loginAsAdmin,
  requiredEnv,
  waitForGraphRunSucceeded,
} from "./liveGraph";

test.describe("live browser graph", () => {
  test("creates a one-image product in the browser, runs the graph, and shows a real generated image", async ({
    page,
  }) => {
    assertLiveBrowserGraphEnabled();
    const adminKey = requiredEnv("ADMIN_ACCESS_KEY");
    const settingsToken = requiredEnv("SETTINGS_ACCESS_TOKEN");
    await lockLocale(page);
    await loginAsAdmin(page, adminKey);
    await assertRealImageProviders(page.request, settingsToken);

    const productName = `e2e-live-graph ${new Date().toISOString().replaceAll(":", "").slice(0, 15)}`;
    await page.goto("/products/new");
    await expect(page.locator("[data-image-type='detail']")).toBeVisible();
    await page.locator("#agent-product-name").fill(productName);
    await page.locator("#agent-product-brief").fill(
      "陶瓷马克杯，暖白釉，电商细节图，保留真实材质和把手结构。",
    );
    await page.locator('[data-image-type="detail"]').click();
    await expect(page.locator('[data-image-type="detail"] input[type="checkbox"]')).toBeChecked();
    await page.locator('[data-image-type="detail"] input[type="number"]').fill("1");
    await expect(page.locator('[data-image-type="detail"] input[type="number"]')).toHaveValue("1");
    await page.locator("[data-agent-product-intake-form] input[type='file']").setInputFiles(
      REFERENCE_PRODUCT_IMAGE,
    );
    await page.getByRole("button", { name: "直接创建进入工作台" }).click();
    await page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/, { timeout: 60_000 });
    await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();

    const productId = new URL(page.url()).pathname.split("/")[2] ?? "";
    expect(productId).toBeTruthy();
    const graphResponse = await page.request.get(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/current`,
    );
    expect(graphResponse.ok(), await graphResponse.text()).toBeTruthy();
    const graph = (await graphResponse.json()) as { id: string };
    expect(graph.id).toBeTruthy();

    await page.locator("[data-graph-run-all]").click();
    await waitForGraphRunSucceeded(page.request, productId, graph.id);

    await page.locator('[data-sidebar-tool="runs"]').click();
    await expect(page.locator("[data-graph-runs-panel] [data-graph-run-status='succeeded']")).toBeVisible();
    await expect(page.locator("[data-graph-runs-panel]").getByText("成功").first()).toBeVisible();

    await page.locator('[data-sidebar-tool="library"]').click();
    const generatedFilter = page.getByRole("button", { name: "生成结果" });
    if (!(await generatedFilter.isVisible())) {
      await page.getByRole("button", { name: "打开目录" }).click();
    }
    await generatedFilter.click();
    const generatedCard = page.locator('[data-gallery-origin-type="workflow_generation"]');
    await expect(generatedCard.first()).toBeVisible();
    await expect(generatedCard.first().locator("img")).toBeVisible();

    const generated = await assertGeneratedImageBytes(page.request, productId);
    expect(generated.id).toBeTruthy();
  });
});
