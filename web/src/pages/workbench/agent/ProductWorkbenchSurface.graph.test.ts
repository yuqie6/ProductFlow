import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "../../../lib/api";
import type {
  CanonicalProductDetail,
  GraphProjection,
  WorkflowRecipePreview,
  WorkflowRecipeSummary,
} from "../../../lib/types";
import { patchWorkbenchUiState, workbenchUiStorageKey } from "../chrome/workbenchUiState";
import {
  buildAgentWorkflowRecipeApplyInput,
  clearAgentWorkflowRecipeIdempotencyKey,
  GraphAgentPanel,
  ProductWorkbenchSurface,
} from "./ProductWorkbenchSurface";

describe("GraphAgentPanel", () => {
  it("renders the Agent chrome instead of an empty sidebar slot", () => {
    const markup = renderToStaticMarkup(createElement(GraphAgentPanel));

    expect(markup).toContain("data-graph-agent-panel");
    expect(markup).toContain("工作流 Agent");
    expect(markup).toContain("还没有挂上这个商品的 Agent 对话。");
  });

  it("surfaces a recoverable ensure failure", () => {
    const markup = renderToStaticMarkup(createElement(GraphAgentPanel, {
      error: new ApiError(500, "当前商品还没有可执行的工作流"),
      onRetry: () => undefined,
    }));

    expect(markup).toContain("当前商品还没有可执行的工作流");
    expect(markup).toContain("重试连接 Agent");
  });

  it("offers opening a canvas conversation when the workbench is missing", () => {
    const markup = renderToStaticMarkup(createElement(GraphAgentPanel, {
      error: new ApiError(409, "商品还没有 Agent 工作区"),
      onOpenConversation: () => undefined,
    }));

    expect(markup).toContain("data-open-canvas-conversation");
    expect(markup).toContain("data-agent-start-preview");
    expect(markup).toContain("让 Agent 参与这次商品创作");
    expect(markup).toContain("创建并打开对话");
    expect(markup).toContain("/agent-onboarding/ceramic-feature.jpg");
  });

  it("keeps the visual onboarding out of recoverable Agent failures", () => {
    const markup = renderToStaticMarkup(createElement(GraphAgentPanel, {
      error: new ApiError(500, "Agent 服务暂时不可用"),
      onRetry: () => undefined,
    }));

    expect(markup).not.toContain("data-agent-start-preview");
    expect(markup).toContain("Agent 服务暂时不可用");
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
      createElement(MemoryRouter, null, createElement(ProductWorkbenchSurface, {
        product,
        initialGraph: graph,
      })),
    ));
    expect(markup).toContain('data-graph-canvas-toolbar="true"');
    expect(markup).toContain('data-canvas-overlay="top-right"');
    expect(markup).toContain('data-sidebar-tool="recipes"');
    expect(markup).toContain("工作流预设");
    expect(markup).toContain('data-graph-main-view-panel="flow"');
  });

  it("opens on results when the graph already has a generation preview", () => {
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
      last_operation_group_id: null,
      can_undo: false,
      can_redo: false,
      nodes: [{
        id: "image-1",
        node_type: "image_generation",
        title: "主图",
        position_x: 0,
        position_y: 0,
        config: { image_type_key: "hero" },
        bound_asset_id: null,
        group_id: null,
        preview_asset_id: "asset-1",
        config_status: "ready",
        unused: false,
        incoming: [],
        outgoing: [],
      }],
      edges: [],
      groups: [],
    } as GraphProjection;
    const markup = renderToStaticMarkup(createElement(
      QueryClientProvider,
      { client },
      createElement(MemoryRouter, null, createElement(ProductWorkbenchSurface, {
        product,
        initialGraph: graph,
      })),
    ));
    expect(markup).toContain('data-graph-main-view-panel="results"');
    expect(markup).not.toContain('data-graph-canvas-toolbar="true"');
  });

  it("honors a stored product-level main-view preference over the conditional default", () => {
    const storage = new Map<string, string>();
    vi.stubGlobal("window", {
      localStorage: {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => {
          storage.set(key, value);
        },
        removeItem: (key: string) => {
          storage.delete(key);
        },
      },
    });
    try {
      patchWorkbenchUiState("p1", { mainView: "flow" });
      expect(storage.get(workbenchUiStorageKey("p1"))).toContain('"mainView":"flow"');

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
        last_operation_group_id: null,
        can_undo: false,
        can_redo: false,
        nodes: [{
          id: "image-1",
          node_type: "image_generation",
          title: "主图",
          position_x: 0,
          position_y: 0,
          config: { image_type_key: "hero" },
          bound_asset_id: null,
          group_id: null,
          preview_asset_id: "asset-1",
          config_status: "ready",
          unused: false,
          incoming: [],
          outgoing: [],
        }],
        edges: [],
        groups: [],
      } as GraphProjection;
      const markup = renderToStaticMarkup(createElement(
        QueryClientProvider,
        { client },
        createElement(MemoryRouter, null, createElement(ProductWorkbenchSurface, {
          product,
          initialGraph: graph,
        })),
      ));
      expect(markup).toContain('data-graph-main-view-panel="flow"');
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Graph recipe apply contract", () => {
  it("uses the confirmed preview revision and digest, then clears only failed apply keys", () => {
    const recipe = {
      current_version: { version: 4 },
    } as WorkflowRecipeSummary;
    const preview = {
      base_graph_revision: 12,
      preview_digest: "a".repeat(64),
    } as WorkflowRecipePreview;
    const keys = new Map([["r1", "key-1"]]);

    expect(buildAgentWorkflowRecipeApplyInput(recipe, preview, "key-1")).toEqual({
      expected_recipe_version: 4,
      expected_graph_revision: 12,
      preview_digest: "a".repeat(64),
      idempotency_key: "key-1",
    });

    clearAgentWorkflowRecipeIdempotencyKey(keys, "archive", "r1");
    expect(keys.get("r1")).toBe("key-1");
    clearAgentWorkflowRecipeIdempotencyKey(keys, "apply", "r1");
    expect(keys.has("r1")).toBe(false);
  });
});
