import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { ApiError } from "../../lib/api";
import type { CanonicalProductDetail, GraphProjection } from "../../lib/types";
import { GraphAgentPanel, GraphWorkbenchPage } from "./GraphWorkbenchPage";

describe("GraphAgentPanel", () => {
  it("renders the Agent chrome instead of an empty sidebar slot", () => {
    const markup = renderToStaticMarkup(createElement(GraphAgentPanel));

    expect(markup).toContain("data-graph-agent-panel");
    expect(markup).toContain("工作流 Agent");
    expect(markup).toContain("还没有挂上这个商品的 Agent 对话。");
  });

  it("surfaces a recoverable ensure failure", () => {
    const markup = renderToStaticMarkup(createElement(GraphAgentPanel, {
      error: new ApiError(409, "当前商品还没有可执行的工作流"),
      onRetry: () => undefined,
    }));

    expect(markup).toContain("当前商品还没有可执行的工作流");
    expect(markup).toContain("重试连接 Agent");
  });

  it("exposes recipe preview and apply on the graph-only workbench", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const product = {
      id: "p1",
      name: "夏季主图",
    } as CanonicalProductDetail;
    const graph = {
      id: "g1",
      product_id: "p1",
      title: "夏季主图",
      schema_version: 3,
      revision: 1,
      source_draft_revision_id: null,
      last_operation_group_id: null,
      can_undo: false,
      can_redo: false,
      nodes: [],
      edges: [],
      groups: [],
    } as GraphProjection;
    const markup = renderToStaticMarkup(createElement(
      QueryClientProvider,
      { client },
      createElement(MemoryRouter, null, createElement(GraphWorkbenchPage, {
        product,
        initialGraph: graph,
      })),
    ));
    expect(markup).toContain('data-sidebar-tool="recipes"');
    expect(markup).toContain("工作流预设");
  });
});
