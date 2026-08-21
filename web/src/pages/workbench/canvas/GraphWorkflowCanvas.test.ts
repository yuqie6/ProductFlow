import { ReactFlowProvider } from "@xyflow/react";
import { createElement, type ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { GraphNode, GraphNodeCatalog, GraphProjection } from "../../../lib/types";
import { GraphNodeCard } from "./GraphWorkflowCanvas";

const catalog: GraphNodeCatalog = {
  version: 1,
  nodes: [
    { node_type: "product_source", output_data_type: "product_facts", kind: "source", accepts: [] },
    { node_type: "image_asset", output_data_type: "image_asset", kind: "source", accepts: [] },
    { node_type: "creative_brief", output_data_type: "creative_brief", kind: "source", accepts: [] },
    { node_type: "visual_system", output_data_type: "visual_system", kind: "source", accepts: [] },
    {
      node_type: "prompt_generation",
      output_data_type: "prompt",
      kind: "processing",
      accepts: [
        { data_type: "product_facts", role: "facts", max_count: null, required_to_run: false },
      ],
    },
    {
      node_type: "image_generation",
      output_data_type: "image_asset",
      kind: "processing",
      accepts: [
        { data_type: "prompt", role: "prompt", max_count: 1, required_to_run: true },
      ],
    },
  ],
};

function graphNode(partial: Pick<GraphNode, "id" | "node_type"> & Partial<GraphNode>): GraphNode {
  return {
    title: partial.title ?? partial.id,
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
    ...partial,
  };
}

function graphWith(node: GraphNode): GraphProjection {
  return {
    id: "g1",
    product_id: "p1",
    title: "夏季主图",
    schema_version: 3,
    revision: 1,
    source_draft_revision_id: null,
    last_operation_group_id: null,
    nodes: [node],
    edges: [],
    groups: [],
  };
}

function renderNodeCard(node: GraphNode, connectable = true): string {
  const graph = graphWith(node);
  const props: ComponentProps<typeof GraphNodeCard> = {
    id: node.id,
    type: "graph-node",
    data: {
      kind: "node",
      node,
      status: "idle",
      runBusy: false,
      structureBusy: false,
      onRun: () => undefined,
      onBind: () => undefined,
      onDuplicate: () => undefined,
      onDelete: () => undefined,
      onSelectNode: () => undefined,
      graph,
      catalog,
    },
    dragging: false,
    zIndex: 0,
    selectable: true,
    deletable: false,
    selected: false,
    draggable: true,
    isConnectable: connectable,
    positionAbsoluteX: 0,
    positionAbsoluteY: 0,
  };
  return renderToStaticMarkup(
    createElement(ReactFlowProvider, null, createElement(GraphNodeCard, props)),
  );
}

function handleMarkup(markup: string): string[] {
  return markup.match(/<div[^>]*class="react-flow__handle[^"]*"[^>]*>/g) ?? [];
}

describe("graph workflow node ports", () => {
  it("renders connectable input and output handles for processing nodes", () => {
    const handles = handleMarkup(renderNodeCard(graphNode({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
    })));

    expect(handles).toHaveLength(2);
    expect(handles.some((handle) => handle.includes('data-handleid="input"') && handle.includes('data-handlepos="left"'))).toBe(true);
    expect(handles.some((handle) => handle.includes('data-handleid="output"') && handle.includes('data-handlepos="right"'))).toBe(true);
    expect(handles.some((handle) => handle.includes('aria-label="输入连接点"'))).toBe(true);
    expect(handles.some((handle) => handle.includes('aria-label="输出连接点"'))).toBe(true);
    expect(handles.every((handle) => !handle.includes('aria-hidden="true"'))).toBe(true);
  });

  it("omits the input handle on source nodes that cannot accept edges", () => {
    const handles = handleMarkup(renderNodeCard(graphNode({
      id: "source",
      node_type: "product_source",
      title: "商品资料",
    })));

    expect(handles).toHaveLength(1);
    expect(handles[0]).toContain('data-handleid="output"');
    expect(handles[0]).not.toContain('data-handleid="input"');
  });
});
