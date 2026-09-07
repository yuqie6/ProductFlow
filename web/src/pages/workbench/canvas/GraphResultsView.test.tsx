import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createElement, type ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { PreferencesProvider } from "../../../lib/preferences";
import { GraphResultsView } from "./GraphResultsView";
import type { GraphResultSection } from "./resultProjection";

const sections: GraphResultSection[] = [
  {
    key: "group-1",
    kind: "group",
    groupId: "group-1",
    title: "主视觉",
    items: [
      {
        nodeId: "node-ok",
        kind: "generation",
        title: "主图 1",
        imageTypeKey: "hero",
        groupId: "group-1",
        groupTitle: "主视觉",
        status: "succeeded",
        failureReason: null,
        currentAssetId: "asset-1",
        showingStaleCurrent: false,
        runnable: true,
      },
      {
        nodeId: "node-missing",
        kind: "generation",
        title: "主图 2",
        imageTypeKey: "hero",
        groupId: "group-1",
        groupTitle: "主视觉",
        status: "idle",
        failureReason: null,
        currentAssetId: null,
        showingStaleCurrent: false,
        runnable: true,
      },
      {
        nodeId: "node-fail",
        kind: "generation",
        title: "主图失败但仍有旧图",
        imageTypeKey: "hero",
        groupId: "group-1",
        groupTitle: "主视觉",
        status: "failed",
        failureReason: "上游缺少输入",
        currentAssetId: "asset-old",
        showingStaleCurrent: true,
        runnable: true,
      },
    ],
  },
  {
    key: "ungrouped",
    kind: "ungrouped",
    groupId: null,
    title: "",
    items: [
      {
        nodeId: "node-solo",
        kind: "generation",
        title: "未分组长标题用于检查截断是否稳定并且不遮挡操作按钮",
        imageTypeKey: "detail",
        groupId: null,
        groupTitle: null,
        status: "unknown",
        failureReason: null,
        currentAssetId: null,
        showingStaleCurrent: false,
        runnable: true,
      },
    ],
  },
  {
    key: "evidence",
    kind: "evidence",
    groupId: null,
    title: "",
    items: [
      {
        nodeId: "evidence-1",
        kind: "evidence",
        title: "认证材料",
        imageTypeKey: null,
        groupId: null,
        groupTitle: null,
        status: "idle",
        failureReason: null,
        currentAssetId: null,
        showingStaleCurrent: false,
        runnable: false,
      },
    ],
  },
];

function renderResults(overrides: Partial<ComponentProps<typeof GraphResultsView>> = {}): string {
  const queryClient = new QueryClient();
  return renderToStaticMarkup(
    createElement(QueryClientProvider, { client: queryClient },
      createElement(PreferencesProvider, null,
        createElement(GraphResultsView, {
          sections,
          busy: false,
          runningNodeId: null,
          selectedNodeIds: [],
          onSelectItem: vi.fn(),
          onLocateItem: vi.fn(),
          onOpenHistory: vi.fn(),
          onRunItem: vi.fn(),
          onOpenLocalEdit: vi.fn(),
          onBindEvidence: vi.fn(),
          ...overrides,
        }),
      ),
    ),
  );
}

describe("GraphResultsView", () => {
  it("renders every result item once with section organization", () => {
    const markup = renderResults();
    expect(markup).toContain("data-graph-results-view");
    expect(markup).toContain('data-graph-result-item="node-ok"');
    expect(markup).toContain('data-graph-result-item="node-missing"');
    expect(markup).toContain('data-graph-result-item="node-fail"');
    expect(markup).toContain('data-graph-result-item="node-solo"');
    expect(markup).toContain('data-graph-result-item="evidence-1"');
    expect(markup.match(/data-graph-result-item="/g)).toHaveLength(5);
    expect(markup).toContain('data-graph-results-section-kind="group"');
    expect(markup).toContain('data-graph-results-section-kind="ungrouped"');
    expect(markup).toContain('data-graph-results-section-kind="evidence"');
  });

  it("exposes locate, history, run, and edit entry points; adoption stays opt-in", () => {
    const markup = renderResults({ selectedNodeIds: ["node-fail"] });
    expect(markup).toContain("data-graph-result-locate");
    expect(markup).toContain("data-graph-result-history");
    expect(markup).toContain("data-graph-result-run");
    expect(markup).toContain("data-graph-result-edit");
    expect(markup).toContain('data-graph-result-stale="true"');
    expect(markup).toContain("上游缺少输入");
    expect(markup).not.toContain("data-graph-result-adopt");
    expect(markup).not.toContain("data-graph-results-export-adoption");
  });

  it("shows delivery adoption controls when handlers and adopted map are provided", () => {
    const markup = renderResults({
      adoptedAssetBySlot: new Map([["node-ok", "asset-1"]]),
      onAdoptItem: vi.fn(),
      onExportAdoption: vi.fn(),
    });
    expect(markup).toContain('data-graph-result-delivery-adopted="true"');
    expect(markup).toContain("data-graph-result-adopt");
    expect(markup).toContain("data-graph-results-export-adoption");
  });

  it("keeps evidence bind entry and omits run for evidence items", () => {
    const markup = renderResults();
    expect(markup).toContain('data-graph-result-kind="evidence"');
    expect(markup).toContain("data-graph-result-bind");
    const evidenceSlice = markup.slice(markup.indexOf('data-graph-result-item="evidence-1"'));
    expect(evidenceSlice).not.toContain("data-graph-result-run");
  });

  it("marks blocked run actions and planned generate state", () => {
    const markup = renderResults({
      blockedReasons: { "node-missing": "缺少提示词" },
      plannedActions: { "node-missing": "generate" },
      onPreviewRun: vi.fn(),
    });
    expect(markup).toContain("缺少提示词");
    expect(markup).toContain('data-graph-planned-action="generate"');
    expect(markup).toContain("disabled");
  });
});
