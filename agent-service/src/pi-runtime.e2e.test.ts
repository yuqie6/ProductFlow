import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import type { Scope, TurnState } from "./contracts.js";
import { PiRuntimeManager } from "./pi-runtime.js";
import type { SkillCatalog } from "./skills.js";
import { TurnStore } from "./store.js";

const scope: Scope = {
  schema_version: 1,
  scope_type: "global",
  conversation_id: "11111111-1111-4111-8111-111111111111",
  task_id: null,
  task_goal: null,
  product_id: null,
  workflow_draft_id: null,
  run_id: "44444444-4444-4444-8444-444444444444",
  system_prompt: "ProductFlow",
  draft_schema: { type: "object" },
  workflow_draft_schema: { type: "object" },
  current_draft_version: 1,
};

const config = {
  listenAddress: "127.0.0.1:0",
  dataRoot: "/tmp/productflow-pi-runtime-e2e",
  productFlowBaseURL: "http://127.0.0.1:29282",
  internalToken: "0123456789abcdef0123456789abcdef",
  requestTimeoutMS: 5_000,
  providerRequestTimeoutMS: 5_000,
  eventPollIntervalMS: 10,
  heartbeatIntervalMS: 100,
  maxBodyBytes: 1024 * 1024,
  maxIterations: 4,
  modelContextWindow: 128_000,
  autoCompactTokenLimit: 96_000,
  maxConcurrentTurns: 1,
  providerAPIKey: "fake-provider-key",
  providerBaseURL: null,
  providerModel: null,
  providerReasoningEffort: null,
  providerReasoningSummary: null,
  providerTextVerbosity: null,
  providerServiceTier: null,
};

describe("Pi runtime fake provider E2E", () => {
  it("runs a Pi session through the durable lease, event, and checkpoint boundaries", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-e2e-"));
    const provider = await createFakeResponsesServer();
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    const events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }> = [];
    const releasedPhases: string[] = [];
    let claimCount = 0;
    let manager: PiRuntimeManager | undefined;

    try {
      const productFlow = createFakeProductFlow(provider.baseURL, checkpoints, events, releasedPhases, () => {
        claimCount += 1;
        return claimCount;
      });
      const store = new TurnStore(root);
      await store.init();
      const skills = {
        root: "/tmp/fake-productflow-skills",
        hash: "fake-skill-catalog",
        names: [],
        prompt: "",
        load: async () => "",
      } satisfies SkillCatalog;
      manager = new PiRuntimeManager({ ...config, dataRoot: root }, store, productFlow, skills);
      store.setEventPublisher((eventScope, event) => manager!.publishDurableEvent(eventScope, event));

      const started = await manager.start({
        lookup: { conversationID: scope.conversation_id },
        input: {
          input_text: "请给出一句确认结果",
          asset_ids: [],
          idempotency_key: "fake-provider-e2e-1",
          page_context: null,
        },
      });
      const terminal = await waitForTerminal(store, scope.run_id, started.turn_id);

      expect(terminal).toMatchObject({
        status: "succeeded",
        output: "fake provider response",
        execution_attempt: 1,
        execution_fencing_token: 1,
      });
      expect(provider.requestCount).toBe(1);
      expect(provider.requestPaths).toEqual(["/v1/responses"]);
      expect(claimCount).toBe(1);
      expect(releasedPhases).toEqual(["terminal"]);
      expect(checkpoints.map((checkpoint) => checkpoint.kind)).toEqual([
        "before_model_request",
        "terminal",
      ]);
      expect(events.map((event) => event.kind)).toEqual([
        "turn.queued",
        "turn.started",
        "tool.step",
        "tool.step",
        "text.delta",
        "turn.succeeded",
      ]);
      expect(events.map((event) => event.sequence)).toEqual([1, 2, 3, 4, 5, 6]);
      expect(events[2]?.payload).toMatchObject({
        kind: "inject_context",
        tool_name: "productflow_context_injection",
        status: "running",
      });
      expect(events[3]?.payload).toMatchObject({
        kind: "inject_context",
        tool_name: "productflow_context_injection",
        status: "succeeded",
      });
      expect((await readdir(store.sessionDir(scope.run_id))).length).toBeGreaterThan(0);
      expect(provider.requestBody).toMatchObject({
        model: "fake-model",
        stream: true,
      });
      const toolNames = (provider.requestBody?.tools as Array<{ name?: string }> | undefined)?.map((tool) => tool.name) ?? [];
      expect(toolNames).toContain("ask_user");
      expect(toolNames).not.toContain("bash");
      expect(toolNames).not.toContain("read");
    } finally {
      await manager?.close();
      await provider.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("does not replay a provider request after the response stream is lost", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-provider-loss-e2e-"));
    const provider = await createFakeResponsesServer("disconnect");
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    const events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }> = [];
    const releasedPhases: string[] = [];
    let manager: PiRuntimeManager | undefined;

    try {
      const productFlow = createFakeProductFlow(provider.baseURL, checkpoints, events, releasedPhases, () => 1);
      const store = new TurnStore(root);
      await store.init();
      const skills = {
        root: "/tmp/fake-productflow-skills",
        hash: "fake-skill-catalog",
        names: [],
        prompt: "",
        load: async () => "",
      } satisfies SkillCatalog;
      manager = new PiRuntimeManager({ ...config, dataRoot: root }, store, productFlow, skills);
      store.setEventPublisher((eventScope, event) => manager!.publishDurableEvent(eventScope, event));

      const started = await manager.start({
        lookup: { conversationID: scope.conversation_id },
        input: {
          input_text: "验证 provider 响应丢失后的收敛",
          asset_ids: [],
          idempotency_key: "fake-provider-loss-e2e-1",
          page_context: null,
        },
      });
      const terminal = await waitForTerminal(store, scope.run_id, started.turn_id);

      expect(terminal.status).toBe("failed");
      expect(terminal.error).toBeTruthy();
      expect(provider.requestCount).toBe(1);
      expect(checkpoints.map((checkpoint) => checkpoint.kind)).toEqual([
        "before_model_request",
        "terminal",
      ]);
      expect(checkpoints[1].payload).toMatchObject({ status: "failed" });
      expect(events.map((event) => event.kind)).toEqual(
        expect.arrayContaining(["turn.queued", "turn.started", "turn.failed"]),
      );
      expect(releasedPhases).toEqual(["terminal"]);
    } finally {
      await manager?.close();
      await provider.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("fails once when the provider connection resets before response headers", async () => {
    const result = await runProviderFailureScenario("reset");

    expect(result.terminal.status).toBe("failed");
    expect(result.terminal.error).toBeTruthy();
    expect(result.requestCount).toBe(1);
    expect(result.checkpoints.map((checkpoint) => checkpoint.kind)).toEqual([
      "before_model_request",
      "terminal",
    ]);
    expect(result.checkpoints[1].payload).toMatchObject({ status: "failed" });
    expect(result.events.map((event) => event.kind)).toEqual(
      expect.arrayContaining(["turn.queued", "turn.started", "turn.failed"]),
    );
    expect(result.releasedPhases).toEqual(["terminal"]);
  });

  it("bounds a delayed provider response without replaying the model request", async () => {
    const result = await runProviderFailureScenario("timeout", 50);

    expect(result.terminal.status).toBe("failed");
    expect(result.terminal.error).toBeTruthy();
    expect(result.requestCount).toBe(1);
    expect(result.checkpoints.map((checkpoint) => checkpoint.kind)).toEqual([
      "before_model_request",
      "terminal",
    ]);
    expect(result.checkpoints[1].payload).toMatchObject({ status: "failed" });
    expect(result.events.map((event) => event.kind)).toEqual(
      expect.arrayContaining(["turn.queued", "turn.started", "turn.failed"]),
    );
    expect(result.releasedPhases).toEqual(["terminal"]);
  });

  it("runs a ProductFlow side-effect tool through intent and result checkpoints", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-tool-e2e-"));
    const provider = await createFakeResponsesServer("workspace");
    const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
    const events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }> = [];
    const releasedPhases: string[] = [];
    let claimCount = 0;
    let manager: PiRuntimeManager | undefined;
    let createdWorkspace: { name: string; idempotencyKey: string } | undefined;

    try {
      const productFlow = createFakeProductFlow(provider.baseURL, checkpoints, events, releasedPhases, () => {
        claimCount += 1;
        return claimCount;
      });
      productFlow.createProductWorkspace = async (
        _conversationID: string,
        name: string,
        idempotencyKey: string,
      ) => {
        createdWorkspace = { name, idempotencyKey };
        return { product_id: "product-fake-provider-e2e", name };
      };
      const store = new TurnStore(root);
      await store.init();
      const skills = {
        root: "/tmp/fake-productflow-skills",
        hash: "fake-skill-catalog",
        names: [],
        prompt: "",
        load: async () => "",
      } satisfies SkillCatalog;
      manager = new PiRuntimeManager({ ...config, dataRoot: root }, store, productFlow, skills);
      store.setEventPublisher((eventScope, event) => manager!.publishDurableEvent(eventScope, event));

      const started = await manager.start({
        lookup: { conversationID: scope.conversation_id },
        input: {
          input_text: "创建一个商品工作区并确认",
          asset_ids: [],
          idempotency_key: "fake-provider-tool-e2e-1",
          page_context: null,
        },
      });
      const terminal = await waitForTerminal(store, scope.run_id, started.turn_id);

      expect(terminal).toMatchObject({ status: "succeeded", output: "fake provider response" });
      expect(provider.requestCount).toBe(2);
      expect(provider.requestPaths).toEqual(["/v1/responses", "/v1/responses"]);
      expect(createdWorkspace).toMatchObject({ name: "演示商品" });
      expect(createdWorkspace?.idempotencyKey).toBe(`pi:${scope.run_id}:${started.turn_id}:call-fake|fc-fake`);
      expect(checkpoints.map((checkpoint) => checkpoint.kind)).toEqual([
        "before_model_request",
        "tool_effect_intent",
        "tool_effect_result",
        "before_model_request",
        "terminal",
      ]);
      const modelCheckpoints = checkpoints.filter((checkpoint) => checkpoint.kind === "before_model_request");
      expect(modelCheckpoints).toHaveLength(2);
      expect(modelCheckpoints[0].payload).toMatchObject({
        attempt: 1,
        fencing_token: 1,
        model_request_sequence: 1,
      });
      expect(modelCheckpoints[1].payload).toMatchObject({
        attempt: 1,
        fencing_token: 1,
        model_request_sequence: 2,
      });
      expect(modelCheckpoints[0].payload.model_request_id).not.toBe(modelCheckpoints[1].payload.model_request_id);
      expect(checkpoints[1].payload).toMatchObject({
        tool_name: "create_product_workspace_v1",
      });
      expect(checkpoints[2].payload).toMatchObject({
        tool_name: "create_product_workspace_v1",
        result: "applied",
      });
      expect(events.map((event) => event.kind)).toEqual(
        expect.arrayContaining(["turn.queued", "turn.started", "tool.step", "text.delta", "turn.succeeded"]),
      );
      expect(events.map((event) => event.sequence)).toEqual(events.map((_event, index) => index + 1));
    } finally {
      await manager?.close();
      await provider.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("loads the persisted Pi session context in a new runtime process", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-session-restart-e2e-"));
    const provider = await createFakeResponsesServer();
    let manager: PiRuntimeManager | undefined;

    try {
      const productFlow = createFakeProductFlow(provider.baseURL, [], [], [], () => 1);
      const skills = {
        root: "/tmp/fake-productflow-skills",
        hash: "fake-skill-catalog",
        names: [],
        prompt: "",
        load: async () => "",
      } satisfies SkillCatalog;
      const firstStore = new TurnStore(root);
      await firstStore.init();
      manager = new PiRuntimeManager({ ...config, dataRoot: root }, firstStore, productFlow, skills);
      firstStore.setEventPublisher((eventScope, event) => manager!.publishDurableEvent(eventScope, event));

      const first = await manager.start({
        lookup: { conversationID: scope.conversation_id },
        input: {
          input_text: "这是第一轮会话上下文，请记住这一句",
          asset_ids: [],
          idempotency_key: "session-restart-first-turn",
          page_context: null,
        },
      });
      await waitForTerminal(firstStore, scope.run_id, first.turn_id);
      await manager.close();
      manager = undefined;

      const restartedStore = new TurnStore(root);
      await restartedStore.init();
      manager = new PiRuntimeManager({ ...config, dataRoot: root }, restartedStore, productFlow, skills);
      restartedStore.setEventPublisher((eventScope, event) => manager!.publishDurableEvent(eventScope, event));
      const second = await manager.start({
        lookup: { conversationID: scope.conversation_id },
        input: {
          input_text: "这是重启后的第二轮，请继续当前会话",
          asset_ids: [],
          idempotency_key: "session-restart-second-turn",
          page_context: null,
        },
      });
      const terminal = await waitForTerminal(restartedStore, scope.run_id, second.turn_id);

      expect(terminal.status).toBe("succeeded");
      expect(provider.requestCount).toBe(2);
      expect(provider.requestBodies).toHaveLength(2);
      const secondRequest = JSON.stringify(provider.requestBodies[1]);
      expect(secondRequest).toContain("这是第一轮会话上下文，请记住这一句");
      expect(secondRequest).toContain("fake provider response");
    } finally {
      await manager?.close();
      await provider.close();
      await rm(root, { recursive: true, force: true });
    }
  });
});

function createFakeProductFlow(
  providerBaseURL: string,
  checkpoints: Array<{ kind: string; payload: Record<string, unknown> }>,
  events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }>,
  releasedPhases: string[],
  nextClaim: () => number,
): ConstructorParameters<typeof PiRuntimeManager>[2] {
  let claimedTurnID = "";
  return {
    conversationContract: async () => ({
      schema_version: 1,
      scope_type: "global",
      conversation_id: scope.conversation_id,
      task_id: null,
      task_goal: null,
      product_id: null,
      workflow_draft_id: null,
      harness_run_id: scope.run_id,
      current_draft_version: 1,
      system_prompt: scope.system_prompt,
      draft_kind: "global",
      draft_schema: scope.draft_schema,
      workflow_draft_schema: scope.workflow_draft_schema,
      tool_contract_version: 9,
    }),
    runtimeContext: async () => ({
      schema_version: 1,
      session_id: "session-fake-provider-e2e",
      conversation_id: scope.conversation_id,
      task_id: null,
      session_summary: null,
      task_summary: null,
    }),
    providerConfig: async () => ({
      schema_version: 1,
      provider_kind: "openai",
      api_key: "fake-provider-key",
      base_url: providerBaseURL,
      model: "fake-model",
      reasoning_effort: null,
      reasoning_summary: null,
      text_verbosity: null,
      service_tier: null,
    }),
    claimTurnExecution: async (_conversationID: string, args: { harness_turn_id: string }) => {
      claimedTurnID = args.harness_turn_id;
      return {
        execution_id: "execution-fake-provider-e2e",
        projection_id: "projection-fake-provider-e2e",
        harness_turn_id: claimedTurnID,
        owner_id: "agent-fake-provider-e2e",
        lease_token: "lease-fake-provider-e2e",
        attempt: nextClaim(),
        fencing_token: 1,
        phase: "claimed" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
    },
    heartbeatTurnExecution: async (
      _conversationID: string,
      _executionID: string,
      args: { phase: "claimed" | "model" | "tool" | "waiting_input" | "external_job" | "terminal" },
    ) => ({
      execution_id: "execution-fake-provider-e2e",
      projection_id: "projection-fake-provider-e2e",
      harness_turn_id: claimedTurnID,
      owner_id: "agent-fake-provider-e2e",
      lease_token: "lease-fake-provider-e2e",
      attempt: 1,
      fencing_token: 1,
      phase: args.phase,
      lease_expires_at: "2099-01-01T00:00:00.000Z",
    }),
    appendTurnCheckpoint: async (
      _conversationID: string,
      _executionID: string,
      args: { kind: string; payload: Record<string, unknown> },
    ) => {
      checkpoints.push(args);
      return {
        id: `checkpoint-${checkpoints.length}`,
        projection_id: "projection-fake-provider-e2e",
        execution_id: "execution-fake-provider-e2e",
        attempt: 1,
        fencing_token: 1,
        sequence: checkpoints.length,
        kind: args.kind,
        created_at: "2026-08-20T00:00:00.000Z",
      };
    },
    appendTurnEvent: async (
      _conversationID: string,
      _executionID: string,
      args: { sequence: number; kind: string; payload: Record<string, unknown> },
    ) => {
      events.push(args);
      return {
        id: `event-${args.sequence}`,
        projection_id: "projection-fake-provider-e2e",
        execution_id: "execution-fake-provider-e2e",
        sequence: args.sequence,
        schema_version: 1 as const,
        kind: args.kind,
        created_at: "2026-08-20T00:00:00.000Z",
      };
    },
    createProductWorkspace: async (_conversationID: string, name: string, idempotencyKey: string) => ({
      product_id: "product-fake-provider-e2e",
      name,
      idempotency_key: idempotencyKey,
    }),
    releaseTurnExecution: async (
      _conversationID: string,
      _executionID: string,
      args: { phase: "claimed" | "model" | "tool" | "waiting_input" | "external_job" | "terminal" },
    ) => {
      releasedPhases.push(args.phase);
      return { released: true };
    },
  } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
}

async function runProviderFailureScenario(
  mode: FakeProviderMode,
  providerRequestTimeoutMS = config.providerRequestTimeoutMS,
): Promise<{
  terminal: TurnState;
  requestCount: number;
  checkpoints: Array<{ kind: string; payload: Record<string, unknown> }>;
  events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }>;
  releasedPhases: string[];
}> {
  const root = await mkdtemp(join(tmpdir(), `productflow-pi-runtime-provider-${mode}-e2e-`));
  const provider = await createFakeResponsesServer(mode);
  const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
  const events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }> = [];
  const releasedPhases: string[] = [];
  let manager: PiRuntimeManager | undefined;

  try {
    const productFlow = createFakeProductFlow(provider.baseURL, checkpoints, events, releasedPhases, () => 1);
    const store = new TurnStore(root);
    await store.init();
    const skills = {
      root: "/tmp/fake-productflow-skills",
      hash: "fake-skill-catalog",
      names: [],
      prompt: "",
      load: async () => "",
    } satisfies SkillCatalog;
    manager = new PiRuntimeManager(
      { ...config, dataRoot: root, providerRequestTimeoutMS },
      store,
      productFlow,
      skills,
    );
    store.setEventPublisher((eventScope, event) => manager!.publishDurableEvent(eventScope, event));

    const started = await manager.start({
      lookup: { conversationID: scope.conversation_id },
      input: {
        input_text: "验证 provider 网络故障后的收敛",
        asset_ids: [],
        idempotency_key: `fake-provider-${mode}-e2e-1`,
        page_context: null,
      },
    });
    const terminal = await waitForTerminal(store, scope.run_id, started.turn_id);
    return {
      terminal,
      requestCount: provider.requestCount,
      checkpoints: [...checkpoints],
      events: [...events],
      releasedPhases: [...releasedPhases],
    };
  } finally {
    await manager?.close();
    await provider.close();
    await rm(root, { recursive: true, force: true });
  }
}

async function waitForTerminal(store: TurnStore, runID: string, turnID: string): Promise<TurnState> {
  for (let attempt = 0; attempt < 300; attempt += 1) {
    const state = await store.getState(runID, turnID);
    if (["awaiting_confirmation", "succeeded", "failed", "canceled", "unknown"].includes(state.status)) return state;
    await new Promise<void>((resolve) => setTimeout(resolve, 10));
  }
  throw new Error("fake provider E2E Turn did not reach a terminal state");
}

type FakeProviderMode = "text" | "workspace" | "disconnect" | "reset" | "timeout";

async function createFakeResponsesServer(mode: FakeProviderMode = "text"): Promise<{
  baseURL: string;
  requestBody?: Record<string, unknown>;
  requestBodies: Record<string, unknown>[];
  requestCount: number;
  requestPaths: string[];
  close: () => Promise<void>;
}> {
  let requestBody: Record<string, unknown> | undefined;
  const requestBodies: Record<string, unknown>[] = [];
  let requestCount = 0;
  const requestPaths: string[] = [];
  const pendingTimers = new Set<ReturnType<typeof setTimeout>>();
  const server = createServer((request, response) => {
    requestPaths.push(request.url ?? "");
    const chunks: Buffer[] = [];
    request.on("data", (chunk: Buffer) => chunks.push(chunk));
    request.on("end", () => {
      requestCount += 1;
      requestBody = JSON.parse(Buffer.concat(chunks).toString("utf8")) as Record<string, unknown>;
      requestBodies.push(requestBody);
      if (mode !== "reset" && mode !== "timeout") {
        response.writeHead(200, {
          "content-type": "text/event-stream",
          connection: "keep-alive",
          "cache-control": "no-cache",
        });
      }
      if (mode === "workspace" && requestCount === 1) writeWorkspaceToolResponse(response);
      else if (mode === "disconnect") writeTruncatedTextResponse(response, requestCount);
      else if (mode === "reset") response.destroy();
      else if (mode === "timeout") {
        const timer = setTimeout(() => {
          pendingTimers.delete(timer);
          response.destroy();
        }, 250);
        pendingTimers.add(timer);
        response.once("close", () => {
          clearTimeout(timer);
          pendingTimers.delete(timer);
        });
      }
      else writeTextResponse(response, requestCount);
      if (mode !== "disconnect" && mode !== "reset" && mode !== "timeout") response.end();
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
  const address = server.address() as AddressInfo;
  return {
    baseURL: `http://127.0.0.1:${address.port}/v1`,
    get requestBody() {
      return requestBody;
    },
    requestBodies,
    get requestCount() {
      return requestCount;
    },
    requestPaths,
    close: async () => {
      for (const timer of pendingTimers) clearTimeout(timer);
      pendingTimers.clear();
      server.closeAllConnections();
      await closeServer(server);
    },
  };
}

function writeTextResponse(response: import("node:http").ServerResponse, responseNumber: number): void {
  const message = {
    type: "message",
    id: `msg-fake-provider-e2e-${responseNumber}`,
    role: "assistant",
    status: "completed",
    phase: "final_answer",
    content: [{ type: "output_text", text: "fake provider response", annotations: [] }],
  };
  const responseBody = {
    id: `resp-fake-provider-e2e-${responseNumber}`,
    object: "response",
    status: "completed",
    output: [message],
    usage: { input_tokens: 1, output_tokens: 3, total_tokens: 4 },
  };
  writeSSE(response, {
    type: "response.created",
    response: { id: responseBody.id, object: "response", status: "in_progress", output: [] },
  });
  writeSSE(response, {
    type: "response.output_item.added",
    output_index: 0,
    item: {
      type: "message",
      id: message.id,
      role: "assistant",
      status: "in_progress",
      content: [],
    },
  });
  writeSSE(response, { type: "response.output_text.delta", output_index: 0, delta: "fake provider response" });
  writeSSE(response, { type: "response.output_text.done", output_index: 0, text: "fake provider response" });
  writeSSE(response, { type: "response.output_item.done", output_index: 0, item: message });
  writeSSE(response, { type: "response.completed", response: responseBody });
}

function writeTruncatedTextResponse(response: import("node:http").ServerResponse, responseNumber: number): void {
  writeSSE(response, {
    type: "response.created",
    response: {
      id: `resp-fake-provider-disconnect-${responseNumber}`,
      object: "response",
      status: "in_progress",
      output: [],
    },
  });
  writeSSE(response, {
    type: "response.output_text.delta",
    output_index: 0,
    delta: "partial provider response",
  });
  response.destroy();
}

function writeWorkspaceToolResponse(response: import("node:http").ServerResponse): void {
  const toolCall = {
    type: "function_call",
    id: "fc-fake",
    call_id: "call-fake",
    name: "create_product_workspace_v1",
    arguments: '{"name":"演示商品"}',
    status: "completed",
  };
  const responseBody = {
    id: "resp-fake-provider-tool-e2e-1",
    object: "response",
    status: "completed",
    output: [toolCall],
    usage: { input_tokens: 1, output_tokens: 3, total_tokens: 4 },
  };
  writeSSE(response, {
    type: "response.created",
    response: { id: responseBody.id, object: "response", status: "in_progress", output: [] },
  });
  writeSSE(response, {
    type: "response.output_item.added",
    output_index: 0,
    item: { ...toolCall, arguments: "" },
  });
  writeSSE(response, {
    type: "response.function_call_arguments.delta",
    output_index: 0,
    delta: toolCall.arguments,
  });
  writeSSE(response, {
    type: "response.function_call_arguments.done",
    output_index: 0,
    arguments: toolCall.arguments,
  });
  writeSSE(response, { type: "response.output_item.done", output_index: 0, item: toolCall });
  writeSSE(response, { type: "response.completed", response: responseBody });
}

function writeSSE(response: import("node:http").ServerResponse, event: Record<string, unknown>): void {
  response.write(`event: ${String(event.type)}\ndata: ${JSON.stringify(event)}\n\n`);
}

async function closeServer(server: Server): Promise<void> {
  if (!server.listening) return;
  await new Promise<void>((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
}
