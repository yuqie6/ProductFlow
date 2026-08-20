import { describe, expect, it } from "vitest";

import type { ProductWorkflowV2, WorkflowNodeV2 } from "../../../lib/types";
import {
  captureV2WorkflowPositions,
  findAddedV2RootNodeId,
  findV2WorkflowEdgeId,
  shouldClearV2WorkflowHistory,
} from "./v2WorkflowHistory";

function node(id: string, nodeType: WorkflowNodeV2["node_type"]): WorkflowNodeV2 {
  return {
    id,
    workflow_id: "workflow",
    schema_version: 2,
    key: id,
    node_type: nodeType,
    title: id,
    position_x: id === "context" ? 24 : 48,
    position_y: 72,
    folder_id: null,
    bound_image_asset_id: null,
    current_prompt_artifact_version_id: null,
    config_json: {},
    status: "idle",
    output_json: null,
    failure_reason: null,
    created_at: "2026-08-15T00:00:00Z",
    updated_at: "2026-08-15T00:00:00Z",
  };
}

function workflow(nodes: WorkflowNodeV2[]): ProductWorkflowV2 {
  return {
    id: "workflow",
    product_id: "product",
    title: "workflow",
    active: true,
    schema_version: 2,
    revision: 1,
    edit_version: 0,
    source_draft_revision_id: "draft",
    visual_system_version_id: "visual",
    materialization_id: "materialization",
    folders: [],
    nodes,
    edges: [],
    created_at: "2026-08-15T00:00:00Z",
    updated_at: "2026-08-15T00:00:00Z",
  };
}

describe("schema-v2 session history helpers", () => {
  it("captures stable positions and resolves a duplicated group root", () => {
    const before = workflow([node("context", "product_context"), node("prompt", "prompt_generation")]);
    const after = workflow([
      ...before.nodes,
      node("prompt-copy", "prompt_generation"),
      node("image-copy", "image_generation"),
    ]);

    expect(captureV2WorkflowPositions(before, ["prompt", "context"])).toEqual([
      { node_id: "context", position_x: 24, position_y: 72 },
      { node_id: "prompt", position_x: 48, position_y: 72 },
    ]);
    expect(findAddedV2RootNodeId(before, after, "prompt_generation")).toBe("prompt-copy");
  });

  it("requires an exact edge pair", () => {
    const current = workflow([node("context", "product_context"), node("prompt", "prompt_generation")]);
    current.edges = [{
      id: "edge",
      workflow_id: current.id,
      key: "edge",
      source_node_id: "context",
      target_node_id: "prompt",
      source_handle: "facts",
      target_handle: "facts",
      created_at: "2026-08-15T00:00:00Z",
    }];

    expect(findV2WorkflowEdgeId(current, "context", "prompt")).toBe("edge");
    expect(() => findV2WorkflowEdgeId(current, "prompt", "context")).toThrow("唯一的连线");
  });

  it("clears local history only for an external workflow version", () => {
    const previous = { workflowId: "workflow", editVersion: 4 };

    expect(shouldClearV2WorkflowHistory(previous, previous, null)).toBe(false);
    expect(shouldClearV2WorkflowHistory(
      previous,
      { workflowId: "workflow", editVersion: 5 },
      5,
    )).toBe(false);
    expect(shouldClearV2WorkflowHistory(
      previous,
      { workflowId: "workflow", editVersion: 5 },
      null,
    )).toBe(true);
    expect(shouldClearV2WorkflowHistory(
      previous,
      { workflowId: "other", editVersion: 0 },
      0,
    )).toBe(true);
  });
});
