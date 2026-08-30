import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { GraphProjection, GraphRun } from "../../../lib/types";
import { GraphRunsPanel } from "./GraphRunsPanel";

const graph: GraphProjection = {
  id: "g1",
  product_id: "p1",
  title: "夏季主图",
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
      id: "edge-prompt",
      node_id: "prompt",
      data_type: "prompt",
      role: "prompt",
      order: 0,
    }],
    outgoing: [],
  }, {
    id: "prompt",
    node_type: "prompt_generation",
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

const run: GraphRun = {
  id: "run-1",
  graph_id: "g1",
  status: "succeeded",
  scope: "to_node",
  requested_node_id: "image",
  graph_revision: 2,
  failure_reason: null,
  is_retryable: false,
  started_at: "2026-08-21T00:00:00Z",
  finished_at: "2026-08-21T00:00:08Z",
  node_runs: [{
    id: "nr-1",
    node_id: "image",
    status: "succeeded",
    sort_order: 0,
    node_title: "主图 1",
    input_trace: [{
      edge_id: "edge-prompt",
      source_node_id: "prompt",
      source_title: "主图提示词",
      role: "prompt",
      order: 0,
    }],
    compiled_context: {
      incoming_edge_ids: ["edge-1"],
      prompt_artifact_id: "art-1",
      reference_asset_ids: ["asset-a"],
      mystery_digest: "deadbeef",
    },
    output: { product_image_asset_id: "out-1" },
    failure_reason: null,
    attempt_count: 1,
    progress_phase: "provider_result_received",
    started_at: "2026-08-21T00:00:00Z",
    finished_at: "2026-08-21T00:00:08Z",
  }],
};

describe("GraphRunsPanel", () => {
  it("shows node runs, labeled context, and jump/preview affordances", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["graph-runs", "p1", "g1"], { items: [run] });
    const markup = renderToStaticMarkup(createElement(
      QueryClientProvider,
      { client },
      createElement(GraphRunsPanel, {
        productId: "p1",
        graph,
        selectedNodeId: "image",
        onJump: () => undefined,
        onPreviewImage: () => undefined,
      }),
    ));
    const firstScreen = markup.split("data-graph-run-inputs-technical")[0];
    expect(firstScreen).toContain("运行到这里");
    expect(firstScreen).toContain("主图 1");
    expect(firstScreen).toContain("主图提示词");
    expect(firstScreen).toContain("提示词");
    expect(firstScreen).toContain("已收到结果");
    expect(firstScreen).toContain("8.0s");
    expect(firstScreen).not.toContain("asset-a");
    expect(markup).toContain("运行证据");
    expect(markup).not.toContain("incoming_edge_ids");
    expect(markup).not.toContain("mystery_digest");
    expect(markup).not.toContain("deadbeef");
    expect(markup).not.toContain("版本 2");
  });

  it("keeps rev N input titles after the live graph is renamed", () => {
    const renamed: GraphProjection = {
      ...graph,
      revision: 3,
      nodes: graph.nodes.map((node) => (
        node.id === "prompt" ? { ...node, title: "改名后的提示词" } : node
      )),
    };
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["graph-runs", "p1", "g1"], { items: [run] });
    const markup = renderToStaticMarkup(createElement(
      QueryClientProvider,
      { client },
      createElement(GraphRunsPanel, {
        productId: "p1",
        graph: renamed,
        selectedNodeId: "image",
        onJump: () => undefined,
        onPreviewImage: () => undefined,
      }),
    ));
    const firstScreen = markup.split("data-graph-run-inputs-technical")[0];
    expect(firstScreen).toContain("主图提示词");
    expect(firstScreen).not.toContain("改名后的提示词");
  });

  it("does not offer run retry while a run is queued or running", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const liveRuns: GraphRun[] = [
      { ...run, id: "queued", status: "queued", is_retryable: true, node_runs: run.node_runs.map((item) => ({ ...item, status: "queued" })) },
      { ...run, id: "running", status: "running", is_retryable: true, node_runs: run.node_runs.map((item) => ({ ...item, status: "running" })) },
    ];
    client.setQueryData(["graph-runs", "p1", "g1"], { items: liveRuns });
    const markup = renderToStaticMarkup(createElement(
      QueryClientProvider,
      { client },
      createElement(GraphRunsPanel, { productId: "p1", graph }),
    ));
    expect(markup).not.toContain('aria-label="重试该运行"');
    expect(markup.match(/aria-label="取消当前运行"/g) ?? []).toHaveLength(2);
  });
});
