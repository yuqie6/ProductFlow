import { ReactFlowProvider } from "@xyflow/react";
import { createElement, type ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { ProductWorkflowV2, WorkflowNodeV2 } from "../../../lib/types";
import type { GlobalFolderNode } from "./graph";
import { WorkflowFolderCard, WorkflowNodeCard } from "./V2WorkflowCanvas";

function renderFolderCard({ inbound, outbound }: { inbound: number; outbound: number }): string {
  const projection: GlobalFolderNode = {
    kind: "folder",
    id: "folder:folder-1",
    folder: {
      id: "folder-1",
      workflow_id: "workflow-1",
      key: "hero",
      title: "首屏海报图",
      order: 0,
      created_at: "2026-08-15T00:00:00Z",
      updated_at: "2026-08-15T00:00:00Z",
    },
    member_ids: ["prompt-1", "image-1"],
    bounds: { x: 0, y: 0, width: 652, height: 520 },
    summary: {
      member_count: 2,
      node_types: ["prompt_generation", "image_generation"],
      status: "idle",
      preview_asset_ids: [],
      inbound_edge_count: inbound,
      outbound_edge_count: outbound,
    },
    position: { x: 0, y: 0 },
  };
  const props: ComponentProps<typeof WorkflowFolderCard> = {
    id: projection.id,
    type: "workflow-folder-v2",
    data: {
      kind: "folder",
      projection,
      revealActive: false,
      structureBusy: false,
      onOpen: () => undefined,
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

  return renderToStaticMarkup(
    createElement(ReactFlowProvider, null, createElement(WorkflowFolderCard, props)),
  );
}

function renderImageNodeCard({ selected, connectable }: { selected: boolean; connectable: boolean }): string {
  const node: WorkflowNodeV2 = {
    id: "image-1",
    workflow_id: "workflow-1",
    schema_version: 2,
    key: "image-1",
    node_type: "image_generation",
    title: "首屏海报图 1",
    position_x: 0,
    position_y: 0,
    folder_id: "folder-1",
    bound_image_asset_id: null,
    current_prompt_artifact_version_id: null,
    config_json: {},
    status: "idle",
    output_json: null,
    failure_reason: null,
    created_at: "2026-08-15T00:00:00Z",
    updated_at: "2026-08-15T00:00:00Z",
  };
  const workflow: ProductWorkflowV2 = {
    id: "workflow-1",
    product_id: "product-1",
    title: "测试工作流",
    active: true,
    schema_version: 2,
    revision: 1,
    edit_version: 1,
    source_draft_revision_id: "draft-revision-1",
    visual_system_version_id: "visual-system-1",
    materialization_id: "materialization-1",
    folders: [],
    nodes: [node],
    edges: [],
    created_at: "2026-08-15T00:00:00Z",
    updated_at: "2026-08-15T00:00:00Z",
  };
  const props: ComponentProps<typeof WorkflowNodeCard> = {
    id: node.id,
    type: "workflow-node-v2",
    data: {
      kind: "node",
      node,
      revealActive: false,
      runBusy: false,
      structureBusy: false,
      canMutate: true,
      onRun: () => undefined,
      onBindReference: () => undefined,
      onDuplicate: () => undefined,
      onDelete: () => undefined,
      onSelectNode: () => undefined,
      workflow,
      inputHandleIds: ["facts", "reference", "prompt"],
      outputHandleIds: ["image"],
      externalInbounds: [],
      externalOutbounds: [],
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
    createElement(ReactFlowProvider, null, createElement(WorkflowNodeCard, props)),
  );
}

function handleMarkup(markup: string): string[] {
  return markup.match(/<div[^>]*class="react-flow__handle [^>]*><\/div>/g) ?? [];
}

describe("v2 workflow folder group frame", () => {
  it("renders group frame with title and member count without fake handles", () => {
    const markup = renderFolderCard({ inbound: 2, outbound: 1 });

    expect(markup).toContain("首屏海报图");
    expect(markup).toContain("2 个节点");
    expect(markup).not.toContain('class="react-flow__handle ');
  });
});

describe("v2 workflow real node ports", () => {
  it("collapses multiple semantic inputs into one read-only presentation anchor by default", () => {
    const handles = handleMarkup(renderImageNodeCard({ selected: false, connectable: true }));
    const visibleTargets = handles.filter(
      (handle) => handle.includes('data-handlepos="left"') && !handle.includes('aria-hidden="true"'),
    );

    expect(handles).toHaveLength(5);
    expect(handles.filter((handle) => handle.includes('aria-hidden="true"'))).toHaveLength(3);
    expect(visibleTargets).toHaveLength(1);
    expect(visibleTargets[0]).toContain('aria-label="输入（3 类）"');
  });

  it("expands readable semantic inputs when the editable node is selected", () => {
    const handles = handleMarkup(renderImageNodeCard({ selected: true, connectable: true }));
    const targets = handles.filter((handle) => handle.includes('data-handlepos="left"'));

    expect(handles).toHaveLength(4);
    expect(targets).toHaveLength(3);
    expect(handles.some((handle) => handle.includes('aria-hidden="true"'))).toBe(false);
    expect(targets.some((handle) => handle.includes('aria-label="输入连接点: 商品资料"'))).toBe(true);
    expect(targets.some((handle) => handle.includes('aria-label="输入连接点: 参考图"'))).toBe(true);
    expect(targets.some((handle) => handle.includes('aria-label="输入连接点: 提示词"'))).toBe(true);
  });
});
