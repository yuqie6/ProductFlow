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
import { toolStepDetailsForResult } from "./tool-step-projection.js";
import { PiRuntimeManager } from "./runtime-manager.js";
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
      let claimCalls = 0;
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
        claimTurnExecution: async (_conversationID: string, args: { harness_turn_id: string; owner_id: string }) => {
          claimCalls += 1;
          if (claimCalls <= 3) throw new ProductFlowError(503, "unavailable", "temporary claim failure");
          return {
            execution_id: "execution-question-cancel",
            projection_id: "projection-question-cancel",
            harness_turn_id: args.harness_turn_id,
            owner_id: args.owner_id,
            lease_token: "lease-question-cancel",
            attempt: 1,
            fencing_token: 1,
            phase: "claimed" as const,
            lease_expires_at: "2099-01-01T00:00:00.000Z",
          };
        },
        heartbeatTurnExecution: async (_conversationID: string, _executionID: string, args: { owner_id: string; lease_token: string; phase: string }) => ({
          execution_id: "execution-question-cancel",
          projection_id: "projection-question-cancel",
          harness_turn_id: "question-cancel",
          owner_id: args.owner_id,
          lease_token: args.lease_token,
          attempt: 1,
          fencing_token: 1,
          phase: args.phase,
          lease_expires_at: "2099-01-01T00:00:00.000Z",
        }),
        appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => args.events.map((event) => ({
          id: `event-${event.sequence}`,
          projection_id: "projection-question-cancel",
          execution_id: "execution-question-cancel",
          sequence: event.sequence,
          schema_version: 1 as const,
          kind: event.kind,
          ignorable: false,
          created_at: "2026-08-31T00:00:00.000Z",
        })),
        releaseTurnExecution: async () => ({ released: true }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
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
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "question/requested", question);

      await expect(manager.cancel({ conversationID: scope.conversation_id }, created.state.turn_id)).rejects.toMatchObject({
        status: 503,
        code: "unavailable",
      });
      const pendingCancel = await store.getState(scope.run_id, created.state.turn_id);
      expect(pendingCancel).toMatchObject({ status: "cancel_requested" });
      expect(pendingCancel).not.toHaveProperty("question");
      expect((await store.events(scope.run_id, created.state.turn_id, 0)).filter((event) => event.kind === "turn/cancel_requested")).toHaveLength(1);

      const canceled = await manager.cancel({ conversationID: scope.conversation_id }, created.state.turn_id);

      expect(canceled).toMatchObject({
        status: "canceled",
        error: "",
      });
      expect((await store.events(scope.run_id, created.state.turn_id, 0)).map((event) => event.kind)).toEqual([
        "question/requested",
        "turn/cancel_requested",
        "turn/end",
      ]);
      expect(claimCalls).toBe(4);
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
        let releaseAttempts = 0;
        const productFlow = {
          appendTurnEvents: async () => {
            publishAttempts += 1;
            throw new Error("event store unavailable");
          },
          releaseTurnExecution: async () => {
            releaseAttempts += 1;
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
          input_text: "测试终态事件失败",
          asset_ids: [],
          idempotency_key: "durable-event-failure",
          page_context: null,
        });
        const internal = runtime as {
          currentTurn: string;
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
        internal.currentTurn = created.state.turn_id;
        await store.updateState(scope.run_id, created.state.turn_id, {
          execution_attempt: 1,
          execution_fencing_token: 1,
        });
        await store.saveDurableHandoff(scope.run_id, created.state.turn_id, internal.executionLease);

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
        expect(releaseAttempts).toBe(0);
        expect((await store.durableHandoffCandidates()).map((candidate) => candidate.turnID)).toEqual([created.state.turn_id]);
        await manager.close();
      } finally {
        await rm(root, { recursive: true, force: true });
      }
    },
  );

  it.each(["waiting_input", "approval"] as const)(
    "recovers a durable %s handoff without inventing or duplicating protocol events",
    async (scenario) => {
      const root = await mkdtemp(join(tmpdir(), `productflow-pi-${scenario}-handoff-`));
      const managerHolder: { manager?: PiRuntimeManager } = {};
      try {
        const appendedKinds: string[] = [];
        const releasedPhases: string[] = [];
        const lease = {
          execution_id: `execution-${scenario}`,
          projection_id: `projection-${scenario}`,
          harness_turn_id: "",
          owner_id: "stable-owner",
          lease_token: `lease-${scenario}`,
          attempt: 1,
          fencing_token: 1,
          phase: scenario === "waiting_input" ? "waiting_input" as const : "model" as const,
          lease_expires_at: "2099-01-01T00:00:00.000Z",
        };
        const productFlow = {
          confirmTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number }> }) => ({
            status: args.events.length === 0 ? "confirmed" as const : "missing" as const,
            confirmed_through: (args.events[0]?.sequence ?? 1) - 1,
            persisted_through: (args.events[0]?.sequence ?? 1) - 1,
            items: [],
          }),
          claimTurnExecution: async () => lease,
          heartbeatTurnExecution: async (_conversationID: string, _executionID: string, args: { phase: typeof lease.phase }) => ({
            ...lease,
            phase: args.phase,
          }),
          appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => {
            appendedKinds.push(...args.events.map((event) => event.kind));
            return args.events.map((event) => ({
              id: `event-${event.sequence}`,
              projection_id: lease.projection_id,
              execution_id: lease.execution_id,
              sequence: event.sequence,
              schema_version: 1 as const,
              kind: event.kind,
              ignorable: false,
              created_at: "2026-08-31T00:00:00.000Z",
            }));
          },
          releaseTurnExecution: async (_conversationID: string, _executionID: string, args: { phase: string }) => {
            releasedPhases.push(args.phase);
          },
        } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
        const store = new TurnStore(root);
        await store.init();
        const created = await store.createTurn(scope, {
          input_text: "恢复协议状态",
          asset_ids: [],
          idempotency_key: `recover-${scenario}`,
          page_context: null,
        });
        lease.harness_turn_id = created.state.turn_id;
        await store.updateState(scope.run_id, created.state.turn_id, {
          status: scenario === "waiting_input" ? "requires_input" : "running",
          execution_attempt: 1,
          execution_fencing_token: 1,
          ...(scenario === "waiting_input" ? {
            question: { id: "q1", header: "确认", question: "继续吗？", options: [{ label: "继续" }] },
          } : {}),
        });
        if (scenario === "waiting_input") {
          await store.appendLocalEvent(scope.run_id, created.state.turn_id, "question/requested", {
            id: "q1", header: "确认", question: "继续吗？", options: [{ label: "继续" }],
          });
        } else {
          await store.appendLocalEvent(scope.run_id, created.state.turn_id, "approval/requested", {
            approval_id: "artifact-step",
            approval_kind: "artifact",
            artifact: { name: "workflow", step_id: "artifact-step", value: { version: 1 } },
          });
        }
        await store.saveDurableHandoff(scope.run_id, created.state.turn_id, lease);
        const manager = new PiRuntimeManager(
          { ...config, dataRoot: root }, store, productFlow,
          {} as ConstructorParameters<typeof PiRuntimeManager>[3], "stable-owner",
        );
        managerHolder.manager = manager;
        const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
        const recovered = await (runtime as {
          recoverDurableHandoff(turnID: string, idempotencyKey: string, executionID: string, projectionID: string): Promise<boolean>;
        }).recoverDurableHandoff(created.state.turn_id, `recover-${scenario}`, lease.execution_id, lease.projection_id);

        expect(recovered).toBe(true);
        if (scenario === "waiting_input") {
          expect(await store.getState(scope.run_id, created.state.turn_id)).toMatchObject({ status: "requires_input" });
          expect(appendedKinds).toEqual(["question/requested"]);
          expect(releasedPhases).toEqual(["waiting_input"]);
        } else {
          expect(await store.getState(scope.run_id, created.state.turn_id)).toMatchObject({
            status: "awaiting_confirmation",
            artifact: { name: "workflow", step_id: "artifact-step", value: { version: 1 } },
          });
          expect((await store.events(scope.run_id, created.state.turn_id, 0)).filter((event) => event.kind === "turn/end")).toEqual([]);
          expect(appendedKinds).toEqual(["approval/requested"]);
          expect(releasedPhases).toEqual(["external_job"]);
        }
      } finally {
        await managerHolder.manager?.close();
        await rm(root, { recursive: true, force: true });
      }
    },
  );

  it.each([false, true])("restores only the current question answer after restart (new unanswered question=%s)", async (hasNextQuestion) => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-answered-handoff-"));
    let manager: PiRuntimeManager | undefined;
    try {
      const store = new TurnStore(root);
      await store.init();
      const created = await store.createTurn(scope, {
        input_text: "恢复已回答问题",
        asset_ids: [],
        idempotency_key: "recover-answered",
        page_context: null,
      });
      const lease = {
        execution_id: "execution-answered",
        projection_id: "projection-answered",
        harness_turn_id: created.state.turn_id,
        owner_id: "stable-owner",
        lease_token: "lease-answered",
        attempt: 1,
        fencing_token: 1,
        phase: "waiting_input" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      await store.updateState(scope.run_id, created.state.turn_id, {
        status: "queued",
        execution_attempt: 1,
        execution_fencing_token: 1,
      });
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "question/requested", {
        id: "q-answered", header: "确认", question: "继续吗？", options: [{ label: "继续" }, { label: "停止" }],
      });
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "question/answered", {
        question_id: "q-answered", answer: { option: 0 },
      });
      if (hasNextQuestion) {
        const nextQuestion = { id: "q-next", header: "继续", question: "第二次选择？", options: [{ label: "继续" }, { label: "停止" }] };
        await store.appendLocalEvent(scope.run_id, created.state.turn_id, "question/requested", nextQuestion);
        await store.updateState(scope.run_id, created.state.turn_id, { status: "requires_input", question: nextQuestion });
      }
      await store.saveDurableHandoff(scope.run_id, created.state.turn_id, lease);
      const productFlow = {
        confirmTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number }> }) => ({
          status: args.events.length === 0 ? "confirmed" as const : "missing" as const, confirmed_through: args.events[0]?.sequence - 1 || 0,
          persisted_through: args.events[0]?.sequence - 1 || 0, items: [],
        }),
        claimTurnExecution: async () => lease,
        heartbeatTurnExecution: async (_conversationID: string, _executionID: string, args: { phase: typeof lease.phase }) => ({ ...lease, phase: args.phase }),
        appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => args.events.map((event) => ({
          id: `event-${event.sequence}`, projection_id: lease.projection_id, execution_id: lease.execution_id,
          sequence: event.sequence, schema_version: 1 as const, kind: event.kind, ignorable: false,
          created_at: "2026-08-31T00:00:00.000Z",
        })),
        releaseTurnExecution: async () => ({ released: true }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const restartedStore = new TurnStore(root);
      await restartedStore.init();
      manager = new PiRuntimeManager(
        { ...config, dataRoot: root, maxConcurrentTurns: 0 }, restartedStore, productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3], "stable-owner",
      );

      const summary = await manager.recoverAfterRestart();
      expect(summary).toMatchObject({ replayed_handoffs: 1, queued_turns: hasNextQuestion ? 0 : 1 });
      expect(await store.getState(scope.run_id, created.state.turn_id)).toMatchObject({ status: hasNextQuestion ? "requires_input" : "queued" });
      expect((await store.events(scope.run_id, created.state.turn_id, 0)).map((event) => event.kind)).toEqual([
        "question/requested", "question/answered",
        ...(hasNextQuestion ? ["question/requested"] : []),
      ]);
    } finally {
      await manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("serializes heartbeat phase changes so an older request cannot overwrite waiting_input", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-phase-order-"));
    let manager: PiRuntimeManager | undefined;
    try {
      const phases: string[] = [];
      let resolveFirst!: () => void;
      const firstResponse = new Promise<void>((resolve) => {
        resolveFirst = resolve;
      });
      const lease = {
        execution_id: "execution-phase",
        projection_id: "projection-phase",
        harness_turn_id: "turn-phase",
        owner_id: "owner-phase",
        lease_token: "lease-phase",
        attempt: 1,
        fencing_token: 1,
        phase: "claimed" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      const productFlow = {
        heartbeatTurnExecution: async (_conversationID: string, _executionID: string, args: { phase: string }) => {
          phases.push(args.phase);
          if (phases.length === 1) await firstResponse;
          return { ...lease, phase: args.phase };
        },
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      const store = new TurnStore(root);
      await store.init();
      manager = new PiRuntimeManager(
        { ...config, dataRoot: root }, store, productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3], "owner-phase",
      );
      const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      const internal = runtime as {
        executionLease?: typeof lease;
        abortController: AbortController;
        updateExecutionPhase(phase: "model" | "waiting_input"): Promise<void>;
      };
      internal.executionLease = lease;
      internal.abortController = new AbortController();
      const model = internal.updateExecutionPhase("model");
      while (phases.length === 0) await new Promise<void>((resolve) => setTimeout(resolve, 0));
      const waiting = internal.updateExecutionPhase("waiting_input");
      resolveFirst();
      await Promise.all([model, waiting]);
      expect(phases).toEqual(["model", "waiting_input"]);
      internal.executionLease = undefined;
    } finally {
      await manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("does not advance the local ACK for a mismatched event receipt", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-receipt-mismatch-"));
    let manager: PiRuntimeManager | undefined;
    try {
      const store = new TurnStore(root);
      await store.init();
      const created = await store.createTurn(scope, {
        input_text: "receipt mismatch",
        asset_ids: [],
        idempotency_key: "receipt-mismatch",
        page_context: null,
      });
      const event = await store.appendLocalEvent(scope.run_id, created.state.turn_id, "turn/start", { status: "running" });
      const lease = {
        execution_id: "execution-receipt",
        projection_id: "projection-receipt",
        harness_turn_id: created.state.turn_id,
        owner_id: "owner-receipt",
        lease_token: "lease-receipt",
        attempt: 1,
        fencing_token: 1,
        phase: "claimed" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      const productFlow = {
        appendTurnEvents: async () => [{
          id: "event-1",
          projection_id: "wrong-projection",
          execution_id: lease.execution_id,
          sequence: 1,
          schema_version: 1,
          kind: "turn/start",
          ignorable: false,
          created_at: "2026-08-31T00:00:00.000Z",
        }],
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      manager = new PiRuntimeManager(
        { ...config, dataRoot: root }, store, productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3], "owner-receipt",
      );
      const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      const internal = runtime as {
        executionLease?: typeof lease;
        appendPublishedBatch(events: readonly typeof event[]): Promise<void>;
      };
      internal.executionLease = lease;
      await expect(internal.appendPublishedBatch([event])).rejects.toMatchObject({ code: "event_receipt_mismatch" });
      expect((await store.unpublishedEvents(scope.run_id, created.state.turn_id)).map((item) => item.sequence)).toEqual([1]);
      internal.executionLease = undefined;
    } finally {
      await manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("retries a 5xx journal batch then ACKs the original sequence", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-batch-5xx-retry-"));
    let manager: PiRuntimeManager | undefined;
    try {
      const store = new TurnStore(root);
      await store.init();
      const created = await store.createTurn(scope, {
        input_text: "重试 journal 5xx",
        asset_ids: [],
        idempotency_key: "batch-5xx-retry",
        page_context: null,
      });
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "text.chunk", { delta: "retry" });
      const event = (await store.unpublishedEvents(scope.run_id, created.state.turn_id))[0];
      if (!event) throw new Error("missing unpublished event");
      const lease = {
        execution_id: "execution-5xx",
        projection_id: "projection-5xx",
        harness_turn_id: created.state.turn_id,
        owner_id: "owner-5xx",
        lease_token: "lease-5xx",
        attempt: 1,
        fencing_token: 1,
        phase: "claimed" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      let appendCalls = 0;
      const productFlow = {
        appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => {
          appendCalls += 1;
          if (appendCalls === 1) throw new ProductFlowError(500, "upstream_error", "服务器内部错误");
          return args.events.map((item) => ({
            id: `event-${item.sequence}`,
            projection_id: lease.projection_id,
            execution_id: lease.execution_id,
            sequence: item.sequence,
            schema_version: 1 as const,
            kind: item.kind,
            ignorable: false,
            created_at: "2026-08-31T00:00:00.000Z",
          }));
        },
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      manager = new PiRuntimeManager(
        { ...config, dataRoot: root }, store, productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3], "owner-5xx",
      );
      const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      const internal = runtime as {
        executionLease?: typeof lease;
        appendPublishedBatch(events: readonly typeof event[]): Promise<void>;
      };
      internal.executionLease = lease;
      await expect(internal.appendPublishedBatch([event])).resolves.toBeUndefined();
      expect(appendCalls).toBe(2);
      expect((await store.unpublishedEvents(scope.run_id, created.state.turn_id))).toEqual([]);
      internal.executionLease = undefined;
    } finally {
      await manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("publishes one original terminal and removes its local persistence fallback during recovery", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-terminal-fallback-recovery-"));
    let manager: PiRuntimeManager | undefined;
    try {
      const store = new TurnStore(root);
      await store.init();
      const created = await store.createTurn(scope, {
        input_text: "terminal fallback",
        asset_ids: [],
        idempotency_key: "terminal-fallback",
        page_context: null,
      });
      const lease = {
        execution_id: "execution-terminal-fallback",
        projection_id: "projection-terminal-fallback",
        harness_turn_id: created.state.turn_id,
        owner_id: "owner-terminal-fallback",
        lease_token: "lease-terminal-fallback",
        attempt: 1,
        fencing_token: 1,
        phase: "model" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      await store.updateState(scope.run_id, created.state.turn_id, {
        status: "unknown",
        execution_attempt: 1,
        execution_fencing_token: 1,
      });
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "text.chunk", { delta: "kept output" });
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "turn/end", { status: "succeeded", reason: "completed" });
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "turn/end", {
        status: "unknown", reason: "unknown", reason_code: "persistence_failed", error: "response lost",
      });
      await store.saveDurableHandoff(scope.run_id, created.state.turn_id, lease);
      const appendedKinds: string[] = [];
      const productFlow = {
        confirmTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number }> }) => ({
          status: args.events.length === 0 ? "confirmed" as const : "missing" as const, confirmed_through: args.events[0]?.sequence - 1 || 0,
          persisted_through: args.events[0]?.sequence - 1 || 0, items: [],
        }),
        claimTurnExecution: async () => lease,
        heartbeatTurnExecution: async (_conversationID: string, _executionID: string, args: { phase: typeof lease.phase }) => ({ ...lease, phase: args.phase }),
        appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => {
          appendedKinds.push(...args.events.map((event) => event.kind));
          return args.events.map((event) => ({
            id: `event-${event.sequence}`, projection_id: lease.projection_id, execution_id: lease.execution_id,
            sequence: event.sequence, schema_version: 1 as const, kind: event.kind, ignorable: false,
            created_at: "2026-08-31T00:00:00.000Z",
          }));
        },
        releaseTurnExecution: async () => ({ released: true }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      manager = new PiRuntimeManager(
        { ...config, dataRoot: root }, store, productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3], lease.owner_id,
      );
      const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      await expect((runtime as {
        recoverDurableHandoff(turnID: string, key: string, executionID: string, projectionID: string): Promise<boolean>;
      }).recoverDurableHandoff(created.state.turn_id, "terminal-fallback", lease.execution_id, lease.projection_id)).resolves.toBe(true);

      expect(appendedKinds).toEqual(["text.chunk", "turn/end"]);
      expect(await store.getState(scope.run_id, created.state.turn_id)).toMatchObject({ status: "succeeded", output: "kept output" });
      expect((await store.events(scope.run_id, created.state.turn_id, 0)).map((event) => event.kind)).toEqual(["text.chunk", "turn/end"]);
      expect(await store.durableHandoffCandidates()).toEqual([]);
    } finally {
      await manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("adopts a PG-only terminal discovered by an empty confirmation probe", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-pg-only-terminal-"));
    let manager: PiRuntimeManager | undefined;
    try {
      const store = new TurnStore(root);
      await store.init();
      const created = await store.createTurn(scope, {
        input_text: "PG scanner terminal",
        asset_ids: [],
        idempotency_key: "pg-only-terminal",
        page_context: null,
      });
      const lease = {
        execution_id: "execution-pg-terminal",
        projection_id: "projection-pg-terminal",
        harness_turn_id: created.state.turn_id,
        owner_id: "owner-pg-terminal",
        lease_token: "lease-pg-terminal",
        attempt: 1,
        fencing_token: 1,
        phase: "model" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      await store.updateState(scope.run_id, created.state.turn_id, {
        status: "running", execution_attempt: 1, execution_fencing_token: 1,
      });
      await store.saveDurableHandoff(scope.run_id, created.state.turn_id, lease);
      const productFlow = {
        confirmTurnEvents: async (_conversationID: string, _executionID: string, args: { events: unknown[] }) => {
          expect(args.events).toEqual([]);
          return {
            status: "confirmed" as const,
            confirmed_through: 0,
            persisted_through: 1,
            items: [],
            terminal: {
              id: "event-pg-terminal",
              projection_id: lease.projection_id,
              execution_id: lease.execution_id,
              sequence: 1,
              schema_version: 1 as const,
              kind: "turn/end",
              ignorable: false,
              created_at: "2026-08-31T00:00:00.000Z",
              payload: { status: "unknown", reason: "unknown", reason_code: "execution_interrupted", error: "lease expired" },
              projection_status: "unknown" as const,
              output: "",
              thinking: "",
              error: "lease expired",
              finished_at: "2026-08-31T00:00:00.000Z",
            },
          };
        },
        claimTurnExecution: async () => {
          throw new Error("claim must not run after authoritative terminal discovery");
        },
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      manager = new PiRuntimeManager(
        { ...config, dataRoot: root }, store, productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3], lease.owner_id,
      );
      const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      await expect((runtime as {
        recoverDurableHandoff(turnID: string, key: string, executionID: string, projectionID: string): Promise<boolean>;
      }).recoverDurableHandoff(created.state.turn_id, "pg-only-terminal", lease.execution_id, lease.projection_id)).resolves.toBe(true);
      expect(await store.getState(scope.run_id, created.state.turn_id)).toMatchObject({ status: "unknown", error: "lease expired" });
      expect(await store.durableHandoffCandidates()).toEqual([]);
    } finally {
      await manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("skips a divergent local prefix on event content 409 and still recovers", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-content-409-handoff-"));
    let manager: PiRuntimeManager | undefined;
    try {
      const store = new TurnStore(root);
      await store.init();
      const created = await store.createTurn(scope, {
        input_text: "divergent local WAL",
        asset_ids: [],
        idempotency_key: "content-409-handoff",
        page_context: null,
      });
      const lease = {
        execution_id: "execution-content-409",
        projection_id: "projection-content-409",
        harness_turn_id: created.state.turn_id,
        owner_id: "owner-content-409",
        lease_token: "lease-content-409",
        attempt: 1,
        fencing_token: 1,
        phase: "model" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "text.chunk", { delta: "local-only" });
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "tool/call", {
        step_id: "context_local", kind: "inject_context", summary: "注入上下文", status: "running",
      });
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "tool/result", {
        step_id: "context_local", kind: "inject_context", summary: "注入上下文", status: "failed",
      });
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "turn/end", {
        reason: "unknown", reason_code: "execution_interrupted", status: "unknown",
        error: "Agent event sequence 已绑定不同内容",
      });
      await store.updateState(scope.run_id, created.state.turn_id, {
        status: "running", execution_attempt: 1, execution_fencing_token: 1,
      });
      await store.saveDurableHandoff(scope.run_id, created.state.turn_id, lease);
      const confirmCalls: number[][] = [];
      const productFlow = {
        confirmTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number }> }) => {
          confirmCalls.push(args.events.map((event) => event.sequence));
          if (args.events.length === 0) {
            return {
              status: "confirmed" as const,
              confirmed_through: 0,
              persisted_through: 1,
              items: [],
            };
          }
          throw new ProductFlowError(409, "event_sequence_conflict", "Agent event sequence 已绑定不同内容");
        },
        claimTurnExecution: async () => lease,
        heartbeatTurnExecution: async (_conversationID: string, _executionID: string, args: { phase: typeof lease.phase }) => ({
          ...lease, phase: args.phase,
        }),
        appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => (
          args.events.map((event) => ({
            id: `event-${event.sequence}`, projection_id: lease.projection_id, execution_id: lease.execution_id,
            sequence: event.sequence, schema_version: 1 as const, kind: event.kind, ignorable: false,
            created_at: "2026-08-31T00:00:00.000Z",
          }))
        ),
        releaseTurnExecution: async () => ({ released: true }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      manager = new PiRuntimeManager(
        { ...config, dataRoot: root }, store, productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3], lease.owner_id,
      );
      await expect(manager.recoverAfterRestart()).resolves.toMatchObject({ replayed_handoffs: 1 });
      expect(confirmCalls).toContainEqual([]);
      expect(confirmCalls).toContainEqual([1, 2, 3, 4]);
      expect(confirmCalls).toContainEqual([1]);
      expect(await store.getState(scope.run_id, created.state.turn_id)).toMatchObject({
        status: "unknown",
        output: "local-only",
      });
      expect(await store.durableHandoffCandidates()).toEqual([]);
      expect((await store.unpublishedEvents(scope.run_id, created.state.turn_id)).map((event) => event.sequence)).toEqual([1, 2, 3, 4]);
    } finally {
      await manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("keeps listening after one leftover durable handoff fails to recover", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-handoff-isolation-"));
    let manager: PiRuntimeManager | undefined;
    try {
      const store = new TurnStore(root);
      await store.init();
      const broken = await store.createTurn(scope, {
        input_text: "broken leftover",
        asset_ids: [],
        idempotency_key: "broken-handoff",
        page_context: null,
      });
      const healthy = await store.createTurn(scope, {
        input_text: "healthy leftover",
        asset_ids: [],
        idempotency_key: "healthy-handoff",
        page_context: null,
      });
      const brokenLease = {
        execution_id: "execution-broken",
        projection_id: "projection-broken",
        harness_turn_id: broken.state.turn_id,
        owner_id: "owner-isolation",
        lease_token: "lease-broken",
        attempt: 1,
        fencing_token: 1,
        phase: "model" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      const healthyLease = {
        ...brokenLease,
        execution_id: "execution-healthy",
        projection_id: "projection-healthy",
        harness_turn_id: healthy.state.turn_id,
        lease_token: "lease-healthy",
      };
      await store.appendLocalEvent(scope.run_id, broken.state.turn_id, "text.chunk", { delta: "broken" });
      await store.appendLocalEvent(scope.run_id, healthy.state.turn_id, "question/requested", {
        id: "q-healthy", header: "确认", question: "继续吗？", options: [{ label: "继续" }],
      });
      await store.updateState(scope.run_id, broken.state.turn_id, {
        status: "running", execution_attempt: 1, execution_fencing_token: 1,
      });
      await store.updateState(scope.run_id, healthy.state.turn_id, {
        status: "requires_input",
        execution_attempt: 1,
        execution_fencing_token: 1,
        question: { id: "q-healthy", header: "确认", question: "继续吗？", options: [{ label: "继续" }] },
      });
      await store.saveDurableHandoff(scope.run_id, broken.state.turn_id, brokenLease);
      await store.saveDurableHandoff(scope.run_id, healthy.state.turn_id, healthyLease);
      const productFlow = {
        confirmTurnEvents: async (_conversationID: string, executionID: string, args: { events: Array<{ sequence: number }> }) => {
          if (executionID === brokenLease.execution_id) {
            throw new ProductFlowError(500, "upstream_error", "ProductFlow request failed");
          }
          return {
            status: args.events.length === 0 ? "confirmed" as const : "missing" as const,
            confirmed_through: (args.events[0]?.sequence ?? 1) - 1,
            persisted_through: 0,
            items: [],
          };
        },
        claimTurnExecution: async (_conversationID: string, args: { idempotency_key: string }) => (
          args.idempotency_key === "healthy-handoff" ? healthyLease : brokenLease
        ),
        heartbeatTurnExecution: async (_conversationID: string, executionID: string, args: { phase: string }) => ({
          ...(executionID === healthyLease.execution_id ? healthyLease : brokenLease),
          phase: args.phase,
        }),
        appendTurnEvents: async (_conversationID: string, executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => (
          args.events.map((event) => ({
            id: `event-${executionID}-${event.sequence}`,
            projection_id: executionID === healthyLease.execution_id ? healthyLease.projection_id : brokenLease.projection_id,
            execution_id: executionID,
            sequence: event.sequence, schema_version: 1 as const, kind: event.kind, ignorable: false,
            created_at: "2026-08-31T00:00:00.000Z",
          }))
        ),
        releaseTurnExecution: async () => ({ released: true }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      manager = new PiRuntimeManager(
        { ...config, dataRoot: root }, store, productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3], brokenLease.owner_id,
      );
      await expect(manager.recoverAfterRestart()).resolves.toMatchObject({ replayed_handoffs: 1 });
      expect(await store.getState(scope.run_id, healthy.state.turn_id)).toMatchObject({ status: "requires_input" });
      expect((await store.durableHandoffCandidates()).map((candidate) => candidate.turnID)).toEqual([broken.state.turn_id]);
    } finally {
      await manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("retries a failed terminal handoff in-process after PostgreSQL recovers", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-in-process-handoff-retry-"));
    let manager: PiRuntimeManager | undefined;
    try {
      const store = new TurnStore(root);
      await store.init();
      const created = await store.createTurn(scope, {
        input_text: "retry without restart",
        asset_ids: [],
        idempotency_key: "in-process-handoff-retry",
        page_context: null,
      });
      const lease = {
        execution_id: "execution-retry",
        projection_id: "projection-retry",
        harness_turn_id: created.state.turn_id,
        owner_id: "owner-retry",
        lease_token: "lease-retry",
        attempt: 1,
        fencing_token: 1,
        phase: "model" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      let appendCalls = 0;
      const productFlow = {
        appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => {
          appendCalls += 1;
          if (appendCalls === 1) throw new Error("temporary PostgreSQL outage");
          return args.events.map((event) => ({
            id: `event-${event.sequence}`, projection_id: lease.projection_id, execution_id: lease.execution_id,
            sequence: event.sequence, schema_version: 1 as const, kind: event.kind, ignorable: false,
            created_at: "2026-08-31T00:00:00.000Z",
          }));
        },
        confirmTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number }> }) => ({
          status: args.events.length === 0 ? "confirmed" as const : "missing" as const,
          confirmed_through: (args.events[0]?.sequence ?? 1) - 1,
          persisted_through: 0,
          items: [],
        }),
        claimTurnExecution: async () => lease,
        heartbeatTurnExecution: async (_conversationID: string, _executionID: string, args: { phase: typeof lease.phase }) => ({ ...lease, phase: args.phase }),
        releaseTurnExecution: async () => ({ released: true }),
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      manager = new PiRuntimeManager(
        { ...config, dataRoot: root }, store, productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3], lease.owner_id,
      );
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "text.chunk", { delta: "eventual result" });
      const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      const internal = runtime as {
        currentTurn: string;
        executionLease?: typeof lease;
        finishTurn(turnID: string, status: "succeeded", details: { output: string }): Promise<void>;
        cleanupAfterTurn(): Promise<void>;
      };
      internal.currentTurn = created.state.turn_id;
      internal.executionLease = lease;
      await store.updateState(scope.run_id, created.state.turn_id, {
        status: "running", execution_attempt: 1, execution_fencing_token: 1,
      });
      await store.saveDurableHandoff(scope.run_id, created.state.turn_id, lease);
      await internal.finishTurn(created.state.turn_id, "succeeded", { output: "eventual result" });
      await internal.cleanupAfterTurn();

      for (let attempt = 0; attempt < 100; attempt += 1) {
        if ((await store.getState(scope.run_id, created.state.turn_id)).status === "succeeded") break;
        await new Promise<void>((resolve) => setTimeout(resolve, 25));
      }
      expect(await store.getState(scope.run_id, created.state.turn_id)).toMatchObject({
        status: "succeeded", output: "eventual result",
      });
      expect(appendCalls).toBeGreaterThanOrEqual(2);
      expect(await store.durableHandoffCandidates()).toEqual([]);
    } finally {
      await manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

  it("leaves a recovered in-flight handoff for Go lease expiry", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-handoff-claim-conflict-"));
    let manager: PiRuntimeManager | undefined;
    try {
      const store = new TurnStore(root);
      await store.init();
      const created = await store.createTurn(scope, {
        input_text: "recover after competing lease",
        asset_ids: [],
        idempotency_key: "handoff-claim-conflict",
        page_context: null,
      });
      const lease = {
        execution_id: "execution-claim-conflict",
        projection_id: "projection-claim-conflict",
        harness_turn_id: created.state.turn_id,
        owner_id: "stable-owner",
        lease_token: "lease-claim-conflict",
        attempt: 1,
        fencing_token: 1,
        phase: "model" as const,
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      };
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "text.chunk", { delta: "partial" });
      await store.updateState(scope.run_id, created.state.turn_id, {
        status: "running", execution_attempt: 1, execution_fencing_token: 1,
      });
      await store.saveDurableHandoff(scope.run_id, created.state.turn_id, lease);
      let claimCalls = 0;
      const releasePhases: string[] = [];
      const productFlow = {
        confirmTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number }> }) => ({
          status: args.events.length === 0 ? "confirmed" as const : "missing" as const,
          confirmed_through: (args.events[0]?.sequence ?? 1) - 1,
          persisted_through: 0,
          items: [],
        }),
        claimTurnExecution: async () => {
          claimCalls += 1;
          return lease;
        },
        heartbeatTurnExecution: async (_conversationID: string, _executionID: string, args: { phase: typeof lease.phase }) => ({ ...lease, phase: args.phase }),
        appendTurnEvents: async (_conversationID: string, _executionID: string, args: { events: Array<{ sequence: number; kind: string }> }) => args.events.map((event) => ({
          id: `event-${event.sequence}`, projection_id: lease.projection_id, execution_id: lease.execution_id,
          sequence: event.sequence, schema_version: 1 as const, kind: event.kind, ignorable: false,
          created_at: "2026-08-31T00:00:00.000Z",
        })),
        releaseTurnExecution: async (_conversationID: string, _executionID: string, args: { phase: string }) => {
          releasePhases.push(args.phase);
          return { released: true };
        },
      } as unknown as ConstructorParameters<typeof PiRuntimeManager>[2];
      manager = new PiRuntimeManager(
        { ...config, dataRoot: root, maxConcurrentTurns: 1 }, store, productFlow,
        {} as ConstructorParameters<typeof PiRuntimeManager>[3], lease.owner_id,
      );

      const runtime = await (manager as unknown as { runtimeFor(input: Scope): Promise<unknown> }).runtimeFor(scope);
      await expect((runtime as {
        recoverDurableHandoff(turnID: string, key: string, executionID: string, projectionID: string): Promise<boolean>;
      }).recoverDurableHandoff(created.state.turn_id, "handoff-claim-conflict", lease.execution_id, lease.projection_id)).resolves.toBe(true);
      expect(claimCalls).toBe(1);
      expect(await store.getState(scope.run_id, created.state.turn_id)).toMatchObject({ status: "running", output: "" });
      expect((await store.events(scope.run_id, created.state.turn_id, 0)).map((event) => event.kind)).toEqual(["text.chunk"]);
      expect((await store.durableHandoffCandidates()).map((candidate) => candidate.turnID)).toEqual([created.state.turn_id]);
      expect(releasePhases).toEqual([]);
      expect(manager.health().active_turns).toBe(0);
    } finally {
      await manager?.close();
      await rm(root, { recursive: true, force: true });
    }
  });

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
      const second = { ...question, id: "question-reference", question: "参考图是哪张？" };
      await store.updateState(scope.run_id, created.state.turn_id, { status: "requires_input", question: second });
      await store.appendEvent(scope.run_id, created.state.turn_id, "question/requested", second);
      const nextAnswer = await manager.answerQuestion(
        { conversationID: scope.conversation_id }, created.state.turn_id, second.id, { text: "第二张图" },
      );
      expect(nextAnswer.status).toBe("queued");
      expect((await store.events(scope.run_id, created.state.turn_id, 0))
        .filter((event) => event.kind === "question/answered").map((event) => event.payload)).toEqual([
          { question_id: question.id, answer: { text: "筋膜枪" } },
          { question_id: second.id, answer: { text: "第二张图" } },
        ]);
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

      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "question/requested", {
        id: "earlier-question", header: "前一问", question: "已回答？", options: [{ label: "继续" }, { label: "停止" }],
      });
      await store.appendLocalEvent(scope.run_id, created.state.turn_id, "question/answered", {
        question_id: "earlier-question", answer: { text: "already answered" },
      });

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
