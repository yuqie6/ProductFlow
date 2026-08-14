import { describe, expect, it } from "vitest";

import { translate } from "../../lib/i18n";
import type { TranslateFunction } from "../../lib/preferences";
import type { ProductWorkflowV2 } from "../../lib/types";
import { labelRecipeVersionSource, resolveRecipeVersionSource } from "./recipeSource";

const workflow = {
  folders: [{ id: "folder-1", title: "核心卖点" }],
} as ProductWorkflowV2;
const t: TranslateFunction = (key, params) => translate("zh-CN", key, params);

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

  it("labels workflow, folder, and selection sources through the shared translator", () => {
    expect(labelRecipeVersionSource({ source_type: "workflow" }, workflow, t)).toEqual({
      source_type: "workflow",
      label: "来源：完整工作流",
    });
    expect(labelRecipeVersionSource(
      { source_type: "folder", folder_id: "folder-1" },
      workflow,
      t,
    )).toEqual({
      source_type: "folder",
      folder_id: "folder-1",
      label: "来源：文件夹「核心卖点」",
    });
    expect(labelRecipeVersionSource(
      { source_type: "selection", node_ids: ["node-1", "node-2"] },
      workflow,
      t,
    )).toEqual({
      source_type: "selection",
      node_ids: ["node-1", "node-2"],
      label: "来源：2 个选中节点",
    });
  });
});
