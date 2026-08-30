import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import type { Scope, StartTurnInput } from "./contracts.js";
import { TurnStore } from "./store.js";

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

const input: StartTurnInput = {
  input_text: "Create a workflow",
  asset_ids: [],
  idempotency_key: "turn-1",
  page_context: null,
};

describe("TurnStore", () => {
  it("replays an idempotent turn and rejects a conflicting payload", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-store-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const first = await store.createTurn(scope, input);
      const replay = await store.createTurn(scope, input);
      expect(replay.created).toBe(false);
      expect(replay.state.turn_id).toBe(first.state.turn_id);
      await expect(
        store.createTurn(scope, { ...input, input_text: "different" }),
      ).rejects.toMatchObject({ code: "idempotency_conflict", status: 409 });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("allows the ProductFlow draft observation to advance within one runtime scope", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-scope-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      await store.ensureRun(scope);
      await expect(store.ensureRun({ ...scope, current_draft_version: 2 })).resolves.toMatchObject({
        scope: { run_id: scope.run_id },
      });
      await expect(
        store.ensureRun({
          ...scope,
          current_draft_version: 3,
          system_prompt: "live graph collaboration",
          has_live_graph: true,
        }),
      ).resolves.toMatchObject({
        scope: { run_id: scope.run_id },
      });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("requeues untouched queued turns and marks interrupted turns unknown", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-recovery-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const queuedScope = { ...scope, run_id: "queued-run" };
      const runningScope = { ...scope, run_id: "running-run" };
      const waitingScope = { ...scope, run_id: "waiting-run" };
      const queued = await store.createTurn(queuedScope, input);
      const running = await store.createTurn(runningScope, input);
      await store.updateState(runningScope.run_id, running.state.turn_id, {
        status: "running",
        tool_steps: [{ step_id: "step-1", kind: "inspect_context", summary: "Read context", status: "running" }],
      });
      const waiting = await store.createTurn(waitingScope, input);
      const question = {
        id: "question-restart-1",
        header: "Missing context",
        question: "Which language should the image use?",
        options: [{ label: "Chinese" }],
      };
      await store.updateState(waitingScope.run_id, waiting.state.turn_id, { status: "requires_input", question });
      await store.appendEvent(waitingScope.run_id, waiting.state.turn_id, "turn.requires_input", {
        status: "requires_input",
        question,
      });

      const result = await store.recoverAfterRestart();

      expect(result.queued).toEqual([{ scope: queuedScope, turnID: queued.state.turn_id }]);
      expect(result.waitingInput).toBe(1);
      expect(result.unknown).toBe(1);
      const recoveredRunning = await store.getState(runningScope.run_id, running.state.turn_id);
      expect(recoveredRunning.status).toBe("unknown");
      expect(recoveredRunning.tool_steps?.[0]?.status).toBe("unknown");
      expect((await store.getState(waitingScope.run_id, waiting.state.turn_id)).status).toBe("requires_input");
      expect((await store.events(runningScope.run_id, running.state.turn_id, 0)).at(-1)?.kind).toBe("turn.unknown");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("defers a queued snapshot that already has a durable execution identity", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-deferred-recovery-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn({ ...scope, run_id: "deferred-run" }, input);
      await store.updateState("deferred-run", turn.state.turn_id, {
        execution_attempt: 1,
        execution_fencing_token: 1,
      });

      const result = await store.recoverAfterRestart();

      expect(result.queued).toEqual([]);
      expect(result.deferred).toBe(1);
      expect(result.unknown).toBe(0);
      await expect(store.getState("deferred-run", turn.state.turn_id)).resolves.toMatchObject({
        status: "queued",
        execution_attempt: 1,
        execution_fencing_token: 1,
      });
      expect((await store.events("deferred-run", turn.state.turn_id, 0)).map((event) => event.kind)).toEqual(["turn.queued"]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("materializes a safe queued handoff with the ProductFlow harness Turn ID", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-handoff-"));
    try {
      const store = new TurnStore(root);
      await store.init();

      const adopted = await store.createTurn(scope, input, "handoff-turn-1");
      const replay = await store.createTurn(scope, input, "handoff-turn-1");

      expect(adopted.created).toBe(true);
      expect(adopted.state.turn_id).toBe("handoff-turn-1");
      expect(replay).toMatchObject({ created: false, state: { turn_id: "handoff-turn-1", status: "queued" } });
      await expect(store.createTurn(scope, input, "different-turn")).rejects.toMatchObject({
        code: "turn_identity_conflict",
        status: 409,
      });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("restores a terminal event when the state write was interrupted", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-terminal-recovery-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, input);
      await store.appendEvent(scope.run_id, turn.state.turn_id, "turn.awaiting_confirmation", {
        status: "awaiting_confirmation",
        output: "recovered",
        error: "",
        artifact: {
          name: "propose_global_draft",
          value: { schema_version: 2 },
          step_id: "step-1",
        },
      });

      const result = await store.recoverAfterRestart();
      const state = await store.getState(scope.run_id, turn.state.turn_id);

      expect(result.restoredTerminal).toBe(1);
      expect(state.status).toBe("awaiting_confirmation");
      expect(state.output).toBe("recovered");
      expect(state.artifact).toEqual({
        name: "propose_global_draft",
        value: { schema_version: 2 },
        step_id: "step-1",
      });
      expect(state.finished_at).toBeTruthy();
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("serializes event sequence writes for one turn", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-events-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, input);
      await Promise.all(
        Array.from({ length: 20 }, (_, index) =>
          store.appendEvent(scope.run_id, turn.state.turn_id, "test.event", { index }),
        ),
      );
      const events = await store.events(scope.run_id, turn.state.turn_id, 0);
      expect(events.map((event) => event.sequence)).toEqual(Array.from({ length: 21 }, (_, index) => index + 1));
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("publishes terminal events before terminal state and wakes event subscribers", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-terminal-events-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, input);
      const waiting = store.waitForEvents(scope.run_id, turn.state.turn_id, 1, { timeoutMS: 1_000 });
      const terminal = await store.terminal(scope.run_id, turn.state.turn_id, "canceled", { output: "partial" });

      expect(terminal.status).toBe("canceled");
      expect((await waiting)).toBe(true);
      expect((await store.events(scope.run_id, turn.state.turn_id, 1)).map((event) => event.kind)).toEqual([
        "turn.canceled",
      ]);
      expect((await store.getState(scope.run_id, turn.state.turn_id)).status).toBe("canceled");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("keeps live deltas in the local journal without publishing them to ProductFlow", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-live-events-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const published: string[] = [];
      store.setEventPublisher(async (_scope, event) => {
        published.push(event.kind);
      });
      const turn = await store.createTurn(scope, input);
      const waiting = store.waitForEvents(scope.run_id, turn.state.turn_id, 1, { timeoutMS: 1_000 });
      await store.appendEvent(scope.run_id, turn.state.turn_id, "text.delta", {
        delta: "hi",
        step_id: "step-1",
        attempt_id: "attempt-1",
      });
      expect(await waiting).toBe(true);
      expect((await store.events(scope.run_id, turn.state.turn_id, 1)).map((event) => event.kind)).toEqual(["text.delta"]);
      await store.appendEvent(scope.run_id, turn.state.turn_id, "tool.step", {
        step_id: "step-1",
        kind: "inspect_context",
        summary: "读取上下文",
        status: "succeeded",
      });
      expect(published).toEqual(["turn.queued", "tool.step"]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("serializes concurrent turn creation and preserves idempotency", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-concurrent-turns-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const results = await Promise.all(Array.from({ length: 8 }, () => store.createTurn(scope, input)));
      expect(results.filter((result) => result.created)).toHaveLength(1);
      expect(new Set(results.map((result) => result.state.turn_id)).size).toBe(1);
      expect((await store.loadRun(scope.run_id)).turn_ids).toHaveLength(1);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
