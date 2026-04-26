import { describe, expect, it } from "vitest";

import type { ProductWorkflow, WorkflowNode } from "../../lib/types";
import { hasActiveWorkflow, outputStringArray } from "./utils";

const baseNode: WorkflowNode = {
  id: "node-1",
  workflow_id: "workflow-1",
  node_type: "reference_image",
  title: "参考图",
  position_x: 0,
  position_y: 0,
  config_json: {},
  status: "idle",
  output_json: null,
  failure_reason: null,
  last_run_at: null,
  created_at: "2026-04-26T00:00:00Z",
  updated_at: "2026-04-26T00:00:00Z",
};

function workflowWith(overrides: Partial<ProductWorkflow>): ProductWorkflow {
  return {
    id: "workflow-1",
    product_id: "product-1",
    title: "默认工作流",
    active: true,
    nodes: [baseNode],
    edges: [],
    runs: [],
    created_at: "2026-04-26T00:00:00Z",
    updated_at: "2026-04-26T00:00:00Z",
    ...overrides,
  };
}

describe("product-detail utils", () => {
  it("reads string arrays from output_json before config_json and filters non-strings", () => {
    const node: WorkflowNode = {
      ...baseNode,
      config_json: { source_asset_ids: ["config-asset"] },
      output_json: { source_asset_ids: ["asset-1", 2, "asset-2", null] },
    };

    expect(outputStringArray(node, "source_asset_ids")).toEqual(["asset-1", "asset-2"]);
  });

  it("detects active workflows from running runs or queued nodes", () => {
    expect(
      hasActiveWorkflow(
        workflowWith({
          runs: [
            {
              id: "run-1",
              workflow_id: "workflow-1",
              status: "running",
              started_at: "2026-04-26T00:00:00Z",
              finished_at: null,
              failure_reason: null,
              node_runs: [],
            },
          ],
        }),
      ),
    ).toBe(true);

    expect(
      hasActiveWorkflow(
        workflowWith({
          nodes: [{ ...baseNode, status: "queued" }],
        }),
      ),
    ).toBe(true);

    expect(hasActiveWorkflow(workflowWith({}))).toBe(false);
  });
});
