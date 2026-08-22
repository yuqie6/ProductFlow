import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { GraphAddNodePanel } from "./GraphAddNodePanel";

describe("GraphAddNodePanel", () => {
  it("lists every v3 node type with a purpose line and no JSON dump", () => {
    const markup = renderToStaticMarkup(createElement(GraphAddNodePanel, {
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

  it("exposes recipes as an optional entrance, not a node type", () => {
    const markup = renderToStaticMarkup(createElement(GraphAddNodePanel, {
      busy: false,
      onCreate: () => undefined,
      onOpenRecipesTab: () => undefined,
    }));
    expect(markup).toContain("打开配方");
  });
});
