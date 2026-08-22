import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { WorkflowRecipeSummary } from "../../../lib/types";
import { RecipeLibraryPanel } from "./RecipeLibraryPanel";

function recipe(kind: WorkflowRecipeSummary["kind"] = "workflow_recipe"): WorkflowRecipeSummary {
  return {
    id: "r1",
    kind,
    current_version_id: "v1",
    archived_at: null,
    created_at: "2026-08-22T00:00:00Z",
    updated_at: "2026-08-22T00:00:00Z",
    current_version: {
      id: "v1",
      recipe_id: "r1",
      version: 1,
      schema_version: 3,
      title: "夏季主图",
      description: null,
      payload: {
        schema_version: 3,
        nodes: [
          {
            key: "prompt",
            node_type: "prompt_generation",
            title: "主图提示词",
            position_x: 0,
            position_y: 0,
            group_key: null,
            config: {},
          },
        ],
        edges: [],
        groups: [],
      },
      payload_hash: "a".repeat(64),
      preferred_visual_system_version_id: null,
      created_at: "2026-08-22T00:00:00Z",
    },
  };
}

describe("RecipeLibraryPanel", () => {
  it("shows node and edge counts, not image types or draft ids", () => {
    const markup = renderToStaticMarkup(createElement(RecipeLibraryPanel, {
      recipes: [recipe()],
      loading: false,
      error: null,
      operationRecipeId: null,
      application: null,
      canAppend: () => true,
      onRetry: () => undefined,
      onPreview: async () => ({
        mode: "create" as const,
        recipe_id: "r1",
        recipe_version: 1,
        nodes: [],
        edges: [],
        groups: [],
      }),
      onApply: () => undefined,
      onAppend: () => undefined,
      onArchive: () => undefined,
    }));
    expect(markup).toContain("1 节点");
    expect(markup).toContain("0 条连线");
    expect(markup).not.toContain("图片类型");
    expect(markup).not.toContain("draft");
    expect(markup).toContain("应用");
  });

  it("lets fragment recipes request a live-graph preview", () => {
    const markup = renderToStaticMarkup(createElement(RecipeLibraryPanel, {
      recipes: [recipe("recipe_fragment")],
      loading: false,
      error: null,
      operationRecipeId: null,
      application: null,
      canAppend: () => true,
      onRetry: () => undefined,
      onPreview: async () => ({
        mode: "merge" as const,
        recipe_id: "r1",
        recipe_version: 1,
        nodes: [],
        edges: [],
        groups: [],
      }),
      onApply: () => undefined,
      onAppend: () => undefined,
      onArchive: () => undefined,
    }));
    expect(markup).toContain("应用");
    expect(markup).not.toContain("局部预设还不能合并到已有工作流");
  });
});
