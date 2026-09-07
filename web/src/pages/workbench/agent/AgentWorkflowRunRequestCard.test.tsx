import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import type { AgentWorkflowRunRequest } from "../../../lib/types";
import { QUOTA_ENTRY_GRAPH_IMAGE_GENERATION } from "../../../lib/quotaPrice";
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
    run_scope: "node",
    target_node_id: "image-prompt-1",
    target_node_ids: [],
    force: true,
    document_action: "rewrite",
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
        quotaPriceLookup: {
          status: "ok",
          priceVersionId: "pv-placeholder-v0",
          entryCode: QUOTA_ENTRY_GRAPH_IMAGE_GENERATION,
          unitPrice: 3,
          estimatedUnits: 3,
          currency: "iu",
        },
        onConfirm: vi.fn(),
        onCancel: vi.fn(),
        onOpenRuns: vi.fn(),
      }),
    );

    expect(markup).toContain("工作流执行请求");
    expect(markup).toContain("春季新品主图");
    expect(markup).toContain("基于工作流版本 v7");
    expect(markup).toContain("文稿动作：改写文稿");
    expect(markup).toContain("确认并执行");
    expect(markup).toContain("取消请求");
    expect(markup).toContain("生图约扣 3 单位");
    expect(markup).toContain("data-agent-workflow-run-quota-estimate");
    expect(markup).toContain("bg-accent");
    expect(markup).not.toContain("bg-blue-");
    expect(markup).not.toContain("bg-cyan-");
    expect(markup).not.toContain("border-zinc-");
    expect(markup).not.toContain("disabled=\"\"");
  });

  it("disables confirm and surfaces unavailable price without silent zero", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentWorkflowRunRequestCard, {
        request: request("awaiting_confirmation"),
        loading: false,
        busy: false,
        error: null,
        quotaPriceLookup: { status: "missing_entry" },
        quotaPriceBlocksConfirm: true,
        onConfirm: vi.fn(),
        onCancel: vi.fn(),
      }),
    );
    expect(markup).toContain("暂无法计价，无法确认执行");
    expect(markup).toContain("data-agent-workflow-run-quota-unavailable");
    expect(markup).toContain("disabled=\"\"");
    expect(markup).not.toContain("生图约扣 0 单位");
    expect(markup).not.toContain("data-agent-workflow-run-quota-estimate");
  });

  it("omits the revision line when the journal has not recorded one", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentWorkflowRunRequestCard, {
        request: { ...request("awaiting_confirmation"), expected_workflow_revision: null },
        loading: false,
        busy: false,
        error: null,
        quotaPriceLookup: {
          status: "ok",
          priceVersionId: "pv-placeholder-v0",
          entryCode: QUOTA_ENTRY_GRAPH_IMAGE_GENERATION,
          unitPrice: 1,
          estimatedUnits: 1,
          currency: "iu",
        },
        onConfirm: vi.fn(),
        onCancel: vi.fn(),
      }),
    );
    expect(markup).toContain("确认并执行");
    expect(markup).not.toContain("基于工作流版本");
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
    expect(markup).not.toContain("data-agent-workflow-run-quota-estimate");
  });

  it("renders a settled request inside the matching turn without chrome spacing", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentWorkflowRunRequestCard, {
        request: { ...request("succeeded"), workflow_run_status: "succeeded" },
        loading: false,
        busy: false,
        error: null,
        placement: "turn",
        onConfirm: vi.fn(),
        onCancel: vi.fn(),
        onOpenRuns: vi.fn(),
      }),
    );
    expect(markup).toContain('data-agent-workflow-run-request-placement="turn"');
    expect(markup).toContain("工作流运行已完成");
    expect(markup).not.toContain("sm:mx-3");
  });
});
