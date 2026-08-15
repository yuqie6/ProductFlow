import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { AgentQuestion, AgentTurn, GalleryAsset, WorkflowDraft } from "../../lib/types";
import { AgentComposer } from "./AgentComposer";
import { hasUnsyncedWorkflowDraftRevision } from "./AgentConversationPanel";
import { AgentMessageList } from "./AgentMessageList";
import { AgentQuestionPrompt } from "./AgentQuestionPrompt";
import { agentEventReducer, createAgentTurnEventState } from "./agentEventReducer";

function turn(overrides: Partial<AgentTurn> = {}): AgentTurn {
  return {
    id: "projection-1",
    conversation_id: "conversation-1",
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
    sync_error: null,
    finished_at: null,
    created_at: "2026-08-14T00:00:00Z",
    updated_at: "2026-08-14T00:00:00Z",
    ...overrides,
  };
}

describe("Agent conversation components", () => {
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
        error: null,
        onChange: () => undefined,
        onOpenAssets: () => undefined,
        onRemoveAsset: () => undefined,
        onPreviewAsset: () => undefined,
        onSubmit: () => undefined,
      }),
    );

    expect(markup).toContain("价格为 299 元");
    expect(markup.match(/h-14 w-14/g)).toHaveLength(2);
    expect(markup).toContain("移除图片 正面图");
    expect(markup).toContain("从商品图库选择图片");
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
});
