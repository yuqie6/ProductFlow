import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import type { AgentWorkflowRunRequest } from "../../lib/types";
import { AgentWorkflowRunRequestCard } from "./AgentWorkflowRunRequestCard";

function request(status: AgentWorkflowRunRequest["status"]): AgentWorkflowRunRequest {
  return {
    id: "request-1",
    conversation_id: "conversation-1",
    task_id: "task-1",
    product_id: "product-1",
    product_name: "春季新品",
    workflow_id: "workflow-1",
    workflow_title: "春季新品主图",
    expected_workflow_revision: 7,
    status,
    workflow_run_id: status === "awaiting_confirmation" ? null : "run-1",
    workflow_run_status: status === "awaiting_confirmation" ? null : "running",
    source_step_id: "step-1",
    failure_reason: null,
    confirmed_at: status === "awaiting_confirmation" ? null : "2026-08-18T00:00:00Z",
    finished_at: null,
    created_at: "2026-08-18T00:00:00Z",
    updated_at: "2026-08-18T00:00:00Z",
  };
}

describe("AgentWorkflowRunRequestCard", () => {
  it("shows the workflow revision and explicit human confirmation action", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentWorkflowRunRequestCard, {
        request: request("awaiting_confirmation"),
        loading: false,
        busy: false,
        error: null,
        onConfirm: vi.fn(),
        onCancel: vi.fn(),
        onOpenRuns: vi.fn(),
      }),
    );

    expect(markup).toContain("工作流执行请求");
    expect(markup).toContain("春季新品主图");
    expect(markup).toContain("基于工作流版本 v7");
    expect(markup).toContain("确认并执行");
    expect(markup).toContain("取消请求");
  });

  it("keeps a submitted request observable and offers the existing run history", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentWorkflowRunRequestCard, {
        request: request("confirmed"),
        loading: false,
        busy: false,
        error: null,
        onConfirm: vi.fn(),
        onCancel: vi.fn(),
        onOpenRuns: vi.fn(),
      }),
    );

    expect(markup).toContain("运行中");
    expect(markup).toContain("查看运行记录");
    expect(markup).not.toContain("确认并执行");
  });
});
