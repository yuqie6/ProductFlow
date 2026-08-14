import { describe, expect, it, vi } from "vitest";

import type { ProductWorkflowV2, WorkflowRevealEvent } from "../../lib/types";
import {
  WorkflowRevealProtocolError,
  applyWorkflowRevealEvent,
  collectWorkflowRevealEvents,
  emptyWorkflowRevealVisibility,
  shouldAnimateWorkflowReveal,
} from "./workflowReveal";

function workflow(): ProductWorkflowV2 {
  return {
    id: "workflow-1",
    product_id: "product-1",
    title: "商品工作流",
    active: true,
    schema_version: 2,
    revision: 4,
    edit_version: 0,
    source_draft_revision_id: "revision-1",
    visual_system_version_id: "visual-1",
    materialization_id: "materialization-1",
    folders: [{
      id: "folder-1",
      workflow_id: "workflow-1",
      key: "hero",
      title: "首图",
      order: 0,
      created_at: "2026-08-14T00:00:00Z",
      updated_at: "2026-08-14T00:00:00Z",
    }],
    nodes: [
      {
        id: "node-1",
        workflow_id: "workflow-1",
        schema_version: 2,
        key: "context",
        node_type: "product_context",
        title: "商品资料",
        position_x: 0,
        position_y: 0,
        folder_id: null,
        bound_image_asset_id: null,
        current_prompt_artifact_version_id: null,
        config_json: {},
        status: "idle",
        output_json: null,
        failure_reason: null,
        created_at: "2026-08-14T00:00:00Z",
        updated_at: "2026-08-14T00:00:00Z",
      },
      {
        id: "node-2",
        workflow_id: "workflow-1",
        schema_version: 2,
        key: "hero-image-1",
        node_type: "image_generation",
        title: "首图 1",
        position_x: 320,
        position_y: 0,
        folder_id: "folder-1",
        bound_image_asset_id: null,
        current_prompt_artifact_version_id: null,
        config_json: {},
        status: "idle",
        output_json: null,
        failure_reason: null,
        created_at: "2026-08-14T00:00:00Z",
        updated_at: "2026-08-14T00:00:00Z",
      },
    ],
    edges: [{
      id: "edge-1",
      workflow_id: "workflow-1",
      key: "context-to-image",
      source_node_id: "node-1",
      target_node_id: "node-2",
      source_handle: "output",
      target_handle: "input",
      created_at: "2026-08-14T00:00:00Z",
    }],
    created_at: "2026-08-14T00:00:00Z",
    updated_at: "2026-08-14T00:00:00Z",
  };
}

function revealEvent(
  sequence: number,
  kind: WorkflowRevealEvent["kind"] | string,
  entityType: string,
  entityId: string,
  payload: Record<string, unknown> = {},
): string {
  return [
    `id: ${sequence}`,
    `event: ${kind}`,
    `data: ${JSON.stringify({
      schema_version: 1,
      materialization_id: "materialization-1",
      sequence,
      kind,
      entity_type: entityType,
      entity_id: entityId,
      payload,
      created_at: "2026-08-14T00:00:00Z",
    })}`,
    "",
    "",
  ].join("\n");
}

function completeStream(): string {
  return [
    revealEvent(1, "folder", "folder", "folder-1", { folder_key: "hero" }),
    revealEvent(2, "node", "node", "node-1", { node_key: "context" }),
    revealEvent(3, "node", "node", "node-2", { node_key: "hero-image-1" }),
    revealEvent(4, "edge", "edge", "edge-1", { edge_key: "context-to-image" }),
    revealEvent(5, "completed", "workflow", "workflow-1", {
      workflow_id: "workflow-1",
      workflow_revision: 4,
    }),
  ].join("");
}

describe("workflow reveal protocol", () => {
  it("parses fragmented finite SSE and applies persisted sequence order", async () => {
    const stream = completeStream();
    const events = await collectWorkflowRevealEvents({
      materializationId: "materialization-1",
      workflow: workflow(),
      signal: new AbortController().signal,
      read: async (_after, onChunk) => {
        onChunk(stream.slice(0, 73));
        onChunk(stream.slice(73, 219));
        onChunk(stream.slice(219));
      },
      retryDelayMs: 0,
    });

    expect(events.map((event) => `${event.sequence}:${event.kind}`)).toEqual([
      "1:folder",
      "2:node",
      "3:node",
      "4:edge",
      "5:completed",
    ]);
    const visibility = events.reduce(applyWorkflowRevealEvent, emptyWorkflowRevealVisibility());
    expect([...visibility.folderIds]).toEqual(["folder-1"]);
    expect([...visibility.nodeIds]).toEqual(["node-1", "node-2"]);
    expect([...visibility.edgeIds]).toEqual(["edge-1"]);
  });

  it("resumes after the last complete sequence and ignores a replayed duplicate", async () => {
    const calls: number[] = [];
    const first = revealEvent(1, "folder", "folder", "folder-1");
    const remainder = completeStream().slice(first.length);
    const read = vi.fn(async (after: number, onChunk: (chunk: string) => void) => {
      calls.push(after);
      if (calls.length === 1) {
        onChunk(first);
        onChunk(revealEvent(1, "folder", "folder", "folder-1"));
        throw new Error("connection reset");
      }
      onChunk(remainder);
    });

    const events = await collectWorkflowRevealEvents({
      materializationId: "materialization-1",
      workflow: workflow(),
      signal: new AbortController().signal,
      read,
      retryDelayMs: 0,
    });

    expect(calls).toEqual([0, 1]);
    expect(events.map((event) => event.sequence)).toEqual([1, 2, 3, 4, 5]);
  });

  it("discards a partial frame at EOF and requests it again from the last durable cursor", async () => {
    const first = revealEvent(1, "folder", "folder", "folder-1");
    const remainder = completeStream().slice(first.length);
    const calls: number[] = [];
    const events = await collectWorkflowRevealEvents({
      materializationId: "materialization-1",
      workflow: workflow(),
      signal: new AbortController().signal,
      retryDelayMs: 0,
      read: async (after, onChunk) => {
        calls.push(after);
        if (calls.length === 1) {
          onChunk(first);
          onChunk(remainder.slice(0, 40));
          return;
        }
        onChunk(remainder);
      },
    });

    expect(calls).toEqual([0, 1]);
    expect(events.map((event) => event.sequence)).toEqual([1, 2, 3, 4, 5]);
  });

  it("rejects unknown kinds, unknown entities, and incomplete coverage", async () => {
    const run = (stream: string) => collectWorkflowRevealEvents({
      materializationId: "materialization-1",
      workflow: workflow(),
      signal: new AbortController().signal,
      read: async (_after, onChunk) => onChunk(stream),
      retryDelayMs: 0,
    });

    await expect(run(revealEvent(1, "future", "node", "node-1"))).rejects.toBeInstanceOf(
      WorkflowRevealProtocolError,
    );
    await expect(run(revealEvent(1, "node", "node", "node-unknown"))).rejects.toThrow(
      "未知实体",
    );
    await expect(run([
      revealEvent(1, "node", "node", "node-1"),
      revealEvent(2, "folder", "folder", "folder-1"),
    ].join(""))).rejects.toThrow("实体顺序无效");
    await expect(run([
      revealEvent(1, "folder", "folder", "folder-1"),
      revealEvent(2, "completed", "workflow", "workflow-1", {
        workflow_id: "workflow-1",
        workflow_revision: 4,
      }),
    ].join(""))).rejects.toThrow("缺少持久化实体事件");
  });

  it("skips visual replay for idempotent responses and reduced-motion users", () => {
    expect(shouldAnimateWorkflowReveal({ enabled: true, created: true, reducedMotion: false })).toBe(true);
    expect(shouldAnimateWorkflowReveal({ enabled: true, created: false, reducedMotion: false })).toBe(false);
    expect(shouldAnimateWorkflowReveal({ enabled: true, created: true, reducedMotion: true })).toBe(false);
  });
});
