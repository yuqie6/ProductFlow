import { expect, test, type Page } from "@playwright/test";

import {
  REFERENCE_PRODUCT_IMAGE,
  loginAsAdmin,
  requiredEnv,
  selectCreateImageType,
} from "./liveGraph";

const SWITCH = "PRODUCTFLOW_RUN_RUNNING_TURN_CANVAS";

interface GraphPayload {
  id: string;
  revision: number;
  nodes: Array<{ id: string; node_type: string; title: string }>;
}

interface WorkbenchPayload {
  conversation: { id: string };
}

interface SubmitTurnPayload {
  turn: {
    id: string;
    status: string;
    harness_run_id: string;
    harness_turn_id: string | null;
    idempotency_key: string;
  };
}

interface LeasePayload {
  execution_id: string;
  lease_token: string;
}

function enabled(): boolean {
  return process.env[SWITCH] === "1";
}

function productIdFrom(page: Page): string {
  const productId = new URL(page.url()).pathname.split("/")[2] ?? "";
  expect(productId).toBeTruthy();
  return productId;
}

async function currentGraph(page: Page): Promise<GraphPayload> {
  const response = await page.request.get(
    `/api/v3/products/${encodeURIComponent(productIdFrom(page))}/workflows/current`,
  );
  expect(response.ok(), await response.text()).toBeTruthy();
  return await response.json() as GraphPayload;
}

async function turnStatus(page: Page, conversationId: string, turnId: string): Promise<string> {
  const response = await page.request.get(
    `/api/v2/products/${encodeURIComponent(productIdFrom(page))}/agent-conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(turnId)}`,
  );
  expect(response.ok(), await response.text()).toBeTruthy();
  const body = await response.json() as { status: string };
  return body.status;
}

test("running Turn does not lock the canvas; node title persist while Turn stays running", async ({ page }) => {
  test.skip(!enabled(), `set ${SWITCH}=1, start Go+PostgreSQL+Web without the Node Agent, then run just web-e2e-running-turn-canvas`);
  const internalToken = requiredEnv("AGENT_SERVICE_INTERNAL_TOKEN");
  await page.addInitScript(() => {
    window.localStorage.setItem("productflow.locale", "zh-CN");
  });
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  await page.goto("/products/new");
  await expect(page.locator("[data-image-type='detail']")).toBeVisible();
  await page.locator("#agent-product-name").fill(`running-turn-canvas ${Date.now()}`);
  await page.locator("#agent-product-brief").fill("画布在 running Turn 下仍可编辑。");
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

  const productId = productIdFrom(page);
  const workbench = await page.request.post(`/api/v2/products/${encodeURIComponent(productId)}/agent-workbench`, {
    headers: { "Idempotency-Key": `running-turn-canvas:${productId}` },
  });
  expect(workbench.ok(), await workbench.text()).toBeTruthy();
  const bootstrap = await workbench.json() as WorkbenchPayload;
  const conversationId = bootstrap.conversation.id;
  expect(conversationId).toBeTruthy();

  const idempotencyKey = `running-turn-${Date.now()}`;
  const turnResponse = await page.request.post(
    `/api/v2/products/${encodeURIComponent(productId)}/agent-conversations/${encodeURIComponent(conversationId)}/turns`,
    { data: { input_text: "保持 running，编辑画布", idempotency_key: idempotencyKey } },
  );
  expect(turnResponse.ok(), await turnResponse.text()).toBeTruthy();
  const submitted = await turnResponse.json() as SubmitTurnPayload;
  const harnessTurnId = submitted.turn.harness_turn_id ?? crypto.randomUUID();

  const claim = await page.request.post(
    `/api/internal/v1/agent-conversations/${encodeURIComponent(conversationId)}/turn-executions/claim`,
    {
      headers: { Authorization: `Bearer ${internalToken}` },
      data: {
        idempotency_key: idempotencyKey,
        harness_turn_id: harnessTurnId,
        owner_id: "e2e-running-turn-canvas",
      },
    },
  );
  expect(claim.ok(), await claim.text()).toBeTruthy();
  const lease = await claim.json() as LeasePayload;
  expect(lease.execution_id).toBeTruthy();
  expect(await turnStatus(page, conversationId, submitted.turn.id)).toBe("running");

  const before = await currentGraph(page);
  const brief = before.nodes.find((node) => node.node_type === "creative_brief");
  expect(brief).toBeTruthy();
  const fit = page.getByRole("button", { name: "适配全图" });
  if (await fit.count()) {
    await fit.evaluate((button: HTMLButtonElement) => button.click());
  }
  const card = page.locator(`[data-workflow-node-id="${brief!.id}"]`);
  await expect(card).toBeAttached();
  await card.evaluate((element: HTMLElement) => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, view: window }));
  });
  const details = page.locator(`[data-sidebar-tool="details"]`).filter({ visible: true });
  await details.click();
  await expect(page.locator("[data-graph-node-inspector]")).toBeVisible();
  const title = `running-turn ${Date.now()}`;
  await page.locator("[data-graph-node-inspector]").getByLabel("标题").fill(title);
  const save = page.locator("[data-graph-node-inspector]").getByRole("button", { name: "保存", exact: true });
  if (await save.isVisible()) {
    await save.click();
  }
  await expect.poll(async () => {
    const next = await currentGraph(page);
    return next.nodes.find((node) => node.id === brief!.id)?.title ?? "";
  }).toBe(title);
  const after = await currentGraph(page);
  expect(after.revision).toBeGreaterThan(before.revision);
  expect(await turnStatus(page, conversationId, submitted.turn.id)).toBe("running");
});
