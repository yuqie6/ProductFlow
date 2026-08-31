import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { AgentQuestion, AgentSession, AgentTurn, GalleryAsset } from "../../../lib/types";
import { AgentAssistantMarkdown } from "./AgentAssistantMarkdown";
import { AgentComposer, classifyImageFiles } from "./AgentComposer";
import {
  agentConversationSubmitTaskId,
  canSubmitAgentConversationMessage,
  resolveAgentCanvasFocusNodeIds,
} from "./AgentConversationPanel";
import { AgentMessageList } from "./AgentMessageList";
import { AgentQuestionPrompt } from "./AgentQuestionPrompt";
import { selectAgentSessionConversation } from "./AgentSessionSwitcher";
import { AgentToolStepList } from "./AgentToolStepList";
import { AgentTurnTimeline } from "./AgentTurnTimeline";
import { agentEventReducer, createAgentTurnEventState } from "./agentEventReducer";

function turn(overrides: Partial<AgentTurn> = {}): AgentTurn {
  return {
    id: "projection-1",
    conversation_id: "conversation-1",
    task_id: null,
    harness_run_id: "run-1",
    harness_turn_id: "harness-turn-1",
    idempotency_key: "key-1",
    input_text: "请整理商品信息",
    input_asset_ids: ["asset/1"],
    status: "running",
    resume_required: false,
    output_text: null,
    thinking_text: null,
    error_text: null,
    question: null,
    question_answer: null,
    continuation_turn_id: null,
    artifact_name: null,
    artifact_step_id: null,
    library_organization_draft_revision_id: null,
    workflow_run_request_id: null,
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
      summary: null,
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

  it("renders assistant markdown as structured, safe output", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentAssistantMarkdown, {
        text: "# 商品方案\n\n请保留 **商品材质**，并使用 `hero` 作为首图类型。",
      }),
    );

    expect(markup).toContain("<h1>商品方案</h1>");
    expect(markup).toContain("<strong>商品材质</strong>");
    expect(markup).toContain("<code>hero</code>");
    expect(markup).not.toContain("<script");
  });

  it("keeps streaming assistant text as plaintext until the turn settles", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentAssistantMarkdown, {
        text: "# 还在写",
        streaming: true,
      }),
    );
    expect(markup).toContain("data-streaming");
    expect(markup).toContain("# 还在写");
    expect(markup).not.toContain("<h1>");
  });

  it("lets an empty product conversation submit the first user message", () => {
    expect(canSubmitAgentConversationMessage({ activeTurn: null })).toBe(true);
    expect(canSubmitAgentConversationMessage({ activeTurn: undefined })).toBe(true);
    expect(canSubmitAgentConversationMessage({ activeTurn: turn({ status: "running" }) })).toBe(false);
    expect(canSubmitAgentConversationMessage({ activeTurn: turn({ status: "requires_input" }) })).toBe(false);
    expect(canSubmitAgentConversationMessage({ activeTurn: turn({ status: "awaiting_confirmation" }) })).toBe(false);
  });

  it("binds a chat Turn to a Task only when the workbench route has one", () => {
    expect(agentConversationSubmitTaskId(null)).toBeNull();
    expect(agentConversationSubmitTaskId(undefined)).toBeNull();
    expect(agentConversationSubmitTaskId("task-1")).toBe("task-1");
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

  it("exposes a reference upload control when the conversation can accept files", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentComposer, {
        value: "",
        selectedAssets: [],
        isSubmitting: false,
        canSubmit: false,
        stopAvailable: false,
        isStopping: false,
        error: null,
        onChange: () => undefined,
        onOpenAssets: () => undefined,
        onRemoveAsset: () => undefined,
        onPreviewAsset: () => undefined,
        onSubmit: () => undefined,
        onStop: () => undefined,
        onUploadFiles: () => undefined,
      }),
    );
    expect(markup).toContain("上传参考图");
    expect(markup).toContain("accept=\"image/jpeg,image/png,image/webp\"");
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

  it("keeps the empty composer focused on the conversation input", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentComposer, {
        value: "",
        selectedAssets: [],
        isSubmitting: false,
        canSubmit: false,
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

    expect(markup).not.toContain("优化构图与主图排版");
    expect(markup).not.toContain("分析视觉风格与打光");
    expect(markup).not.toContain("生成当前工作流执行草案");
    expect(markup).toContain("data-agent-composer");
    expect(markup).toContain("Enter 发送，Shift + Enter 换行");
  });

  it("accepts jpeg/png/webp and files without a MIME type when the extension matches", () => {
    const png = new File([""], "front.png", { type: "image/png" });
    const gif = new File([""], "loop.gif", { type: "image/gif" });
    const unnamedPng = new File([""], "detail.PNG", { type: "" });

    expect(classifyImageFiles([png, gif, unnamedPng])).toEqual({
      accepted: [png, unnamedPng],
      rejected: true,
    });
    expect(classifyImageFiles([gif]).rejected).toBe(true);
    expect(classifyImageFiles([png])).toEqual({ accepted: [png], rejected: false });
  });

  it("renders an upload control when the conversation can accept reference files", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentComposer, {
        value: "",
        selectedAssets: [],
        isSubmitting: false,
        canSubmit: false,
        stopAvailable: false,
        isStopping: false,
        error: null,
        onChange: () => undefined,
        onOpenAssets: () => undefined,
        onRemoveAsset: () => undefined,
        onPreviewAsset: () => undefined,
        onSubmit: () => undefined,
        onStop: () => undefined,
        onUploadFiles: () => undefined,
      }),
    );

    expect(markup).toContain("上传参考图");
    expect(markup).toContain('type="file"');
    expect(markup).toContain("把参考图拖进来，直接说你要什么");
  });

  it("renders listed Question options, free text, and skip in the composer slot", () => {
    const question: AgentQuestion = {
      id: "question-1",
      header: "价格",
      question: "商品价格是多少？",
      options: [{ label: "299 元" }, { label: "暂不展示", description: "工作流不生成价格文案" }],
    };
    const openMarkup = renderToStaticMarkup(
      createElement(AgentQuestionPrompt, {
        question,
        busy: false,
        error: null,
        onAnswer: () => undefined,
      }),
    );

    expect(openMarkup).toContain("商品价格是多少？");
    expect(openMarkup).toContain("299 元");
    expect(openMarkup).toContain("输入其他回答");
    expect(openMarkup).toContain("跳过");
  });

  it("puts the question card inside the composer instead of a panel above it", () => {
    const question: AgentQuestion = {
      id: "question-1",
      header: "商品名",
      question: "这个商品叫什么名字？",
      options: [{ label: "筋膜枪" }, { label: "稍后再说" }],
    };
    const markup = renderToStaticMarkup(
      createElement(AgentComposer, {
        value: "",
        selectedAssets: [],
        isSubmitting: false,
        canSubmit: false,
        stopAvailable: true,
        isStopping: false,
        error: null,
        question,
        questionBusy: false,
        questionError: null,
        onAnswerQuestion: () => undefined,
        onChange: () => undefined,
        onOpenAssets: () => undefined,
        onRemoveAsset: () => undefined,
        onPreviewAsset: () => undefined,
        onSubmit: () => undefined,
        onStop: () => undefined,
      }),
    );
    expect(markup).toContain("data-agent-composer");
    expect(markup).toContain("这个商品叫什么名字？");
    expect(markup).toContain("筋膜枪");
    expect(markup).toContain("跳过");
    expect(markup).not.toContain("把参考图拖进来，直接说你要什么");
  });

  it("hides orphan question continuation turns from the timeline", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [
          turn({
            id: "projection-1",
            input_text: "可以帮我创建商品工作流吗",
            continuation_turn_id: "projection-2",
            status: "requires_input",
          }),
          turn({
            id: "projection-2",
            input_text: "继续当前 Agent 任务。针对问题“这个商品叫什么名字？”，用户回答：筋膜枪。",
            status: "queued",
          }),
        ],
        activeTurnId: "projection-1",
        eventState: null,
        initialTurnPending: false,
      }),
    );
    expect(markup).toContain("可以帮我创建商品工作流吗");
    expect(markup).not.toContain("继续当前 Agent 任务");
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
        kind: "item.delta",
        payload: { item_id: "attempt-1", item_kind: "assistant_text", delta: "流式回答", step_id: "step-1", attempt_id: "attempt-1" },
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
    const globalMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({ input_asset_ids: ["media/1"] })],
        activeTurnId: null,
        eventState: null,
        initialTurnPending: false,
        onPreviewAsset: () => undefined,
        getAssetThumbnailUrl: (assetId) => `/api/media-library/${encodeURIComponent(assetId)}/download?variant=thumbnail`,
      }),
    );

    expect(activeMarkup).toContain("流式回答");
    expect(activeMarkup).toContain("/api/v2/product-image-assets/asset%2F1/download?variant=thumbnail");
    expect(terminalMarkup).toContain("最终回答");
    expect(terminalMarkup).not.toContain("流式回答");
    expect(globalMarkup).toContain("/api/media-library/media%2F1/download?variant=thumbnail");
  });

  it("shows a pending user echo before the prompt Turn is persisted", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [],
        activeTurnId: null,
        eventState: null,
        initialTurnPending: false,
        pendingEcho: {
          text: "先发这一句",
          assetIds: [],
          createdAt: "2026-08-30T00:00:00Z",
        },
      }),
    );
    expect(markup).toContain("data-agent-pending-echo");
    expect(markup).toContain("先发这一句");
    expect(markup).not.toContain("data-agent-empty");
  });

  it("places the pending user echo after existing turns", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({ input_text: "上一句" })],
        activeTurnId: null,
        eventState: null,
        initialTurnPending: false,
        pendingEcho: {
          text: "还在提交",
          assetIds: [],
          createdAt: "2026-08-30T00:00:01Z",
        },
      }),
    );
    expect(markup.indexOf("上一句")).toBeGreaterThan(-1);
    expect(markup.indexOf("上一句")).toBeLessThan(markup.indexOf("data-agent-pending-echo"));
    expect(markup).toContain("还在提交");
  });

  it("renders a live thinking row before tools and folds process after the final answer", () => {
    let eventState = createAgentTurnEventState("projection-1");
    eventState = agentEventReducer(eventState, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 1,
        created_at: "2026-08-14T00:00:01Z",
        kind: "item.delta",
        payload: { item_id: "attempt-1", item_kind: "thinking", delta: "内部推理", step_id: "step-1", attempt_id: "attempt-1", content_index: 0 },
      },
    });
    const thinkingMarkup = renderToStaticMarkup(
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
    expect(thinkingMarkup).toContain("data-agent-thinking-row");
    expect(thinkingMarkup).toContain("data-agent-thinking-running");
    expect(thinkingMarkup).toContain("内部推理");
    expect(thinkingMarkup).not.toContain("data-agent-turn-process");

    eventState = agentEventReducer(eventState, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 2,
        created_at: "2026-08-14T00:00:02Z",
        kind: "item.completed",
        payload: {
          item_id: "step-tool",
          item_kind: "tool_call",
          step_id: "step-tool",
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
        sequence: 3,
        created_at: "2026-08-14T00:00:03Z",
        kind: "item.delta",
        payload: { item_id: "attempt-1", item_kind: "assistant_text", delta: "流式终答", step_id: "step-1", attempt_id: "attempt-1" },
      },
    });
    const liveMarkup = renderToStaticMarkup(
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
    const thinkingIndex = liveMarkup.indexOf("data-agent-thinking-row");
    const toolIndex = liveMarkup.indexOf("data-agent-tool-step-id=\"step-tool\"");
    const textIndex = liveMarkup.indexOf("流式终答");
    expect(thinkingIndex).toBeGreaterThan(-1);
    expect(toolIndex).toBeGreaterThan(thinkingIndex);
    expect(textIndex).toBeGreaterThan(toolIndex);
    expect(liveMarkup).toContain("data-agent-item-card");
    expect(liveMarkup).not.toContain("data-agent-turn-process");

    const compactMarkup = renderToStaticMarkup(
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
    expect(compactMarkup).toContain("data-agent-turn-process");
    expect(compactMarkup).not.toMatch(/data-agent-turn-process[^>]*open/);
    expect(compactMarkup).toContain("已处理 1 步");
    expect(compactMarkup).toContain("内部推理");
    expect(compactMarkup).toMatch(/data-agent-turn-body[\s\S]*最终回答/);
    expect(compactMarkup).not.toContain("流式终答");
  });

  it("lets a historical thinking snapshot expand inside the folded process", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({
          status: "succeeded",
          thinking_text: "刷新后的思考",
          output_text: "最终回答",
        })],
        activeTurnId: null,
        eventState: null,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onPreviewAsset: () => undefined,
      }),
    );
    expect(markup).toContain("data-agent-turn-process");
    expect(markup).toContain("已思考");
    expect(markup).toContain("data-agent-thinking-row");
    expect(markup).toContain("刷新后的思考");
    expect(markup).toContain("data-agent-thinking-text");
    expect(markup).toMatch(/data-agent-turn-body[\s\S]*最终回答/);
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

  it("renders a graph proposal card from tool meta and focuses the canvas", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentTurnTimeline, {
        blocks: [{ type: "tool", key: "tool:propose-1", step_id: "propose-1" }],
        toolSteps: [
          {
            step_id: "propose-1",
            kind: "propose_graph",
            summary: "提交未应用的图提案",
            status: "succeeded",
            tool_name: "propose_graph_change_set_v1",
            meta: {
              pending_confirmation: true,
              proposal_id: "proposal-1",
              summary: "加一个镜头",
              operation_summaries: ["add_node"],
              affected_node_ids: ["node-1"],
            },
          },
        ],
        live: false,
        fold: false,
        onCanvasFocus: () => undefined,
      }),
    );
    expect(markup).toContain("data-agent-graph-proposal-card");
    expect(markup).toContain("proposal-1");
    expect(markup).toContain("加一个镜头");
    expect(markup).toContain("data-agent-graph-proposal-focus");
    expect(markup).toContain("等待你在画布上确认");
  });

  it("renders apply_graph and propose_graph tool rows without crashing", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentToolStepList, {
        steps: [
          {
            step_id: "apply-1",
            kind: "apply_graph",
            summary: "立即写入 live graph ChangeSet",
            status: "succeeded",
            tool_name: "apply_graph_change_set_v1",
          },
          {
            step_id: "propose-1",
            kind: "propose_graph",
            summary: "提交未应用的图提案",
            status: "running",
            tool_name: "propose_graph_change_set_v1",
          },
        ],
      }),
    );

    expect(markup).toContain("更新工作流");
    expect(markup).toContain("准备画布修改");
    expect(markup).toContain("data-agent-tool-step-id=\"apply-1\"");
    expect(markup).toContain("data-agent-tool-step-id=\"propose-1\"");
  });

  it("shows a compact ask_question answer summary instead of a second user bubble", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentToolStepList, {
        steps: [
          {
            step_id: "ask-1",
            kind: "ask_question",
            summary: "等待用户回答结构化问题",
            status: "succeeded",
            tool_name: "ask_user",
            details: {
              phase: "question",
              question_text: "这个商品叫什么名字？",
              output_summary: "筋膜枪",
            },
          },
        ],
      }),
    );
    expect(markup).toContain("筋膜枪");
    expect(markup).toContain("提出问题");
    expect(markup).not.toContain("继续当前 Agent 任务");
  });

  it("keeps tool names, source fields, and validation internals out of the merchant timeline", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentToolStepList, {
        steps: [
          {
            step_id: "skill-1",
            kind: "load_skill",
            summary: "加载版本化 ProductFlow Skill 指令",
            status: "succeeded",
            tool_name: "load_productflow_skill",
            details: {
              phase: "skill_load",
              skill_name: "library-organization",
              instruction_excerpt: "# Library Organization\n\n保留已核验事实。",
              instruction_truncated: false,
              output_summary: "已加载版本化 Skill 指令；完整内容已提供给模型。",
            },
          },
          {
            step_id: "draft-1",
            kind: "propose_draft",
            summary: "提交完整 ProductFlow 草案",
            status: "failed",
            tool_name: "propose_global_draft",
            details: {
              phase: "tool_result",
              error_code: "library_organization_draft_validation_failed",
              retryable: true,
              validation_issues: [
                { path: "image_types.0.images.0.delivery_spec.crop_anchor", message: "contain 不能指定 crop_anchor" },
              ],
            },
          },
          {
            step_id: "context-1",
            kind: "inspect_context",
            summary: "读取 ProductFlow 当前上下文",
            status: "succeeded",
            tool_name: "get_product_workflow_context_v1",
            details: {
              phase: "tool_result",
              context_sections: ["product_facts", "intake", "draft_guidance"],
              output_summary: "已读取当前商品事实、参考资产和提交前校验指导。",
            },
          },
          {
            step_id: "apply-1",
            kind: "apply_graph",
            summary: "立即写入 live graph ChangeSet",
            status: "succeeded",
            tool_name: "apply_graph_change_set_v1",
            details: {
              phase: "tool_result",
              operation_summaries: ["rename_node"],
              affected_node_ids: ["n1"],
              truncated: true,
            },
          },
        ],
      }),
    );

    expect(markup).toContain("准备任务");
    expect(markup).toContain("读取商品信息");
    expect(markup).toContain("部分设置需要调整后才能继续");
    expect(markup).toContain("data-agent-tool-step-details");
    expect(markup).toContain("open=\"\"");
    expect(markup).not.toContain("load_productflow_skill");
    expect(markup).not.toContain("library-organization");
    expect(markup).not.toContain("# Library Organization");
    expect(markup).not.toContain("library_organization_draft_validation_failed");
    expect(markup).not.toContain("image_types.0.images.0.delivery_spec.crop_anchor");
    expect(markup).not.toContain("draft_guidance");
    expect(markup).not.toContain("rename_node");
    expect(markup).not.toContain("n1");
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
        kind: "item.completed",
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
        kind: "item.completed",
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
    expect(historicalMarkup).toContain("读取商品信息");
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
        kind: "item.completed",
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
        kind: "turn.completed",
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
    expect(staleMarkup).toContain("整理方案");
    expect(staleMarkup).not.toContain("完成工作流方案");
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
        kind: "item.completed",
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

  it("renders each displayed turn from its own event journal instead of snapshot fold", () => {
    let first = createAgentTurnEventState("projection-1");
    first = agentEventReducer(first, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 1,
        created_at: "2026-08-14T00:00:01Z",
        kind: "item.delta",
        payload: { item_id: "attempt-1", item_kind: "assistant_text", delta: "中间说明", step_id: "step-1", attempt_id: "attempt-1" },
      },
    });
    first = agentEventReducer(first, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 2,
        created_at: "2026-08-14T00:00:02Z",
        kind: "item.completed",
        payload: {
          item_id: "step-mid",
          item_kind: "tool_call",
          step_id: "step-mid",
          kind: "inspect_context",
          summary: "读取商品上下文",
          status: "succeeded",
        },
      },
    });
    first = agentEventReducer(first, {
      type: "event",
      event: {
        schema_version: 1,
        run_id: "run-1",
        turn_id: "harness-turn-1",
        sequence: 3,
        created_at: "2026-08-14T00:00:03Z",
        kind: "item.delta",
        payload: { item_id: "attempt-1", item_kind: "assistant_text", delta: "终答", step_id: "step-1", attempt_id: "attempt-1" },
      },
    });
    const journalMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({
          status: "succeeded",
          thinking_text: "快照思考",
          output_text: "终答",
          tool_steps: [{
            step_id: "step-mid",
            kind: "inspect_context",
            summary: "读取商品上下文",
            status: "succeeded",
          }],
        })],
        activeTurnId: null,
        eventStates: { "projection-1": first },
        initialTurnPending: false,
      }),
    );
    const emptyLogMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({
          status: "succeeded",
          thinking_text: "快照思考",
          output_text: "终答",
        })],
        activeTurnId: null,
        eventStates: { "projection-1": createAgentTurnEventState("projection-1") },
        initialTurnPending: false,
      }),
    );

    const midText = journalMarkup.indexOf("中间说明");
    const tool = journalMarkup.indexOf("data-agent-tool-step-id=\"step-mid\"");
    const answer = journalMarkup.indexOf("终答");
    expect(midText).toBeGreaterThan(-1);
    expect(tool).toBeGreaterThan(midText);
    expect(answer).toBeGreaterThan(tool);
    expect(journalMarkup).not.toContain("快照思考");
    expect(emptyLogMarkup).toContain("快照思考");
    expect(emptyLogMarkup).toContain("终答");
  });

  it("adds copy and retry actions to settled user and assistant messages", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({ status: "succeeded", thinking_text: "不要复制这段思考", output_text: "已整理完成" })],
        activeTurnId: null,
        eventState: null,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onRetryTurn: () => undefined,
      }),
    );

    expect(markup.match(/aria-label="复制消息"/g)).toHaveLength(2);
    expect(markup).toContain("不要复制这段思考");
    expect(markup).toContain("data-agent-turn-process");
    expect(markup).toContain("agent-markdown");
    expect(markup).toContain("data-agent-message-actions");
    expect(markup).toContain("data-agent-turn-retry");
    expect(markup).toContain("aria-label=\"重试\"");
    expect(markup).not.toContain("sm:opacity-0");
  });

  it("renders turn status, failures, and the matching Draft action in the turn tail", () => {
    const failed = turn({
      status: "failed",
      error_text: "草案同步失败",
      library_organization_draft_revision_id: "revision-1",
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
    expect(markup).toContain("审阅素材整理方案");
    expect(staleRevisionMarkup).not.toContain("审阅素材整理方案");
    expect(markup).not.toContain("data-agent-turn-retry");
  });

  it("keeps retry on settled Turns and only disables it while another Turn is running", () => {
    const failed = turn({
      status: "failed",
      error_text: "Agent 生成失败，请重试；持续失败请检查 Agent 供应商配置",
    });
    const retryableMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [failed],
        activeTurnId: null,
        eventState: null,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onRetryTurn: () => undefined,
      }),
    );
    const blockedMarkup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [failed, turn({ id: "projection-2", status: "running" })],
        activeTurnId: "projection-2",
        eventState: null,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onRetryTurn: () => undefined,
      }),
    );

    expect(retryableMarkup).toContain("data-agent-turn-retry");
    expect(retryableMarkup).toContain("data-agent-message-actions");
    expect(retryableMarkup).toContain("aria-label=\"重试\"");
    expect(retryableMarkup).not.toMatch(/role="alert"[^]*data-agent-turn-retry/);
    expect(blockedMarkup).toContain("data-agent-turn-retry");
    expect(blockedMarkup).toMatch(/<button(?=[^>]*data-agent-turn-retry)(?=[^>]*disabled)[^>]*>/);
  });

  it("keeps one user bubble when a failed Turn is retried", () => {
    const failed = turn({
      id: "projection-1",
      status: "failed",
      error_text: "Agent 生成失败，请重试；持续失败请检查 Agent 供应商配置",
      idempotency_key: "orig-key",
    });
    const retry = turn({
      id: "projection-2",
      status: "running",
      input_text: failed.input_text,
      output_text: null,
      error_text: null,
      idempotency_key: "retry:projection-1:attempt-2",
    });
    const markup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [failed, retry],
        activeTurnId: "projection-2",
        eventState: null,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
        onRetryTurn: () => undefined,
      }),
    );

    expect(markup.split(failed.input_text).length - 1).toBe(1);
    expect(markup).not.toContain("Agent 生成失败");
    expect(markup).toContain("Agent 正在处理");
    expect(markup).toContain("data-agent-turn-retry");
    expect(markup).toMatch(/<button(?=[^>]*data-agent-turn-retry)(?=[^>]*disabled)[^>]*>/);
  });

  it("renders a sync diagnostic as a warning while the Turn remains active", () => {
    const markup = renderToStaticMarkup(
      createElement(AgentMessageList, {
        turns: [turn({ status: "running", sync_error: "Agent 服务暂时不可用" })],
        activeTurnId: "projection-1",
        eventState: null,
        initialTurnPending: false,
        hasOlder: false,
        loadingOlder: false,
        onLoadOlder: async () => undefined,
      }),
    );

    expect(markup).toContain("正在同步最终状态");
    expect(markup).toContain("Agent 服务暂时不可用");
    expect(markup).not.toContain('role="alert"');
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

  it("resolves canvas focus ids to existing graph nodes, including group members and edge ends", () => {
    const graph = {
      id: "g1",
      product_id: "p1",
      title: "夏季主图",
      schema_version: 3,
      revision: 1,
      last_operation_group_id: null,
      can_undo: false,
      can_redo: false,
      nodes: [
        { id: "prompt", node_type: "image_prompt", title: "提示词", position_x: 0, position_y: 0, config: {}, bound_asset_id: null, group_id: "group-1", preview_asset_id: null, config_status: "ready", unused: false, incoming: [], outgoing: [] },
        { id: "image", node_type: "image_generation", title: "生图", position_x: 0, position_y: 0, config: {}, bound_asset_id: null, group_id: "group-1", preview_asset_id: null, config_status: "ready", unused: false, incoming: [], outgoing: [] },
      ],
      edges: [{ id: "e1", source_node_id: "prompt", target_node_id: "image", data_type: "prompt", role: "prompt", order: 0 }],
      groups: [{ id: "group-1", title: "主图", member_ids: ["prompt", "image"] }],
    };
    expect(resolveAgentCanvasFocusNodeIds({
      node_ids: ["image", "missing"],
      edge_ids: [],
      group_ids: [],
    }, graph as never)).toEqual(["image"]);
    expect(resolveAgentCanvasFocusNodeIds({
      node_ids: [],
      edge_ids: ["e1"],
      group_ids: [],
    }, graph as never).sort()).toEqual(["image", "prompt"]);
    expect(resolveAgentCanvasFocusNodeIds({
      node_ids: [],
      edge_ids: [],
      group_ids: ["group-1"],
    }, graph as never).sort()).toEqual(["image", "prompt"]);
    expect(resolveAgentCanvasFocusNodeIds({
      node_ids: ["image"],
      edge_ids: ["e1"],
      group_ids: [],
    }, null)).toEqual(["image"]);
    expect(resolveAgentCanvasFocusNodeIds({
      node_ids: [],
      edge_ids: ["e1"],
      group_ids: ["group-1"],
    }, null)).toEqual([]);
  });
});
