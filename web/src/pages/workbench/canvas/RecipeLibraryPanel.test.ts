import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { ConfirmDialog } from "../../../components/ConfirmDialog";
import type { WorkflowRecipePreview, WorkflowRecipeSummary } from "../../../lib/types";
import {
  confirmRecipeApply,
  filterRecipesByOrigin,
  RecipeApplyPreviewBody,
  RecipeLibraryPanel,
} from "./RecipeLibraryPanel";

function recipe(
  origin: WorkflowRecipeSummary["origin"] = "official",
): WorkflowRecipeSummary {
  return {
    id: "r1",
    kind: "workflow_recipe",
    origin,
    official_key: origin === "official" ? "hero" : null,
    current_version_id: "v1",
    archived_at: null,
    created_at: "2026-08-22T00:00:00Z",
    updated_at: "2026-08-22T00:00:00Z",
    current_version: {
      id: "v1",
      recipe_id: "r1",
      version: 1,
      schema_version: 3,
      catalog_version: 5,
      creation_source: origin === "official" ? "official_seed" : "user_extract",
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
      governance: origin === "official" ? {
        applicable_image_types: ["hero"],
        required_inputs: ["product_identity"],
        default_result: "image_generation",
        thumbnail: null,
        provider_sample: null,
      } : null,
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
        base_graph_revision: 1,
        preview_digest: "d".repeat(64),
        nodes: [],
        edges: [],
        groups: [],
        updated_nodes: [],
        required_bindings: [],
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

  it("filters user recipes for the second tab", () => {
    const visible = filterRecipesByOrigin([recipe(), recipe("user")], "user");
    expect(visible).toHaveLength(1);
    expect(visible[0]?.origin).toBe("user");
  });

  it("hides append and archive for official recipes and shows governance", () => {
    const markup = renderToStaticMarkup(createElement(RecipeLibraryPanel, {
      recipes: [recipe("official")],
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
        base_graph_revision: 1,
        preview_digest: "d".repeat(64),
        nodes: [],
        edges: [],
        groups: [],
        updated_nodes: [],
        required_bindings: [],
      }),
      onApply: () => undefined,
      onAppend: () => undefined,
      onArchive: () => undefined,
    }));
    expect(markup).toContain("官方配方");
    expect(markup).toContain("适用图种");
    expect(markup).not.toContain("追加版本");
    expect(markup).not.toContain("归档预设");
  });

  it("lists preview mode plus node and edge titles", () => {
    const preview: WorkflowRecipePreview = {
      mode: "merge",
      recipe_id: "r1",
      recipe_version: 1,
      base_graph_revision: 7,
      preview_digest: "d".repeat(64),
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
      updated_nodes: [{
        id: "existing-image",
        node_type: "image_generation",
        title: "已有主图",
        changed_config_keys: ["generation_spec", "prompt"],
      }],
      required_bindings: ["product_identity"],
    };
    const markup = renderToStaticMarkup(createElement(RecipeApplyPreviewBody, { preview }));
    expect(markup).toContain("data-recipe-preview-mode=\"merge\"");
    expect(markup).toContain("将合并进当前工作流");
    expect(markup).toContain("主图提示词");
    expect(markup).toContain("主图 1");
    expect(markup).toContain("主图提示词 → 主图 1");
    expect(markup).toContain("将更新现有节点");
    expect(markup).toContain("generation_spec, prompt");
    expect(markup).toContain("product_identity");
  });

  it("passes the exact confirmed preview object to apply", () => {
    const onApply = vi.fn();
    const selectedRecipe = recipe("official");
    const preview: WorkflowRecipePreview = {
      mode: "merge",
      recipe_id: selectedRecipe.id,
      recipe_version: selectedRecipe.current_version.version,
      base_graph_revision: 9,
      preview_digest: "e".repeat(64),
      nodes: [],
      edges: [],
      groups: [],
      updated_nodes: [],
      required_bindings: [],
    };

    confirmRecipeApply(selectedRecipe, preview, onApply);

    expect(onApply).toHaveBeenCalledWith(selectedRecipe, preview);
    expect(onApply.mock.calls[0]?.[1]).toBe(preview);
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
