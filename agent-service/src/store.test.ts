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
