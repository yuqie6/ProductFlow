import { describe, expect, it } from "vitest";

import type { ProductWorkflowV2 } from "../../lib/types";
import { resolveRecipeVersionSource } from "./recipeSource";

const workflow = {
  folders: [{ id: "folder-1" }],
} as ProductWorkflowV2;

describe("workflow recipe version source", () => {
  it("always appends a full recipe from the complete workflow", () => {
    expect(resolveRecipeVersionSource("workflow_recipe", workflow, "folder-1", ["node-1"]))
      .toEqual({ source_type: "workflow" });
  });

  it("appends a fragment from selection before the open folder", () => {
    expect(resolveRecipeVersionSource("recipe_fragment", workflow, "folder-1", ["node-1", "node-2"]))
      .toEqual({ source_type: "selection", node_ids: ["node-1", "node-2"] });
  });

  it("uses an existing open folder and rejects a fragment without a fragment source", () => {
    expect(resolveRecipeVersionSource("recipe_fragment", workflow, "folder-1", []))
      .toEqual({ source_type: "folder", folder_id: "folder-1" });
    expect(resolveRecipeVersionSource("recipe_fragment", workflow, null, [])).toBeNull();
    expect(resolveRecipeVersionSource("recipe_fragment", workflow, "deleted-folder", [])).toBeNull();
    expect(resolveRecipeVersionSource("recipe_fragment", null, "folder-1", ["node-1"])).toBeNull();
  });
});
