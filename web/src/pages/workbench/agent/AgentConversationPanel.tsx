import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, CircleAlert, Flag, Loader2, Maximize2, Play, RotateCw, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";

import { GalleryImagePreviewDialog } from "../../../components/GalleryImagePreviewDialog";
import { api, ApiError } from "../../../lib/api";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { TranslationKey } from "../../../lib/i18n";
import type {
  AgentAttachment,
  AgentCanvasFocus,
  AgentConversation,
  AgentPageContextSnapshotInput,
  AgentQuestionAnswer,
  AgentTaskStatus,
  AgentTurn,
  AgentWorkflowRunRequest,
  GalleryAsset,
  GraphProjection,
  ProductImageAsset,
} from "../../../lib/types";
import {
  ProductImageExplorer,
  type ImageExplorerSelectionTarget,
} from "../chrome/image-explorer/ProductImageExplorer";
import { agentTurnRetrySubmitInput, canRetryAgentTurn, retryIdempotencyKey } from "./agentTurnRetry";
import { AgentComposer, AGENT_COMPOSER_MAX_ASSETS } from "./AgentComposer";
import { AgentMessageList } from "./AgentMessageList";
import { AgentQuestionPrompt } from "./AgentQuestionPrompt";
import { AgentSessionSwitcher } from "./AgentSessionSwitcher";
import { AgentWorkflowRunRequestCard } from "./AgentWorkflowRunRequestCard";
import { agentProductWorkbenchPath } from "./productWorkbenchRoute";
import { useAgentConversation } from "./useAgentConversation";
import { useAgentTurnEvents } from "./useAgentTurnEvents";

interface AgentConversationPanelProps {
  productId: string;
  productName: string;
  conversation: AgentConversation;
  graph?: GraphProjection | null;
  taskId?: string | null;
  pageContext?: AgentPageContextSnapshotInput | null;
  className?: string;
  onOpenRuns?: () => void;
  onExpandGlobalAgent?: () => void;
  onCanvasFocus?: (nodeIds: string[]) => void;
}

export function AgentConversationPanel({
  productId,
  productName,
  conversation,
  graph = null,
  taskId = null,
  pageContext = null,
  className = "",
  onOpenRuns,
  onExpandGlobalAgent,
  onCanvasFocus,
}: AgentConversationPanelProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const agent = useAgentConversation({ productId, conversation, graph, taskId, pageContext });
  const workflowRunRequestQueryKey = [
    "agent-workflow-run-request",
    productId,
    conversation.id,
  ] as const;
  const workflowRunRequestQuery = useQuery({
    queryKey: workflowRunRequestQueryKey,
    queryFn: () => api.getAgentWorkflowRunRequest(productId, conversation.id),
    refetchInterval: (query) => workflowRunRequestRefetchIntervalMs(query.state.data),
  });
  const [composerText, setComposerText] = useState("");
  const [composerAssets, setComposerAssets] = useState<GalleryAsset[]>([]);
  const [assetSelectorOpen, setAssetSelectorOpen] = useState(false);
  const [preview, setPreview] = useState<DownloadableImage | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [answeredQuestionId, setAnsweredQuestionId] = useState<string | null>(null);
  const composerKeyRef = useRef(globalThis.crypto.randomUUID());
  const retryKeysRef = useRef(new Map<string, string>());
  const [retryingTurnId, setRetryingTurnId] = useState<string | null>(null);
  const appliedCanvasFocusRef = useRef<string | null>(null);

  const cacheWorkflowRunRequest = (request: AgentWorkflowRunRequest) => {
    queryClient.setQueryData(workflowRunRequestQueryKey, request);
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: ["agent-turns", productId, conversation.id] }),
      queryClient.invalidateQueries({ queryKey: ["agent-turn", productId, conversation.id] }),
      queryClient.invalidateQueries({ queryKey: ["agent-workbench", productId] }),
      queryClient.invalidateQueries({ queryKey: ["agent-tasks"] }),
      queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] }),
      queryClient.invalidateQueries({ queryKey: ["graph-runs", productId, request.workflow_id] }),
      queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] }),
      queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] }),
      queryClient.invalidateQueries({ queryKey: ["product", productId] }),
      queryClient.invalidateQueries({ queryKey: ["products"] }),
    ]);
  };
  const confirmWorkflowRunRequestMutation = useMutation({
    mutationFn: () => {
      const request = workflowRunRequestQuery.data;
      if (!request) {
        throw new Error(t("agentWorkbench.workflowRunRequest.notFound"));
      }
      return api.confirmAgentWorkflowRunRequest(productId, conversation.id, request.id);
    },
    onSuccess: cacheWorkflowRunRequest,
  });
  const cancelWorkflowRunRequestMutation = useMutation({
    mutationFn: () => {
      const request = workflowRunRequestQuery.data;
      if (!request) {
        throw new Error(t("agentWorkbench.workflowRunRequest.notFound"));
      }
      return api.cancelAgentWorkflowRunRequest(productId, conversation.id, request.id);
    },
    onSuccess: cacheWorkflowRunRequest,
  });

  const events = useAgentTurnEvents({
    getEventsUrl: (turnId, after) => api.getAgentTurnEventsUrl(productId, conversation.id, turnId, after),
    runId: agent.activeTurn?.harness_run_id ?? null,
    turn: agent.activeTurn,
    onTerminal: () => void agent.refreshLatestTurn(),
  });
  const activeQuestion =
    events.state.turn_key === agent.activeTurn?.id && events.state.question
      ? events.state.question
      : agent.activeTurn?.question ?? null;

  useEffect(() => setAnsweredQuestionId(null), [activeQuestion?.id]);
  useEffect(() => {
    if (!onCanvasFocus) return;
    const turns = [agent.latestTurn, ...agent.turns].filter((item): item is AgentTurn => Boolean(item));
    let selected: { created_at: string; focus: AgentCanvasFocus } | null = null;
    for (const item of turns) {
      const focus = item.canvas_focus;
      if (!focus?.request_id) continue;
      if (
        !selected
        || item.created_at > selected.created_at
        || (item.created_at === selected.created_at && focus.request_id > selected.focus.request_id)
      ) {
        selected = { created_at: item.created_at, focus };
      }
    }
    if (!selected || appliedCanvasFocusRef.current === selected.focus.request_id) return;
    const nodeIds = resolveAgentCanvasFocusNodeIds(selected.focus, graph);
    if (!nodeIds.length) {
      const needsGraph = selected.focus.edge_ids.length > 0 || selected.focus.group_ids.length > 0;
      if (needsGraph && !graph) return;
      appliedCanvasFocusRef.current = selected.focus.request_id;
      return;
    }
    appliedCanvasFocusRef.current = selected.focus.request_id;
    onCanvasFocus(nodeIds);
  }, [agent.latestTurn, agent.turns, graph, onCanvasFocus]);
  useEffect(() => {
    if (agent.latestTurn?.status === "awaiting_confirmation" || agent.latestTurn?.workflow_run_request_id) {
      void queryClient.invalidateQueries({ queryKey: workflowRunRequestQueryKey });
    }
  }, [agent.latestTurn?.id, agent.latestTurn?.status, agent.latestTurn?.workflow_run_request_id, conversation.id, productId, queryClient]);
  useEffect(() => {
    const live = Boolean(agent.activeTurn);
    const refresh = () => {
      void queryClient.invalidateQueries({ queryKey: ["graph-runs", productId] });
    };
    if (!live) return;
    refresh();
    const timer = window.setInterval(refresh, 1_500);
    return () => {
      window.clearInterval(timer);
      refresh();
    };
  }, [agent.activeTurn?.id, agent.activeTurn?.status, productId, queryClient]);
  useEffect(() => {
    const request = workflowRunRequestQuery.data;
    if (!request) return;
    if (request.status === "succeeded" || request.status === "failed" || request.status === "cancelled" || request.workflow_run_status === "unknown") {
      void queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
      void queryClient.invalidateQueries({ queryKey: ["graph-runs", productId, request.workflow_id] });
      void queryClient.invalidateQueries({ queryKey: ["agent-tasks"] });
    }
  }, [productId, queryClient, workflowRunRequestQuery.data?.status, workflowRunRequestQuery.data?.workflow_id, workflowRunRequestQuery.data?.workflow_run_status]);
  useEffect(() => {
    if (!assetSelectorOpen && !preview) {
      return;
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        if (preview) {
          setPreview(null);
        } else {
          setAssetSelectorOpen(false);
        }
      }
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [assetSelectorOpen, preview]);

  const rotateComposerKey = () => {
    composerKeyRef.current = globalThis.crypto.randomUUID();
  };
  const uploadAssetsMutation = useMutation({
    mutationFn: (files: File[]) => api.addCanonicalProductImages(productId, files),
    onSuccess: (result) => {
      setComposerAssets((current) => mergeUploadedComposerAssets(current, result.items));
      rotateComposerKey();
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] }),
        queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] }),
        queryClient.invalidateQueries({ queryKey: ["product", productId] }),
      ]);
    },
  });
  const canSubmitMessage = canSubmitAgentConversationMessage({ activeTurn: agent.activeTurn });
  const submitMessage = async () => {
    const normalized = composerText.trim();
    if (!normalized || !canSubmitMessage || agent.submitTurnMutation.isPending) {
      return;
    }
    try {
      await agent.submitTurnMutation.mutateAsync({
        input_text: normalized,
        asset_ids: composerAssets.map((asset) => asset.id),
        idempotency_key: composerKeyRef.current,
        task_id: agentConversationSubmitTaskId(taskId),
        page_context: pageContext
          ? {
            ...pageContext,
            selected_asset_ids: composerAssets.map((asset) => asset.id),
            captured_at: new Date().toISOString(),
          }
          : null,
      });
      setComposerText("");
      setComposerAssets([]);
      rotateComposerKey();
    } catch {
      // 失败时 mutation 状态会渲染错误，草稿和幂等键保持不变
    }
  };
  const answerQuestion = async (answer: AgentQuestionAnswer) => {
    if (!agent.activeTurn || !activeQuestion || agent.answerQuestionMutation.isPending) {
      return;
    }
    try {
      await agent.answerQuestionMutation.mutateAsync({
        projectionId: agent.activeTurn.id,
        questionId: activeQuestion.id,
        answer,
      });
      setAnsweredQuestionId(activeQuestion.id);
    } catch {
      // 已持久化的答案和续跑仍可通过同一 question key 重试
    }
  };
  const retryTurn = async (turn: AgentTurn) => {
    if (
      !canRetryAgentTurn({ turn }) ||
      Boolean(agent.activeTurn) ||
      agent.submitTurnMutation.isPending
    ) {
      return;
    }
    let key = retryKeysRef.current.get(turn.id);
    if (!key) {
      key = retryIdempotencyKey(turn.id);
      retryKeysRef.current.set(turn.id, key);
    }
    setRetryingTurnId(turn.id);
    try {
      await agent.submitTurnMutation.mutateAsync(
        agentTurnRetrySubmitInput(turn, {
          idempotencyKey: key,
          taskId: turn.task_id ?? agentConversationSubmitTaskId(taskId),
          pageContext: pageContext
            ? {
              ...pageContext,
              selected_asset_ids: turn.input_asset_ids,
              captured_at: new Date().toISOString(),
            }
            : null,
        }),
      );
      retryKeysRef.current.delete(turn.id);
    } catch {
      // 失败请求沿用同一续跑键
    } finally {
      setRetryingTurnId(null);
    }
  };
  const previewSelectedAsset = (asset: AgentAttachment) => {
    setPreviewError(null);
    setPreview({
      previewUrl: api.toApiUrl(asset.preview_url),
      downloadUrl: api.toApiUrl(asset.download_url),
      filename: asset.original_filename,
      alt: asset.display_name,
    });
  };
  const previewTurnAsset = async (assetId: string) => {
    setPreviewError(null);
    try {
      const asset = await api.getGalleryAsset(productId, assetId);
      previewSelectedAsset(asset);
    } catch (error) {
      setPreviewError(errorDetail(error, t("agentWorkbench.previewFailed")));
    }
  };

  const selectorTarget: ImageExplorerSelectionTarget = {
    selectedAssets: composerAssets,
    maxSelected: AGENT_COMPOSER_MAX_ASSETS,
    confirmLabel: t("agentWorkbench.attachSelected"),
    selectionLabel: (count, maximum) =>
      t("agentWorkbench.assetsSelected", { count, maximum }),
    limitMessage: t("agentWorkbench.assetLimit", { maximum: AGENT_COMPOSER_MAX_ASSETS }),
    onConfirm: (assets) => {
      setComposerAssets(assets);
      rotateComposerKey();
      setAssetSelectorOpen(false);
    },
  };
  const questionAnswered = Boolean(
    activeQuestion &&
    (answeredQuestionId === activeQuestion.id ||
      events.state.question_answered ||
      agent.activeTurn?.resume_required),
  );
  const questionError = errorDetailOrNull(agent.answerQuestionMutation.error);
  const composerError =
    errorDetailOrNull(agent.submitTurnMutation.error) ?? errorDetailOrNull(uploadAssetsMutation.error);
  const listError = errorDetailOrNull(agent.turnsQuery.error);
  const controlError =
    errorDetailOrNull(agent.cancelTurnMutation.error) ??
    errorDetailOrNull(agent.resumeTurnMutation.error) ??
    (!activeQuestion ? questionError : null) ??
    events.streamError ??
    previewError;
  const workflowRunRequestError = errorDetailOrNull(workflowRunRequestQuery.error)
    ?? errorDetailOrNull(confirmWorkflowRunRequestMutation.error)
    ?? errorDetailOrNull(cancelWorkflowRunRequestMutation.error);
  const connectionLabel = events.state.terminal_kind
    ? t("agentWorkbench.connection.syncing")
    : events.connectionState === "open"
      ? t("agentWorkbench.connection.open")
      : events.connectionState === "reconnecting"
        ? t("agentWorkbench.connection.reconnecting")
        : t("agentWorkbench.connection.connecting");

  const dialogs = (
    <>
      {assetSelectorOpen ? (
        <div
          role="dialog"
          aria-modal="true"
          aria-label={t("agentWorkbench.assetSelector")}
          className="fixed inset-0 z-[100] flex items-center justify-center bg-zinc-950/70 p-2 backdrop-blur-sm sm:p-4"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) {
              setAssetSelectorOpen(false);
            }
          }}
        >
          <div className="flex h-[min(780px,calc(100dvh-1rem))] w-full max-w-6xl min-h-0 flex-col overflow-hidden rounded-md bg-white shadow-2xl dark:bg-[#090d13] sm:h-[min(780px,calc(100dvh-2rem))]">
            <header className="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-zinc-200 px-4 dark:border-slate-800">
              <div className="min-w-0">
                <h2 className="truncate text-sm font-semibold text-zinc-950 dark:text-white">{t("agentWorkbench.assetSelector")}</h2>
                <p className="mt-0.5 text-xs text-zinc-500 dark:text-slate-400">{productName}</p>
              </div>
              <button
                type="button"
                onClick={() => setAssetSelectorOpen(false)}
                aria-label={t("agentWorkbench.closeAssetSelector")}
                title={t("agentWorkbench.closeAssetSelector")}
                className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md text-zinc-500 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 dark:text-slate-400 dark:hover:bg-slate-800"
              >
                <X size={18} />
              </button>
            </header>
            <div className="min-h-0 flex-1 overflow-y-auto p-3 sm:p-4">
              <ProductImageExplorer
                productId={productId}
                productName={productName}
                onPreviewImage={setPreview}
                selectionTarget={selectorTarget}
              />
            </div>
          </div>
        </div>
      ) : null}
      {preview ? (
        <GalleryImagePreviewDialog
          ariaLabel={t("agentWorkbench.previewAsset", { name: preview.alt })}
          imageUrl={preview.previewUrl}
          imageAlt={preview.alt}
          title={preview.alt}
          subtitle={preview.filename}
          body={preview.filename}
          providerNotesTitle={t("agentWorkbench.assetDetails")}
          downloadUrl={preview.downloadUrl}
          downloadLabel={t("agentWorkbench.downloadAsset")}
          closeLabel={t("agentWorkbench.closePreview")}
          onClose={() => setPreview(null)}
        />
      ) : null}
    </>
  );

  return (
    <section
      data-agent-conversation-panel
      className={`flex min-h-0 flex-col overflow-hidden bg-surface-base text-text-primary ${className}`}
    >
      <header className="flex min-h-14 shrink-0 items-center gap-3 border-b border-border-l1 bg-surface-raised/90 px-4 py-2.5 backdrop-blur">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent">
          <Bot size={18} />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-sm font-semibold text-text-primary">{t("agentWorkbench.agent")}</h2>
          <p className="truncate text-xs text-text-secondary">{productName}</p>
        </div>
        {onExpandGlobalAgent ? (
          <button
            type="button"
            onClick={onExpandGlobalAgent}
            aria-label={t("agentWorkbench.expandGlobalAgent")}
            title={t("agentWorkbench.expandGlobalAgent")}
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-slate-200 bg-white text-slate-600 hover:border-indigo-300 hover:bg-indigo-50/50 hover:text-indigo-700 dark:border-slate-800 dark:bg-[#0c121e] dark:text-slate-300 dark:hover:border-violet-500/40 dark:hover:text-violet-200 transition-colors"
          >
            <Maximize2 size={15} />
          </button>
        ) : null}
        {agent.activeTurn ? (
          <>
            <span
              role="status"
              aria-label={connectionLabel}
              title={connectionLabel}
              className={`h-2.5 w-2.5 shrink-0 rounded-full ${events.state.terminal_kind
                ? "animate-pulse bg-blue-600 dark:bg-cyan-400"
                : events.connectionState === "open"
                  ? "bg-emerald-500"
                  : events.connectionState === "reconnecting"
                    ? "animate-pulse bg-amber-500"
                    : "animate-pulse bg-zinc-400 dark:bg-slate-500"
                }`}
            />

          </>
        ) : null}
      </header>

      <AgentSessionSwitcher conversation={conversation} productName={productName} />
      <AgentGoalLoopBar
        productId={productId}
        conversation={conversation}
        taskId={taskId ?? null}
      />

      {listError ? (
        <PanelError
          message={listError}
          action={t("agentWorkbench.retry")}
          onAction={() => void agent.turnsQuery.refetch()}
        />
      ) : null}
      {controlError ? <PanelError message={controlError} /> : null}

      <AgentMessageList
        turns={agent.turns}
        activeTurnId={agent.activeTurn?.id ?? null}
        eventState={events.state}
        initialTurnPending={agent.turnsQuery.isLoading}
        hasOlder={Boolean(agent.turnsQuery.hasNextPage)}
        loadingOlder={agent.turnsQuery.isFetchingNextPage}
        onLoadOlder={() => agent.turnsQuery.fetchNextPage()}
        onPreviewAsset={(assetId) => void previewTurnAsset(assetId)}
        onRetryTurn={(turn) => void retryTurn(turn)}
        retryingTurnId={retryingTurnId}
      />

      <AgentWorkflowRunRequestCard
        request={workflowRunRequestQuery.data ?? null}
        loading={workflowRunRequestQuery.isLoading}
        busy={confirmWorkflowRunRequestMutation.isPending || cancelWorkflowRunRequestMutation.isPending}
        error={workflowRunRequestError}
        onConfirm={() => confirmWorkflowRunRequestMutation.mutate()}
        onCancel={() => cancelWorkflowRunRequestMutation.mutate()}
        onOpenRuns={onOpenRuns}
      />

      {activeQuestion && agent.activeTurn ? (
        <AgentQuestionPrompt
          question={activeQuestion}
          answered={questionAnswered}
          resumeRequired={Boolean(agent.activeTurn.resume_required)}
          busy={agent.answerQuestionMutation.isPending || agent.resumeTurnMutation.isPending}
          error={questionError}
          onAnswer={(answer) => void answerQuestion(answer)}
          onResume={() => agent.resumeTurnMutation.mutate(agent.activeTurn?.id ?? "")}
        />
      ) : null}

      {agent.activeTurn?.resume_required && !activeQuestion ? (
        <div className="flex items-center gap-3 border-t border-zinc-200 bg-blue-50 px-4 py-3 dark:border-slate-800 dark:bg-cyan-400/5">
          <CircleAlert size={16} className="shrink-0 text-blue-700 dark:text-cyan-300" />
          <span className="min-w-0 flex-1 text-xs text-blue-900 dark:text-cyan-100">{t("agentWorkbench.resumeRequired")}</span>
          <button
            type="button"
            onClick={() => agent.resumeTurnMutation.mutate(agent.activeTurn?.id ?? "")}
            disabled={agent.resumeTurnMutation.isPending}
            className="inline-flex h-10 items-center gap-2 rounded-md bg-blue-600 px-3 text-xs font-semibold text-white disabled:opacity-50 dark:bg-cyan-400 dark:text-[#071018]"
          >
            {agent.resumeTurnMutation.isPending ? <Loader2 size={14} className="animate-spin" /> : <Play size={14} />}
            {t("agentWorkbench.resume")}
          </button>
        </div>
      ) : null}

      {!activeQuestion ? (
        <AgentComposer
          value={composerText}
          selectedAssets={composerAssets}
          isSubmitting={agent.submitTurnMutation.isPending}
          canSubmit={canSubmitMessage}
          stopAvailable={Boolean(agent.activeTurn)}
          isStopping={
            agent.cancelTurnMutation.isPending ||
            agent.activeTurn?.status === "cancel_requested" ||
            Boolean(events.state.terminal_kind)
          }
          error={composerError}
          placeholder={
            agent.turns.length === 0 && graphHasCreateTemplate(graph)
              ? t("agentWorkbench.composerPlaceholder.intakeLanded")
              : undefined
          }
          onChange={setComposerText}
          onOpenAssets={() => setAssetSelectorOpen(true)}
          onRemoveAsset={(assetId) => {
            setComposerAssets((current) => current.filter((asset) => asset.id !== assetId));
            rotateComposerKey();
          }}
          onPreviewAsset={previewSelectedAsset}
          onSubmit={() => void submitMessage()}
          onStop={() => agent.cancelTurnMutation.mutate(agent.activeTurn?.id ?? "")}
          onUploadFiles={(files) => uploadAssetsMutation.mutate(files)}
          isUploading={uploadAssetsMutation.isPending}
        />
      ) : null}

      {typeof document === "undefined" ? dialogs : createPortal(dialogs, document.body)}
    </section>
  );
}

function graphHasCreateTemplate(graph: GraphProjection | null | undefined): boolean {
  return Boolean(
    graph?.nodes.some(
      (node) => node.node_type === "image_generation" || node.node_type === "prompt_generation",
    ),
  );
}

function galleryAssetFromUpload(asset: ProductImageAsset): GalleryAsset {
  return {
    ...asset,
    user_folder_name: null,
    image_type_title: null,
    generation: null,
    rendition: null,
  };
}

function mergeUploadedComposerAssets(
  current: readonly GalleryAsset[],
  uploaded: readonly ProductImageAsset[],
): GalleryAsset[] {
  const seen = new Set(current.map((asset) => asset.id));
  const next = [...current];
  for (const asset of uploaded) {
    if (seen.has(asset.id) || next.length >= AGENT_COMPOSER_MAX_ASSETS) {
      continue;
    }
    seen.add(asset.id);
    next.push(galleryAssetFromUpload(asset));
  }
  return next;
}

export function resolveAgentCanvasFocusNodeIds(
  focus: Pick<AgentCanvasFocus, "node_ids" | "edge_ids" | "group_ids">,
  graph: GraphProjection | null | undefined,
): string[] {
  const collected = [...focus.node_ids];
  if (graph) {
    for (const edge of graph.edges) {
      if (focus.edge_ids.includes(edge.id)) {
        collected.push(edge.source_node_id, edge.target_node_id);
      }
    }
    for (const group of graph.groups) {
      if (focus.group_ids.includes(group.id)) {
        collected.push(...group.member_ids);
      }
    }
  }
  const known = graph ? new Set(graph.nodes.map((node) => node.id)) : null;
  const unique: string[] = [];
  const seen = new Set<string>();
  for (const id of collected) {
    if (seen.has(id) || (known && !known.has(id))) continue;
    seen.add(id);
    unique.push(id);
  }
  return unique;
}


export function workflowRunRequestRefetchIntervalMs(
  request: Pick<AgentWorkflowRunRequest, "status" | "workflow_run_status"> | null | undefined,
): number | false {
  if (!request) return false;
  if (request.status === "awaiting_confirmation") return 1_500;
  if (request.status !== "confirmed") return false;
  const runStatus = request.workflow_run_status;
  if (runStatus === "succeeded" || runStatus === "failed" || runStatus === "cancelled" || runStatus === "unknown") {
    return false;
  }
  return 1_200;
}

export function canSubmitAgentConversationMessage(input: {
  activeTurn: AgentTurn | null | undefined;
}): boolean {
  return input.activeTurn == null;
}

export function agentConversationSubmitTaskId(routeTaskId: string | null | undefined): string | null {
  return routeTaskId ?? null;
}

const GOAL_LOOP_ACTIVE: ReadonlySet<AgentTaskStatus> = new Set([
  "queued",
  "running",
  "waiting_user",
  "awaiting_confirmation",
]);
const GOAL_LOOP_COMPLETABLE: ReadonlySet<AgentTaskStatus> = new Set([
  "waiting_user",
  "awaiting_confirmation",
  "paused",
]);
const GOAL_LOOP_PAUSABLE: ReadonlySet<AgentTaskStatus> = new Set([
  "queued",
  "waiting_user",
  "awaiting_confirmation",
]);
const GOAL_STATUS_LABEL: Record<AgentTaskStatus, TranslationKey> = {
  queued: "globalAgent.taskStatus.queued",
  running: "globalAgent.taskStatus.running",
  waiting_user: "globalAgent.taskStatus.waitingUser",
  awaiting_confirmation: "globalAgent.taskStatus.awaitingConfirmation",
  succeeded: "globalAgent.taskStatus.succeeded",
  failed: "globalAgent.taskStatus.failed",
  canceled: "globalAgent.taskStatus.canceled",
  paused: "globalAgent.taskStatus.paused",
  unknown: "globalAgent.taskStatus.unknown",
};

function AgentGoalLoopBar({
  productId,
  conversation,
  taskId,
}: {
  productId: string;
  conversation: AgentConversation;
  taskId: string | null;
}) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const sessionId = conversation.session_id;
  const [formOpen, setFormOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [goal, setGoal] = useState("");
  const taskQuery = useQuery({
    queryKey: ["agent-task", taskId],
    queryFn: () => api.getAgentTask(taskId as string),
    enabled: Boolean(taskId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "running"
        || status === "queued"
        || status === "awaiting_confirmation"
        || status === "waiting_user"
        ? 1_500
        : false;
    },
  });
  const invalidateTasks = () => {
    void queryClient.invalidateQueries({ queryKey: ["agent-tasks"] });
    void queryClient.invalidateQueries({ queryKey: ["agent-task", taskId] });
  };
  const createMutation = useMutation({
    mutationFn: () => {
      if (!sessionId) {
        throw new Error(t("agentWorkbench.goal.sessionRequired"));
      }
      return api.createAgentTask({
        session_id: sessionId,
        conversation_id: conversation.id,
        title: title.trim(),
        goal: goal.trim(),
      });
    },
    onSuccess: (task) => {
      setFormOpen(false);
      setTitle("");
      setGoal("");
      void queryClient.invalidateQueries({ queryKey: ["agent-tasks"] });
      navigate(agentProductWorkbenchPath(productId, sessionId, task.id));
    },
  });
  const leaveGoal = () => {
    invalidateTasks();
    if (sessionId) {
      navigate(agentProductWorkbenchPath(productId, sessionId));
    }
  };
  const completeMutation = useMutation({
    mutationFn: (id: string) => api.completeAgentTask(id),
    onSuccess: leaveGoal,
  });
  const pauseMutation = useMutation({
    mutationFn: (id: string) => api.pauseAgentTask(id),
    onSuccess: invalidateTasks,
  });
  const resumeMutation = useMutation({
    mutationFn: (id: string) => api.resumeAgentTask(id),
    onSuccess: invalidateTasks,
  });
  const cancelMutation = useMutation({
    mutationFn: (id: string) => api.cancelAgentTask(id),
    onSuccess: leaveGoal,
  });
  const task = taskQuery.data ?? null;
  const busy =
    createMutation.isPending ||
    completeMutation.isPending ||
    pauseMutation.isPending ||
    resumeMutation.isPending ||
    cancelMutation.isPending;
  const error =
    errorDetailOrNull(taskQuery.error) ??
    errorDetailOrNull(createMutation.error) ??
    errorDetailOrNull(completeMutation.error) ??
    errorDetailOrNull(pauseMutation.error) ??
    errorDetailOrNull(resumeMutation.error) ??
    errorDetailOrNull(cancelMutation.error);
  const canStart = Boolean(sessionId) && title.trim().length > 0 && goal.trim().length > 0;
  const active = task != null && (GOAL_LOOP_ACTIVE.has(task.status) || task.status === "paused");
  const showForm = formOpen && !active;

  return (
    <div className="shrink-0 border-b border-border-l1 bg-surface-raised px-4 py-2.5">
      {showForm ? (
        <form
          className="flex flex-col gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            if (canStart && !busy) createMutation.mutate();
          }}
        >
          <label className="flex flex-col gap-1 text-[11px] font-medium text-text-secondary">
            {t("agentWorkbench.goal.titleLabel")}
            <input
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder={t("agentWorkbench.goal.titlePlaceholder")}
              className="h-8 rounded-md border border-border-l1 bg-surface-base px-2 text-xs text-text-primary"
            />
          </label>
          <label className="flex flex-col gap-1 text-[11px] font-medium text-text-secondary">
            {t("agentWorkbench.goal.goalLabel")}
            <textarea
              value={goal}
              onChange={(event) => setGoal(event.target.value)}
              placeholder={t("agentWorkbench.goal.goalPlaceholder")}
              rows={2}
              className="rounded-md border border-border-l1 bg-surface-base px-2 py-1.5 text-xs text-text-primary"
            />
          </label>
          <div className="flex flex-wrap gap-1.5">
            <button
              type="submit"
              disabled={!canStart || busy}
              className="inline-flex h-8 items-center rounded-md bg-accent px-2.5 text-[11px] font-semibold text-white disabled:opacity-50"
            >
              {t("agentWorkbench.goal.start")}
            </button>
            <button
              type="button"
              onClick={() => setFormOpen(false)}
              className="inline-flex h-8 items-center rounded-md border border-border-l1 px-2.5 text-[11px] font-semibold text-text-secondary"
            >
              {t("agentWorkbench.goal.cancelForm")}
            </button>
          </div>
        </form>
      ) : task ? (
        <div className="flex flex-col gap-2">
          <div className="flex min-w-0 items-start gap-2">
            <Flag size={14} className="mt-0.5 shrink-0 text-accent" />
            <div className="min-w-0 flex-1">
              <p className="truncate text-xs font-semibold text-text-primary">{task.title}</p>
              <p className="truncate text-[11px] text-text-secondary">
                {t(GOAL_STATUS_LABEL[task.status])}
                {task.waiting_reason === "goal_loop" ? ` · ${t("agentWorkbench.goal.loopHint")}` : ""}
              </p>
            </div>
          </div>
          {active ? (
            <div className="flex flex-wrap gap-1.5">
              {GOAL_LOOP_COMPLETABLE.has(task.status) ? (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => completeMutation.mutate(task.id)}
                  className="inline-flex h-8 items-center rounded-md bg-accent px-2.5 text-[11px] font-semibold text-white disabled:opacity-50"
                >
                  {t("agentWorkbench.goal.complete")}
                </button>
              ) : null}
              {GOAL_LOOP_PAUSABLE.has(task.status) ? (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => pauseMutation.mutate(task.id)}
                  className="inline-flex h-8 items-center rounded-md border border-border-l1 px-2.5 text-[11px] font-semibold text-text-primary disabled:opacity-50"
                >
                  {t("agentWorkbench.goal.pause")}
                </button>
              ) : null}
              {task.status === "paused" ? (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => resumeMutation.mutate(task.id)}
                  className="inline-flex h-8 items-center rounded-md border border-border-l1 px-2.5 text-[11px] font-semibold text-text-primary disabled:opacity-50"
                >
                  {t("agentWorkbench.goal.resume")}
                </button>
              ) : null}
              <button
                type="button"
                disabled={busy}
                onClick={() => cancelMutation.mutate(task.id)}
                className="inline-flex h-8 items-center rounded-md border border-border-l1 px-2.5 text-[11px] font-semibold text-text-secondary disabled:opacity-50"
              >
                {t("agentWorkbench.goal.clear")}
              </button>
            </div>
          ) : (
            <button
              type="button"
              onClick={() => setFormOpen(true)}
              className="inline-flex h-8 w-fit items-center rounded-md border border-border-l1 px-2.5 text-[11px] font-semibold text-text-primary"
            >
              {t("agentWorkbench.goal.start")}
            </button>
          )}
        </div>
      ) : (
        <button
          type="button"
          disabled={!sessionId}
          onClick={() => setFormOpen(true)}
          className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border-l1 px-2.5 text-[11px] font-semibold text-text-primary disabled:opacity-50"
        >
          <Flag size={13} />
          {sessionId ? t("agentWorkbench.goal.openForm") : t("agentWorkbench.goal.sessionRequired")}
        </button>
      )}
      {error ? <p className="mt-1.5 text-[11px] text-red-600 dark:text-red-300">{error}</p> : null}
    </div>
  );
}

function PanelError({
  message,
  action,
  onAction,
}: {
  message: string;
  action?: string;
  onAction?: () => void;
}) {
  return (
    <div role="alert" className="flex items-start gap-2 border-b border-red-200 bg-red-50 px-4 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/20 dark:bg-red-500/10 dark:text-red-200">
      <CircleAlert size={15} className="mt-0.5 shrink-0" />
      <span className="min-w-0 flex-1">{message}</span>
      {action && onAction ? (
        <button type="button" onClick={onAction} className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md px-2 font-semibold hover:bg-red-100 dark:hover:bg-red-500/15">
          <RotateCw size={13} />
          {action}
        </button>
      ) : null}
    </div>
  );
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}

function errorDetailOrNull(error: unknown): string | null {
  return error ? errorDetail(error, "Agent 请求失败") : null;
}
