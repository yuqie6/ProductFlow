import { describe, expect, it } from "vitest";
import {
  applyRetryBranch,
  prepareSessionForTurn,
  retryBranchAction,
  retrySourceTurnId,
  type SessionEntryView,
} from "./session-retry.js";

function entry(overrides: Partial<SessionEntryView> & Pick<SessionEntryView, "id">): SessionEntryView {
  return {
    parentId: null,
    type: "custom",
    ...overrides,
  };
}

describe("session retry branch", () => {
  it("parses the source Turn id from a retry idempotency key", () => {
    expect(retrySourceTurnId("retry:projection-1:attempt-2")).toBe("projection-1");
    expect(retrySourceTurnId("ordinary-key")).toBeNull();
  });

  it("does not move the leaf on an ordinary later Turn", () => {
    expect(retryBranchAction({
      idempotencyKey: "turn-2",
      inputText: "第二问",
      entries: [],
      leafId: "leaf-1",
    })).toEqual({ kind: "none" });
  });

  it("resets the leaf when retrying the first Turn marker", () => {
    const marker = entry({
      id: "mark-1",
      type: "custom",
      customType: "productflow.turn",
      data: { turn_id: "harness-1", projection_id: "projection-1" },
    });
    expect(retryBranchAction({
      idempotencyKey: "retry:projection-1:attempt-2",
      inputText: "第一问",
      entries: [marker],
      leafId: "mark-1",
    })).toEqual({ kind: "reset" });
  });

  it("branches to the parent of an earlier Turn marker so later history is off the leaf", () => {
    const first = entry({
      id: "mark-1",
      type: "custom",
      customType: "productflow.turn",
      data: { turn_id: "harness-1", projection_id: "projection-1" },
    });
    const user1 = entry({
      id: "user-1",
      parentId: "mark-1",
      type: "message",
      messageRole: "user",
      messageText: "第一问",
    });
    const second = entry({
      id: "mark-2",
      parentId: "user-1",
      type: "custom",
      customType: "productflow.turn",
      data: { turn_id: "harness-2", projection_id: "projection-2" },
    });
    const user2 = entry({
      id: "user-2",
      parentId: "mark-2",
      type: "message",
      messageRole: "user",
      messageText: "第二问",
    });
    expect(retryBranchAction({
      idempotencyKey: "retry:projection-1:attempt-2",
      inputText: "第一问",
      entries: [first, user1, second, user2],
      leafId: "user-2",
    })).toEqual({ kind: "reset" });
    expect(retryBranchAction({
      idempotencyKey: "retry:projection-2:attempt-2",
      inputText: "第二问",
      entries: [first, user1, second, user2],
      leafId: "user-2",
    })).toEqual({ kind: "branch", entryId: "user-1" });
  });

  it("falls back to the first matching user message when the session has no Turn markers", () => {
    const user1 = entry({
      id: "user-1",
      type: "message",
      messageRole: "user",
      messageText: "第一问",
    });
    const user2 = entry({
      id: "user-2",
      parentId: "user-1",
      type: "message",
      messageRole: "user",
      messageText: "第二问",
    });
    expect(retryBranchAction({
      idempotencyKey: "retry:projection-1:attempt-2",
      inputText: "第一问",
      entries: [user1, user2],
      leafId: "user-2",
    })).toEqual({ kind: "reset" });
  });

  it("applies the cut then tags the new Turn on the retry leaf", () => {
    const entries: SessionEntryView[] = [
      entry({
        id: "mark-1",
        type: "custom",
        customType: "productflow.turn",
        data: { turn_id: "harness-1", projection_id: "projection-1" },
      }),
      entry({
        id: "user-1",
        parentId: "mark-1",
        type: "message",
        messageRole: "user",
        messageText: "第一问",
      }),
    ];
    let leafId: string | null = "user-1";
    const tagged: unknown[] = [];
    const manager = {
      branch(id: string) { leafId = id; },
      resetLeaf() { leafId = null; },
      appendCustomEntry(customType: string, data?: unknown) {
        tagged.push({ customType, data, parentId: leafId });
        return "mark-2";
      },
      getEntries() { return entries; },
      getLeafId() { return leafId; },
    };
    const action = prepareSessionForTurn(manager, {
      turnId: "harness-2",
      projectionId: "projection-2",
      idempotencyKey: "retry:projection-1:attempt-2",
      inputText: "第一问",
    });
    expect(action).toEqual({ kind: "reset" });
    expect(leafId).toBeNull();
    expect(tagged).toEqual([{
      customType: "productflow.turn",
      data: { turn_id: "harness-2", projection_id: "projection-2" },
      parentId: null,
    }]);
  });

  it("keeps a missing branch target from moving the leaf", () => {
    const calls: string[] = [];
    applyRetryBranch({
      branch(id) { calls.push(id); },
      resetLeaf() { calls.push("reset"); },
    }, { kind: "none" });
    expect(calls).toEqual([]);
  });
});
