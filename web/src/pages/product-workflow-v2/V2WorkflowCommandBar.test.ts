import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { ProductWorkflowV2 } from "../../lib/types";
import { V2WorkflowCommandBar } from "./V2WorkflowCommandBar";

const workflow: ProductWorkflowV2 = {
  id: "workflow",
  product_id: "product",
  title: "商品工作流",
  active: true,
  schema_version: 2,
  revision: 1,
  edit_version: 3,
  source_draft_revision_id: "draft",
  visual_system_version_id: "visual",
  materialization_id: "materialization",
  folders: [],
  nodes: [],
  edges: [],
  created_at: "2026-08-15T00:00:00Z",
  updated_at: "2026-08-15T00:00:00Z",
};

describe("V2WorkflowCommandBar", () => {
  it("keeps session undo and redo in the existing canvas command bar", () => {
    const markup = renderToStaticMarkup(createElement(V2WorkflowCommandBar, {
      workflow,
      openFolderId: null,
      selectedNodeIds: [],
      structureBusy: false,
      variant: "header",
      canUndo: true,
      canRedo: false,
      onUndo: () => undefined,
      onRedo: () => undefined,
      onOpenFolder: () => undefined,
      onCreateFolder: () => undefined,
      onMoveSelection: () => undefined,
      onSaveRecipe: () => undefined,
      onRenameFolder: () => undefined,
      onDissolveFolder: () => undefined,
    }));

    expect(markup).toContain('aria-label="撤销"');
    expect(markup).toContain('aria-label="重做"');
    expect(markup).toContain('disabled=""');
  });
});
