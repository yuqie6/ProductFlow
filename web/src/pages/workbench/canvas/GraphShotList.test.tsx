import { createElement, type ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import type { GraphShotProjection } from "./shotProjection";
import { GraphShotList } from "./GraphShotList";

const shot: GraphShotProjection = {
  groupId: "group-1",
  title: "春季主图",
  imageNodeIds: ["image-1", "image-2"],
  primaryImageNodeId: "image-1",
  primaryImageAssetId: "asset-1",
  imageNodeCount: 2,
  completedImageCount: 1,
  latestNodeStatus: "failed",
  latestFailureReason: "供应商超时",
  currentResultAssetIds: ["asset-1"],
};

function renderList(overrides: Partial<ComponentProps<typeof GraphShotList>> = {}): string {
  return renderToStaticMarkup(
    createElement(GraphShotList, {
      shots: [shot],
      busy: false,
      runningGroupId: null,
      onOpenNode: vi.fn(),
      onRunShot: vi.fn(),
      onRunAll: vi.fn(),
      ...overrides,
    }),
  );
}

describe("GraphShotList", () => {
  it("renders projected counts, current thumbnail, failure reason, and actions", () => {
    const markup = renderList();

    expect(markup).toContain('data-graph-shot-list');
    expect(markup).toContain('data-graph-shot-id="group-1"');
    expect(markup).toContain("春季主图");
    expect(markup).toContain("2 张");
    expect(markup).toContain("已完成 1 张");
    expect(markup).toContain("供应商超时");
    expect(markup).toContain("asset-1");
    expect(markup).toContain("打开节点详情");
    expect(markup).toContain("生成套图");
  });

  it("keeps a shot action visibly busy for the whole active shot", () => {
    const markup = renderList({ runningGroupId: "group-1" });

    expect(markup).toContain('data-graph-shot-running="true"');
    expect(markup).toContain('aria-busy="true"');
    expect(markup).toContain("提交中");
    expect(markup).toContain("disabled");
  });

  it("exposes the local edit action only with the projected primary asset", () => {
    const markup = renderList({ onOpenLocalEdit: vi.fn() });

    expect(markup).toContain('data-graph-shot-local-edit');
    expect(markup).toContain("局部编辑");
  });
});
