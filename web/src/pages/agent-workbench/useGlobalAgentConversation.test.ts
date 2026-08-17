import { describe, expect, it } from "vitest";

import { initialGlobalTaskTurnInput } from "./useGlobalAgentConversation";

describe("global Agent Task bootstrap input", () => {
  it("uses the task goal and a task-scoped idempotency key", () => {
    const input = initialGlobalTaskTurnInput(
      "global-conversation",
      "task-1",
      "整理最近生成的场景图",
      {
        route: "/media-library",
        page_type: "media_library",
        product_id: null,
        workflow_id: null,
        selected_asset_ids: ["asset-1"],
        visible_asset_ids: ["asset-1", "asset-2"],
        filters: { tag: "scene" },
        workflow_revision: null,
        library_revision: null,
        captured_at: "2026-08-18T00:00:00.000Z",
      },
    );

    expect(input.input_text).toBe("整理最近生成的场景图");
    expect(input.asset_ids).toEqual([]);
    expect(input.task_id).toBe("task-1");
    expect(input.idempotency_key).toBe("initial:global-conversation:task-1");
    expect(input.page_context).toEqual(expect.objectContaining({
      route: "/media-library",
      selected_asset_ids: ["asset-1"],
    }));
  });
});
