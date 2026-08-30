import { describe, expect, it } from "vitest";

import type {
  GraphNode,
  GraphNodeRun,
  GraphProjection,
  GraphRun,
} from "../../../lib/types";
import { graphHasImageGenerationGroups, projectGraphShots } from "./shotProjection";

function makeNode(
  id: string,
  nodeType: GraphNode["node_type"],
  groupId: string | null,
  previewAssetId: string | null = null,
): GraphNode {
  return {
    id,
    node_type: nodeType,
    title: id,
    position_x: 0,
    position_y: 0,
    config: {},
    bound_asset_id: null,
    group_id: groupId,
    preview_asset_id: previewAssetId,
    config_status: "ready",
    unused: false,
    incoming: [],
    outgoing: [],
  };
}

function makeGraph(): GraphProjection {
  return {
    id: "graph-1",
    product_id: "product-1",
    title: "商品工作流",
    schema_version: 3,
    revision: 4,
    last_operation_group_id: null,
    can_undo: false,
    can_redo: false,
    nodes: [
      makeNode("image-1", "image_generation", "group-1", "asset-old"),
      makeNode("image-2", "image_generation", "group-1"),
      makeNode("prompt-1", "prompt_generation", "group-2"),
    ],
    edges: [],
    groups: [
      { id: "group-1", title: "主图", member_ids: ["image-1", "image-2"] },
      { id: "group-2", title: "非生图分组", member_ids: ["prompt-1"] },
    ],
    pending_proposal: null,
  };
}

function makeNodeRun(input: Partial<GraphNodeRun> & { id: string; node_id: string; status: GraphNodeRun["status"] }): GraphNodeRun {
  return {
    id: input.id,
    node_id: input.node_id,
    node_title: input.node_title ?? input.node_id,
    status: input.status,
    sort_order: input.sort_order ?? 0,
    compiled_context: null,
    input_trace: [],
    output: input.output ?? null,
    failure_reason: input.failure_reason ?? null,
    attempt_count: input.attempt_count ?? 0,
    started_at: input.started_at ?? "2026-08-20T00:00:00Z",
    finished_at: input.finished_at ?? null,
  };
}

function makeRun(input: {
  id: string;
  status: GraphRun["status"];
  startedAt: string;
  nodeRuns: GraphNodeRun[];
  failureReason?: string | null;
}): GraphRun {
  return {
    id: input.id,
    graph_id: "graph-1",
    status: input.status,
    scope: "node",
    requested_node_id: input.nodeRuns[0]?.node_id ?? null,
    graph_revision: 4,
    failure_reason: input.failureReason ?? null,
    is_retryable: true,
    node_runs: input.nodeRuns,
    started_at: input.startedAt,
    finished_at: input.status === "running" ? null : input.startedAt,
  };
}

describe("projectGraphShots", () => {
  it("only projects groups containing image generation nodes", () => {
    const graph = makeGraph();

    expect(graphHasImageGenerationGroups(graph)).toBe(true);
    expect(projectGraphShots(graph, [])).toHaveLength(1);
    expect(projectGraphShots(graph, [makeRun({
      id: "run-prompt",
      status: "succeeded",
      startedAt: "2026-08-20T00:00:00Z",
      nodeRuns: [makeNodeRun({ id: "node-prompt", node_id: "prompt-1", status: "succeeded" })],
    })])).toHaveLength(1);
  });

  it("treats an existing preview as completed even when that node is not first in the group", () => {
    const graph = makeGraph();
    graph.nodes = [graph.nodes[1], graph.nodes[0], graph.nodes[2]];

    const shot = projectGraphShots(graph, [])[0];

    expect(shot.latestNodeStatus).toBe("succeeded");
    expect(shot.completedImageCount).toBe(1);
    expect(shot.currentResultAssetIds).toEqual(["asset-old"]);
  });

  it("uses one deterministic latest node-run fact for status, failure, and result", () => {
    const graph = makeGraph();
    const olderSuccess = makeRun({
      id: "run-old",
      status: "succeeded",
      startedAt: "2026-08-20T00:00:00Z",
      nodeRuns: [makeNodeRun({
        id: "node-old",
        node_id: "image-1",
        status: "succeeded",
        output: { product_image_asset_id: "asset-old-run" },
        started_at: "2026-08-20T00:00:00Z",
        finished_at: "2026-08-20T00:01:00Z",
      })],
    });
    const newerFailure = makeRun({
      id: "run-new",
      status: "failed",
      startedAt: "2026-08-21T00:00:00Z",
      failureReason: "供应商超时",
      nodeRuns: [makeNodeRun({
        id: "node-new",
        node_id: "image-1",
        status: "failed",
        started_at: "2026-08-21T00:00:00Z",
        finished_at: "2026-08-21T00:01:00Z",
      })],
    });

    const forward = projectGraphShots(graph, [olderSuccess, newerFailure])[0];
    const reverse = projectGraphShots(graph, [newerFailure, olderSuccess])[0];

    expect(forward).toEqual(reverse);
    expect(forward.primaryImageNodeId).toBe("image-1");
    expect(forward.primaryImageAssetId).toBe("asset-old");
    expect(forward.latestNodeStatus).toBe("failed");
    expect(forward.latestFailureReason).toBe("供应商超时");
    expect(forward.completedImageCount).toBe(1);
    expect(forward.currentResultAssetIds).toContain("asset-old");
  });

  it("prefers the node's current artifact over a succeeded run output after adopt", () => {
    const graph = makeGraph();
    graph.nodes[0] = { ...graph.nodes[0], preview_asset_id: "asset-adopted" };
    const generated = makeRun({
      id: "run-generated",
      status: "succeeded",
      startedAt: "2026-08-22T00:00:00Z",
      nodeRuns: [makeNodeRun({
        id: "node-generated",
        node_id: "image-1",
        status: "succeeded",
        output: { product_image_asset_id: "asset-generated" },
        started_at: "2026-08-22T00:00:00Z",
        finished_at: "2026-08-22T00:01:00Z",
      })],
    });

    const shot = projectGraphShots(graph, [generated])[0];

    expect(shot.primaryImageAssetId).toBe("asset-adopted");
    expect(shot.currentResultAssetIds).toEqual(["asset-adopted"]);
  });

  it("uses a skipped run output as the current shot result", () => {
    const graph = makeGraph();
    graph.nodes[0] = { ...graph.nodes[0], preview_asset_id: null };
    const reused = makeRun({
      id: "run-reused",
      status: "succeeded",
      startedAt: "2026-08-23T00:00:00Z",
      nodeRuns: [makeNodeRun({
        id: "node-reused",
        node_id: "image-1",
        status: "skipped",
        output: { product_image_asset_id: "asset-reused" },
        started_at: "2026-08-23T00:00:00Z",
        finished_at: "2026-08-23T00:00:00Z",
      })],
    });

    const shot = projectGraphShots(graph, [reused])[0];

    expect(shot.primaryImageAssetId).toBe("asset-reused");
    expect(shot.completedImageCount).toBe(1);
    expect(shot.currentResultAssetIds).toEqual(["asset-reused"]);
  });

  it("lets an active run override historical status while retaining the current graph preview", () => {
    const graph = makeGraph();
    const historical = makeRun({
      id: "run-history",
      status: "succeeded",
      startedAt: "2026-08-25T00:00:00Z",
      nodeRuns: [makeNodeRun({
        id: "node-history",
        node_id: "image-1",
        status: "succeeded",
        output: { product_image_asset_id: "asset-history" },
        started_at: "2026-08-25T00:00:00Z",
        finished_at: "2026-08-25T00:01:00Z",
      })],
    });
    const active = makeRun({
      id: "run-active",
      status: "running",
      startedAt: "2026-08-19T00:00:00Z",
      nodeRuns: [makeNodeRun({
        id: "node-active",
        node_id: "image-1",
        status: "running",
        started_at: "2026-08-19T00:00:00Z",
      })],
    });

    const shot = projectGraphShots(graph, [historical, active])[0];

    expect(shot.latestNodeStatus).toBe("running");
    expect(shot.primaryImageAssetId).toBe("asset-old");
    expect(shot.latestFailureReason).toBeNull();
    expect(shot.currentResultAssetIds).toEqual(["asset-old"]);
  });
});
