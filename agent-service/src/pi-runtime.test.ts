import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { ProductFlowError, TOOL_CONTRACT_VERSION, type Scope } from "./contracts.js";
import { PiRuntimeManager, toolStepDetailsForResult } from "./pi-runtime.js";
import { TurnStore } from "./store.js";

const scope: Scope = {
  schema_version: 1,
  scope_type: "product_workflow",
  conversation_id: "11111111-1111-4111-8111-111111111111",
  task_id: null,
  task_goal: null,
  product_id: "22222222-2222-4222-8222-222222222222",
  workflow_draft_id: "33333333-3333-4333-8333-333333333333",
  run_id: "44444444-4444-4444-8444-444444444444",
  system_prompt: "ProductFlow",
  draft_schema: { type: "object" },
  workflow_draft_schema: { type: "object" },
  current_draft_version: 1,
  has_live_graph: false,
};

const config = {
  listenAddress: "127.0.0.1:0",
  dataRoot: "/tmp/productflow-pi-runtime-test",
  productFlowBaseURL: "http://127.0.0.1:29282",
  internalToken: "0123456789abcdef0123456789abcdef",
  requestTimeoutMS: 1000,
  providerRequestTimeoutMS: 5_000,
  eventPollIntervalMS: 10,
  heartbeatIntervalMS: 100,
  maxBodyBytes: 1024 * 1024,
  maxIterations: 4,
  modelContextWindow: 128_000,
  autoCompactTokenLimit: 96_000,
  maxConcurrentTurns: 1,
  providerAPIKey: "",
  providerBaseURL: null,
  providerModel: null,
  providerReasoningEffort: null,
  providerReasoningSummary: null,
  providerTextVerbosity: null,
  providerServiceTier: null,
};

describe("PiRuntimeManager turn state", () => {
  it("does not guess whether an unclassified tool failure is retryable", () => {
    expect(toolStepDetailsForResult("get_product_workflow_context_v1", {}, true)).toEqual({
      phase: "tool_result",
      output_summary: "工具调用失败，详情见错误信息。",
    });
  });

  it("records the Node Catalog as an authoritative product context section", () => {
    expect(toolStepDetailsForResult("get_product_workflow_context_v1", {}, false)).toMatchObject({
      phase: "tool_result",
      context_sections: [
        "product_facts",
        "intake",
        "live_graph",
        "verified_reference_assets",
        "node_catalog",
      ],
      output_summary: expect.stringContaining("Node Catalog config_fields"),
    });
    expect(toolStepDetailsForResult("get_product_workflow_context_v1", {}, false)?.output_summary).toContain(
      "唯一来源",
    );
  });

  it("starts each Turn with a fresh checkpoint sequence", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        {} as ConstructorParameters<typeof PiRuntimeManager>[2],
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      const runtime = await (manager as unknown as {
        runtimeFor(input: Scope): Promise<unknown>;
      }).runtimeFor(scope);
      const internal = runtime as {
        checkpointSequence: number;
        resetTurnState(): void;
      };

      internal.checkpointSequence = 6;
      internal.resetTurnState();

      expect(internal.checkpointSequence).toBe(0);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("persists a queued cancellation through a durable lease without entering the model", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-cancel-"));
    try {
      const durableEvents: Array<{ sequence: number; kind: string }> = [];
      const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
      let released = false;
      const productFlow = {
        conversationContract: async () => ({
          schema_version: 1,
          scope_type: "product_workflow",
          conversation_id: scope.conversation_id,
          task_id: null,
          task_goal: null,
          product_id: scope.product_id,
          workflow_draft_id: scope.workflow_draft_id,
          harness_run_id: scope.run_id,
          current_draft_version: 1,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: { type: "object" },
          workflow_draft_schema: { type: "object" },
          tool_contract_version: TOOL_CONTRACT_VERSION,
        }),
        claimTurnExecution: async (_conversationID: string, args: { harness_turn_id: string }) => ({
          execution_id: "execution-cancel",
          projection_id: "projection-cancel",
          harness_turn_id: args.harness_turn_id,
          owner_id: "agent-cancel",
          lease_token: "lease-cancel",
          attempt: 1,
          fencing_token: 1,
          phase: "claimed" as const,
          lease_expires_at: "2099-01-01T00:00:00.000Z",
        }),
        heartbeatTurnExecution: async () => ({
          execution_id: "execution-cancel",
          projection_id: "projection-cancel",
          harness_turn_id: createdTurnID,
          owner_id: "agent-cancel",
          lease_token: "lease-cancel",
          attempt: 1,
          fencing_token: 1,
          phase: "terminal" as const,
          lease_expires_at: "2099-01-01T00:00:00.000Z",
        }),
        appendTurnEvent: async (_conversationID: string, _executionID: string, args: { sequence: number; kind: string }) => {
          durableEvents.push({ sequence: args.sequence, kind: args.kind });
          return {
            id: `event-${args.sequence}`,
            projection_id: "projection-cancel",
            execution_id: "execution-cancel",
            sequence: args.sequence,
            schema_version: 1 as const,
            kind: args.kind,
            created_at: "2026-08-20T00:00:00.000Z",
          };
        },
        appendTurnCheckpoint: async (
          _conversationID: string,
          _executionID: string,
          args: { kind: string; payload: Record<string, unknown> },
        ) => {
          checkpoints.push(args);
          return {
            id: "checkpoint-cancel",
            projection_id: "projection-cancel",
            execution_id: "execution-cancel",
            attempt: 1,
            fencing_token: 1,
            sequence: 1,
            kind: args.kind,
            created_at: "2026-08-20T00:00:00.000Z",
          };
        },
        releaseTurnExecution: async () => {
          released = true;
          return { released: true };
        },
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      let createdTurnID = "";
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      store.setEventPublisher((eventScope, event) => manager.publishDurableEvent(eventScope, event));
      await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      const created = await store.createTurn(scope, {
        input_text: "取消尚未开始的 Turn",
        asset_ids: [],
        idempotency_key: "cancel-before-model",
        page_context: null,
      });
      createdTurnID = created.state.turn_id;

      const canceled = await manager.cancel({ conversationID: scope.conversation_id }, createdTurnID);

      expect(canceled).toMatchObject({ status: "canceled", turn_id: createdTurnID });
      expect(durableEvents).toEqual([
        { sequence: 1, kind: "turn.queued" },
        { sequence: 2, kind: "turn.canceled" },
      ]);
      expect(checkpoints).toHaveLength(1);
      expect(checkpoints[0]).toMatchObject({ kind: "terminal", payload: { status: "canceled" } });
      expect(released).toBe(true);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("fails a Turn when ProductFlow rejects the pre-lease claim", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-claim-rejected-"));
    try {
      const productFlow = {
        conversationContract: async () => ({
          schema_version: 1,
          scope_type: "product_workflow",
          conversation_id: scope.conversation_id,
          task_id: null,
          task_goal: null,
          product_id: scope.product_id,
          workflow_draft_id: scope.workflow_draft_id,
          harness_run_id: scope.run_id,
          current_draft_version: 1,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: { type: "object" },
          workflow_draft_schema: { type: "object" },
          tool_contract_version: TOOL_CONTRACT_VERSION,
        }),
        claimTurnExecution: async () => {
          throw new ProductFlowError(404, "not_found", "Agent Turn projection does not exist");
        },
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );

      const started = await manager.start({
        lookup: { conversationID: scope.conversation_id },
        input: {
          input_text: "触发 claim 拒绝",
          asset_ids: [],
          idempotency_key: "claim-rejected",
          page_context: null,
        },
      });
      let state = started;
      for (let attempt = 0; attempt < 50 && state.status === "queued"; attempt += 1) {
        await new Promise<void>((resolve) => setTimeout(resolve, 5));
        state = await store.getState(scope.run_id, started.turn_id);
      }

      expect(state).toMatchObject({
        status: "failed",
        error: "Agent Turn projection does not exist",
      });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("does not abort the active Turn when a different queued Turn is canceled", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-cancel-other-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        {} as ConstructorParameters<typeof PiRuntimeManager>[2],
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      const runtime = await (manager as unknown as {
        runtimeFor(input: Scope): Promise<unknown>;
      }).runtimeFor(scope);
      const controller = new AbortController();
      const internal = runtime as {
        currentTurn: string;
        abortController: AbortController;
        cancel(turnID: string): void;
      };
      internal.currentTurn = "active-turn";
      internal.abortController = controller;

      internal.cancel("queued-turn");

      expect(controller.signal.aborted).toBe(false);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("does not publish another queued Turn with the active Turn lease", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-cross-turn-event-"));
    try {
      const published: string[] = [];
      const productFlow = {
        conversationContract: async () => ({
          schema_version: 1,
          scope_type: "product_workflow",
          conversation_id: scope.conversation_id,
          task_id: null,
          task_goal: null,
          product_id: scope.product_id,
          workflow_draft_id: scope.workflow_draft_id,
          harness_run_id: scope.run_id,
          current_draft_version: 1,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: { type: "object" },
          workflow_draft_schema: { type: "object" },
          tool_contract_version: TOOL_CONTRACT_VERSION,
        }),
        appendTurnEvent: async (_conversationID: string, _executionID: string, args: { turn_id: string }) => {
          published.push(args.turn_id);
          throw new Error("the active execution lease must not publish another Turn");
        },
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      store.setEventPublisher((eventScope, event) => manager.publishDurableEvent(eventScope, event));
      const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      const queued = await store.createTurn(scope, {
        input_text: "取消另一个 queued Turn",
        asset_ids: [],
        idempotency_key: "cross-turn-event-queued",
        page_context: null,
      });
      const internal = runtime as {
        activeTurnID: string;
        currentTurn: string;
        executionLease: { execution_id: string; owner_id: string; lease_token: string };
      };
      internal.activeTurnID = "active-turn";
      internal.currentTurn = "active-turn";
      internal.executionLease = {
        execution_id: "active-execution",
        owner_id: "active-owner",
        lease_token: "active-lease",
      };

      const canceled = await manager.cancel({ conversationID: scope.conversation_id }, queued.state.turn_id);

      expect(canceled.status).toBe("cancel_requested");
      expect(published).toEqual([]);
      expect((await store.events(scope.run_id, queued.state.turn_id, 0)).map((event) => event.kind)).toEqual([
        "turn.queued",
        "turn.cancel_requested",
      ]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("cancels a recovered question when its in-memory waiter is gone", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-question-recovery-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const productFlow = {
        conversationContract: async () => ({
          schema_version: 1,
          scope_type: "product_workflow",
          conversation_id: scope.conversation_id,
          task_id: null,
          task_goal: null,
          product_id: scope.product_id,
          workflow_draft_id: scope.workflow_draft_id,
          harness_run_id: scope.run_id,
          current_draft_version: 1,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: { type: "object" },
          workflow_draft_schema: { type: "object" },
          tool_contract_version: TOOL_CONTRACT_VERSION,
        }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      store.setEventPublisher((eventScope, event) => manager.publishDurableEvent(eventScope, event));
      const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      const created = await store.createTurn(scope, {
        input_text: "等待用户回答",
        asset_ids: [],
        idempotency_key: "question-recovery-cancel",
        page_context: null,
      });
      const question = {
        id: "question-recovered",
        header: "语言",
        question: "使用哪种语言？",
        options: [{ label: "中文" }, { label: "英文" }],
      };
      await store.updateState(scope.run_id, created.state.turn_id, {
        status: "requires_input",
        question,
      });
      await store.appendEvent(scope.run_id, created.state.turn_id, "turn.requires_input", {
        status: "requires_input",
        question,
      });

      const canceled = await manager.cancel({ conversationID: scope.conversation_id }, created.state.turn_id);

      expect(canceled).toMatchObject({
        status: "canceled",
        error: "",
      });
      expect((await store.events(scope.run_id, created.state.turn_id, 0)).map((event) => event.kind)).toEqual([
        "turn.queued",
        "turn.requires_input",
        "turn.canceled",
      ]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("downgrades a terminal Turn when its durable event cannot be published", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-event-failure-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const productFlow = {
        appendTurnEvent: async () => {
          throw new Error("event store unavailable");
        },
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      store.setEventPublisher((eventScope, event) => manager.publishDurableEvent(eventScope, event));
      const runtime = await (manager as unknown as {
        runtimeFor(input: Scope): Promise<unknown>;
      }).runtimeFor(scope);
      const created = await store.createTurn(scope, {
        input_text: "测试终态事件失败",
        asset_ids: [],
        idempotency_key: "durable-event-failure",
        page_context: null,
      });
      const internal = runtime as {
        executionLease: {
          execution_id: string;
          projection_id: string;
          harness_turn_id: string;
          owner_id: string;
          lease_token: string;
          attempt: number;
          fencing_token: number;
          phase: "claimed";
          lease_expires_at: string;
        };
        finishTurn(
          turnID: string,
          status: "succeeded",
          details: { output?: string },
        ): Promise<void>;
      };
      internal.executionLease = {
        execution_id: "execution-1",
        projection_id: "projection-1",
        harness_turn_id: created.state.turn_id,
        owner_id: "agent-1",
        lease_token: "lease-1",
        attempt: 1,
        fencing_token: 1,
        phase: "claimed",
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };

      await internal.finishTurn(created.state.turn_id, "succeeded", { output: "本地结果" });

      await expect(store.getState(scope.run_id, created.state.turn_id)).resolves.toMatchObject({
        status: "unknown",
        error: "event store unavailable",
      });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("downgrades a terminal Turn when its terminal checkpoint cannot be persisted", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-checkpoint-failure-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const productFlow = {
        appendTurnCheckpoint: async () => {
          throw new Error("terminal checkpoint unavailable");
        },
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      const runtime = await (manager as unknown as {
        runtimeFor(input: Scope): Promise<unknown>;
      }).runtimeFor(scope);
      const created = await store.createTurn(scope, {
        input_text: "测试终态 checkpoint 失败",
        asset_ids: [],
        idempotency_key: "terminal-checkpoint-failure",
        page_context: null,
      });
      const internal = runtime as {
        executionLease: {
          execution_id: string;
          projection_id: string;
          harness_turn_id: string;
          owner_id: string;
          lease_token: string;
          attempt: number;
          fencing_token: number;
          phase: "claimed";
          lease_expires_at: string;
        };
        finishTurn(
          turnID: string,
          status: "succeeded",
          details: { output?: string },
        ): Promise<void>;
      };
      internal.executionLease = {
        execution_id: "execution-checkpoint-failure",
        projection_id: "projection-checkpoint-failure",
        harness_turn_id: created.state.turn_id,
        owner_id: "agent-checkpoint-failure",
        lease_token: "lease-checkpoint-failure",
        attempt: 1,
        fencing_token: 1,
        phase: "claimed",
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };

      await internal.finishTurn(created.state.turn_id, "succeeded", { output: "本地结果" });

      await expect(store.getState(scope.run_id, created.state.turn_id)).resolves.toMatchObject({
        status: "unknown",
        error: "terminal checkpoint unavailable",
      });
      const events = await store.events(scope.run_id, created.state.turn_id, 0);
      expect(events.at(-1)?.kind).toBe("turn.unknown");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("aborts the active Pi session as soon as a side effect becomes unknown", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-unknown-effect-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        {} as ConstructorParameters<typeof PiRuntimeManager>[2],
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      const runtime = await (manager as unknown as {
        runtimeFor(input: Scope): Promise<unknown>;
      }).runtimeFor(scope);
      const aborted = new AbortController();
      let sessionAborted = false;
      const internal = runtime as {
        abortController: AbortController;
        session: { abort(): Promise<void> };
        markEffectUnknown(toolCallID: string, reason?: string): void;
        effectUnknownError?: string;
        unknownToolStepIDs: Set<string>;
      };
      internal.abortController = aborted;
      internal.session = {
        abort: async () => {
          sessionAborted = true;
        },
      };

      internal.markEffectUnknown("tool-unknown", "reconciliation unavailable");
      await new Promise<void>((resolve) => setImmediate(resolve));

      expect(aborted.signal.aborted).toBe(true);
      expect(sessionAborted).toBe(true);
      expect(internal.effectUnknownError).toBe("reconciliation unavailable");
      expect(internal.unknownToolStepIDs.has("tool-unknown")).toBe(true);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
