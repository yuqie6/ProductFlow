import { describe, expect, it, vi } from "vitest";

import type { LocalImageEditTask } from "../../../lib/types";
import {
  continuedEditTargetNodeId,
  persistedDraftIdempotencyKey,
  revokeLocalImageEditObjectUrl,
  selectLatestActiveLocalImageEditTask,
  shouldPollLocalImageEditTask,
} from "./LocalImageEditController";

describe("LocalImageEditController recovery rules", () => {
  it("recovers draft and unknown tasks for the exact source/target without auto-retrying them", () => {
    const request = { sourceAssetId: "source-1", targetNodeId: "node-1" };
    const draft = makeTask({ id: "draft", status: "draft", updated_at: "2026-08-24T00:02:00Z" });
    const unknown = makeTask({ id: "unknown", status: "unknown", updated_at: "2026-08-24T00:03:00Z" });
    const wrongTarget = makeTask({ id: "wrong-target", status: "running", target_node_id: "node-2" });
    const terminal = makeTask({ id: "succeeded", status: "succeeded" });

    expect(selectLatestActiveLocalImageEditTask([draft, unknown, wrongTarget, terminal], request)).toBe(unknown);
  });

  it("derives a stable idempotency key for a recovered draft", () => {
    const task = makeTask({ id: "draft-1", revision: 7 });

    expect(persistedDraftIdempotencyKey(task)).toBe(persistedDraftIdempotencyKey({ ...task }));
    expect(persistedDraftIdempotencyKey(task)).toMatch(/^local-edit-draft-/);
    expect(persistedDraftIdempotencyKey(task)).not.toBe(persistedDraftIdempotencyKey({ ...task, revision: 8 }));
  });

  it("polls only durable active states", () => {
    expect(shouldPollLocalImageEditTask("queued")).toBe(true);
    expect(shouldPollLocalImageEditTask("running")).toBe(true);
    expect(shouldPollLocalImageEditTask("draft")).toBe(false);
    expect(shouldPollLocalImageEditTask("unknown")).toBe(false);
    expect(shouldPollLocalImageEditTask("failed")).toBe(false);
  });

  it("revokes source object URLs on controller cleanup", () => {
    const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined);
    revokeLocalImageEditObjectUrl("blob:local-edit-source");
    expect(revoke).toHaveBeenCalledWith("blob:local-edit-source");
    revoke.mockRestore();
  });

  it("continues as a library-only edit until the result has an active adoption", () => {
    const task = makeTask({
      target_node_id: "node-1",
      adoption_events: [{
        id: "adopt-1",
        task_id: "task-1",
        graph_id: "graph-1",
        node_id: "node-1",
        event_type: "adopt",
        from_artifact_id: "artifact-old",
        to_artifact_id: "artifact-new",
        related_event_id: null,
        created_at: "2026-08-24T00:01:00Z",
      }],
    });
    expect(continuedEditTargetNodeId(makeTask({ adoption_events: [] }), "node-1")).toBeNull();
    expect(continuedEditTargetNodeId(task, "node-1")).toBe("node-1");
    expect(continuedEditTargetNodeId({
      ...task,
      adoption_events: [{ ...task.adoption_events[0], event_type: "revert", created_at: "2026-08-24T00:02:00Z" }],
    }, "node-1")).toBeNull();
  });
});

function makeTask(overrides: Partial<LocalImageEditTask> = {}): LocalImageEditTask {
  return {
    id: "task-1",
    revision: 1,
    status: "queued",
    source_asset: { id: "source-1" },
    target_node_id: "node-1",
    created_at: "2026-08-24T00:00:00Z",
    updated_at: "2026-08-24T00:00:00Z",
    adoption_events: [],
    ...overrides,
  } as LocalImageEditTask;
}
