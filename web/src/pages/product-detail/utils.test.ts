import { describe, expect, it } from "vitest";

import type { ProductWorkflow, WorkflowNode } from "../../lib/types";
import {
  hasActiveWorkflow,
  imageWorkflowNodeWaitingLabel,
  isImageWorkflowNodeWaiting,
  mergeProductWorkflowStatusIntoDetail,
  outputStringArray,
  shouldRefreshProductWorkflowDetailFromStatus,
  workflowNodeStatusLabel,
} from "./utils";

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

  it("scopes visible image waiting state to image-generation and reference nodes", () => {
    expect(isImageWorkflowNodeWaiting({ ...baseNode, node_type: "reference_image", status: "queued" })).toBe(true);
    expect(imageWorkflowNodeWaitingLabel({ ...baseNode, node_type: "reference_image", status: "running" })).toBe(
      "参考图更新中",
    );
    expect(imageWorkflowNodeWaitingLabel({ ...baseNode, node_type: "image_generation", status: "queued" })).toBe(
      "生图排队中",
    );
    expect(isImageWorkflowNodeWaiting({ ...baseNode, node_type: "copy_generation", status: "running" })).toBe(false);
    expect(imageWorkflowNodeWaitingLabel({ ...baseNode, node_type: "reference_image", status: "succeeded" })).toBe("");
  });

  it("labels idle product context nodes as usable static context", () => {
    expect(workflowNodeStatusLabel({ ...baseNode, node_type: "product_context", status: "idle" })).toBe("可用");
    expect(workflowNodeStatusLabel({ ...baseNode, node_type: "image_generation", status: "idle" })).toBe("未运行");
  });

  it("merges lightweight workflow status without replacing structure or node outputs", () => {
    const workflow = workflowWith({
      nodes: [
        {
          ...baseNode,
          status: "running",
          config_json: { instruction: "keep config" },
          output_json: { copy_set_id: "copy-1" },
          failure_reason: null,
          last_run_at: "2026-04-26T00:01:00Z",
        },
      ],
      edges: [
        {
          id: "edge-1",
          workflow_id: "workflow-1",
          source_node_id: "node-1",
          target_node_id: "node-2",
          source_handle: "output",
          target_handle: "input",
          created_at: "2026-04-26T00:00:00Z",
        },
      ],
      runs: [
        {
          id: "run-1",
          workflow_id: "workflow-1",
          status: "running",
          started_at: "2026-04-26T00:01:00Z",
          finished_at: null,
          failure_reason: null,
          node_runs: [
            {
              id: "node-run-1",
              workflow_run_id: "run-1",
              node_id: "node-1",
              status: "running",
              output_json: { artifact: "keep output" },
              failure_reason: null,
              copy_set_id: "copy-1",
              poster_variant_id: null,
              image_session_asset_id: null,
              started_at: "2026-04-26T00:01:00Z",
              finished_at: null,
            },
          ],
        },
      ],
    });

    const merged = mergeProductWorkflowStatusIntoDetail(workflow, {
      id: "workflow-1",
      product_id: "product-1",
      title: "默认工作流",
      active: true,
      has_active_workflow: false,
      nodes: [
        {
          id: "node-1",
          workflow_id: "workflow-1",
          status: "failed",
          failure_reason: "生成失败",
          last_run_at: "2026-04-26T00:02:00Z",
          updated_at: "2026-04-26T00:02:01Z",
        },
      ],
      runs: [
        {
          id: "run-1",
          workflow_id: "workflow-1",
          status: "failed",
          started_at: "2026-04-26T00:01:00Z",
          finished_at: "2026-04-26T00:02:00Z",
          failure_reason: "生成失败",
          node_runs: [
            {
              id: "node-run-1",
              workflow_run_id: "run-1",
              node_id: "node-1",
              status: "failed",
              failure_reason: "生成失败",
              started_at: "2026-04-26T00:01:00Z",
              finished_at: "2026-04-26T00:02:00Z",
            },
          ],
        },
      ],
      created_at: "2026-04-26T00:00:00Z",
      updated_at: "2026-04-26T00:02:01Z",
    });

    expect(merged.edges).toBe(workflow.edges);
    expect(merged.nodes[0].config_json).toEqual({ instruction: "keep config" });
    expect(merged.nodes[0].output_json).toEqual({ copy_set_id: "copy-1" });
    expect(merged.nodes[0].status).toBe("failed");
    expect(merged.nodes[0].failure_reason).toBe("生成失败");
    expect(merged.runs[0].status).toBe("failed");
    expect(merged.runs[0].node_runs[0].output_json).toEqual({ artifact: "keep output" });
    expect(merged.runs[0].node_runs[0].copy_set_id).toBe("copy-1");
  });

  it("refreshes the full workflow when lightweight status reaches a terminal state", () => {
    const activeWorkflow = workflowWith({
      nodes: [{ ...baseNode, status: "running" }],
      runs: [
        {
          id: "run-1",
          workflow_id: "workflow-1",
          status: "running",
          started_at: "2026-04-26T00:01:00Z",
          finished_at: null,
          failure_reason: null,
          node_runs: [],
        },
      ],
    });

    expect(
      shouldRefreshProductWorkflowDetailFromStatus(activeWorkflow, {
        id: "workflow-1",
        product_id: "product-1",
        title: "默认工作流",
        active: true,
        has_active_workflow: false,
        nodes: [
          {
            id: "node-1",
            workflow_id: "workflow-1",
            status: "succeeded",
            failure_reason: null,
            last_run_at: "2026-04-26T00:02:00Z",
            updated_at: "2026-04-26T00:02:00Z",
          },
        ],
        runs: [
          {
            id: "run-1",
            workflow_id: "workflow-1",
            status: "succeeded",
            started_at: "2026-04-26T00:01:00Z",
            finished_at: "2026-04-26T00:02:00Z",
            failure_reason: null,
            node_runs: [],
          },
        ],
        created_at: "2026-04-26T00:00:00Z",
        updated_at: "2026-04-26T00:02:00Z",
      }),
    ).toBe(true);

    expect(
      shouldRefreshProductWorkflowDetailFromStatus(activeWorkflow, {
        id: "workflow-1",
        product_id: "product-1",
        title: "默认工作流",
        active: true,
        has_active_workflow: true,
        nodes: [
          {
            id: "node-1",
            workflow_id: "workflow-1",
            status: "running",
            failure_reason: null,
            last_run_at: "2026-04-26T00:01:00Z",
            updated_at: "2026-04-26T00:01:30Z",
          },
        ],
        runs: [
          {
            id: "run-1",
            workflow_id: "workflow-1",
            status: "running",
            started_at: "2026-04-26T00:01:00Z",
            finished_at: null,
            failure_reason: null,
            node_runs: [],
          },
        ],
        created_at: "2026-04-26T00:00:00Z",
        updated_at: "2026-04-26T00:01:30Z",
      }),
    ).toBe(false);
  });
});
