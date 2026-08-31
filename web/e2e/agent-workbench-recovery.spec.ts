import { expect, test, type APIRequestContext } from "@playwright/test";

import {
  assertLiveBrowserGraphEnabled,
  lockLocale,
  loginAsAdmin,
  requiredEnv,
} from "./liveGraph";

interface SessionPayload {
  id: string;
  conversations: Array<{ conversation_id: string }>;
}

interface SubmitTurnPayload {
  turn: {
    id: string;
    harness_run_id: string;
    harness_turn_id: string | null;
    idempotency_key: string;
  };
}

interface LeasePayload {
  execution_id: string;
  lease_token: string;
}

interface EventPagePayload {
  items: Array<{ sequence: number; kind: string }>;
}

declare global {
  interface Window {
    __agentEventSources?: string[];
  }
}

async function claimTurn(
  request: APIRequestContext,
  conversationId: string,
  submitted: SubmitTurnPayload,
  owner: string,
): Promise<LeasePayload | null> {
  const claim = await request.post(
    `/api/internal/v1/agent-conversations/${encodeURIComponent(conversationId)}/turn-executions/claim`,
    {
      headers: { Authorization: `Bearer ${requiredEnv("AGENT_SERVICE_INTERNAL_TOKEN")}` },
      data: {
        idempotency_key: submitted.turn.idempotency_key,
        harness_turn_id: submitted.turn.harness_turn_id,
        owner_id: owner,
      },
    },
  );
  if (!claim.ok()) return null;
  return (await claim.json()) as LeasePayload;
}

async function submitAndClaimTurn(
  request: APIRequestContext,
  inputText: string,
  owner: string,
): Promise<{ session: SessionPayload; conversationId: string; submitted: SubmitTurnPayload; lease: LeasePayload } | null> {
  for (let attempt = 0; attempt < 8; attempt += 1) {
    const sessionResponse = await request.post("/api/v2/agent-sessions", {
      data: { title: `${owner} ${Date.now()} ${attempt}` },
    });
    if (!sessionResponse.ok()) continue;
    const session = (await sessionResponse.json()) as SessionPayload;
    const conversationId = session.conversations[0]?.conversation_id;
    if (!conversationId) continue;
    const turnResponse = await request.post(
      `/api/v2/agent-conversations/${encodeURIComponent(conversationId)}/turns`,
      { data: { input_text: inputText, idempotency_key: `${owner}-${Date.now()}-${attempt}` } },
    );
    if (!turnResponse.ok()) continue;
    const submitted = (await turnResponse.json()) as SubmitTurnPayload;
    if (!submitted.turn.harness_turn_id) continue;
    const lease = await claimTurn(request, conversationId, submitted, owner);
    if (lease) return { session, conversationId, submitted, lease };
  }
  return null;
}

async function appendEvents(
  request: APIRequestContext,
  conversationId: string,
  lease: LeasePayload,
  submitted: SubmitTurnPayload,
  owner: string,
  events: Array<{ sequence: number; kind: string; payload: Record<string, unknown> }>,
): Promise<void> {
  const posted = await request.post(
    `/api/internal/v1/agent-conversations/${encodeURIComponent(conversationId)}/turn-executions/${encodeURIComponent(lease.execution_id)}/events/batch`,
    {
      headers: { Authorization: `Bearer ${requiredEnv("AGENT_SERVICE_INTERNAL_TOKEN")}` },
      data: {
        owner_id: owner,
        lease_token: lease.lease_token,
        events: events.map((event) => ({
          ...event,
          schema_version: 1,
          run_id: submitted.turn.harness_run_id,
          turn_id: submitted.turn.harness_turn_id,
          created_at: new Date().toISOString(),
        })),
      },
    },
  );
  expect(posted.ok(), await posted.text()).toBeTruthy();
}

test("Chromium workbench shows effect_reconciled copy for an unknown Turn", async ({ page }) => {
  assertLiveBrowserGraphEnabled();
  await lockLocale(page);
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  await page.goto("/products");

  const claimed = await submitAndClaimTurn(page.request, "验证中断文案", "e2e-s207");
  if (!claimed) {
    test.skip(true, "live Agent worker already claimed this Turn");
    return;
  }
  const { session, conversationId, submitted, lease } = claimed;
  await appendEvents(page.request, conversationId, lease, submitted, "e2e-s207", [
    { sequence: 1, kind: "text.chunk", payload: { delta: "未写完", attempt_id: "a", step_id: "e2e-s207", content_index: 0 } },
    {
      sequence: 2,
      kind: "turn/end",
      payload: { status: "unknown", reason: "unknown", reason_code: "effect_reconciled" },
    },
  ]);

  await page.evaluate((sessionId) => {
    window.dispatchEvent(new CustomEvent("productflow:open-agent", { detail: { tab: "chat", sessionId } }));
  }, session.id);
  await expect(page.locator("[data-agent-turn-reason='effect_reconciled']")).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText("操作已完成，回复在中断前未写完")).toBeVisible();
});

test("Chromium reloads a parked approval Turn without reopening it", async ({ page }) => {
  assertLiveBrowserGraphEnabled();
  await lockLocale(page);
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  await page.goto("/products");

  const claimed = await submitAndClaimTurn(page.request, "验证审批重载", "e2e-s302");
  if (!claimed) {
    test.skip(true, "live Agent worker already claimed this Turn");
    return;
  }
  const { session, conversationId, submitted, lease } = claimed;
  await appendEvents(page.request, conversationId, lease, submitted, "e2e-s302", [
    {
      sequence: 1,
      kind: "approval/requested",
      payload: { approval_id: `appr-${Date.now()}`, approval_kind: "workflow_run" },
    },
    {
      sequence: 2,
      kind: "turn/end",
      payload: { status: "awaiting_confirmation", reason: "awaiting_confirmation" },
    },
  ]);

  await page.evaluate((sessionId) => {
    window.dispatchEvent(new CustomEvent("productflow:open-agent", { detail: { tab: "chat", sessionId } }));
  }, session.id);
  await expect(page.locator("[data-agent-turn-status='awaiting_confirmation']")).toBeVisible({ timeout: 20_000 });

  await page.reload();
  await page.evaluate((sessionId) => {
    window.dispatchEvent(new CustomEvent("productflow:open-agent", { detail: { tab: "chat", sessionId } }));
  }, session.id);
  await expect(page.locator("[data-agent-turn-status='awaiting_confirmation']")).toBeVisible({ timeout: 20_000 });
  const pageResponse = await page.request.get(
    `/api/v2/agent-conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(submitted.turn.id)}/events/page?after=0&limit=250`,
  );
  expect(pageResponse.ok(), await pageResponse.text()).toBeTruthy();
  const body = (await pageResponse.json()) as EventPagePayload;
  expect(body.items.filter((item) => item.kind === "approval.requested")).toHaveLength(1);
  expect(body.items.filter((item) => item.kind === "approval.resolved")).toHaveLength(0);
});

test("Chromium Workbench and Dock share one EventSource for the same live Turn", async ({ page }) => {
  assertLiveBrowserGraphEnabled();
  await page.addInitScript(() => {
    window.localStorage.setItem("productflow.locale", "zh-CN");
    const Original = window.EventSource;
    const opened: string[] = [];
    window.__agentEventSources = opened;
    window.EventSource = class extends Original {
      constructor(url: string | URL, init?: EventSourceInit) {
        super(url, init);
        opened.push(String(url));
      }
    };
  });
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));

  let productId = "";
  let conversationId = "";
  let sessionId = "";
  let submitted: SubmitTurnPayload | null = null;
  let lease: LeasePayload | null = null;
  for (let attempt = 0; attempt < 8; attempt += 1) {
    const created = await page.request.post("/api/v2/agent-product-workspaces/drafts", {
      headers: { "Idempotency-Key": `e2e-s406-${Date.now()}-${attempt}` },
      data: { name: `e2e-s406 ${Date.now()} ${attempt}` },
    });
    if (!created.ok()) continue;
    const snapshot = await created.json() as {
      product: { id: string };
      conversation: { id: string; session_id: string | null };
    };
    const turnResponse = await page.request.post(
      `/api/v2/products/${encodeURIComponent(snapshot.product.id)}/agent-conversations/${encodeURIComponent(snapshot.conversation.id)}/turns`,
      { data: { input_text: "验证单 EventSource", idempotency_key: `e2e-s406-${Date.now()}-${attempt}` } },
    );
    if (!turnResponse.ok()) continue;
    const nextSubmitted = (await turnResponse.json()) as SubmitTurnPayload;
    const nextLease = await claimTurn(page.request, snapshot.conversation.id, nextSubmitted, "e2e-s406");
    if (!nextLease) continue;
    productId = snapshot.product.id;
    conversationId = snapshot.conversation.id;
    sessionId = snapshot.conversation.session_id ?? "";
    submitted = nextSubmitted;
    lease = nextLease;
    break;
  }
  if (!submitted || !lease) {
    test.skip(true, "live Agent worker already claimed this Turn");
    return;
  }
  await appendEvents(page.request, conversationId, lease, submitted, "e2e-s406", [
    { sequence: 1, kind: "turn/start", payload: { status: "running", attempt_id: "e2e-s406" } },
    { sequence: 2, kind: "text.chunk", payload: { delta: "直播", attempt_id: "e2e-s406", step_id: "e2e-s406", content_index: 0 } },
  ]);

  await page.goto(`/products/${encodeURIComponent(productId)}?agent_session_id=${encodeURIComponent(sessionId)}`);
  await page.locator('[data-sidebar-tool="agent"]').click();
  await expect(page.locator("[data-agent-composer]")).toBeVisible({ timeout: 20_000 });
  await page.getByRole("button", { name: "放大至全局主控台" }).click();
  await expect(page.locator("#global-agent-dock-panel")).toBeVisible({ timeout: 20_000 });

  const sources = await page.evaluate((turnId) => {
    const all = window.__agentEventSources ?? [];
    return all.filter((url) => url.includes(`/turns/${turnId}/events`) && !url.includes("/page"));
  }, submitted.turn.id);
  const uniquePaths = new Set(sources.map((url) => url.split("?")[0]));
  expect(uniquePaths.size, sources.join("\n")).toBe(1);
});
