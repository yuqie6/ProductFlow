import type {
  ProductWorkflowV2,
  WorkflowRecipeKind,
  WorkflowRecipeSourceType,
} from "../../lib/types";

export interface RecipeVersionSource {
  source_type: WorkflowRecipeSourceType;
  folder_id?: string | null;
  node_ids?: string[];
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
