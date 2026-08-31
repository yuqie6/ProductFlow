import { describe, expect, it } from "vitest";
import type { TurnEvent } from "./contracts.js";
import { JournalEventBatcher, isJournalFlushBarrier } from "./journal-publisher.js";

function event(sequence: number, kind = "text.chunk"): TurnEvent {
  return {
    schema_version: 1,
    run_id: "run-1",
    turn_id: "turn-1",
    sequence,
    created_at: "2026-08-31T00:00:00.000Z",
    kind,
    payload: kind === "text.chunk" ? { delta: String(sequence) } : { status: "succeeded" },
  };
}

describe("JournalEventBatcher", () => {
  it("merges multiple events until a terminal barrier drains them", async () => {
    const batches: number[][] = [];
    const publisher = new JournalEventBatcher(async (events) => {
      batches.push(events.map((item) => item.sequence));
    }, { flushMS: 1_000 });

    await publisher.enqueue(event(1));
    await publisher.enqueue(event(2));
    await publisher.enqueue(event(3, "turn/end"), true);

    expect(batches).toEqual([[1, 2, 3]]);
    expect(publisher.pendingCount).toBe(0);
  });

  it("flushes at the configured quantity bound", async () => {
    const batches: number[][] = [];
    const publisher = new JournalEventBatcher(async (events) => {
      batches.push(events.map((item) => item.sequence));
    }, { flushMS: 1_000, maxEvents: 2 });

    await publisher.enqueue(event(1));
    await publisher.enqueue(event(2));

    expect(batches).toEqual([[1, 2]]);
  });

  it("flushes a time-bounded batch without a structural barrier", async () => {
    const batches: number[][] = [];
    const publisher = new JournalEventBatcher(async (events) => {
      batches.push(events.map((item) => item.sequence));
    }, { flushMS: 5 });

    await publisher.enqueue(event(1));
    await publisher.enqueue(event(2));
    await new Promise<void>((resolve) => setTimeout(resolve, 20));

    expect(batches).toEqual([[1, 2]]);
  });

  it("keeps a failed transactional batch at the head for an ordered retry", async () => {
    const attempts: number[][] = [];
    let fail = true;
    const publisher = new JournalEventBatcher(async (events) => {
      attempts.push(events.map((item) => item.sequence));
      if (fail) throw new Error("database unavailable");
    }, { flushMS: 1_000 });

    await publisher.enqueue(event(1));
    await expect(publisher.enqueue(event(2, "tool/result"), true)).rejects.toThrow("database unavailable");
    expect(publisher.pendingSequences()).toEqual([1, 2]);

    fail = false;
    await publisher.drain();
    expect(attempts).toEqual([[1, 2], [1, 2]]);
    expect(publisher.pendingCount).toBe(0);
  });

  it("classifies tool, question, approval, message and terminal events as barriers", () => {
    expect([
      "tool/call",
      "tool/result",
      "question/requested",
      "question/answered",
      "approval/requested",
      "approval/resolved",
      "assistant/message",
      "turn/end",
    ].every(isJournalFlushBarrier)).toBe(true);
    expect(isJournalFlushBarrier("text.chunk")).toBe(false);
    expect(isJournalFlushBarrier("thinking.chunk")).toBe(false);
  });
});
