import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { GraphNode, GraphProjection } from "../../../lib/types";
import { GraphNodeInspector } from "./GraphNodeInspector";

function node(partial: Partial<GraphNode> & Pick<GraphNode, "id" | "node_type">): GraphNode {
  return {
    title: partial.title ?? partial.id,
    position_x: 0,
    position_y: 0,
    config: {},
    bound_asset_id: null,
    group_id: null,
    preview_asset_id: null,
    config_status: "incomplete",
    unused: false,
    incoming: [],
    outgoing: [],
    ...partial,
  };
}

const graph: GraphProjection = {
  id: "g1",
  product_id: "p1",
  title: "夏季主图",
  schema_version: 3,
  revision: 4,
  source_draft_revision_id: null,
    last_operation_group_id: null,
    can_undo: false,
    can_redo: false,
  nodes: [
    node({ id: "source", node_type: "product_source", title: "商品资料", config_status: "ready" }),
    node({
      id: "brief",
      node_type: "creative_brief",
      title: "创作要求",
      config: { goal: "突出瓶身", design_goals: ["主图清晰"] },
    }),
    node({
      id: "asset",
      node_type: "image_asset",
      title: "参考图 1",
      bound_asset_id: "asset-1",
      unused: true,
    }),
    node({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
      config: {
        generation_spec: {
          aspect_ratio: "4:5",
          resolution_tier: "high",
          quality_intent: "high",
          reference_fidelity: "high",
          background_intent: "auto",
          text_policy: "none",
        },
      },
    }),
  ],
  edges: [],
  groups: [],
};

function renderInspector(selected: GraphNode | null): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderToStaticMarkup(createElement(
    QueryClientProvider,
    { client },
    createElement(GraphNodeInspector, {
      graph,
      node: selected,
      busy: false,
      onCommit: async () => graph,
    }),
  ));
}

describe("GraphNodeInspector", () => {
  it("shows graph summary instead of a JSON dump when nothing is selected", () => {
    const markup = renderInspector(null);
    expect(markup).toContain("夏季主图");
    expect(markup).toContain("版本 4");
    expect(markup).toContain("点画布上的节点");
    expect(markup).not.toContain("generation_spec");
    expect(markup).not.toContain("保存配置");
  });

  it("renders typed generation settings instead of a raw config textarea", () => {
    const markup = renderInspector(graph.nodes.find((item) => item.id === "image") ?? null);
    expect(markup).toContain("画面比例");
    expect(markup).toContain("4:5");
    expect(markup).toContain("先连上提示词节点");
    expect(markup).not.toContain("generation_spec");
    expect(markup).toContain("运行该节点");
    expect(markup).toContain("运行到这里");
  });

  it("renders creative brief fields and bound-but-unused state", () => {
    const brief = renderInspector(graph.nodes.find((item) => item.id === "brief") ?? null);
    expect(brief).toContain("设计目标");
    expect(brief).toContain("突出瓶身");
    expect(brief).not.toContain("JSON");

    const asset = renderInspector(graph.nodes.find((item) => item.id === "asset") ?? null);
    expect(asset).toContain("已选图，还没被用到");
    expect(asset).not.toContain("还没选图");
  });

  it("marks a product source without a binding as incomplete instead of using the workbench product", () => {
    const markup = renderInspector(graph.nodes.find((item) => item.id === "source") ?? null);
    expect(markup).toContain("尚未绑定商品资料");
    expect(markup).toContain("搜索已有商品");
    expect(markup).not.toContain("这里只展示商品信息");
  });

  it("lets a visual system be edited as overlay fields instead of a version UUID", () => {
    const markup = renderInspector(node({
      id: "visual",
      node_type: "visual_system",
      title: "视觉规范",
      config: { visual_overlay: { style: ["干净白底"] } },
    }));
    expect(markup).toContain("风格关键词");
    expect(markup).toContain("干净白底");
    expect(markup).toContain("背景色");
    expect(markup).not.toContain("visual_system_version_id");
  });
});
