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
  workflow_draft_id: "33333333-3333-4333-8333-333333333333",
  run_id: "44444444-4444-4444-8444-444444444444",
  system_prompt: "ProductFlow",
  draft_schema: { type: "object" },
  workflow_draft_schema: { type: "object" },
  current_draft_version: 1,
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
      await store.updateState(waitingScope.run_id, waiting.state.turn_id, { status: "requires_input" });

      const result = await store.recoverAfterRestart();

      expect(result.queued).toEqual([{ scope: queuedScope, turnID: queued.state.turn_id }]);
      expect(result.unknown).toBe(2);
      const recoveredRunning = await store.getState(runningScope.run_id, running.state.turn_id);
      expect(recoveredRunning.status).toBe("unknown");
      expect(recoveredRunning.tool_steps?.[0]?.status).toBe("unknown");
      expect((await store.getState(waitingScope.run_id, waiting.state.turn_id)).status).toBe("unknown");
      expect((await store.events(runningScope.run_id, running.state.turn_id, 0)).at(-1)?.kind).toBe("turn.unknown");
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
          name: "propose_workflow_draft",
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
        name: "propose_workflow_draft",
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
