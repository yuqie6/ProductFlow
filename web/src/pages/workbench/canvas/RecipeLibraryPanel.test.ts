import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { ConfirmDialog } from "../../../components/ConfirmDialog";
import type { WorkflowRecipePreview, WorkflowRecipeSummary } from "../../../lib/types";
import { RecipeApplyPreviewBody, RecipeLibraryPanel } from "./RecipeLibraryPanel";

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

  it("lists preview mode plus node and edge titles", () => {
    const preview: WorkflowRecipePreview = {
      mode: "merge",
      recipe_id: "r1",
      recipe_version: 1,
      nodes: [
        { key: "prompt", node_type: "prompt_generation", title: "主图提示词", position_x: 0, position_y: 0 },
        { key: "image", node_type: "image_generation", title: "主图 1", position_x: 40, position_y: 0 },
      ],
      edges: [{
        key: "e1",
        source_node_key: "prompt",
        target_node_key: "image",
        role: "prompt",
        data_type: "prompt",
        order: 0,
      }],
      groups: [],
    };
    const markup = renderToStaticMarkup(createElement(RecipeApplyPreviewBody, { preview }));
    expect(markup).toContain("data-recipe-preview-mode=\"merge\"");
    expect(markup).toContain("将合并进当前工作流");
    expect(markup).toContain("主图提示词");
    expect(markup).toContain("主图 1");
    expect(markup).toContain("主图提示词 → 主图 1");
  });

  it("disables confirm when preview failed", () => {
    const markup = renderToStaticMarkup(createElement(ConfirmDialog, {
      open: true,
      title: "将出现这些节点",
      description: "配方无法合并进当前工作流",
      confirmLabel: "确认应用",
      cancelLabel: "取消",
      confirmDisabled: true,
      destructive: false,
      onConfirm: () => undefined,
      onClose: () => undefined,
    }));
    expect(markup).toMatch(/确认应用<\/button>/);
    expect(markup).toContain("disabled");
    expect(markup).toContain("配方无法合并进当前工作流");
  });
});
