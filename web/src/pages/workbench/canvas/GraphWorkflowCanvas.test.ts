import { ReactFlowProvider } from "@xyflow/react";
import { createElement, type ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { GraphNode, GraphNodeCatalog, GraphProjection } from "../../../lib/types";
import {
  GRAPH_PORT_MAX_VISUAL_SCALE,
  graphEdgeEmphasis,
  graphPortVisualScale,
} from "./graphCanvasVisual";
import { graphNodeHasPinnableOutput } from "./graphLayout";
import { GraphGroupCard, GraphNodeCard, rejectedGraphConnectionNotice } from "./GraphWorkflowCanvas";

const catalog: GraphNodeCatalog = {
  version: 1,
  nodes: [
    { node_type: "product_source", output_data_type: "product_facts", kind: "source", accepts: [] },
    { node_type: "image_asset", output_data_type: "image_asset", kind: "source", accepts: [] },
    { node_type: "creative_brief", output_data_type: "creative_brief", kind: "processing", accepts: [] },
    { node_type: "visual_system", output_data_type: "visual_system", kind: "processing", accepts: [] },
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
    can_undo: false,
    can_redo: false,
    nodes: [node],
    edges: [],
    groups: [],
  };
}

function renderNodeCard(
  node: GraphNode,
  connectable = true,
  runDisabled = false,
  selected = false,
): string {
  const graph = graphWith(node);
  const props: ComponentProps<typeof GraphNodeCard> = {
    id: node.id,
    type: "graph-node",
    data: {
      kind: "node",
      node,
      status: "idle",
      failureReason: null,
      lastRunAt: null,
      retryable: false,
      runBusy: false,
      runDisabled,
      structureBusy: false,
      onRun: () => undefined,
      onRunToNode: () => undefined,
      onBind: () => undefined,
      onPin: () => undefined,
      onDuplicate: () => undefined,
      onSaveRecipe: () => undefined,
      onDelete: () => undefined,
      selectedCount: 1,
      selectionPrimary: true,
      missingRunLabels: [],
      onSelectNode: () => undefined,
      graph,
      catalog,
    },
    dragging: false,
    zIndex: 0,
    selectable: true,
    deletable: false,
    selected,
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

  it("gives processing and source nodes distinct type colors and can show failure on the card", () => {
    const image = renderNodeCard(graphNode({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
    }));
    const source = renderNodeCard(graphNode({
      id: "source",
      node_type: "product_source",
      title: "商品资料",
    }));
    expect(image).toContain("data-node-kind=\"image_generation\"");
    expect(source).toContain("data-node-kind=\"product_source\"");
    expect(image).toContain("cyan-");
    expect(source).toContain("purple-");
    expect(image).not.toBe(source);
  });

  it("offers pin-as-image-asset on an image_generation node with current output", () => {
    expect(graphNodeHasPinnableOutput(graphNode({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
      preview_asset_id: "asset-out",
    }))).toBe(true);
    expect(graphNodeHasPinnableOutput(graphNode({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
    }))).toBe(false);
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

  it("writes failure on the card instead of only in the run sidebar", () => {
    const graph = graphWith(graphNode({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
    }));
    const props: ComponentProps<typeof GraphNodeCard> = {
      id: "image",
      type: "graph-node",
      data: {
        kind: "node",
        node: graph.nodes[0],
        status: "failed",
        failureReason: "模型超时",
        lastRunAt: "2026-08-21T00:00:00Z",
        retryable: true,
        runBusy: false,
        runDisabled: false,
        structureBusy: false,
        onRun: () => undefined,
        onRunToNode: () => undefined,
        onBind: () => undefined,
        onDuplicate: () => undefined,
        onDelete: () => undefined,
        onSaveRecipe: () => undefined,
        selectedCount: 1,
        selectionPrimary: true,
        missingRunLabels: [],
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
      isConnectable: true,
      positionAbsoluteX: 0,
      positionAbsoluteY: 0,
    };
    const markup = renderToStaticMarkup(
      createElement(ReactFlowProvider, null, createElement(GraphNodeCard, props)),
    );
    expect(markup).toContain("模型超时");
    expect(markup).toContain("可重试");
  });

  it("shows missing Catalog required_to_run inputs on the card", () => {
    const graph = graphWith(graphNode({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
    }));
    const props: ComponentProps<typeof GraphNodeCard> = {
      id: "image",
      type: "graph-node",
      data: {
        kind: "node",
        node: graph.nodes[0],
        status: "idle",
        failureReason: null,
        lastRunAt: null,
        retryable: false,
        runBusy: false,
        runDisabled: false,
        structureBusy: false,
        onRun: () => undefined,
        onRunToNode: () => undefined,
        onBind: () => undefined,
        onDuplicate: () => undefined,
        onSaveRecipe: () => undefined,
        onDelete: () => undefined,
        selectedCount: 1,
        selectionPrimary: true,
        missingRunLabels: ["还缺提示词，先连上再运行"],
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
      isConnectable: true,
      positionAbsoluteX: 0,
      positionAbsoluteY: 0,
    };
    const gapMarkup = renderToStaticMarkup(
      createElement(ReactFlowProvider, null, createElement(GraphNodeCard, props)),
    );
    expect(gapMarkup).toContain("data-graph-missing-run-input");
    expect(gapMarkup).toContain("还缺提示词，先连上再运行");
  });

  it("puts copy, group, delete, and save-as-recipe on a multi-selection toolbar", () => {
    const graph = graphWith(graphNode({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
    }));
    const props: ComponentProps<typeof GraphNodeCard> = {
      id: "image",
      type: "graph-node",
      data: {
        kind: "node",
        node: graph.nodes[0],
        status: "idle",
        failureReason: null,
        lastRunAt: null,
        retryable: false,
        runBusy: false,
        runDisabled: false,
        structureBusy: false,
        onRun: () => undefined,
        onRunToNode: () => undefined,
        onBind: () => undefined,
        onDuplicate: () => undefined,
        onSaveRecipe: () => undefined,
        onDelete: () => undefined,
        onDuplicateSelection: () => undefined,
        onGroupSelection: () => undefined,
        onSaveSelection: () => undefined,
        onDeleteSelection: () => undefined,
        selectedCount: 2,
        selectionPrimary: true,
        missingRunLabels: [],
        onSelectNode: () => undefined,
        graph,
        catalog,
      },
      dragging: false,
      zIndex: 0,
      selectable: true,
      deletable: false,
      selected: true,
      draggable: true,
      isConnectable: true,
      positionAbsoluteX: 0,
      positionAbsoluteY: 0,
    };
    const markup = renderToStaticMarkup(
      createElement(ReactFlowProvider, null, createElement(GraphNodeCard, props)),
    );
    expect(markup).toContain("data-graph-selection-actions");
    expect(markup).toContain("复制");
    expect(markup).toContain("编组");
    expect(markup).toContain("存为配方");
    expect(markup).toContain("删除");
  });
});

describe("graph canvas visual scale", () => {
  it("keeps port scale bounded when zoomed out", () => {
    expect(graphPortVisualScale(0.2)).toBeLessThanOrEqual(GRAPH_PORT_MAX_VISUAL_SCALE);
    expect(graphPortVisualScale(0.2)).toBe(GRAPH_PORT_MAX_VISUAL_SCALE);
    expect(graphPortVisualScale(1)).toBe(1);
    expect(GRAPH_PORT_MAX_VISUAL_SCALE).toBeLessThanOrEqual(1.4);
  });

  it("recedes unselected edges and emphasizes edges touching the selection", () => {
    expect(graphEdgeEmphasis({ edgeSelected: false, sourceSelected: false, targetSelected: false })).toBe("receded");
    expect(graphEdgeEmphasis({ edgeSelected: false, sourceSelected: true, targetSelected: false })).toBe("active");
    expect(graphEdgeEmphasis({ edgeSelected: true, sourceSelected: false, targetSelected: false })).toBe("active");
  });
});

describe("rejectedGraphConnectionNotice", () => {
  it("returns the catalog reason when a drop is illegal", () => {
    const graph: GraphProjection = {
      id: "g1",
      product_id: "p1",
      title: "夏季主图",
      schema_version: 3,
      revision: 1,
      source_draft_revision_id: null,
      last_operation_group_id: null,
      can_undo: false,
      can_redo: false,
      nodes: [
        graphNode({ id: "source", node_type: "product_source", title: "商品资料" }),
        graphNode({ id: "image", node_type: "image_generation", title: "主图 1" }),
      ],
      edges: [],
      groups: [],
    };
    expect(rejectedGraphConnectionNotice(graph, {
      isValid: false,
      fromNodeId: "source",
      toNodeId: "image",
      toHandleId: "input",
    }, catalog)).toBe("graph.connect.incompatible");
    expect(rejectedGraphConnectionNotice(graph, {
      isValid: true,
      fromNodeId: "source",
      toNodeId: "image",
      toHandleId: "input",
    }, catalog)).toBeNull();
    expect(rejectedGraphConnectionNotice(graph, {
      isValid: false,
      fromNodeId: "source",
      toNodeId: null,
      toHandleId: "input",
    }, catalog)).toBeNull();
    expect(rejectedGraphConnectionNotice(graph, {
      isValid: false,
      fromNodeId: "source",
      toNodeId: "image",
      toHandleId: null,
    }, catalog)).toBeNull();
  });
});

describe("graph group chrome", () => {
  it("can enter a group and does not grow ports or run controls", () => {
    const props: ComponentProps<typeof GraphGroupCard> = {
      id: "group:group-1",
      type: "graph-group",
      data: {
        kind: "group",
        group: { id: "group-1", title: "主图组", member_ids: ["prompt"] },
        bounds: { x: 0, y: 0, width: 320, height: 280 },
        runDisabled: false,
        structureBusy: false,
        onEnter: () => undefined,
        onRename: () => undefined,
        onDissolve: () => undefined,
      },
      dragging: false,
      zIndex: 0,
      selectable: true,
      deletable: false,
      selected: false,
      draggable: true,
      isConnectable: false,
      positionAbsoluteX: 0,
      positionAbsoluteY: 0,
    };
    const markup = renderToStaticMarkup(
      createElement(ReactFlowProvider, null, createElement(GraphGroupCard, props)),
    );
    expect(markup).toContain("进入");
    expect(markup).toContain("data-enter-group");
    expect(markup).toContain("主图组");
    expect(markup).toContain('aria-hidden="true"');
    expect(handleMarkup(markup)).toEqual([]);
    expect(markup).not.toContain("运行该节点");
    expect(markup).not.toContain("graph.canvas.runNode");
  });

  it("can disable the group run without locking group navigation", () => {
    const props: ComponentProps<typeof GraphGroupCard> = {
      id: "group:group-1",
      type: "graph-group",
      data: {
        kind: "group",
        group: { id: "group-1", title: "主图组", member_ids: ["prompt"] },
        bounds: { x: 0, y: 0, width: 320, height: 280 },
        runDisabled: true,
        structureBusy: false,
        onEnter: () => undefined,
        onRename: () => undefined,
        onDissolve: () => undefined,
        onRunShot: () => undefined,
      },
      dragging: false,
      zIndex: 0,
      selectable: true,
      deletable: false,
      selected: false,
      draggable: true,
      isConnectable: false,
      positionAbsoluteX: 0,
      positionAbsoluteY: 0,
    };
    const markup = renderToStaticMarkup(
      createElement(ReactFlowProvider, null, createElement(GraphGroupCard, props)),
    );
    const runButton = markup.match(/<button[^>]*data-run-shot=""[^>]*>/)?.[0] ?? "";
    const enterButton = markup.match(/<button[^>]*data-enter-group=""[^>]*>/)?.[0] ?? "";

    expect(runButton).toContain('disabled=""');
    expect(enterButton).not.toContain(' disabled=""');
  });
});
