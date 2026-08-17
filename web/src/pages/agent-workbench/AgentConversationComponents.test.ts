import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { AgentQuestion, AgentSession, AgentTurn, GalleryAsset, WorkflowDraft } from "../../lib/types";
import { AgentComposer } from "./AgentComposer";
import { hasUnsyncedWorkflowDraftRevision } from "./AgentConversationPanel";
import { AgentMessageList } from "./AgentMessageList";
import { AgentQuestionPrompt } from "./AgentQuestionPrompt";
import { selectAgentSessionConversation } from "./AgentSessionSwitcher";
import { AgentToolStepList } from "./AgentToolStepList";
import { agentEventReducer, createAgentTurnEventState } from "./agentEventReducer";

function turn(overrides: Partial<AgentTurn> = {}): AgentTurn {
  return {
    id: "projection-1",
    conversation_id: "conversation-1",
    task_id: null,
    harness_turn_id: "harness-turn-1",
    idempotency_key: "key-1",
    input_text: "请整理商品信息",
    input_asset_ids: ["asset/1"],
    status: "running",
    resume_required: false,
    output_text: null,
    error_text: null,
    question: null,
    artifact_name: null,
    artifact_step_id: null,
    workflow_draft_revision_id: null,
    page_context_snapshot_id: null,
    sync_error: null,
    finished_at: null,
    created_at: "2026-08-14T00:00:00Z",
    updated_at: "2026-08-14T00:00:00Z",
    ...overrides,
  };
}

describe("Agent conversation components", () => {
  it("opens the most recently updated product workspace when switching sessions", () => {
    const session = {
      id: "session-1",
      title: "春季素材",
      status: "active",
      archived_at: null,
      conversation_count: 2,
      conversations: [
        {
          conversation_id: "conversation-new",
          scope_type: "product_workflow",
          product_id: "product-new",
          product_name: "新品",
          conversation_status: "collecting",
          updated_at: "2026-08-17T00:00:00Z",
        },
      ],
      created_at: "2026-08-16T00:00:00Z",
      updated_at: "2026-08-17T00:00:00Z",
    } satisfies AgentSession;

    expect(selectAgentSessionConversation(session)?.product_id).toBe("product-new");
    expect(selectAgentSessionConversation({ ...session, conversations: [] })).toBeNull();
  });

  it("refreshes the draft only after ProductFlow projects the proposed revision ID", () => {
    const staleDraft = {
      id: "draft-1",
      current_revision: null,
    } as WorkflowDraft;
    const synchronizedDraft = {
      ...staleDraft,
      current_revision: { id: "revision-1" },
    } as WorkflowDraft;
    const projected = turn({
      status: "awaiting_confirmation",
      workflow_draft_revision_id: "revision-1",
    });

    expect(hasUnsyncedWorkflowDraftRevision(staleDraft, projected)).toBe(true);
    expect(hasUnsyncedWorkflowDraftRevision(synchronizedDraft, projected)).toBe(false);
    expect(hasUnsyncedWorkflowDraftRevision(staleDraft, turn())).toBe(false);
  });

  it("renders composer attachments as equal removable thumbnails and keeps the draft", () => {
    const selectedAssets = [
      {
        id: "asset-1",
        display_name: "正面图",
        thumbnail_url: "/thumb-1",
      } as GalleryAsset,
      {
        id: "asset-2",
        display_name: "细节图",
        thumbnail_url: "/thumb-2",
      } as GalleryAsset,
    ];
    const markup = renderToStaticMarkup(
      createElement(AgentComposer, {
        value: "价格为 299 元",
        selectedAssets,
        isSubmitting: false,
        canSubmit: true,
        stopAvailable: false,
        isStopping: false,
        error: null,
        onChange: () => undefined,
        onOpenAssets: () => undefined,
        onRemoveAsset: () => undefined,
        onPreviewAsset: () => undefined,
        onSubmit: () => undefined,
        onStop: () => undefined,
      }),
    );

    expect(markup).toContain("价格为 299 元");
    expect(markup.match(/h-14 w-14/g)).toHaveLength(2);
    expect(markup).toContain("移除图片 正面图");
    expect(markup).toContain("从商品图库选择图片");
  });

  it("switches the composer primary action to Stop without discarding the typed draft", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentComposer, {
        value: "下一轮补充文案",
        selectedAssets: [],
        isSubmitting: false,
        canSubmit: false,
        stopAvailable: true,
        isStopping: false,
        error: null,
        onChange: () => undefined,
        onOpenAssets: () => undefined,
        onRemoveAsset: () => undefined,
        onPreviewAsset: () => undefined,
        onSubmit: () => undefined,
        onStop: () => undefined,
      }),
    );

    expect(markup).toContain("下一轮补充文案");
    expect(markup).toContain("aria-label=\"取消当前处理\"");
    expect(markup).not.toContain("aria-label=\"发送消息\"");
  });

  it("renders listed Question options, free text, and the recoverable resume state", () => {
    const question: AgentQuestion = {
      id: "question-1",
      header: "价格",
      question: "商品价格是多少？",
      options: [{ label: "299 元" }, { label: "暂不展示", description: "工作流不生成价格文案" }],
    };
    const openMarkup = renderToStaticMarkup(
      createElement(AgentQuestionPrompt, {
        question,
        answered: false,
        resumeRequired: false,
        busy: false,
        error: null,
        onAnswer: () => undefined,
        onResume: () => undefined,
      }),
    );
    const recoveryMarkup = renderToStaticMarkup(
      createElement(AgentQuestionPrompt, {
        question,
        answered: true,
        resumeRequired: true,
        busy: false,
        error: "恢复失败",
        onAnswer: () => undefined,
        onResume: () => undefined,
      }),
    );

    expect(openMarkup).toContain("商品价格是多少？");
    expect(openMarkup).toContain("299 元");
    expect(openMarkup).toContain("输入其他回答");
    expect(recoveryMarkup).toContain("回答已保存");
    expect(recoveryMarkup).toContain("恢复失败");
    expect(recoveryMarkup).toContain("继续执行");
  });

  it("renders live delta for an active Turn and canonical output after terminal projection sync", () => {
    let eventState = createAgentTurnEventState("projection-1");
    eventState = agentEventReducer(eventState, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 1,
        created_at: "2026-08-14T00:00:01Z",
        kind: "text.delta",
        payload: { delta: "流式回答", step_id: "step-1", attempt_id: "attempt-1" },
      },
    });
    const activeMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn()],
        activeTurnId: "projection-1",
        eventState,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
      }),
    );
    const terminalMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({ status: "succeeded", output_text: "最终回答" })],
        activeTurnId: null,
        eventState,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
      }),
    );

    expect(activeMarkup).toContain("流式回答");
    expect(activeMarkup).toContain("/api/v2/product-image-assets/asset%2F1/download?variant=thumbnail");
    expect(terminalMarkup).toContain("最终回答");
    expect(terminalMarkup).not.toContain("流式回答");
  });

  it("renders compact localized tool step rows with textual statuses", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentToolStepList, {
        live: true,
        steps: [
          {
            step_id: "step-1",
            kind: "inspect_image",
            summary: "检查商品正面图",
            status: "running",
          },
          {
            step_id: "step-2",
            kind: "propose_draft",
            summary: "整理工作流方案",
            status: "failed",
          },
        ],
      }),
    );

    expect(markup).toContain("<ul aria-label=\"Agent 工具步骤\" aria-live=\"polite\"");
    expect(markup.match(/<li /g)).toHaveLength(2);
    expect(markup).toContain("检查图片");
    expect(markup).toContain("正在处理");
    expect(markup).toContain("失败");
    expect(markup).toContain("motion-reduce:animate-none");
    expect(markup).not.toContain("button");
  });

  it("renders historical snapshot steps and merges live statuses without duplicating actions", () => {
    const historical = turn({
      status: "succeeded",
      output_text: "已处理完成",
      tool_steps: [
        {
          step_id: "step-1",
          kind: "inspect_context",
          summary: "读取商品上下文",
          status: "succeeded",
        },
      ],
    });
    const historicalMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [historical],
        activeTurnId: null,
        eventState: null,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
      }),
    );
    let eventState = createAgentTurnEventState("projection-1");
    eventState = agentEventReducer(eventState, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 1,
        created_at: "2026-08-14T00:00:01Z",
        kind: "tool.step",
        payload: {
          step_id: "step-1",
          kind: "inspect_context",
          summary: "读取商品上下文",
          status: "succeeded",
        },
      },
    });
    eventState = agentEventReducer(eventState, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 2,
        created_at: "2026-08-14T00:00:02Z",
        kind: "tool.step",
        payload: {
          step_id: "step-2",
          kind: "read_history",
          summary: "读取历史方案",
          status: "running",
        },
      },
    });
    const activeMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [
          turn({
            tool_steps: [
              {
                step_id: "step-1",
                kind: "inspect_context",
                summary: "读取商品上下文",
                status: "running",
              },
            ],
          }),
        ],
        activeTurnId: "projection-1",
        eventState,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
      }),
    );
    const omittedMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({ status: "succeeded", output_text: "无工具步骤" })],
        activeTurnId: null,
        eventState: null,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
      }),
    );

    expect(historicalMarkup).toContain("data-agent-tool-step-id=\"step-1\"");
    expect(historicalMarkup).toContain("检查上下文");
    expect(activeMarkup.match(/data-agent-tool-step-id=/g)).toHaveLength(2);
    expect(activeMarkup).toContain("data-agent-tool-step-status=\"succeeded\"");
    expect(activeMarkup).toContain("读取历史");
    expect(activeMarkup).toContain("data-agent-turn-tail");
    expect(omittedMarkup).not.toContain("data-agent-tool-step-id");
  });

  it("retains terminal live tool steps until the snapshot converges without duplication", () => {
    let eventState = createAgentTurnEventState("projection-1");
    eventState = agentEventReducer(eventState, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 1,
        created_at: "2026-08-14T00:00:01Z",
        kind: "tool.step",
        payload: {
          step_id: "step-final",
          kind: "propose_draft",
          summary: "完成工作流方案",
          status: "succeeded",
        },
      },
    });
    eventState = agentEventReducer(eventState, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 2,
        created_at: "2026-08-14T00:00:02Z",
        kind: "turn.succeeded",
        payload: {},
      },
    });
    const staleTurn = turn({ status: "succeeded", output_text: "最终回答" });
    const staleMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [staleTurn],
        activeTurnId: null,
        eventState,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
      }),
    );
    const convergedMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({
          status: "succeeded",
          output_text: "最终回答",
          tool_steps: [eventState.tool_steps["step-final"].step],
        })],
        activeTurnId: null,
        eventState,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
      }),
    );

    expect(staleMarkup).toContain("data-agent-tool-step-id=\"step-final\"");
    expect(staleMarkup).toContain("完成工作流方案");
    expect(staleMarkup).not.toContain("aria-live=\"polite\"");
    expect(convergedMarkup.match(/data-agent-tool-step-id="step-final"/g)).toHaveLength(1);
  });

  it("does not project retained event steps onto a different active turn", () => {
    let eventState = createAgentTurnEventState("projection-1");
    eventState = agentEventReducer(eventState, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 1,
        created_at: "2026-08-14T00:00:01Z",
        kind: "tool.step",
        payload: {
          step_id: "stale-step",
          kind: "inspect_context",
          summary: "上一轮步骤",
          status: "succeeded",
        },
      },
    });
    const markup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({ id: "projection-2", harness_turn_id: "harness-turn-2" })],
        activeTurnId: "projection-2",
        eventState,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
      }),
    );

    expect(markup).not.toContain("stale-step");
    expect(markup).not.toContain("上一轮步骤");
  });

  it("renders turn status, failures, and the matching Draft action in the turn tail", () => {
    const failed = turn({
      status: "failed",
      error_text: "草案同步失败",
      workflow_draft_revision_id: "revision-1",
    });
    const markup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [failed],
        activeTurnId: null,
        eventState: null,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        reviewDraftRevisionId: "revision-1",
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
        onReviewDraft: () => undefined,
      }),
    );
    const staleRevisionMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [failed],
        activeTurnId: null,
        eventState: null,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        reviewDraftRevisionId: "revision-2",
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
        onReviewDraft: () => undefined,
      }),
    );

    expect(markup).toContain("data-agent-turn-tail");
    expect(markup).toContain("data-agent-turn-status=\"failed\"");
    expect(markup).toContain("失败");
    expect(markup).toContain("草案同步失败");
    expect(markup).toContain("审阅工作流方案");
    expect(staleRevisionMarkup).not.toContain("审阅工作流方案");
  });

  it("does not show a processing placeholder after the Turn starts waiting for user action", () => {
    for (const status of ["requires_input", "awaiting_confirmation"] as const) {
      const markup = renderToStaticMarkup(
        createElement(AgentMessageList, {
          turns: [turn({ status })],
          activeTurnId: "projection-1",
          eventState: null,
          initialTurnPending: false,
          hasOlder: false,
          loadingOlder: false,
          onLoadOlder: async () => undefined,
          onPreviewAsset: () => undefined,
        }),
      );

      expect(markup).not.toContain("Agent 正在处理");
    }
  });
});
