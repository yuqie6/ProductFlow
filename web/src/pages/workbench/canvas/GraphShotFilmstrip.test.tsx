import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createElement, type ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { PreferencesProvider } from "../../../lib/preferences";
import { GraphShotFilmstrip } from "./GraphShotFilmstrip";

const shot = {
  groupId: "group-1",
  title: "主视觉",
  imageNodeIds: ["node-1", "node-2"],
  primaryImageNodeId: "node-1",
  primaryImageAssetId: "asset-1",
  imageNodeCount: 2,
  completedImageCount: 1,
  latestNodeStatus: "failed" as const,
  latestFailureReason: "上游缺少输入",
  currentResultAssetIds: ["asset-1"],
};

function renderFilmstrip(overrides: Partial<ComponentProps<typeof GraphShotFilmstrip>> = {}): string {
  const queryClient = new QueryClient();
  return renderToStaticMarkup(
    createElement(QueryClientProvider, { client: queryClient },
      createElement(PreferencesProvider, null,
        createElement(GraphShotFilmstrip, {
          shots: [shot],
          busy: false,
          runningGroupId: null,
          selectedNodeIds: [],
          onFocusShot: vi.fn(),
          onRunShot: vi.fn(),
          ...overrides,
        }),
      ),
    ),
  );
}

describe("GraphShotFilmstrip", () => {
  it("renders a horizontal filmstrip with a focus target and compact title", () => {
    const markup = renderFilmstrip();

    expect(markup).toContain("data-graph-shot-filmstrip");
    expect(markup).toContain("overflow-x-auto");
    expect(markup).toContain("data-graph-shot-focus");
    expect(markup).toContain("主视觉");
    expect(markup).not.toContain("data-graph-shot-open-node");
    expect(markup).not.toContain("最新状态");
  });

  it("expresses failure through status data, border, and tooltip text", () => {
    const markup = renderFilmstrip();

    expect(markup).toContain('data-graph-shot-status="failed"');
    expect(markup).toContain("border-state-error");
    expect(markup).toContain("上游缺少输入");
  });

  it("keeps per-shot run and blocked reason on the icon action", () => {
    const markup = renderFilmstrip({
      blockedReasons: { "group-1": "缺少提示词" },
      onPreviewRun: vi.fn(),
      plannedActions: { "node-1": "generate" },
    });

    expect(markup).toContain("data-graph-shot-run");
    expect(markup).toContain("缺少提示词");
    expect(markup).toContain('data-graph-planned-action="generate"');
    expect(markup).toContain("disabled");
  });

  it("marks a shot selected when one of its image nodes is selected", () => {
    const markup = renderFilmstrip({ selectedNodeIds: ["node-2"] });

    expect(markup).toContain('aria-pressed="true"');
  });
});
