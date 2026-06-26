import { describe, expect, it } from "vitest";

import type { TranslationKey, TranslationParams } from "../../lib/i18n";
import type { ProductWorkflow, WorkflowNode, WorkflowRun, WorkflowRunStatusSummary } from "../../lib/types";
import {
  getWorkflowNodeCancelableRun,
  getWorkflowNodeActiveRunContext,
  getWorkflowNodeRunActionState,
  hasActiveWorkflow,
  imageWorkflowNodeWaitingLabel,
  isImageWorkflowNodeWaiting,
  mergeProductWorkflowStatusIntoDetail,
  outputStringArray,
  shouldRefreshProductWorkflowDetailFromStatus,
  workflowNodeActivityText,
  workflowNodeRunDurationText,
  workflowNodeRunProviderSummary,
  workflowNodeRunStatusLabel,
  workflowNodeStatusLabel,
  workflowRetryHintLabel,
  workflowRunQueueText,
  workflowRunRetryMetadata,
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
  is_retryable: false,
  attempt_count: 0,
  retry_count: 0,
  non_retryable_reason: null,
  retry_hint: null,
  last_run_at: null,
  created_at: "2026-04-26T00:00:00Z",
  updated_at: "2026-04-26T00:00:00Z",
};

function stubT(values: Partial<Record<TranslationKey, string>>) {
  return (key: TranslationKey, params?: TranslationParams): string => {
    let text = values[key] ?? key;
    if (params) {
      for (const [name, value] of Object.entries(params)) {
        text = text.replaceAll(`{${name}}`, String(value));
      }
    }
    return text;
  };
}

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

function workflowRun(overrides: Partial<WorkflowRun>): WorkflowRun {
  return {
    id: "run-1",
    workflow_id: "workflow-1",
    status: "running",
    started_at: "2026-04-26T00:00:00Z",
    finished_at: null,
    failure_reason: null,
    progress_metadata: null,
    is_retryable: false,
    is_cancelable: true,
    queue_active_count: 1,
    queue_running_count: 0,
    queue_queued_count: 1,
    queue_max_concurrent_tasks: 3,
    queued_ahead_count: 0,
    queue_position: 1,
    node_runs: [],
    ...overrides,
  };
}

function workflowRunStatus(overrides: Partial<WorkflowRunStatusSummary>): WorkflowRunStatusSummary {
  return {
    id: "run-1",
    workflow_id: "workflow-1",
    status: "running",
    started_at: "2026-04-26T00:00:00Z",
    finished_at: null,
    failure_reason: null,
    progress_metadata: null,
    is_retryable: false,
    is_cancelable: true,
    queue_active_count: 1,
    queue_running_count: 0,
    queue_queued_count: 1,
    queue_max_concurrent_tasks: 3,
    queued_ahead_count: 0,
    queue_position: 1,
    node_runs: [],
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
            workflowRun({}),
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

  it("reads workflow run retry metadata and includes the previous failure in active retry text", () => {
    const run = workflowRun({
      progress_metadata: {
        last_failure_reason: "图片供应商拒绝了本次内容",
        last_failure_retryable: true,
        retry_hint: "retry_later",
        source_run_id: "failed-run-1",
        manual_retry: true,
      },
    });

    expect(workflowRunRetryMetadata(run)).toEqual({
      last_failure_reason: "图片供应商拒绝了本次内容",
      last_failure_retryable: true,
      retry_hint: "retry_later",
      source_run_id: "failed-run-1",
      manual_retry: true,
    });
    expect(
      workflowRunQueueText(
        run,
        stubT({
          "detail.runRetryingAfterFailure": "上次失败：{reason}。正在重试。",
          "detail.runQueuedText": "排队第 {position} 位，前方 {ahead} 个；全局活跃 {active}/{max}。",
        }),
      ),
    ).toBe("上次失败：图片供应商拒绝了本次内容。正在重试。 排队第 1 位，前方 0 个；全局活跃 1/3。");
    expect(
      workflowRetryHintLabel(
        "revise_input",
        stubT({
          "detail.retryHint.reviseInput": "调整输入后再运行",
        }),
      ),
    ).toBe("调整输入后再运行");
  });

  it("scopes visible image waiting state to reference nodes", () => {
    expect(isImageWorkflowNodeWaiting({ ...baseNode, node_type: "reference_image", status: "queued" })).toBe(true);
    expect(imageWorkflowNodeWaitingLabel({ ...baseNode, node_type: "reference_image", status: "running" })).toBe(
      "承载图片节点更新中",
    );
    expect(isImageWorkflowNodeWaiting({ ...baseNode, node_type: "image_generation", status: "queued" })).toBe(false);
    expect(imageWorkflowNodeWaitingLabel({ ...baseNode, node_type: "image_generation", status: "queued" })).toBe("");
    expect(isImageWorkflowNodeWaiting({ ...baseNode, node_type: "copy_generation", status: "running" })).toBe(false);
    expect(imageWorkflowNodeWaitingLabel({ ...baseNode, node_type: "reference_image", status: "succeeded" })).toBe("");
  });

  it("formats active node and node-run status labels through shared helpers", () => {
    expect(workflowNodeActivityText({ ...baseNode, node_type: "copy_generation", status: "queued" })).toBe("节点已加入运行队列");
    expect(workflowNodeActivityText({ ...baseNode, node_type: "copy_generation", status: "running" })).toBe("节点正在运行");
    expect(workflowNodeActivityText({ ...baseNode, node_type: "copy_generation", status: "idle" })).toBe("");
    expect(workflowNodeRunStatusLabel("failed")).toBe("失败");
    expect(workflowNodeRunStatusLabel("cancelled")).toBe("已取消");
    expect(
      workflowNodeActivityText(
        { ...baseNode, node_type: "copy_generation", status: "running" },
        stubT({ "detail.nodeActivity.running": "Node running" }),
      ),
    ).toBe("Node running");
  });

  it("keeps node run action state scoped to the node instead of globally disabling every run button", () => {
    const idleOptions = { runSubmissionPending: false, pendingStartNodeId: null };

    expect(getWorkflowNodeRunActionState({ ...baseNode, id: "node-1", status: "queued" }, idleOptions)).toMatchObject({
      disabled: true,
      pending: true,
      label: "排队中",
    });

    expect(getWorkflowNodeRunActionState({ ...baseNode, id: "node-2", status: "running" }, idleOptions)).toMatchObject({
      disabled: true,
      pending: true,
      label: "运行中",
    });

    for (const status of ["idle", "succeeded", "failed"] as const) {
      expect(
        getWorkflowNodeRunActionState(
          { ...baseNode, id: `node-${status}`, status, is_retryable: status === "failed" },
          idleOptions,
        ),
      ).toMatchObject({
        disabled: false,
        pending: false,
        label: "从此节点运行",
      });
    }

    expect(
      getWorkflowNodeRunActionState(
        { ...baseNode, id: "node-3", status: "idle" },
        { runSubmissionPending: true, pendingStartNodeId: "node-3" },
      ),
    ).toMatchObject({
      disabled: true,
      pending: true,
      label: "提交中",
    });

    expect(
      getWorkflowNodeRunActionState(
        { ...baseNode, id: "node-4", status: "idle" },
        { runSubmissionPending: true, pendingStartNodeId: "node-3" },
      ),
    ).toMatchObject({
      disabled: true,
      pending: false,
      label: "从此节点运行",
    });

    expect(
      getWorkflowNodeRunActionState(
        { ...baseNode, id: "node-5", status: "idle" },
        { runSubmissionPending: false, pendingStartNodeId: null },
      ),
    ).toMatchObject({
      disabled: false,
      pending: false,
      label: "从此节点运行",
    });

    expect(
      getWorkflowNodeRunActionState(
        { ...baseNode, id: "node-en", status: "failed", is_retryable: true },
        idleOptions,
        stubT({
          "detail.retry": "Retry",
          "detail.runAction.runFromNode": "Run from this node",
          "detail.runAction.retryTitle": "Run from this node again",
        }),
      ),
    ).toMatchObject({
      disabled: false,
      pending: false,
      label: "Run from this node",
      title: "Run from this node again",
    });

    expect(
      getWorkflowNodeRunActionState(
        { ...baseNode, id: "node-nonretryable", status: "failed", is_retryable: false },
        idleOptions,
      ),
    ).toMatchObject({
      disabled: true,
      pending: false,
      label: "不可原样重试",
    });

    expect(
      getWorkflowNodeRunActionState(
        { ...baseNode, id: "node-cancelled", status: "cancelled", is_retryable: false },
        idleOptions,
      ),
    ).toMatchObject({
      disabled: false,
      pending: false,
      label: "从此节点运行",
    });
  });

  it("finds the cancelable run that currently owns the selected node", () => {
    const activeRun = workflowRun({
      id: "run-active",
      is_cancelable: true,
      node_runs: [
        {
          id: "node-run-active",
          workflow_run_id: "run-active",
          node_id: "node-active",
          status: "running",
          output_json: null,
          failure_reason: null,
          copy_set_id: null,
          poster_variant_id: null,
          started_at: "2026-04-26T00:01:00Z",
          finished_at: null,
        },
      ],
    });
    const unrelatedActiveRun = workflowRun({
      id: "run-unrelated",
      is_cancelable: true,
      node_runs: [
        {
          id: "node-run-unrelated",
          workflow_run_id: "run-unrelated",
          node_id: "node-other",
          status: "queued",
          output_json: null,
          failure_reason: null,
          copy_set_id: null,
          poster_variant_id: null,
          started_at: "2026-04-26T00:01:00Z",
          finished_at: null,
        },
      ],
    });
    const finishedRun = workflowRun({
      id: "run-finished",
      status: "succeeded",
      is_cancelable: false,
      node_runs: [
        {
          id: "node-run-finished",
          workflow_run_id: "run-finished",
          node_id: "node-active",
          status: "succeeded",
          output_json: null,
          failure_reason: null,
          copy_set_id: null,
          poster_variant_id: null,
          started_at: "2026-04-26T00:00:00Z",
          finished_at: "2026-04-26T00:02:00Z",
        },
      ],
    });
    const workflow = workflowWith({
      runs: [finishedRun, unrelatedActiveRun, activeRun],
    });

    expect(getWorkflowNodeCancelableRun(workflow, { id: "node-active" })?.id).toBe("run-active");
    expect(getWorkflowNodeCancelableRun(workflow, { id: "node-other" })?.id).toBe("run-unrelated");
    expect(getWorkflowNodeCancelableRun(workflow, { id: "node-idle" })).toBeNull();
    expect(getWorkflowNodeCancelableRun(null, { id: "node-active" })).toBeNull();
    expect(getWorkflowNodeCancelableRun(workflow, null)).toBeNull();
  });

  it("finds active run context for the selected node", () => {
    const queuedRun = workflowRun({
      id: "run-queued",
      status: "running",
      node_runs: [
        {
          id: "node-run-queued",
          workflow_run_id: "run-queued",
          node_id: "node-active",
          status: "queued",
          output_json: null,
          failure_reason: null,
          copy_set_id: null,
          poster_variant_id: null,
          started_at: "2026-04-26T00:01:00Z",
          finished_at: null,
        },
      ],
    });
    const finishedRun = workflowRun({
      id: "run-finished",
      status: "succeeded",
      is_cancelable: false,
      node_runs: [
        {
          id: "node-run-finished",
          workflow_run_id: "run-finished",
          node_id: "node-active",
          status: "succeeded",
          output_json: null,
          failure_reason: null,
          copy_set_id: null,
          poster_variant_id: null,
          started_at: "2026-04-26T00:00:00Z",
          finished_at: "2026-04-26T00:02:00Z",
        },
      ],
    });
    const workflow = workflowWith({ runs: [finishedRun, queuedRun] });

    expect(getWorkflowNodeActiveRunContext(workflow, { id: "node-active" })).toMatchObject({
      run: { id: "run-queued" },
      nodeRun: { id: "node-run-queued" },
    });
    expect(getWorkflowNodeActiveRunContext(workflow, { id: "node-idle" })).toBeNull();
    expect(getWorkflowNodeActiveRunContext(null, { id: "node-active" })).toBeNull();
  });

  it("formats workflow run queue and running text", () => {
    expect(
      workflowRunQueueText(
        workflowRun({
          queue_position: 2,
          queued_ahead_count: 1,
          queue_active_count: 3,
          queue_max_concurrent_tasks: 4,
        }),
      ),
    ).toBe("排队第 2 位，前方 1 个；全局活跃 3/4。");
    expect(workflowRunQueueText(workflowRun({ queue_position: null, status: "running", queue_running_count: 2, queue_queued_count: 5 }))).toBe(
      "全局运行 2 个，排队 5 个。",
    );
    expect(workflowRunQueueText(workflowRun({ queue_position: null, status: "succeeded" }))).toBe("");
  });

  it("formats node-run duration and provider summary without exposing raw output", () => {
    expect(
      workflowNodeRunDurationText({
        started_at: "2026-04-26T00:00:00Z",
        finished_at: "2026-04-26T00:00:08Z",
      }),
    ).toBe("耗时 8 秒");
    expect(
      workflowNodeRunDurationText({
        started_at: "2026-04-26T00:00:00Z",
        finished_at: "2026-04-26T00:02:05Z",
      }),
    ).toBe("耗时 2 分 5 秒");
    expect(
      workflowNodeRunProviderSummary({
        output_json: {
          provider_results: [
            {
              provider_name: "openai-responses",
              model_name: "gpt-image-1",
              provider_response_status: "completed",
              provider_response_id: "resp_1",
              provider_output_json: { raw: "hidden" },
            },
          ],
        },
      }),
    ).toBe("供应商 openai-responses / gpt-image-1，状态 completed，响应 resp_1");
    expect(workflowNodeRunProviderSummary({ output_json: { provider_results: [] } })).toBe("");
  });

  it("labels idle product context nodes as usable static context", () => {
    expect(workflowNodeStatusLabel({ ...baseNode, node_type: "product_context", status: "idle" })).toBe("可用");
    expect(workflowNodeStatusLabel({ ...baseNode, node_type: "image_generation", status: "idle" })).toBe("未运行");
    expect(workflowNodeStatusLabel({ ...baseNode, node_type: "image_generation", status: "idle" }, stubT({
      "detail.nodeStatus.idle": "Not run",
    }))).toBe("Not run");
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
        workflowRun({
          started_at: "2026-04-26T00:01:00Z",
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
              started_at: "2026-04-26T00:01:00Z",
              finished_at: null,
            },
          ],
        }),
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
          is_retryable: false,
          attempt_count: 2,
          retry_count: 1,
          non_retryable_reason: "生成失败",
          retry_hint: "revise_input",
          last_run_at: "2026-04-26T00:02:00Z",
          updated_at: "2026-04-26T00:02:01Z",
        },
      ],
      runs: [
        workflowRunStatus({
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
        }),
      ],
      created_at: "2026-04-26T00:00:00Z",
      updated_at: "2026-04-26T00:02:01Z",
    });

    expect(merged.edges).toBe(workflow.edges);
    expect(merged.nodes[0].config_json).toEqual({ instruction: "keep config" });
    expect(merged.nodes[0].output_json).toEqual({ copy_set_id: "copy-1" });
    expect(merged.nodes[0].status).toBe("failed");
    expect(merged.nodes[0].failure_reason).toBe("生成失败");
    expect(merged.nodes[0].is_retryable).toBe(false);
    expect(merged.nodes[0].attempt_count).toBe(2);
    expect(merged.nodes[0].retry_count).toBe(1);
    expect(merged.nodes[0].non_retryable_reason).toBe("生成失败");
    expect(merged.nodes[0].retry_hint).toBe("revise_input");
    expect(merged.runs[0].status).toBe("failed");
    expect(merged.runs[0].node_runs[0].output_json).toEqual({ artifact: "keep output" });
    expect(merged.runs[0].node_runs[0].copy_set_id).toBe("copy-1");
  });

  it("refreshes the full workflow when lightweight status reaches a terminal state", () => {
    const activeWorkflow = workflowWith({
      nodes: [{ ...baseNode, status: "running" }],
      runs: [
        workflowRun({
          started_at: "2026-04-26T00:01:00Z",
        }),
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
            is_retryable: false,
            attempt_count: 1,
            retry_count: 0,
            non_retryable_reason: null,
            retry_hint: null,
            last_run_at: "2026-04-26T00:02:00Z",
            updated_at: "2026-04-26T00:02:00Z",
          },
        ],
        runs: [
          workflowRunStatus({
            status: "succeeded",
            started_at: "2026-04-26T00:01:00Z",
            finished_at: "2026-04-26T00:02:00Z",
            failure_reason: null,
            is_cancelable: false,
          }),
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
            is_retryable: false,
            attempt_count: 1,
            retry_count: 0,
            non_retryable_reason: null,
            retry_hint: null,
            last_run_at: "2026-04-26T00:01:00Z",
            updated_at: "2026-04-26T00:01:30Z",
          },
        ],
        runs: [
          workflowRunStatus({
            started_at: "2026-04-26T00:01:00Z",
          }),
        ],
        created_at: "2026-04-26T00:00:00Z",
        updated_at: "2026-04-26T00:01:30Z",
      }),
    ).toBe(false);
  });
});
