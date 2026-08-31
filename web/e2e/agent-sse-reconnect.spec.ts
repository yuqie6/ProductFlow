import { expect, test, type Page } from "@playwright/test";

import {
  assertLiveBrowserGraphEnabled,
  lockLocale,
  loginAsAdmin,
  requiredEnv,
} from "./liveGraph";

interface SessionPayload {
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

interface BrowserSSEEvent {
  type: string;
  lastEventId: string;
  data: string;
}

async function waitForNamedEvent(page: Page, url: string, kind: string, timeoutMs: number): Promise<BrowserSSEEvent> {
  return page.evaluate(
    ({ eventUrl, eventKind, timeoutMs: waitMs }) => {
      return new Promise<BrowserSSEEvent>((resolve, reject) => {
        const source = new EventSource(eventUrl, { withCredentials: true });
        const timer = window.setTimeout(() => {
          source.close();
          reject(new Error(`timed out waiting for ${eventKind} from ${eventUrl}`));
        }, waitMs);
        const onEvent = (event: Event) => {
          const message = event as MessageEvent<string>;
          window.clearTimeout(timer);
          source.close();
          resolve({
            type: message.type,
            lastEventId: message.lastEventId,
            data: typeof message.data === "string" ? message.data : "",
          });
        };
        source.addEventListener(eventKind, onEvent);
        source.onerror = () => {
          // EventSource retries itself; the timeout above is the gate.
        };
      });
    },
    { eventUrl: url, eventKind: kind, timeoutMs },
  );
}

test("browser EventSource reconnects Agent SSE from the persisted cursor", async ({ page }) => {
  assertLiveBrowserGraphEnabled();
  const internalToken = requiredEnv("AGENT_SERVICE_INTERNAL_TOKEN");
  await lockLocale(page);
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  await page.goto("/products");
  await expect(page).toHaveURL(/\/products/);

  const sessionResponse = await page.request.post("/api/v2/agent-sessions", {
    data: { title: `e2e-sse-reconnect ${Date.now()}` },
  });
  expect(sessionResponse.ok(), await sessionResponse.text()).toBeTruthy();
  const session = (await sessionResponse.json()) as SessionPayload;
  const conversationId = session.conversations[0]?.conversation_id;
  expect(conversationId).toBeTruthy();

  const idempotencyKey = `e2e-sse-${Date.now()}`;
  const turnResponse = await page.request.post(
    `/api/v2/agent-conversations/${encodeURIComponent(conversationId)}/turns`,
    { data: { input_text: "验证浏览器 SSE cursor 重连", idempotency_key: idempotencyKey } },
  );
  expect(turnResponse.ok(), await turnResponse.text()).toBeTruthy();
  const submitted = (await turnResponse.json()) as SubmitTurnPayload;
  expect(submitted.turn.id).toBeTruthy();
  const harnessTurnId = submitted.turn.harness_turn_id ?? crypto.randomUUID();

  const eventsURL = `/api/v2/agent-conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(submitted.turn.id)}/events`;
  const auth = { Authorization: `Bearer ${internalToken}` };
  const claim = await page.request.post(
    `/api/internal/v1/agent-conversations/${encodeURIComponent(conversationId)}/turn-executions/claim`,
    {
      headers: auth,
      data: {
        idempotency_key: idempotencyKey,
        harness_turn_id: harnessTurnId,
        owner_id: "e2e-sse-reconnect",
      },
    },
  );

  if (claim.ok()) {
    const lease = (await claim.json()) as LeasePayload;
    const append = async (sequence: number, kind: string, payload: Record<string, unknown>) => {
      const posted = await page.request.post(
        `/api/internal/v1/agent-conversations/${encodeURIComponent(conversationId)}/turn-executions/${encodeURIComponent(lease.execution_id)}/events/batch`,
        {
          headers: auth,
          data: {
            owner_id: "e2e-sse-reconnect",
            lease_token: lease.lease_token,
            events: [{
              sequence,
              schema_version: 1,
              run_id: submitted.turn.harness_run_id,
              turn_id: harnessTurnId,
              kind,
              payload,
              created_at: new Date().toISOString(),
            }],
          },
        },
      );
      expect(posted.ok(), await posted.text()).toBeTruthy();
    };

    await append(1, "turn/start", { status: "running", attempt_id: "e2e-attempt" });
    const first = await waitForNamedEvent(page, eventsURL, "turn.started", 15_000);
    expect(first.lastEventId).toBe("1");

    await append(2, "text.chunk", {
      delta: "cursor replay",
      step_id: "e2e-step",
      attempt_id: "e2e-attempt",
      content_index: 0,
    });
    const replay = await waitForNamedEvent(page, `${eventsURL}?after=1`, "item.delta", 15_000);
    expect(replay.lastEventId).toBe("2");

    await append(3, "turn/end", { reason: "completed", status: "succeeded", output: "cursor replay" });
    const completed = await waitForNamedEvent(page, `${eventsURL}?after=2`, "turn.completed", 15_000);
    expect(completed.lastEventId).toBe("3");
    const released = await page.request.post(
      `/api/internal/v1/agent-conversations/${encodeURIComponent(conversationId)}/turn-executions/${encodeURIComponent(lease.execution_id)}/release`,
      {
        headers: auth,
        data: {
          owner_id: "e2e-sse-reconnect",
          lease_token: lease.lease_token,
          phase: "terminal",
        },
      },
    );
    expect(released.ok(), await released.text()).toBeTruthy();
    return;
  }

  const first = await waitForNamedEvent(page, eventsURL, "turn.started", 60_000);
  expect(Number(first.lastEventId)).toBeGreaterThan(0);
  const after = first.lastEventId;
  const replay = await waitForNamedEvent(page, `${eventsURL}?after=${encodeURIComponent(after)}`, "item.delta", 60_000);
  expect(Number(replay.lastEventId)).toBeGreaterThan(Number(after));
});
