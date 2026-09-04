import { expect, test } from "@playwright/test";

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

test("Chromium ConversationRuntime repairs a durable event gap within 5s", async ({ page }) => {
  assertLiveBrowserGraphEnabled();
  const internalToken = requiredEnv("AGENT_SERVICE_INTERNAL_TOKEN");
  await lockLocale(page);
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  await page.goto("/products");
  await expect(page).toHaveURL(/\/products/);

  const sessionResponse = await page.request.post("/api/v2/agent-sessions", {
    data: { title: `e2e-runtime-gap ${Date.now()}` },
  });
  expect(sessionResponse.ok(), await sessionResponse.text()).toBeTruthy();
  const session = (await sessionResponse.json()) as SessionPayload;
  const conversationId = session.conversations[0]?.conversation_id;
  expect(conversationId).toBeTruthy();

  const idempotencyKey = `e2e-runtime-gap-${Date.now()}`;
  const turnResponse = await page.request.post(
    `/api/v2/agent-conversations/${encodeURIComponent(conversationId)}/turns`,
    { data: { input_text: "验证 ConversationRuntime 补洞", idempotency_key: idempotencyKey } },
  );
  expect(turnResponse.ok(), await turnResponse.text()).toBeTruthy();
  const submitted = (await turnResponse.json()) as SubmitTurnPayload;
  const harnessTurnId = submitted.turn.harness_turn_id;
  expect(harnessTurnId).toBeTruthy();

  const auth = { Authorization: `Bearer ${internalToken}` };
  const claim = await page.request.post(
    `/api/internal/v1/agent-conversations/${encodeURIComponent(conversationId)}/turn-executions/claim`,
    {
      headers: auth,
      data: {
        idempotency_key: idempotencyKey,
        harness_turn_id: harnessTurnId,
        owner_id: "e2e-runtime-gap",
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
            owner_id: "e2e-runtime-gap",
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
    await append(1, "turn/start", { status: "running", attempt_id: "e2e-gap" });
    await append(2, "text.chunk", {
      delta: "补齐缺口",
      step_id: "e2e-step",
      attempt_id: "e2e-gap",
      content_index: 0,
    });
    await append(3, "turn/end", { reason: "completed", status: "succeeded", output: "补齐缺口" });
    await page.request.post(
      `/api/internal/v1/agent-conversations/${encodeURIComponent(conversationId)}/turn-executions/${encodeURIComponent(lease.execution_id)}/release`,
      {
        headers: auth,
        data: { owner_id: "e2e-runtime-gap", lease_token: lease.lease_token, phase: "terminal" },
      },
    );
  } else {
    await expect.poll(async () => {
      const pageResponse = await page.request.get(
        `/api/v2/agent-conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(submitted.turn.id)}/events/page?after=0&limit=250`,
      );
      if (!pageResponse.ok()) return 0;
      const body = (await pageResponse.json()) as { items: Array<{ sequence: number }> };
      return body.items.some((item) => item.sequence === 2) ? 2 : 0;
    }, { timeout: 30_000 }).toBe(2);
  }

  const eventsURL = `/api/v2/agent-conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(submitted.turn.id)}/events`;
  const repairMs = await page.evaluate(async ({ eventsURL: url, runId, turnId }) => {
    const mod = await import(
      // @ts-expect-error Vite serves this workbench module to Chromium.
      "/src/pages/workbench/agent/conversation/runtime.ts"
    ) as {
      subscribeToConversationEvents: (input: {
        url: string;
        scope: { run_id: string; turn_id: string };
        createEventSource: (sourceURL: string) => EventSource;
        fetchEventPage: (pageURL: string, signal?: AbortSignal) => Promise<{
          items: unknown[];
          next_after: number;
          has_more: boolean;
          stream_state: "live" | "terminal";
        }>;
        onEvent: (event: { sequence: number }) => void;
      }) => () => void;
    };
    class FakeSource extends EventTarget {
      closed = false;
      constructor(readonly url: string) { super(); }
      close() { this.closed = true; }
      emit(type: string, data: string) {
        this.dispatchEvent(new MessageEvent(type, { data }));
      }
    }
    const envelope = (sequence: number, kind: string, payload: Record<string, unknown>) => JSON.stringify({
      schema_version: 1,
      run_id: runId,
      turn_id: turnId,
      sequence,
      created_at: "2026-08-31T00:00:00Z",
      kind,
      payload,
    });
    const sources: FakeSource[] = [];
    const received: number[] = [];
    const started = performance.now();
    await new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(() => reject(new Error("gap repair exceeded 5s")), 5_000);
      const close = mod.subscribeToConversationEvents({
        url,
        scope: { run_id: runId, turn_id: turnId },
        createEventSource: (sourceURL) => {
          const source = new FakeSource(sourceURL);
          sources.push(source);
          return source as unknown as EventSource;
        },
        fetchEventPage: async (pageURL, signal) => {
          const response = await fetch(pageURL, { credentials: "include", signal });
          if (!response.ok) throw new Error(`event page ${response.status}`);
          return response.json() as Promise<{
            items: unknown[];
            next_after: number;
            has_more: boolean;
            stream_state: "live" | "terminal";
          }>;
        },
        onEvent: (event) => {
          received.push(event.sequence);
          if (received.includes(1) && received.includes(2) && received.includes(3)) {
            window.clearTimeout(timer);
            close();
            resolve();
          }
        },
      });
      sources[0]?.emit("turn.started", envelope(1, "turn.started", { status: "running" }));
      sources[0]?.emit("turn.completed", envelope(3, "turn.completed", { reason: "completed" }));
    });
    return performance.now() - started;
  }, {
    eventsURL,
    runId: submitted.turn.harness_run_id,
    turnId: harnessTurnId as string,
  });

  expect(repairMs).toBeLessThan(5_000);
});

test("Chromium ConversationRuntime matrix covers generation, overflow, terminal gap, and parked approval", async ({ page }) => {
  assertLiveBrowserGraphEnabled();
  await lockLocale(page);
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  await page.goto("/products");

  const result = await page.evaluate(async () => {
    const mod = await import(
      // @ts-expect-error Vite serves this workbench module to Chromium.
      "/src/pages/workbench/agent/conversation/runtime.ts"
    ) as {
      subscribeToConversationEvents: (input: Record<string, unknown>) => () => void;
    };
    class FakeSource extends EventTarget {
      closed = false;
      constructor(readonly url = "") { super(); }
      close() { this.closed = true; }
      emit(type: string, data?: string) {
        this.dispatchEvent(data === undefined ? new Event(type) : new MessageEvent(type, { data }));
      }
    }
    const envelope = (sequence: number, kind = "turn.started", payload: Record<string, unknown> = { status: "running" }) =>
      JSON.stringify({
        schema_version: 1, run_id: "run-1", turn_id: "turn-1", sequence,
        created_at: "2026-08-31T00:00:00Z", kind, payload,
      });
    const sleep = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms));

    const sources: FakeSource[] = [];
    const staleReceived: number[] = [];
    const staleErrors: string[] = [];
    const closeGeneration = mod.subscribeToConversationEvents({
      url: "/events-generation",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: (url: string) => {
        const source = new FakeSource(url);
        sources.push(source);
        return source;
      },
      onEvent: (event: { sequence: number }) => staleReceived.push(event.sequence),
      onStreamError: (message: string | null) => {
        if (message) staleErrors.push(message);
      },
    });
    sources[0].emit("error");
    await sleep(300);
    const second = sources[1];
    sources[0].emit("open");
    sources[0].emit("error", JSON.stringify({ error: { message: "stale" } }));
    sources[0].emit("turn.started", envelope(1));
    second?.emit("turn.started", envelope(1));
    closeGeneration();

    const overflowErrors: string[] = [];
    const overflowSource = new FakeSource();
    const closeOverflow = mod.subscribeToConversationEvents({
      url: "/events-overflow",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => overflowSource,
      fetchEventPage: () => new Promise(() => undefined),
      onEvent: () => undefined,
      onProtocolError: (error: Error) => overflowErrors.push(error.message),
    });
    overflowSource.emit("turn.started", envelope(2, "turn.started", { status: "界".repeat(400_000) }));
    closeOverflow();

    const terminalErrors: string[] = [];
    const terminalSource = new FakeSource();
    const closeTerminal = mod.subscribeToConversationEvents({
      url: "/events-terminal-gap",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => terminalSource,
      fetchEventPage: async () => ({
        items: [JSON.parse(envelope(3))],
        next_after: 3,
        has_more: false,
        stream_state: "terminal",
      }),
      onEvent: () => undefined,
      onProtocolError: (error: Error) => terminalErrors.push(error.message),
    });
    terminalSource.emit("turn.started", envelope(1));
    terminalSource.emit("stream.complete");
    await sleep(50);
    closeTerminal();

    const parkedSource = new FakeSource();
    const closeParked = mod.subscribeToConversationEvents({
      url: "/events-parked",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => parkedSource,
      onEvent: () => undefined,
    });
    parkedSource.emit("turn.awaiting_confirmation", envelope(1, "turn.awaiting_confirmation", { reason: "awaiting_confirmation" }));
    const parkedClosed = parkedSource.closed;
    closeParked();

    return {
      staleReceived,
      staleIgnored: staleErrors.length === 0,
      overflow: overflowErrors.some((message) => message.includes("缓存超过限制")),
      overflowClosed: overflowSource.closed,
      terminalGap: terminalErrors.some((message) => message.includes("未返回 sequence 2")),
      parkedStayedOpen: parkedClosed === false,
    };
  });

  expect(result.staleReceived).toEqual([1]);
  expect(result.staleIgnored).toBe(true);
  expect(result.overflow).toBe(true);
  expect(result.overflowClosed).toBe(true);
  expect(result.terminalGap).toBe(true);
  expect(result.parkedStayedOpen).toBe(true);
});

test("Chromium ConversationRuntime closes the old EventSource while repairing a gap", async ({ page }) => {
  assertLiveBrowserGraphEnabled();
  await lockLocale(page);
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  await page.goto("/products");

  const result = await page.evaluate(async () => {
    const mod = await import(
      // @ts-expect-error Vite serves this workbench module to Chromium.
      "/src/pages/workbench/agent/conversation/runtime.ts"
    ) as {
      subscribeToConversationEvents: (input: {
        url: string;
        scope: { run_id: string; turn_id: string };
        createEventSource: (url: string) => EventSource;
        fetchEventPage: () => Promise<{
          items: Array<{
            schema_version: number;
            run_id: string;
            turn_id: string;
            sequence: number;
            created_at: string;
            kind: string;
            payload: Record<string, unknown>;
          }>;
          next_after: number;
          has_more: boolean;
          stream_state: "live";
        }>;
        onEvent: (event: { sequence: number }) => void;
      }) => () => void;
    };

    class FakeSource extends EventTarget {
      closed = false;
      constructor(readonly url: string) {
        super();
      }
      close() {
        this.closed = true;
      }
      emit(type: string, data: string) {
        this.dispatchEvent(new MessageEvent(type, { data }));
      }
    }

    const sources: FakeSource[] = [];
    const received: number[] = [];
    const event = (sequence: number) => JSON.stringify({
      schema_version: 1,
      run_id: "run-1",
      turn_id: "turn-1",
      sequence,
      created_at: "2026-08-31T00:00:00Z",
      kind: "turn.started",
      payload: { status: "running" },
    });
    const close = mod.subscribeToConversationEvents({
      url: "/events",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: (url) => {
        const source = new FakeSource(url);
        sources.push(source);
        return source as unknown as EventSource;
      },
      fetchEventPage: async () => ({
        items: [JSON.parse(event(2))],
        next_after: 2,
        has_more: false,
        stream_state: "live",
      }),
      onEvent: (value) => received.push(value.sequence),
    });
    sources[0].emit("turn.started", event(1));
    sources[0].emit("turn.started", event(3));
    await new Promise((resolve) => window.setTimeout(resolve, 50));
    close();
    return {
      received,
      firstClosed: sources[0]?.closed === true,
      secondURL: sources[1]?.url ?? "",
    };
  });

  expect(result.received).toEqual([1, 2, 3]);
  expect(result.firstClosed).toBe(true);
  expect(result.secondURL).toContain("after=3");
});

test("Chromium ConversationRuntime applies duplicate seq1 then seq2 without dropping the connection", async ({ page }) => {
  assertLiveBrowserGraphEnabled();
  await lockLocale(page);
  await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  await page.goto("/products");

  const result = await page.evaluate(async () => {
    const mod = await import(
      // @ts-expect-error Vite serves this workbench module to Chromium.
      "/src/pages/workbench/agent/conversation/runtime.ts"
    ) as {
      subscribeToConversationEvents: (input: {
        url: string;
        scope: { run_id: string; turn_id: string };
        createEventSource: (url: string) => EventSource;
        fetchEventPage: () => Promise<never>;
        onEvent: (event: { sequence: number }) => void;
        onConnectionState?: (state: string) => void;
        onStreamError?: (message: string | null) => void;
        onProtocolError?: (error: Error) => void;
      }) => () => void;
    };

    class FakeSource extends EventTarget {
      closed = false;
      constructor(readonly url: string) {
        super();
      }
      close() {
        this.closed = true;
      }
      emit(type: string, data?: string) {
        this.dispatchEvent(data === undefined ? new Event(type) : new MessageEvent(type, { data }));
      }
    }

    const sources: FakeSource[] = [];
    const received: number[] = [];
    const states: string[] = [];
    const streamErrors: Array<string | null> = [];
    const protocolErrors: string[] = [];
    const envelope = (sequence: number, kind: string, payload: Record<string, unknown>) => JSON.stringify({
      schema_version: 1,
      run_id: "run-1",
      turn_id: "turn-1",
      sequence,
      created_at: "2026-08-31T00:00:00Z",
      kind,
      payload,
    });
    const close = mod.subscribeToConversationEvents({
      url: "/events",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: (url) => {
        const source = new FakeSource(url);
        sources.push(source);
        return source as unknown as EventSource;
      },
      fetchEventPage: async () => {
        throw new Error("duplicate seq1 must not open a gap repair");
      },
      onEvent: (value) => received.push(value.sequence),
      onConnectionState: (state) => states.push(state),
      onStreamError: (message) => streamErrors.push(message),
      onProtocolError: (error) => protocolErrors.push(error.message),
    });
    sources[0].emit("open");
    sources[0].emit("turn.started", envelope(1, "turn.started", { status: "running" }));
    sources[0].emit("turn.started", envelope(1, "turn.started", { status: "running" }));
    sources[0].emit("turn.started", envelope(2, "turn.started", { status: "running" }));
    await new Promise((resolve) => window.setTimeout(resolve, 50));
    const stillOpen = sources[0]?.closed === false && sources.length === 1;
    close();
    return { received, states, streamErrors, protocolErrors, stillOpen, generationCount: sources.length };
  });

  expect(result.protocolErrors).toEqual([]);
  expect(result.streamErrors.filter((message) => message)).toEqual([]);
  expect(result.received).toEqual([1, 2]);
  expect(result.states).toContain("open");
  expect(result.states).not.toContain("reconnecting");
  expect(result.stillOpen).toBe(true);
  expect(result.generationCount).toBe(1);
});
