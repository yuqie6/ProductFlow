import { ReactFlowProvider } from "@xyflow/react";
import { createElement, type ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { GlobalFolderNode } from "./graph";
import { WorkflowFolderCard } from "./V2WorkflowCanvas";

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

describe("v2 workflow folder projection card", () => {
  it("renders presentation-only handles for projected inbound and outbound edges", () => {
    const markup = renderFolderCard({ inbound: 2, outbound: 1 });

    expect(markup.match(/class="react-flow__handle /g)).toHaveLength(2);
    expect(markup).toContain('data-handlepos="left"');
    expect(markup).toContain('data-handlepos="right"');
    expect(markup).toContain('aria-label="输入 2"');
    expect(markup).toContain('aria-label="输出 1"');
  });

  it("omits handles when the folder has no projected boundary edges", () => {
    const markup = renderFolderCard({ inbound: 0, outbound: 0 });

    expect(markup).not.toContain('class="react-flow__handle ');
  });
});
