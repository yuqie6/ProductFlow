import type {
  ProductWorkflowV2,
  WorkflowRecipeKind,
  WorkflowRecipeSourceType,
} from "../../../lib/types";
import type { TranslateFunction } from "../../../lib/preferences";

export interface RecipeVersionSource {
  source_type: WorkflowRecipeSourceType;
  folder_id?: string | null;
  node_ids?: string[];
}

export interface RecipeSourceSelection extends RecipeVersionSource {
  label: string;
}

export function resolveRecipeVersionSource(
  recipeKind: WorkflowRecipeKind,
  workflow: ProductWorkflowV2 | null,
  openFolderId: string | null,
  selectedNodeIds: string[],
): RecipeVersionSource | null {
  if (!workflow) return null;
  if (recipeKind === "workflow_recipe") {
    return { source_type: "workflow" };
  }
  if (selectedNodeIds.length) {
    return { source_type: "selection", node_ids: selectedNodeIds };
  }
  if (openFolderId && workflow.folders.some((folder) => folder.id === openFolderId)) {
    return { source_type: "folder", folder_id: openFolderId };
  }
  return null;
}

export function labelRecipeVersionSource(
  source: RecipeVersionSource,
  workflow: ProductWorkflowV2,
  t: TranslateFunction,
): RecipeSourceSelection {
  if (source.source_type === "selection") {
    return {
      ...source,
      label: t("workflowV2.recipe.sourceSelection", { count: source.node_ids?.length ?? 0 }),
    };
  }
  if (source.source_type === "folder") {
    const title = workflow.folders.find((folder) => folder.id === source.folder_id)?.title
      ?? source.folder_id
      ?? "";
    return { ...source, label: t("workflowV2.recipe.sourceFolder", { title }) };
  }
  return { ...source, label: t("workflowV2.recipe.sourceFull") };
}
