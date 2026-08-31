import { describe, expect, it } from "vitest";

import type { GraphNodeRun, GraphProjection, GraphRun } from "../../../lib/types";
import {
  graphArtifactTypeLabelKey,
  graphContextEntries,
  graphIncomingSourceEntries,
  graphNodeRunPresentations,
  graphNodeRunPreviewAssetId,
  graphOutputActionLabelKey,
  graphOutputQualityLabelKey,
  graphRunInputTraceEntries,
  graphRunScopeLabelKey,
  graphRunsAreLive,
  humanizeTechnicalKey,
} from "./graphRunDisplay";

describe("humanizeTechnicalKey", () => {
  it("formats unknown backend labels without exposing snake case", () => {
    expect(humanizeTechnicalKey("future_provider_name")).toBe("Future Provider Name");
  });
});

const graph: GraphProjection = {
  id: "g1",
  product_id: "p1",
  title: "t",
  schema_version: 3,
  revision: 2,
    last_operation_group_id: null,
    can_undo: false,
    can_redo: false,
  nodes: [{
    id: "image",
    node_type: "image_generation",
    title: "主图 1",
    position_x: 0,
    position_y: 0,
    config: {},
    bound_asset_id: null,
    group_id: null,
    preview_asset_id: "preview-1",
    config_status: "ready",
    unused: false,
    incoming: [{
      id: "edge-1",
      node_id: "prompt",
      data_type: "prompt",
      role: "prompt",
      order: 0,
    }],
    outgoing: [],
  }, {
    id: "prompt",
    node_type: "image_prompt",
    title: "主图提示词",
    position_x: 0,
    position_y: 0,
    config: {},
    bound_asset_id: null,
    group_id: null,
    preview_asset_id: null,
    config_status: "ready",
    unused: false,
    incoming: [],
    outgoing: [],
  }],
  edges: [],
  groups: [],
};

function nodeRun(partial: Partial<GraphNodeRun>): GraphNodeRun {
  return {
    id: "nr1",
    node_id: "image",
    status: "succeeded",
    sort_order: 0,
    compiled_context: null,
    output: null,
    failure_reason: null,
    attempt_count: 0,
    started_at: "2026-08-21T00:00:00Z",
    finished_at: "2026-08-21T00:00:01Z",
    ...partial,
  };
}

describe("graph run display", () => {
  it("labels run scopes without exposing enum strings to the page", () => {
    expect(graphRunScopeLabelKey("graph")).toBe("graph.runs.scope.graph");
    expect(graphRunScopeLabelKey("node")).toBe("graph.runs.scope.node");
    expect(graphRunScopeLabelKey("to_node")).toBe("graph.runs.scope.toNode");
    expect(graphRunScopeLabelKey("selection")).toBe("graph.runs.scope.selection");
  });

  it("prefers an explicit product image on the node run, then the current preview", () => {
    expect(graphNodeRunPreviewAssetId(nodeRun({ output: { product_image_asset_id: "out-1" } }), graph)).toBe("out-1");
    expect(graphNodeRunPreviewAssetId(nodeRun({ output: { artifact_id: "art-1" } }), graph)).toBe("preview-1");
    expect(graphNodeRunPreviewAssetId(nodeRun({ status: "failed" }), graph)).toBeNull();
  });

  it("projects incoming sources as node title and role", () => {
    expect(graphIncomingSourceEntries(graph.nodes[0], graph)).toEqual([
      {
        id: "edge-1",
        sourceNodeId: "prompt",
        title: "主图提示词",
        role: "prompt",
        order: 0,
        artifactId: null,
        artifactType: null,
        assetId: null,
        versionId: null,
      },
    ]);
  });

  it("keeps the row when the source title is blank or the node is gone", () => {
    const untitled = {
      ...graph,
      nodes: graph.nodes.map((node) => node.id === "prompt" ? { ...node, title: "  " } : node),
    };
    expect(graphIncomingSourceEntries(untitled.nodes[0], untitled)).toEqual([
      {
        id: "edge-1",
        sourceNodeId: "prompt",
        title: "—",
        role: "prompt",
        order: 0,
        artifactId: null,
        artifactType: null,
        assetId: null,
        versionId: null,
      },
    ]);
    const missing = {
      ...graph,
      nodes: graph.nodes.filter((node) => node.id !== "prompt"),
    };
    expect(graphIncomingSourceEntries(missing.nodes[0], missing)).toEqual([
      {
        id: "edge-1",
        sourceNodeId: "prompt",
        title: "",
        role: "prompt",
        order: 0,
        artifactId: null,
        artifactType: null,
        assetId: null,
        versionId: null,
      },
    ]);
  });

  it("keeps opaque source ids and per-edge artifact identities in run order", () => {
    expect(graphRunInputTraceEntries(nodeRun({
      compiled_context: {
        prompt_edge_id: "edge-prompt",
        prompt_artifact_id: "artifact-prompt",
      },
      input_trace: [
        {
          edge_id: "edge-reference",
          source_node_id: "opaque-reference-node",
          source_title: null,
          role: "reference",
          order: 2,
          artifact_id: "artifact-reference",
          artifact_type: "image",
          asset_id: "asset-reference",
          version_id: "version-reference",
        },
        {
          edge_id: "edge-prompt",
          source_node_id: "opaque-prompt-node",
          source_title: "Prompt",
          role: "prompt",
          order: 0,
        },
      ],
    }))).toEqual([
      {
        id: "edge-prompt",
        sourceNodeId: "opaque-prompt-node",
        title: "Prompt",
        role: "prompt",
        order: 0,
        artifactId: "artifact-prompt",
        artifactType: "prompt",
        assetId: null,
        versionId: null,
      },
      {
        id: "edge-reference",
        sourceNodeId: "opaque-reference-node",
        title: "",
        role: "reference",
        order: 2,
        artifactId: "artifact-reference",
        artifactType: "image",
        assetId: "asset-reference",
        versionId: "version-reference",
      },
    ]);
  });

  it("flattens compiled context for the evidence list", () => {
    expect(graphContextEntries({
      fact_count: 3,
      reference_asset_ids: ["a", "b"],
      mystery_digest: "abc",
    })).toEqual([
      { key: "fact_count", labelKey: "graph.runs.context.factCount", value: "3" },
      { key: "reference_asset_ids", labelKey: "graph.runs.context.referenceAssets", value: "a · b" },
    ]);
  });

  it("maps backend artifact, quality, and action enums to display labels", () => {
    expect(graphArtifactTypeLabelKey("creative_brief")).toBe("graph.artifactType.creativeBrief");
    expect(graphOutputQualityLabelKey("ultra")).toBe("graph.output.quality.ultra");
    expect(graphOutputActionLabelKey("generate")).toBe("graph.output.action.generate");
    expect(graphArtifactTypeLabelKey("future_type")).toBeNull();
  });

  it("projects the latest node failure onto the card presentation", () => {
    const failed: GraphRun = {
      id: "run-1",
      graph_id: "g1",
      status: "failed",
      scope: "node",
      requested_node_id: "image",
      graph_revision: 2,
      failure_reason: "上游失败",
      is_retryable: true,
      node_runs: [nodeRun({ attempt_count: 1, status: "failed", failure_reason: "模型超时", finished_at: "2026-08-21T00:00:02Z" })],
      started_at: "2026-08-21T00:00:00Z",
      finished_at: "2026-08-21T00:00:02Z",
    };
    expect(graphNodeRunPresentations([failed]).image).toEqual({
      status: "failed",
      failureReason: "模型超时",
      lastRunAt: "2026-08-21T00:00:02Z",
      retryable: true,
      runId: "run-1",
      lastPlannedAction: null,
      progressPhase: null,
      elapsedLabel: "2.0s",
      attemptCount: 1,
    });
  });

  it("uses the persisted provider attempt count instead of historical run count", () => {
    const latest: GraphRun = {
      id: "run-latest",
      graph_id: "g1",
      status: "failed",
      scope: "node",
      requested_node_id: "image",
      graph_revision: 2,
      failure_reason: "模型超时",
      is_retryable: true,
      node_runs: [nodeRun({ id: "nr-latest", attempt_count: 3, status: "failed", finished_at: "2026-08-21T00:00:03Z" })],
      started_at: "2026-08-21T00:00:00Z",
      finished_at: "2026-08-21T00:00:03Z",
    };
    const older: GraphRun = {
      ...latest,
      id: "run-older",
      node_runs: [nodeRun({ id: "nr-older", attempt_count: 1, status: "failed", finished_at: "2026-08-20T00:00:02Z" })],
      started_at: "2026-08-20T00:00:00Z",
      finished_at: "2026-08-20T00:00:02Z",
    };
    expect(graphNodeRunPresentations([latest, older]).image.attemptCount).toBe(3);
  });

  it.each(["succeeded", "skipped", "failed", "cancelled", "unknown"] as const)(
    "does not project stale progress for terminal %s nodes",
    (status) => {
      const terminal: GraphRun = {
        id: `run-${status}`,
        graph_id: "g1",
        status: status === "skipped" ? "succeeded" : status,
        scope: "node",
        requested_node_id: "image",
        graph_revision: 2,
        failure_reason: null,
        is_retryable: false,
        node_runs: [nodeRun({ status, progress_phase: "claimed" })],
        started_at: "2026-08-21T00:00:00Z",
        finished_at: "2026-08-21T00:00:01Z",
      };
      expect(graphNodeRunPresentations([terminal]).image.progressPhase).toBeNull();
    },
  );

  it("keeps progress for live nodes", () => {
    const running: GraphRun = {
      id: "run-running-phase",
      graph_id: "g1",
      status: "running",
      scope: "node",
      requested_node_id: "image",
      graph_revision: 2,
      failure_reason: null,
      is_retryable: false,
      node_runs: [nodeRun({ status: "running", progress_phase: "provider_call", finished_at: null })],
      started_at: "2026-08-21T00:00:00Z",
      finished_at: null,
    };
    expect(graphNodeRunPresentations([running]).image.progressPhase).toBe("provider_call");
  });

  it("polls queued and running graph runs, not terminal ones", () => {
    const queued = {
      id: "run-q",
      graph_id: "g1",
      status: "running",
      scope: "graph",
      requested_node_id: null,
      graph_revision: 2,
      failure_reason: null,
      is_retryable: false,
      node_runs: [nodeRun({ status: "queued" })],
      started_at: "2026-08-21T00:00:00Z",
      finished_at: null,
    } satisfies GraphRun;
    expect(graphRunsAreLive([queued])).toBe(true);
    expect(graphRunsAreLive([{ ...queued, status: "running" }])).toBe(true);
    expect(graphRunsAreLive([{ ...queued, status: "succeeded", finished_at: "2026-08-21T00:01:00Z" }])).toBe(false);
    expect(graphRunsAreLive([])).toBe(false);
  });

  it("overlays the running graph run even when a newer queued run exists", () => {
    const running: GraphRun = {
      id: "run-running",
      graph_id: "g1",
      status: "running",
      scope: "graph",
      requested_node_id: null,
      graph_revision: 2,
      failure_reason: null,
      is_retryable: false,
      node_runs: [nodeRun({ status: "running", finished_at: null })],
      started_at: "2026-08-21T00:00:00Z",
      finished_at: null,
    };
    const queued: GraphRun = {
      id: "run-queued",
      graph_id: "g1",
      status: "queued",
      scope: "graph",
      requested_node_id: null,
      graph_revision: 2,
      failure_reason: null,
      is_retryable: false,
      node_runs: [],
      started_at: "2026-08-21T00:01:00Z",
      finished_at: null,
    };
    expect(graphNodeRunPresentations([queued, running]).image.status).toBe("running");
    expect(graphNodeRunPresentations([queued, running]).image.runId).toBe("run-running");
  });
});
