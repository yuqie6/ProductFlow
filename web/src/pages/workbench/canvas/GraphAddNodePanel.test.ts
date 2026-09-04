import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { GraphAddNodePanel } from "./GraphAddNodePanel";
import { humanizeCatalogKey } from "./CatalogConfigFields";
import type { GraphNodeCatalog } from "../../../lib/types";

const catalog: GraphNodeCatalog = {
  version: 1,
  nodes: [
    { node_type: "product_source", output_data_type: "product_facts", kind: "source", accepts: [] },
    { node_type: "image_asset", output_data_type: "image_asset", kind: "source", accepts: [] },
    { node_type: "creative_brief", output_data_type: "creative_brief", kind: "document", accepts: [] },
    { node_type: "visual_system", output_data_type: "visual_system", kind: "document", accepts: [] },
    { node_type: "image_prompt", output_data_type: "prompt", kind: "document", accepts: [] },
    { node_type: "image_generation", output_data_type: "image_asset", kind: "effect", accepts: [] },
  ],
};

describe("humanizeCatalogKey", () => {
  it("turns unknown catalog keys into readable labels", () => {
    expect(humanizeCatalogKey("future_quality_mode")).toBe("Future Quality Mode");
  });
});

describe("GraphAddNodePanel", () => {
  it("lists every v3 node type with a purpose line and no JSON dump", () => {
    const markup = renderToStaticMarkup(createElement(GraphAddNodePanel, {
      catalog,
      busy: false,
      onCreate: () => undefined,
    }));
    expect(markup).toContain("商品资料");
    expect(markup).toContain("图片素材");
    expect(markup).toContain("创作要求");
    expect(markup).toContain("视觉规范");
    expect(markup).toContain("提示词生成");
    expect(markup).toContain("图片生成");
    expect(markup).toContain("提供商品名称、类目和卖点");
    expect(markup).not.toContain("product_source");
  });

  it("shows a recoverable error instead of a hardcoded palette when catalog is missing", () => {
    const markup = renderToStaticMarkup(createElement(GraphAddNodePanel, {
      catalog: null,
      catalogError: "配置暂时加载失败。",
      onRetryCatalog: () => undefined,
      busy: false,
      onCreate: () => undefined,
    }));
    expect(markup).toContain("配置暂时加载失败。");
    expect(markup).toContain("role=\"alert\"");
    expect(markup).not.toContain("提供商品名称、类目和卖点");
  });

  it("shows selection commands only when they apply, including dissolve", () => {
    const withoutSelection = renderToStaticMarkup(createElement(GraphAddNodePanel, {
      busy: false,
      onCreate: () => undefined,
      onDuplicate: () => undefined,
      onGroup: () => undefined,
      onDissolve: () => undefined,
    }));
    expect(withoutSelection).not.toContain("复制");
    expect(withoutSelection).not.toContain("取消编组");

    const withSelection = renderToStaticMarkup(createElement(GraphAddNodePanel, {
      busy: false,
      onCreate: () => undefined,
      canDuplicate: true,
      canGroup: true,
      canDissolve: true,
      onDuplicate: () => undefined,
      onGroup: () => undefined,
      onDissolve: () => undefined,
    }));
    expect(withSelection).toContain("复制");
    expect(withSelection).toContain("编组");
    expect(withSelection).toContain("取消编组");
  });

  it("shows save-recipe commands when they apply", () => {
    const markup = renderToStaticMarkup(createElement(GraphAddNodePanel, {
      busy: false,
      onCreate: () => undefined,
      canSaveFull: true,
      canSaveGroup: true,
      canSaveSelection: true,
      onSaveFull: () => undefined,
      onSaveGroup: () => undefined,
      onSaveSelection: () => undefined,
    }));
    expect(markup).toContain("保存完整工作流预设");
    expect(markup).toContain("保存当前分组为预设");
    expect(markup).toContain("进入分组，或选中同一组的节点。");
    expect(markup).toContain("保存选中节点为预设");
  });

  it("shows add-shot when the canvas can create a shot", () => {
    const markup = renderToStaticMarkup(createElement(GraphAddNodePanel, {
      busy: false,
      onCreate: () => undefined,
      onCreateShot: () => undefined,
    }));
    expect(markup).toContain("添加场景");
    expect(markup).toContain("data-add-shot");
    expect(markup).toContain("封面主图");
  });

  it("exposes recipes as an optional entrance, not a node type", () => {
    const markup = renderToStaticMarkup(createElement(GraphAddNodePanel, {
      busy: false,
      onCreate: () => undefined,
      onOpenRecipesTab: () => undefined,
    }));
    expect(markup).toContain("打开配方");
  });
});
