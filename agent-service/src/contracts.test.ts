import { describe, expect, it } from "vitest";
import { type Scope, validateScope } from "./contracts.js";

const productScope: Scope = {
  schema_version: 1,
  scope_type: "product_workflow",
  conversation_id: "11111111-1111-4111-8111-111111111111",
  task_id: null,
  task_goal: null,
  product_id: "22222222-2222-4222-8222-222222222222",
  workflow_draft_id: null,
  run_id: "44444444-4444-4444-8444-444444444444",
  system_prompt: "ProductFlow",
  draft_schema: {},
  workflow_draft_schema: {},
  current_draft_version: 0,
  has_live_graph: true,
};

describe("validateScope", () => {
  it("accepts a live-graph product conversation without a WorkflowDraft", () => {
    expect(() => validateScope(productScope)).not.toThrow();
  });

  it("still accepts a historical product conversation that carries a Draft id", () => {
    expect(() => validateScope({ ...productScope, workflow_draft_id: "33333333-3333-4333-8333-333333333333" })).not.toThrow();
  });

  it("rejects a product conversation that has no product identity", () => {
    expect(() => validateScope({ ...productScope, product_id: null })).toThrow(/incomplete product Agent contract/);
  });

  it("rejects a global conversation that still carries product Draft identity", () => {
    expect(() =>
      validateScope({
        ...productScope,
        scope_type: "global",
        product_id: null,
        workflow_draft_id: "33333333-3333-4333-8333-333333333333",
        has_live_graph: false,
      }),
    ).toThrow(/must not include product scope IDs/);
  });
});
