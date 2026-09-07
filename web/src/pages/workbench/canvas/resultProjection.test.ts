import { describe, expect, it } from "vitest";

import type {
  GraphNode,
  GraphNodeRun,
  GraphProjection,
  GraphRun,
} from "../../../lib/types";
import {
  assertUniqueResultNodeIds,
  flattenGraphResultItems,
  graphHasResultItems,
  projectGraphResults,
} from "./resultProjection";

function makeNode(
  id: string,
  nodeType: GraphNode["node_type"],
  groupId: string | null,
  previewAssetId: string | null = null,
  config: Record<string, unknown> = {},
  boundAssetId: string | null = null,
): GraphNode {
  return {
    id,
    node_type: nodeType,
    title: id,
    position_x: 0,
    position_y: 0,
    config,
    bound_asset_id: boundAssetId,
    group_id: groupId,
    preview_asset_id: previewAssetId,
    config_status: "ready",
    unused: false,
    incoming: [],
    outgoing: [],
  };
}

function makeGraph(nodes: GraphNode[], groups: GraphProjection["groups"] = []): GraphProjection {
  return {
    id: "graph-1",
    product_id: "product-1",
    title: "商品工作流",
    schema_version: 3,
    revision: 4,
    last_operation_group_id: null,
    can_undo: false,
    can_redo: false,
    nodes,
    edges: [],
    groups,
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

describe("projectGraphResults", () => {
  it("projects each generation node once across grouped, multi-image, and ungrouped", () => {
    const graph = makeGraph(
      [
        makeNode("image-1", "image_generation", "group-1", "asset-1", { image_type_key: "hero" }),
        makeNode("image-2", "image_generation", "group-1", null, { image_type_key: "hero" }),
        makeNode("image-solo", "image_generation", null, null, { image_type_key: "detail" }),
        makeNode("prompt-1", "image_prompt", "group-1"),
        makeNode("evidence-1", "image_asset", null, null, { role: "evidence" }, null),
        makeNode("identity-1", "image_asset", null, null, { role: "product_identity" }, "ref-1"),
      ],
      [
        { id: "group-1", title: "主图", member_ids: ["image-1", "image-2", "prompt-1"] },
      ],
    );

    expect(graphHasResultItems(graph)).toBe(true);
    const sections = projectGraphResults(graph, []);
    const items = flattenGraphResultItems(sections);
    expect(assertUniqueResultNodeIds(sections)).toEqual([]);
    expect(items.map((item) => item.nodeId).sort()).toEqual([
      "evidence-1",
      "image-1",
      "image-2",
      "image-solo",
    ]);
    expect(sections.find((section) => section.key === "group-1")?.items).toHaveLength(2);
    expect(sections.find((section) => section.kind === "ungrouped")?.items.map((item) => item.nodeId)).toEqual([
      "image-solo",
    ]);
    expect(sections.find((section) => section.kind === "evidence")?.items).toHaveLength(1);
    expect(items.find((item) => item.nodeId === "image-2")?.currentAssetId).toBeNull();
    expect(items.find((item) => item.nodeId === "image-2")?.status).toBe("idle");
  });

  it("keeps an old current preview when the latest run failed", () => {
    const graph = makeGraph([
      makeNode("image-1", "image_generation", "group-1", "asset-old", { image_type_key: "detail" }),
    ], [{ id: "group-1", title: "细节", member_ids: ["image-1"] }]);
    const failed = makeRun({
      id: "run-fail",
      status: "failed",
      startedAt: "2026-08-21T00:00:00Z",
      failureReason: "供应商超时",
      nodeRuns: [makeNodeRun({
        id: "node-fail",
        node_id: "image-1",
        status: "failed",
        failure_reason: "供应商超时",
        started_at: "2026-08-21T00:00:00Z",
        finished_at: "2026-08-21T00:01:00Z",
      })],
    });

    const item = flattenGraphResultItems(projectGraphResults(graph, [failed]))[0];
    expect(item.status).toBe("failed");
    expect(item.currentAssetId).toBe("asset-old");
    expect(item.showingStaleCurrent).toBe(true);
    expect(item.failureReason).toBe("供应商超时");
  });

  it("uses skipped run output when the node has no preview yet", () => {
    const graph = makeGraph([
      makeNode("image-1", "image_generation", null, null),
    ]);
    const skipped = makeRun({
      id: "run-skip",
      status: "succeeded",
      startedAt: "2026-08-22T00:00:00Z",
      nodeRuns: [makeNodeRun({
        id: "node-skip",
        node_id: "image-1",
        status: "skipped",
        output: { product_image_asset_id: "asset-reused" },
        started_at: "2026-08-22T00:00:00Z",
        finished_at: "2026-08-22T00:00:00Z",
      })],
    });

    const item = flattenGraphResultItems(projectGraphResults(graph, [skipped]))[0];
    expect(item.currentAssetId).toBe("asset-reused");
    expect(item.status).toBe("skipped");
    expect(item.showingStaleCurrent).toBe(false);
  });

  it("marks unknown status without relabeling a current asset as a new success", () => {
    const graph = makeGraph([
      makeNode("image-1", "image_generation", null, "asset-old"),
    ]);
    const unknown = makeRun({
      id: "run-unknown",
      status: "unknown",
      startedAt: "2026-08-23T00:00:00Z",
      nodeRuns: [makeNodeRun({
        id: "node-unknown",
        node_id: "image-1",
        status: "unknown",
        started_at: "2026-08-23T00:00:00Z",
        finished_at: "2026-08-23T00:01:00Z",
      })],
    });

    const item = flattenGraphResultItems(projectGraphResults(graph, [unknown]))[0];
    expect(item.status).toBe("unknown");
    expect(item.currentAssetId).toBe("asset-old");
    expect(item.showingStaleCurrent).toBe(true);
  });

  it("projects thirty planned generation nodes without duplicates", () => {
    const nodes = Array.from({ length: 30 }, (_, index) => (
      makeNode(
        `image-${index}`,
        "image_generation",
        index < 20 ? "group-bulk" : null,
        index % 3 === 0 ? `asset-${index}` : null,
        { image_type_key: "scene" },
      )
    ));
    const graph = makeGraph(nodes, [
      { id: "group-bulk", title: "场景套图", member_ids: nodes.slice(0, 20).map((node) => node.id) },
    ]);

    const sections = projectGraphResults(graph, []);
    expect(assertUniqueResultNodeIds(sections)).toEqual([]);
    expect(flattenGraphResultItems(sections)).toHaveLength(30);
    expect(sections.find((section) => section.key === "group-bulk")?.items).toHaveLength(20);
    expect(sections.find((section) => section.kind === "ungrouped")?.items).toHaveLength(10);
  });

  it("shows bound evidence assets and empty evidence placeholders", () => {
    const graph = makeGraph([
      makeNode("evidence-empty", "image_asset", null, null, { role: "evidence" }),
      makeNode("evidence-bound", "image_asset", null, null, { role: "evidence" }, "asset-cert"),
    ]);
    const items = flattenGraphResultItems(projectGraphResults(graph, []));
    expect(items).toHaveLength(2);
    expect(items.find((item) => item.nodeId === "evidence-empty")).toMatchObject({
      kind: "evidence",
      currentAssetId: null,
      status: "idle",
      runnable: false,
    });
    expect(items.find((item) => item.nodeId === "evidence-bound")).toMatchObject({
      currentAssetId: "asset-cert",
      status: "succeeded",
    });
  });
});
