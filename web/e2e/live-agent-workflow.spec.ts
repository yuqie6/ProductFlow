import { expect, test } from "@playwright/test";

import {
  REFERENCE_PRODUCT_IMAGE,
  assertGeneratedImageBytes,
  assertRealImageProviders,
  lockLocale,
  loginAsAdmin,
  requiredEnv,
  selectCreateImageType,
  waitForGraphRunSucceeded,
} from "./liveGraph";

const SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_WORKFLOW";

test("Agent approval card submits one real WorkflowRun with generated images", async ({ page }) => {
  test.skip(process.env[SWITCH] !== "1", `set ${SWITCH}=1, start just dev with real Agent/providers, then run just web-e2e-live-agent-workflow`);
  test.setTimeout(20 * 60 * 1000);
  const adminKey = requiredEnv("ADMIN_ACCESS_KEY");
  await lockLocale(page);
  await loginAsAdmin(page, adminKey);
  await assertRealImageProviders(page.request);

  await page.goto("/products/new");
  await expect(page.locator("[data-image-type='detail']")).toBeVisible();
  await page.locator("#agent-product-name").fill(`e2e-live-agent-workflow ${Date.now()}`);
  await page.locator("#agent-product-brief").fill("陶瓷马克杯，暖白釉，请按当前工作流跑完整图。");
  await selectCreateImageType(page, "detail");
  await page.locator('[data-image-type="detail"] input[type="number"]').fill("1");
  await page.locator("[data-agent-product-intake-form] input[type='file']").setInputFiles(REFERENCE_PRODUCT_IMAGE);
  await expect(page.locator('[data-create-reference-count="1"]')).toBeVisible();
  const submit = page.getByRole("button", { name: "只建画布" });
  await expect(submit).toBeEnabled();
  await Promise.all([
    page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/, { timeout: 60_000 }),
    submit.click(),
  ]);
  await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();

  const productId = new URL(page.url()).pathname.split("/")[2] ?? "";
  expect(productId).toBeTruthy();
  const graphResponse = await page.request.get(
    `/api/v3/products/${encodeURIComponent(productId)}/workflows/current`,
  );
  expect(graphResponse.ok(), await graphResponse.text()).toBeTruthy();
  const graph = (await graphResponse.json()) as { id: string };
  expect(graph.id).toBeTruthy();

  await page.locator('[data-sidebar-tool="agent"]').click();
  const composer = page.locator("[data-agent-composer] textarea");
  const openConversation = page.locator("[data-open-canvas-conversation]");
  await expect(openConversation.or(composer)).toBeVisible({ timeout: 20_000 });
  if (await openConversation.isVisible()) {
    await openConversation.click();
  }
  await expect(composer).toBeVisible({ timeout: 20_000 });
  await composer.fill("请执行当前工作流，提交一次完整图运行。不要只改节点。");
  await page.getByRole("button", { name: "发送消息" }).click();

  const approval = page.locator("[data-agent-workflow-run-request]");
  await expect(approval).toBeVisible({ timeout: 6 * 60 * 1000 });
  await approval.getByRole("button", { name: "确认并执行" }).click();

  await waitForGraphRunSucceeded(page.request, productId, graph.id);
  const generated = await assertGeneratedImageBytes(page.request, productId);
  expect(generated.id).toBeTruthy();
});
