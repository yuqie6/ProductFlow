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
  source_draft_revision_id: null,
  last_operation_group_id: null,
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
    compiled_context: {
      incoming_edge_ids: ["edge-1"],
      prompt_artifact_id: "art-1",
      reference_asset_ids: ["asset-a"],
    },
    output: { product_image_asset_id: "out-1" },
    failure_reason: null,
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
    expect(markup).toContain("运行到这里");
    expect(markup).toContain("主图 1");
    expect(markup).toContain("运行证据");
    expect(markup).toContain("连入数量");
    expect(markup).toContain("提示词结果");
    expect(markup).toContain("参考图");
    expect(markup).not.toContain("incoming_edge_ids");
  });
});
