import { describe, expect, it } from "vitest";

import { agentTurnRetrySubmitInput, canRetryAgentTurn, groupAgentTurnAttempts } from "./agentTurnRetry";

describe("agentTurnRetry", () => {
  it("retries any Turn that still has the original input", () => {
    expect(canRetryAgentTurn({ turn: { input_text: "继续生成" } })).toBe(true);
    expect(canRetryAgentTurn({ turn: { input_text: "卖点和文案，你来生成" } })).toBe(true);
  });

  it("does not retry when the original input is empty", () => {
    expect(canRetryAgentTurn({ turn: { input_text: "  " } })).toBe(false);
    expect(canRetryAgentTurn({ turn: { input_text: "" } })).toBe(false);
  });

  it("resubmits the original input on a new idempotency key", () => {
    expect(
      agentTurnRetrySubmitInput(
        {
          input_text: "卖点和文案，你来生成",
          input_asset_ids: ["asset-1"],
          task_id: "task-1",
        },
        { idempotencyKey: "retry-key-1", pageContext: null },
      ),
    ).toEqual({
      input_text: "卖点和文案，你来生成",
      asset_ids: ["asset-1"],
      idempotency_key: "retry-key-1",
      task_id: "task-1",
      page_context: null,
    });
  });

  it("projects producer-correlated retries onto the original user bubble", () => {
    const original = {
      id: "turn-a",
      status: "failed" as const,
      input_text: "卖点和文案，你来生成",
      input_asset_ids: [] as string[],
      task_id: null,
      idempotency_key: "first",
    };
    const retry = {
      ...original,
      id: "turn-b",
      status: "running" as const,
      idempotency_key: "retry:turn-a:attempt-2",
    };
    const nested = {
      ...original,
      id: "turn-c",
      status: "succeeded" as const,
      idempotency_key: "retry:turn-b:attempt-3",
    };
    const groups = groupAgentTurnAttempts([
      original as never,
      retry as never,
      nested as never,
    ]);

    expect(groups).toHaveLength(1);
    expect(groups[0].root.id).toBe("turn-a");
    expect(groups[0].latest.id).toBe("turn-c");
    expect(groups[0].attempts.map((turn) => turn.id)).toEqual(["turn-a", "turn-b", "turn-c"]);
  });

  it("keeps unrelated prompts as separate bubbles", () => {
    const first = {
      id: "turn-a",
      status: "succeeded" as const,
      input_text: "第一问",
      input_asset_ids: [] as string[],
      task_id: null,
      idempotency_key: "first",
    };
    const second = {
      ...first,
      id: "turn-b",
      input_text: "第二问",
      idempotency_key: "second",
    };
    const groups = groupAgentTurnAttempts([first as never, second as never]);
    expect(groups).toHaveLength(2);
    expect(groups.map((group) => group.root.id)).toEqual(["turn-a", "turn-b"]);
  });
});
