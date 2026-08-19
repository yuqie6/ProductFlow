import { describe, expect, it } from "vitest";
import type { Scope } from "./contracts.js";
import type { ProductFlowClient } from "./productflow.js";
import { createProductFlowTools, type ToolRuntime } from "./tools.js";

function runtime(scope: Scope): ToolRuntime {
  return {
    client: {} as ProductFlowClient,
    scope,
    signal: new AbortController().signal,
    loadSkill: async (name, resourcePath) => `loaded:${name}:${resourcePath ?? "body"}`,
    askUser: async () => ({ text: "answer" }),
    proposeArtifact: async () => undefined,
    markWorkflowRunRequested: () => undefined,
    idempotencyKey: (id) => `pi-test-${id}`,
  };
}

const baseScope: Scope = {
  schema_version: 1,
  scope_type: "product_workflow",
  conversation_id: "11111111-1111-4111-8111-111111111111",
  task_id: null,
  task_goal: null,
  product_id: "22222222-2222-4222-8222-222222222222",
  workflow_draft_id: "33333333-3333-4333-8333-333333333333",
  run_id: "44444444-4444-4444-8444-444444444444",
  system_prompt: "ProductFlow",
  draft_schema: { type: "object" },
  workflow_draft_schema: { type: "object" },
  current_draft_version: 1,
};

describe("ProductFlow Pi tools", () => {
  it("keeps product scope tools bounded and confirmation-oriented", () => {
    const names = createProductFlowTools(runtime(baseScope)).map((tool) => tool.name).sort();
    expect(names).toContain("load_productflow_skill");
    expect(names).toContain("propose_workflow_draft");
    expect(names).toContain("request_workflow_run_v1");
    expect(names).not.toContain("create_product_image_folder_v1");
    expect(names).not.toContain("rename_product_image_asset_v1");
    expect(names).not.toContain("move_product_image_assets_v1");
    expect(names).not.toContain("propose_global_draft");
  });

  it("uses the global draft envelope and does not expose product-only context", () => {
    const globalScope: Scope = {
      ...baseScope,
      scope_type: "global",
      product_id: null,
      workflow_draft_id: null,
      draft_schema: { type: "object" },
      workflow_draft_schema: {},
    };
    const names = createProductFlowTools(runtime(globalScope)).map((tool) => tool.name).sort();
    expect(names).toContain("load_productflow_skill");
    expect(names).toContain("propose_global_draft");
    expect(names).toContain("list_products_v1");
    expect(names).toContain("create_product_workspace_v1");
    expect(names).not.toContain("get_product_workflow_context_v1");
    expect(names).not.toContain("propose_workflow_draft");
  });
});
