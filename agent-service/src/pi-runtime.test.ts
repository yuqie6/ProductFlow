import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  isTerminalStatus,
  ProductFlowError,
  resolvedToolContractVersion,
  type Scope,
  type TurnState,
} from "./contracts.js";
import { PiRuntimeManager, toolStepDetailsForResult } from "./pi-runtime.js";
import { JOURNAL_EVENT_MAX_PAYLOAD_BYTES } from "./pi-chunks.js";
import { RuntimeError, TurnStore } from "./store.js";

const scope: Scope = {
  schema_version: 1,
  scope_type: "product_workflow",
  conversation_id: "11111111-1111-4111-8111-111111111111",
  task_id: null,
  task_goal: null,
  product_id: "22222222-2222-4222-8222-222222222222",
  run_id: "44444444-4444-4444-8444-444444444444",
  system_prompt: "ProductFlow",
  draft_schema: { type: "object" },
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
  questionTimeoutMS: 900_000,
};

async function waitForTerminalTurn(store: TurnStore, runID: string, turnID: string): Promise<TurnState> {
  for (let attempt = 0; attempt < 300; attempt += 1) {
    const state = await store.getState(runID, turnID);
    if (isTerminalStatus(state.status)) return state;
    await new Promise<void>((resolve) => setTimeout(resolve, 10));
  }
  const last = await store.getState(runID, turnID);
  throw new Error(`Turn stayed non-terminal: ${last.status}`);
}

describe("PiRuntimeManager turn state", () => {
  it("starts a live-graph product Turn", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-no-draft-"));
    const managerHolder: { manager?: PiRuntimeManager } = {};
    try {
      const productFlow = {
        conversationContract: async () => ({
          schema_version: 1,
          scope_type: "product_workflow",
          conversation_id: scope.conversation_id,
          task_id: null,
          task_goal: null,
          product_id: scope.product_id,
          harness_run_id: scope.run_id,
          current_draft_version: 0,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: {},
          tool_contract_version: resolvedToolContractVersion({}),
          has_live_graph: true,
        }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      managerHolder.manager = manager;
      const started = await manager.start({
        lookup: { conversationID: scope.conversation_id },
        input: {
          input_text: "你好",
          asset_ids: [],
          idempotency_key: "live-graph-no-draft",
          page_context: null,
        },
      });
      expect(started.status).toBe("queued");
      expect(started.run_id).toBe(scope.run_id);
    } finally {
      await managerHolder.manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("maps a product contract missing product identity to a contract mismatch", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-no-product-"));
    const managerHolder: { manager?: PiRuntimeManager } = {};
    try {
      const productFlow = {
        conversationContract: async () => ({
          schema_version: 1,
          scope_type: "product_workflow",
          conversation_id: scope.conversation_id,
          task_id: null,
          task_goal: null,
          product_id: null,
          harness_run_id: scope.run_id,
          current_draft_version: 0,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: {},
          tool_contract_version: resolvedToolContractVersion({}),
          has_live_graph: true,
        }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      managerHolder.manager = manager;
      await expect(
        manager.start({
          lookup: { conversationID: scope.conversation_id },
          input: {
            input_text: "你好",
            asset_ids: [],
            idempotency_key: "missing-product-identity",
            page_context: null,
          },
        }),
      ).rejects.toEqual(
        expect.objectContaining({
          name: RuntimeError.name,
          status: 502,
          code: "contract_mismatch",
          message: "ProductFlow returned an incomplete product Agent contract",
        }),
      );
    } finally {
      await managerHolder.manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

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
    expect(
      toolStepDetailsForResult("apply_graph_change_set_v1", {
        details: {
          truncated: true,
          operation_summaries: ["rename_node", "rename_node"],
          affected_node_ids: ["n1"],
          pending_confirmation: false,
        },
      }, false),
    ).toMatchObject({
      phase: "tool_result",
      truncated: true,
      operation_summaries: ["rename_node", "rename_node"],
      affected_node_ids: ["n1"],
    });
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
          harness_run_id: scope.run_id,
          current_draft_version: 1,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: { type: "object" },
          tool_contract_version: resolvedToolContractVersion({ type: "object" }),
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
        appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => {
          durableEvents.push(...args.events.map((event) => ({ sequence: event.sequence, kind: event.kind })));
          return args.events.map((event) => ({
            id: `event-${event.sequence}`,
            projection_id: "projection-cancel",
            execution_id: "execution-cancel",
            sequence: event.sequence,
            schema_version: 1 as const,
            kind: event.kind,
            created_at: "2026-08-20T00:00:00.000Z",
          }));
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
      expect(durableEvents).toEqual([{ sequence: 1, kind: "turn/end" }]);
      expect(checkpoints).toEqual([]);
      expect(released).toBe(true);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("fails a Turn when ProductFlow rejects the pre-lease claim", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-claim-rejected-"));
    const managerHolder: { manager?: PiRuntimeManager } = {};
    try {
      const productFlow = {
        conversationContract: async () => ({
          schema_version: 1,
          scope_type: "product_workflow",
          conversation_id: scope.conversation_id,
          task_id: null,
          task_goal: null,
          product_id: scope.product_id,
          harness_run_id: scope.run_id,
          current_draft_version: 1,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: { type: "object" },
          tool_contract_version: resolvedToolContractVersion({ type: "object" }),
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
      managerHolder.manager = manager;

      const started = await manager.start({
        lookup: { conversationID: scope.conversation_id },
        input: {
          input_text: "触发 claim 拒绝",
          asset_ids: [],
          idempotency_key: "claim-rejected",
          page_context: null,
        },
      });
      const state = await waitForTerminalTurn(store, scope.run_id, started.turn_id);

      expect(state).toMatchObject({
        status: "failed",
        error: "Agent Turn projection does not exist",
      });
    } finally {
      await managerHolder.manager?.close();
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
          harness_run_id: scope.run_id,
          current_draft_version: 1,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: { type: "object" },
          tool_contract_version: resolvedToolContractVersion({ type: "object" }),
        }),
        appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ turn_id: string }> }) => {
          published.push(...args.events.map((event) => event.turn_id));
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
        "turn/cancel_requested",
      ]);
      expect((await store.events(scope.run_id, queued.state.turn_id, 0))[0]?.ignorable).toBe(true);
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
          harness_run_id: scope.run_id,
          current_draft_version: 1,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: { type: "object" },
          tool_contract_version: resolvedToolContractVersion({ type: "object" }),
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
      await store.appendEvent(scope.run_id, created.state.turn_id, "question/requested", question);

      const canceled = await manager.cancel({ conversationID: scope.conversation_id }, created.state.turn_id);

      expect(canceled).toMatchObject({
        status: "canceled",
        error: "",
      });
      expect((await store.events(scope.run_id, created.state.turn_id, 0)).map((event) => event.kind)).toEqual([
        "question/requested",
        "turn/end",
      ]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it.each(["succeeded", "unknown"] as const)(
    "keeps local state unknown when publishing the %s terminal event fails",
    async (requestedStatus) => {
      const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-event-failure-"));
      try {
        const store = new TurnStore(root);
        await store.init();
        let publishAttempts = 0;
        const productFlow = {
          appendTurnEvents: async () => {
            publishAttempts += 1;
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
            status: "succeeded" | "unknown",
            details: { output?: string },
          ): Promise<void>;
          cleanupAfterTurn(): Promise<void>;
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

        await internal.finishTurn(created.state.turn_id, requestedStatus, { output: "本地结果" });

        await expect(store.getState(scope.run_id, created.state.turn_id)).resolves.toMatchObject({
          status: "unknown",
          output: "本地结果",
          error: "event store unavailable",
        });
        const terminalEvents = (await store.events(scope.run_id, created.state.turn_id, 0))
          .filter((event) => event.kind === "turn/end");
        expect(terminalEvents).toHaveLength(requestedStatus === "succeeded" ? 2 : 1);
        expect(terminalEvents.at(-1)?.payload.status).toBe("unknown");
        expect((internal as unknown as { eventBatcher: { pendingCount: number } }).eventBatcher.pendingCount).toBeGreaterThan(0);
        await internal.cleanupAfterTurn();
        expect(publishAttempts).toBe(1);
      } finally {
        await rm(root, { recursive: true, force: true });
      }
    },
  );

  it("keeps the PG-acknowledged terminal state without writing a second terminal checkpoint", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-checkpoint-failure-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const durableEvents: string[] = [];
      let checkpointCalls = 0;
      const productFlow = {
        appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => {
          durableEvents.push(...args.events.map((event) => event.kind));
          return args.events.map((event) => ({
            id: `event-${event.sequence}`,
            projection_id: "projection-checkpoint-failure",
            execution_id: "execution-checkpoint-failure",
            sequence: event.sequence,
            schema_version: 1 as const,
            kind: event.kind,
            ignorable: false,
            created_at: "2026-08-31T00:00:00.000Z",
          }));
        },
        appendTurnCheckpoint: async () => {
          checkpointCalls += 1;
          throw new Error("terminal checkpoint unavailable");
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
        status: "succeeded",
        error: "",
      });
      const events = await store.events(scope.run_id, created.state.turn_id, 0);
      expect(events.at(-1)?.kind).toBe("turn/end");
      expect(durableEvents).toEqual(["turn/end"]);
      expect(checkpointCalls).toBe(0);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("chunks a long assistant output under the PG payload and sequence limits", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-long-output-"));
    try {
      const batches: Array<Array<{ sequence: number; kind: string; payload: Record<string, unknown> }>> = [];
      const productFlow = {
        appendTurnEvents: async (
          _conversationID: string,
          _executionID: string,
          args: { events: Array<{ sequence: number; kind: string; payload: Record<string, unknown> }> },
        ) => {
          batches.push(args.events);
          return args.events.map((event) => ({
            id: `event-${event.sequence}`,
            projection_id: "projection-long-output",
            execution_id: "execution-long-output",
            sequence: event.sequence,
            schema_version: 1 as const,
            kind: event.kind,
            ignorable: false,
            created_at: "2026-08-31T00:00:00.000Z",
          }));
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
      const created = await store.createTurn(scope, {
        input_text: "测试长输出",
        asset_ids: [],
        idempotency_key: "long-output",
        page_context: null,
      });
      const internal = runtime as {
        currentTurn: string;
        abortController: AbortController;
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
        resetTurnState(): void;
        projectAssistantMessageEvent(turnID: string, raw: { type: string; delta?: string; contentIndex?: number; reason?: string }): void;
        finishTurn(turnID: string, status: "succeeded", details: { output: string }): Promise<void>;
      };
      internal.resetTurnState();
      internal.currentTurn = created.state.turn_id;
      internal.abortController = new AbortController();
      internal.executionLease = {
        execution_id: "execution-long-output",
        projection_id: "projection-long-output",
        harness_turn_id: created.state.turn_id,
        owner_id: "agent-long-output",
        lease_token: "lease-long-output",
        attempt: 1,
        fencing_token: 1,
        phase: "claimed",
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      const output = "长输出内容".repeat(16_000);

      internal.projectAssistantMessageEvent(created.state.turn_id, { type: "text_delta", delta: output, contentIndex: 0 });
      internal.projectAssistantMessageEvent(created.state.turn_id, { type: "done", reason: "stop" });
      await internal.finishTurn(created.state.turn_id, "succeeded", { output });

      const state = await store.getState(scope.run_id, created.state.turn_id);
      const events = await store.events(scope.run_id, created.state.turn_id, 0);
      expect(state).toMatchObject({ status: "succeeded", output });
      expect(events.filter((event) => event.kind === "text.chunk").map((event) => String(event.payload.delta)).join("")).toBe(output);
      expect(events.length).toBeLessThan(20);
      expect(events.every((event) => Buffer.byteLength(JSON.stringify(event.payload), "utf8") <= JOURNAL_EVENT_MAX_PAYLOAD_BYTES)).toBe(true);
      expect(events.find((event) => event.kind === "assistant/message")?.payload).not.toHaveProperty("text");
      expect(events.find((event) => event.kind === "assistant/message")?.payload).not.toHaveProperty("thinking");
      expect(events.at(-1)?.kind).toBe("turn/end");
      expect(events.at(-1)?.payload).not.toHaveProperty("output");
      expect(batches.flat().map((event) => event.sequence)).toEqual(events.map((event) => event.sequence));
      expect(batches.every((batch) => batch.length >= 1)).toBe(true);
      expect((internal as unknown as { eventBatcher: { pendingCount: number } }).eventBatcher.pendingCount).toBe(0);
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

  it("keeps the execution signal live while pausing the Pi session for approval", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-approval-"));
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
      const execution = new AbortController();
      let sessionAborted = false;
      const internal = runtime as {
        abortController: AbortController;
        session: { abort(): Promise<void> };
        requestApproval(approval: Record<string, unknown>): void;
      };
      internal.abortController = execution;
      internal.session = { abort: async () => { sessionAborted = true; } };

      internal.requestApproval({ approval_kind: "workflow_run", approval_id: "approval-1" });
      await Promise.resolve();

      expect(sessionAborted).toBe(true);
      expect(execution.signal.aborted).toBe(false);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("stores an answer when the in-process waiter is gone", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-dead-waiter-answer-"));
    const managerHolder: { manager?: PiRuntimeManager } = {};
    try {
      const productFlow = {
        conversationContract: async () => ({
          schema_version: 1,
          scope_type: "product_workflow",
          conversation_id: scope.conversation_id,
          task_id: null,
          task_goal: null,
          product_id: scope.product_id,
          harness_run_id: scope.run_id,
          current_draft_version: 1,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: { type: "object" },
          tool_contract_version: resolvedToolContractVersion({ type: "object" }),
        }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      managerHolder.manager = manager;
      const created = await store.createTurn(scope, {
        input_text: "帮我建商品",
        asset_ids: [],
        idempotency_key: "dead-waiter-answer",
        page_context: null,
      });
      const question = {
        id: "question-name",
        header: "商品名",
        question: "这个商品叫什么名字？",
        options: [{ label: "还没想好" }, { label: "稍后再说" }],
      };
      await store.updateState(scope.run_id, created.state.turn_id, { status: "requires_input", question });
      await store.appendEvent(scope.run_id, created.state.turn_id, "question/requested", question);

      const answered = await manager.answerQuestion(
        { conversationID: scope.conversation_id },
        created.state.turn_id,
        question.id,
        { text: "筋膜枪" },
      );
      expect(answered.status).toBe("queued");
      expect(answered.question).toBeUndefined();
      expect((await store.events(scope.run_id, created.state.turn_id, 0)).map((event) => event.kind)).toContain(
        "question/answered",
      );
    } finally {
      await managerHolder.manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("expires a live question as no_answer without a new user prompt", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-question-timeout-"));
    const managerHolder: { manager?: PiRuntimeManager } = {};
    try {
      const productFlow = {
        conversationContract: async () => ({
          schema_version: 1,
          scope_type: "product_workflow",
          conversation_id: scope.conversation_id,
          task_id: null,
          task_goal: null,
          product_id: scope.product_id,
          harness_run_id: scope.run_id,
          current_draft_version: 1,
          system_prompt: "ProductFlow",
          draft_kind: "workflow",
          draft_schema: { type: "object" },
          tool_contract_version: resolvedToolContractVersion({ type: "object" }),
        }),
        appendTurnCheckpoint: async () => ({
          id: "checkpoint-timeout",
          projection_id: "projection-timeout",
          execution_id: "execution-timeout",
          attempt: 1,
          fencing_token: 1,
          sequence: 1,
          kind: "question_required",
          created_at: "2099-01-01T00:00:00.000Z",
        }),
        heartbeatTurnExecution: async () => ({
          execution_id: "execution-timeout",
          projection_id: "projection-timeout",
          harness_turn_id: "turn-timeout",
          owner_id: "owner-timeout",
          lease_token: "lease-timeout",
          attempt: 1,
          fencing_token: 1,
          phase: "waiting_input" as const,
          lease_expires_at: "2099-01-01T00:00:00.000Z",
        }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root, questionTimeoutMS: 20 },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      managerHolder.manager = manager;
      const created = await store.createTurn(scope, {
        input_text: "帮我建商品",
        asset_ids: [],
        idempotency_key: "question-timeout",
        page_context: null,
      });
      const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      const internal = runtime as {
        currentTurn: string;
        abortController: AbortController;
        executionLease: {
          execution_id: string;
          owner_id: string;
          lease_token: string;
        };
        askUser(question: {
          id: string;
          header: string;
          question: string;
          options: Array<{ label: string }>;
        }): Promise<{ skip?: true; text?: string }>;
      };
      internal.currentTurn = created.state.turn_id;
      internal.abortController = new AbortController();
      internal.executionLease = {
        execution_id: "execution-timeout",
        owner_id: "owner-timeout",
        lease_token: "lease-timeout",
      };

      const answer = await internal.askUser({
        id: "question-timeout",
        header: "商品名",
        question: "这个商品叫什么名字？",
        options: [{ label: "还没想好" }, { label: "稍后再说" }],
      });
      expect(answer).toEqual({ skip: true });
      expect((await store.getState(scope.run_id, created.state.turn_id)).status).toBe("running");
      expect((await store.events(scope.run_id, created.state.turn_id, 0)).at(-1)).toMatchObject({
        kind: "question/answered",
        payload: { answer: { skip: true }, status: "no_answer" },
      });
    } finally {
      await managerHolder.manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });
});
