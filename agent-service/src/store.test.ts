import { appendFile, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import type { Scope, StartTurnInput } from "./contracts.js";
import { TurnStore } from "./store.js";
import { JOURNAL_EVENT_MAX_PAYLOAD_BYTES, JOURNAL_EVENT_MAX_SEQUENCE } from "./pi-chunks.js";

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
  it.runIf(process.env.PRODUCTFLOW_RUN_AGENT_LOCAL_JOURNAL_CAPACITY === "1")(
    "appends and reloads 10k durable WAL events without quadratic rewrite",
    async () => {
      const root = await mkdtemp(join(tmpdir(), "productflow-pi-local-journal-capacity-"));
      try {
        const store = new TurnStore(root);
        await store.init();
        const turn = await store.createTurn(scope, { ...input, idempotency_key: "local-journal-capacity" });
        const latencies: number[] = [];
        const startedAt = performance.now();
        for (let index = 0; index < 10_000; index += 1) {
          const eventStartedAt = performance.now();
          await store.appendLocalEvent(scope.run_id, turn.state.turn_id, "text.chunk", {
            delta: `chunk-${index.toString().padStart(5, "0")}-${"x".repeat(1000)}`,
            attempt_id: "capacity",
            step_id: "capacity",
            content_index: index,
          });
          latencies.push(performance.now() - eventStartedAt);
        }
        const durationMS = performance.now() - startedAt;
        latencies.sort((left, right) => left - right);
        const p95MS = latencies[Math.floor(latencies.length * 0.95)]!;
        expect(durationMS).toBeLessThan(120_000);
        expect(p95MS).toBeLessThan(50);

        const restarted = new TurnStore(root);
        await restarted.init();
        const events = await restarted.events(scope.run_id, turn.state.turn_id, 0);
        expect(events).toHaveLength(10_000);
        expect(events[0]?.sequence).toBe(1);
        expect(events.at(-1)?.sequence).toBe(10_000);
        process.stdout.write(`local journal 10k duration=${durationMS.toFixed(1)}ms p95=${p95MS.toFixed(2)}ms\n`);
      } finally {
        await rm(root, { recursive: true, force: true });
      }
    },
    150_000,
  );

  it("persists one publisher identity and rejects a second live process lock", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-owner-"));
    try {
      const first = new TurnStore(root);
      const second = new TurnStore(root);
      await first.init();
      await second.init();
      expect(await second.publisherID()).toBe(await first.publisherID());

      const lock = await first.acquireProcessLock();
      await expect(second.acquireProcessLock()).rejects.toMatchObject({ code: "data_root_locked", status: 409 });
      await lock.release();
      const lockAgain = await second.acquireProcessLock();
      await lockAgain.release();
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("reports an unexpected advisory-lock process exit and releases ownership", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-owner-loss-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      let resolveLost!: (error: Error) => void;
      const lost = new Promise<Error>((resolve) => {
        resolveLost = resolve;
      });
      const lock = await store.acquireProcessLock(resolveLost);
      process.kill(lock.processID, "SIGKILL");
      expect((await lost).message).toMatch(/lock was lost/u);

      const replacement = await store.acquireProcessLock();
      await replacement.release();
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("persists an independent PG ACK watermark and rebuilds the unpublished tail", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-event-ack-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, input);
      await store.appendLocalEvent(scope.run_id, turn.state.turn_id, "text.chunk", { delta: "one" });
      await store.appendLocalEvent(scope.run_id, turn.state.turn_id, "text.chunk", { delta: "two" });
      expect((await store.unpublishedEvents(scope.run_id, turn.state.turn_id)).map((event) => event.sequence)).toEqual([1, 2]);

      await store.markEventsPublished(scope.run_id, turn.state.turn_id, 1);
      const restarted = new TurnStore(root);
      await restarted.init();
      expect((await restarted.unpublishedEvents(scope.run_id, turn.state.turn_id)).map((event) => event.sequence)).toEqual([2]);
      await restarted.markEventsPublished(scope.run_id, turn.state.turn_id, 2);
      expect(await restarted.unpublishedEvents(scope.run_id, turn.state.turn_id)).toEqual([]);
      await expect(restarted.markEventsPublished(scope.run_id, turn.state.turn_id, 3)).rejects.toMatchObject({
        code: "published_sequence_invalid",
        status: 409,
      });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("recovers a handoff killed before its first journal event", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-empty-handoff-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, { ...input, idempotency_key: "empty-handoff" });
      await store.updateState(scope.run_id, turn.state.turn_id, {
        execution_attempt: 1,
        execution_fencing_token: 1,
      });
      await store.saveDurableHandoff(scope.run_id, turn.state.turn_id, {
        execution_id: "execution-empty",
        projection_id: "projection-empty",
        harness_turn_id: turn.state.turn_id,
        owner_id: "owner-empty",
        lease_token: "lease-empty",
        attempt: 1,
        fencing_token: 1,
        phase: "claimed",
        lease_expires_at: "2099-01-01T00:00:00.000Z",
      });

      const restarted = new TurnStore(root);
      await restarted.init();
      expect((await restarted.durableHandoffCandidates()).map((candidate) => candidate.turnID)).toEqual([turn.state.turn_id]);
      expect(await restarted.unpublishedEvents(scope.run_id, turn.state.turn_id)).toEqual([]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("removes only a local persistence fallback after PG confirms the preceding terminal", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-confirmed-terminal-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, { ...input, idempotency_key: "confirmed-terminal" });
      await store.appendLocalEvent(scope.run_id, turn.state.turn_id, "turn/end", {
        status: "succeeded",
        reason: "completed",
      });
      await store.markEventsPublished(scope.run_id, turn.state.turn_id, 1);
      await store.appendLocalEvent(scope.run_id, turn.state.turn_id, "turn/end", {
        status: "unknown",
        reason: "unknown",
        reason_code: "persistence_failed",
        error: "response was lost",
      });
      await store.updateState(scope.run_id, turn.state.turn_id, {
        status: "unknown",
        error: "response was lost",
      });

      expect(await store.reconcileConfirmedTerminal(scope.run_id, turn.state.turn_id)).toBe(true);
      expect(await store.getState(scope.run_id, turn.state.turn_id)).toMatchObject({ status: "succeeded", error: "" });
      expect((await store.events(scope.run_id, turn.state.turn_id, 0)).map((event) => event.sequence)).toEqual([1]);
      const restarted = new TurnStore(root);
      await restarted.init();
      expect((await restarted.events(scope.run_id, turn.state.turn_id, 0)).map((event) => event.sequence)).toEqual([1]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("truncates only a torn final WAL record and rejects corrupt completed records", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-torn-wal-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, { ...input, idempotency_key: "torn-wal" });
      await store.appendLocalEvent(scope.run_id, turn.state.turn_id, "text.chunk", { delta: "complete" });
      const path = join(root, "runs", scope.run_id, "events", `${turn.state.turn_id}.jsonl`);
      await appendFile(path, '{"schema_version":1,"sequence":2');

      const restarted = new TurnStore(root);
      await restarted.init();
      expect((await restarted.events(scope.run_id, turn.state.turn_id, 0)).map((event) => event.sequence)).toEqual([1]);
      expect(await readFile(path, "utf8")).toMatch(/\n$/u);

      await appendFile(path, '{"invalid":}\n');
      const corrupt = new TurnStore(root);
      await corrupt.init();
      await expect(corrupt.events(scope.run_id, turn.state.turn_id, 0)).rejects.toMatchObject({
        code: "event_journal_corrupt",
        status: 409,
      });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

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
      await store.appendEvent(waitingScope.run_id, waiting.state.turn_id, "question/requested", question);

      const result = await store.recoverAfterRestart();

      expect(result.queued).toEqual([{ scope: queuedScope, turnID: queued.state.turn_id }]);
      expect(result.waitingInput).toBe(1);
      expect(result.unknown).toBe(1);
      const recoveredRunning = await store.getState(runningScope.run_id, running.state.turn_id);
      expect(recoveredRunning.status).toBe("unknown");
      expect(recoveredRunning.tool_steps?.[0]?.status).toBe("unknown");
      expect((await store.getState(waitingScope.run_id, waiting.state.turn_id)).status).toBe("requires_input");
      expect((await store.events(runningScope.run_id, running.state.turn_id, 0)).at(-1)?.kind).toBe("turn/end");
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
      expect((await store.events("deferred-run", turn.state.turn_id, 0)).map((event) => event.kind)).toEqual([]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("requeues a durable cancel intent even when an older state write preceded the event", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-cancel-recovery-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn({ ...scope, run_id: "cancel-recovery-run" }, input);
      await store.updateState("cancel-recovery-run", turn.state.turn_id, {
        status: "cancel_requested",
        execution_attempt: 1,
        execution_fencing_token: 1,
      });

      const result = await store.recoverAfterRestart();

      expect(result.queued).toEqual([{
        scope: { ...scope, run_id: "cancel-recovery-run" },
        turnID: turn.state.turn_id,
      }]);
      expect(result.deferred).toBe(0);
      expect((await store.events("cancel-recovery-run", turn.state.turn_id, 0)).map((event) => event.kind)).toEqual([
        "turn/cancel_requested",
      ]);
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
      await store.updateState(scope.run_id, turn.state.turn_id, {
        status: "running",
        output: "recovered",
      });
      await store.appendLocalEvent(scope.run_id, turn.state.turn_id, "text.chunk", { delta: "recovered" });
      await store.appendEvent(scope.run_id, turn.state.turn_id, "turn/end", {
        reason: "awaiting_confirmation",
        status: "awaiting_confirmation",
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
      expect((await store.events(scope.run_id, turn.state.turn_id, 0)).at(-1)?.payload).not.toHaveProperty("output");
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
      expect(events.map((event) => event.sequence)).toEqual(Array.from({ length: 20 }, (_, index) => index + 1));
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("persists terminal events before terminal state", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-terminal-events-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, input);
      const terminal = await store.terminal(scope.run_id, turn.state.turn_id, "canceled", { output: "partial" });

      expect(terminal.status).toBe("canceled");
      expect((await store.events(scope.run_id, turn.state.turn_id, 0)).map((event) => event.kind)).toEqual([
        "turn/end",
      ]);
      expect((await store.getState(scope.run_id, turn.state.turn_id)).status).toBe("canceled");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("publishes all journal events to ProductFlow", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-live-events-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const published: string[] = [];
      const persistedBeforePublish: boolean[] = [];
      store.setEventPublisher(async (_scope, event) => {
        persistedBeforePublish.push((await store.events(scope.run_id, event.turn_id, event.sequence - 1))[0]?.sequence === event.sequence);
        published.push(event.kind);
      });
      const turn = await store.createTurn(scope, input);
      await store.appendEvent(scope.run_id, turn.state.turn_id, "text.chunk", {
        delta: "hi",
        step_id: "step-1",
        attempt_id: "attempt-1",
      });
      expect((await store.events(scope.run_id, turn.state.turn_id, 0)).map((event) => event.kind)).toEqual(["text.chunk"]);
      await store.appendEvent(scope.run_id, turn.state.turn_id, "tool/result", {
        step_id: "step-1",
        kind: "inspect_context",
        summary: "读取上下文",
        status: "succeeded",
      });
      expect(published).toEqual(["text.chunk", "tool/result"]);
      expect(persistedBeforePublish).toEqual([true, true]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("keeps the append-only local sequence when ProductFlow rejects persistence", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-event-reject-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, input);
      store.setEventPublisher(async () => {
        throw new Error("ProductFlow unavailable");
      });

      await expect(store.appendEvent(scope.run_id, turn.state.turn_id, "text.chunk", {
        delta: "not durable",
      })).rejects.toThrow("ProductFlow unavailable");
      expect((await store.events(scope.run_id, turn.state.turn_id, 0)).map((event) => event.payload)).toEqual([
        { delta: "not durable" },
      ]);

      store.setEventPublisher(async () => undefined);
      const accepted = await store.appendEvent(scope.run_id, turn.state.turn_id, "text.chunk", {
        delta: "durable",
      });
      expect(accepted.sequence).toBe(2);
      expect((await store.events(scope.run_id, turn.state.turn_id, 0)).map((event) => event.payload)).toEqual([
        { delta: "not durable" },
        { delta: "durable" },
      ]);
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

  it("rejects an oversized payload before consuming a journal sequence", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-event-payload-limit-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, input);

      await expect(store.appendEvent(scope.run_id, turn.state.turn_id, "text.chunk", {
        delta: "x".repeat(JOURNAL_EVENT_MAX_PAYLOAD_BYTES),
      })).rejects.toMatchObject({ code: "event_payload_too_large", status: 413 });
      const accepted = await store.appendEvent(scope.run_id, turn.state.turn_id, "text.chunk", { delta: "ok" });

      expect(accepted.sequence).toBe(1);
      expect((await store.events(scope.run_id, turn.state.turn_id, 0))).toHaveLength(1);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("rejects a journal event after the PG sequence boundary", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-event-sequence-limit-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const turn = await store.createTurn(scope, input);
      const cache = (store as unknown as {
        eventCache: Map<string, { sequence: number; items: unknown[] }>;
      }).eventCache;
      cache.set(`${scope.run_id}:${turn.state.turn_id}`, {
        sequence: JOURNAL_EVENT_MAX_SEQUENCE,
        items: [],
      });

      await expect(store.appendEvent(scope.run_id, turn.state.turn_id, "text.chunk", { delta: "overflow" }))
        .rejects.toMatchObject({ code: "event_sequence_exhausted", status: 409 });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
