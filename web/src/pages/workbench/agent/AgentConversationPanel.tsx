import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Flag, Maximize2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { GalleryImagePreviewDialog } from "../../../components/GalleryImagePreviewDialog";
import { Dialog, DialogContent } from "../../../components/ui/dialog";
import { IconButton } from "../../../components/ui/icon-button";
import { api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { TranslationKey } from "../../../lib/i18n";
import type {
  AgentCanvasFocus,
  AgentConversation,
  AgentPageContextSnapshotInput,
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
import { AGENT_COMPOSER_MAX_ASSETS } from "./AgentComposer";
import { AgentSessionSwitcher } from "./AgentSessionSwitcher";
import { AgentWorkflowRunRequestCard } from "./AgentWorkflowRunRequestCard";
import {
  ConversationWorkbench,
  PanelError,
} from "./ConversationPanel";
import {
  agentConversationSubmitTaskId,
  canSubmitAgentConversationMessage,
  errorDetailOrNull,
  mergeWorkflowRunRequest,
  resolveAgentCanvasFocusNodeIds,
  workflowRequestFromTurn,
} from "./conversation/helpers";
import { useConversationChrome } from "./conversation/useConversationChrome";
import { agentProductWorkbenchPath } from "./productWorkbenchRoute";
import { useAgentConversation } from "./useAgentConversation";
import { useAgentTurnEventMap, useAgentTurnEvents } from "./useAgentTurnEvents";

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
  onAgentPresenceChange?: (editing: boolean) => void;
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
  onAgentPresenceChange,
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
  });
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

  const events = useAgentTurnEvents({
    getEventsUrl: (turnId, after) => api.getAgentTurnEventsUrl(productId, conversation.id, turnId, after),
    runId: agent.activeTurn?.harness_run_id ?? null,
    turn: agent.activeTurn,
    onEvent: (event) => {
      if (taskId && [
        "turn.started",
        "item.started",
        "item.completed",
        "approval.requested",
        "approval.resolved",
        "turn.completed",
        "turn.awaiting_confirmation",
        "turn.failed",
        "turn.canceled",
        "turn.unknown",
      ].includes(event.kind)) {
        void queryClient.invalidateQueries({ queryKey: ["agent-task", taskId] });
      }
      if (event.kind !== "item.started" && event.kind !== "item.completed") return;
      const kind = typeof event.payload.kind === "string"
        ? event.payload.kind
        : typeof event.payload.tool_name === "string"
          ? event.payload.tool_name
          : "";
      const graphTool = kind === "apply_graph"
        || kind === "propose_graph"
        || kind === "apply_graph_change_set_v1"
        || kind === "propose_graph_change_set_v1";
      if (graphTool) {
        onAgentPresenceChange?.(
          event.kind === "item.started",
        );
      }
      if (kind === "apply_graph" || kind === "propose_graph") {
        void queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
      }
    },
    onArtifactProposed: () => {
      void queryClient.invalidateQueries({ queryKey: workflowRunRequestQueryKey });
    },
    onTerminal: () => {
      onAgentPresenceChange?.(false);
      void agent.refreshLatestTurn();
      if (taskId) void queryClient.invalidateQueries({ queryKey: ["agent-task", taskId] });
      void queryClient.invalidateQueries({ queryKey: workflowRunRequestQueryKey });
      void queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
    },
  });
  const eventStates = useAgentTurnEventMap({
    getEventsUrl: (turnId, after) => api.getAgentTurnEventsUrl(productId, conversation.id, turnId, after),
    turns: agent.turns,
  });
  const chrome = useConversationChrome<GalleryAsset>({
    conversationKey: conversation.id,
    agent,
    events,
    pageContext,
    submitTaskId: agentConversationSubmitTaskId(taskId),
    loadPreviewAsset: (assetId) => api.getGalleryAsset(productId, assetId),
    previewFailedLabel: t("agentWorkbench.previewFailed"),
  });
  const pendingRequestId = () =>
    mergeWorkflowRunRequest(
      workflowRunRequestQuery.data,
      workflowRequestFromTurn(agent.latestTurn, eventStates),
    )?.id;
  const confirmWorkflowRunRequestMutation = useMutation({
    mutationFn: () => {
      const requestId = pendingRequestId();
      if (!requestId) {
        throw new Error(t("agentWorkbench.workflowRunRequest.notFound"));
      }
      return api.confirmAgentWorkflowRunRequest(productId, conversation.id, requestId);
    },
    onSuccess: cacheWorkflowRunRequest,
  });
  const cancelWorkflowRunRequestMutation = useMutation({
    mutationFn: () => {
      const requestId = pendingRequestId();
      if (!requestId) {
        throw new Error(t("agentWorkbench.workflowRunRequest.notFound"));
      }
      return api.cancelAgentWorkflowRunRequest(productId, conversation.id, requestId);
    },
    onSuccess: cacheWorkflowRunRequest,
  });
  const uploadAssetsMutation = useMutation({
    mutationFn: (files: File[]) => api.addCanonicalProductImages(productId, files),
    onSuccess: (result) => {
      chrome.setComposerAssets((current) => mergeUploadedComposerAssets(current, result.items));
      chrome.rotateComposerKey();
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] }),
        queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] }),
        queryClient.invalidateQueries({ queryKey: ["product", productId] }),
      ]);
    },
  });

  useEffect(() => {
    if (!agent.activeTurn) onAgentPresenceChange?.(false);
  }, [agent.activeTurn, onAgentPresenceChange]);
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
      const waitingForGraph = selected.focus.node_ids.length > 0
        || selected.focus.edge_ids.length > 0
        || selected.focus.group_ids.length > 0;
      if (waitingForGraph) return;
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
    const request = workflowRunRequestQuery.data;
    if (!request) return;
    if (request.status === "succeeded" || request.status === "failed" || request.status === "cancelled" || request.workflow_run_status === "unknown") {
      void queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
      void queryClient.invalidateQueries({ queryKey: ["graph-runs", productId, request.workflow_id] });
      void queryClient.invalidateQueries({ queryKey: ["agent-tasks"] });
    }
  }, [productId, queryClient, workflowRunRequestQuery.data?.status, workflowRunRequestQuery.data?.workflow_id, workflowRunRequestQuery.data?.workflow_run_status]);

  const selectorTarget: ImageExplorerSelectionTarget = {
    selectedAssets: chrome.composerAssets,
    maxSelected: AGENT_COMPOSER_MAX_ASSETS,
    confirmLabel: t("agentWorkbench.attachSelected"),
    selectionLabel: (count, maximum) =>
      t("agentWorkbench.assetsSelected", { count, maximum }),
    limitMessage: t("agentWorkbench.assetLimit", { maximum: AGENT_COMPOSER_MAX_ASSETS }),
    onConfirm: (assets) => {
      chrome.setComposerAssets(assets);
      chrome.rotateComposerKey();
      chrome.setAssetPickerOpen(false);
    },
  };
  const listError = errorDetailOrNull(agent.turnsQuery.error);
  const controlError =
    errorDetailOrNull(agent.cancelTurnMutation.error) ??
    errorDetailOrNull(agent.resumeTurnMutation.error) ??
    (!chrome.activeQuestion ? errorDetailOrNull(agent.answerQuestionMutation.error) : null) ??
    events.streamError ??
    chrome.previewError;
  const workflowRunRequestError = errorDetailOrNull(workflowRunRequestQuery.error)
    ?? errorDetailOrNull(confirmWorkflowRunRequestMutation.error)
    ?? errorDetailOrNull(cancelWorkflowRunRequestMutation.error);
  const composerError =
    errorDetailOrNull(agent.submitTurnMutation.error) ?? errorDetailOrNull(uploadAssetsMutation.error);
  const pendingRequest = mergeWorkflowRunRequest(
    workflowRunRequestQuery.data,
    workflowRequestFromTurn(agent.latestTurn, eventStates),
  );
  const connectionLabel = events.state.terminal_kind
    ? t("agentWorkbench.connection.syncing")
    : events.connectionState === "open"
      ? t("agentWorkbench.connection.open")
      : events.connectionState === "reconnecting"
        ? t("agentWorkbench.connection.reconnecting")
        : t("agentWorkbench.connection.connecting");

  const dialogs = (
    <>
      {chrome.assetPickerOpen ? (
        <Dialog open onOpenChange={(open) => { if (!open) chrome.setAssetPickerOpen(false); }}>
          <DialogContent
            title={t("agentWorkbench.assetSelector")}
            description={productName}
            size="xl"
            className="flex h-[min(780px,calc(100dvh-1rem))] max-w-6xl flex-col"
            bodyClassName="min-h-0 flex-1 overflow-y-auto p-3 sm:p-4"
            closeLabel={t("agentWorkbench.closeAssetSelector")}
          >
            <ProductImageExplorer
              productId={productId}
              productName={productName}
              onPreviewImage={chrome.setPreview}
              selectionTarget={selectorTarget}
            />
          </DialogContent>
        </Dialog>
      ) : null}
      {chrome.preview ? (
        <GalleryImagePreviewDialog
          ariaLabel={t("agentWorkbench.previewAsset", { name: chrome.preview.alt })}
          imageUrl={chrome.preview.previewUrl}
          imageAlt={chrome.preview.alt}
          title={chrome.preview.alt}
          subtitle={chrome.preview.filename}
          body={chrome.preview.filename}
          providerNotesTitle={t("agentWorkbench.assetDetails")}
          downloadUrl={chrome.preview.downloadUrl}
          downloadLabel={t("agentWorkbench.downloadAsset")}
          closeLabel={t("agentWorkbench.closePreview")}
          onClose={() => chrome.setPreview(null)}
        />
      ) : null}
    </>
  );

  return (
    <ConversationWorkbench
      variant="product"
      className={className}
      header={(
        <header className="flex min-h-14 shrink-0 items-center gap-3 border-b border-border-l1 bg-surface-raised/90 px-4 py-2.5 backdrop-blur">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent">
            <Bot size={18} />
          </span>
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-sm font-semibold text-text-primary">{t("agentWorkbench.agent")}</h2>
            <p className="truncate text-xs text-text-secondary">{productName}</p>
          </div>
          {onExpandGlobalAgent ? (
            <IconButton
              label={t("agentWorkbench.expandGlobalAgent")}
              variant="secondary"
              size="toolbar"
              onClick={onExpandGlobalAgent}
            >
              <Maximize2 size={15} />
            </IconButton>
          ) : null}
          {agent.activeTurn ? (
            <span
              role="status"
              aria-label={connectionLabel}
              className={`h-2.5 w-2.5 shrink-0 rounded-full ${events.state.terminal_kind
                ? "animate-pulse bg-accent"
                : events.connectionState === "open"
                  ? "bg-state-success"
                  : events.connectionState === "reconnecting"
                    ? "animate-pulse bg-state-warning"
                    : "animate-pulse bg-text-muted"
                }`}
            />
          ) : null}
        </header>
      )}
      top={(
        <>
          <AgentSessionSwitcher conversation={conversation} productName={productName} />
          <AgentGoalLoopBar
            productId={productId}
            conversation={conversation}
            taskId={taskId ?? null}
          />
        </>
      )}
      notices={(
        <>
          {listError ? (
            <PanelError
              message={listError}
              action={t("agentWorkbench.retry")}
              onAction={() => void agent.turnsQuery.refetch()}
            />
          ) : null}
          {controlError ? <PanelError message={controlError} /> : null}
        </>
      )}
      approval={(
        <AgentWorkflowRunRequestCard
          request={pendingRequest}
          loading={workflowRunRequestQuery.isLoading}
          busy={confirmWorkflowRunRequestMutation.isPending || cancelWorkflowRunRequestMutation.isPending}
          error={workflowRunRequestError}
          onConfirm={() => confirmWorkflowRunRequestMutation.mutate()}
          onCancel={() => cancelWorkflowRunRequestMutation.mutate()}
          onOpenRuns={onOpenRuns}
        />
      )}
      chrome={chrome}
      agent={agent}
      eventStates={eventStates}
      onCanvasFocus={onCanvasFocus}
      showOlder
      composerPlaceholder={
        agent.turns.length === 0 && graphHasCreateTemplate(graph)
          ? t("agentWorkbench.composerPlaceholder.intakeLanded")
          : agent.latestTurn?.status === "awaiting_confirmation"
            ? t("agentWorkbench.composer.blockedConfirmation")
            : undefined
      }
      composerError={composerError}
      onOpenAssets={() => chrome.setAssetPickerOpen(true)}
      onUploadFiles={(files) => uploadAssetsMutation.mutate(files)}
      isUploading={uploadAssetsMutation.isPending}
      dialogs={dialogs}
    />
  );
}

function graphHasCreateTemplate(graph: GraphProjection | null | undefined): boolean {
  return Boolean(
    graph?.nodes.some(
      (node) => node.node_type === "image_generation" || node.node_type === "image_prompt",
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

export {
  agentConversationSubmitTaskId,
  canSubmitAgentConversationMessage,
  resolveAgentCanvasFocusNodeIds,
};

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
      {error ? <p className="mt-1.5 text-[11px] text-state-error">{error}</p> : null}
    </div>
  );
}
