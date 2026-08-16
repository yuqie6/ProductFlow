import { useQueryClient } from "@tanstack/react-query";
import { Bot, CircleAlert, ListChecks, Loader2, Play, RotateCw, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { GalleryImagePreviewDialog } from "../../components/GalleryImagePreviewDialog";
import { api, ApiError } from "../../lib/api";
import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type {
  AgentConversation,
  AgentQuestionAnswer,
  AgentTurn,
  GalleryAsset,
  WorkflowDraft,
} from "../../lib/types";
import {
  ProductImageExplorer,
  type ImageExplorerSelectionTarget,
} from "../product-detail/image-explorer/ProductImageExplorer";
import { AgentComposer } from "./AgentComposer";
import { AgentMessageList } from "./AgentMessageList";
import { AgentQuestionPrompt } from "./AgentQuestionPrompt";
import { AgentResumeAfterAnswerError, useAgentConversation } from "./useAgentConversation";
import { useAgentTurnEvents } from "./useAgentTurnEvents";

const AGENT_COMPOSER_MAX_ASSETS = 6;

interface AgentConversationPanelProps {
  productId: string;
  productName: string;
  conversation: AgentConversation;
  workflowDraft: WorkflowDraft;
  className?: string;
  reviewDraftAvailable?: boolean;
  onReviewDraft?: () => void;
}

export function AgentConversationPanel({
  productId,
  productName,
  conversation,
  workflowDraft,
  className = "",
  reviewDraftAvailable = false,
  onReviewDraft,
}: AgentConversationPanelProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const agent = useAgentConversation({ productId, conversation, workflowDraft });
  const [composerText, setComposerText] = useState("");
  const [composerAssets, setComposerAssets] = useState<GalleryAsset[]>([]);
  const [assetSelectorOpen, setAssetSelectorOpen] = useState(false);
  const [preview, setPreview] = useState<DownloadableImage | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [answeredQuestionId, setAnsweredQuestionId] = useState<string | null>(null);
  const composerKeyRef = useRef(globalThis.crypto.randomUUID());

  const events = useAgentTurnEvents({
    productId,
    conversation,
    turn: agent.activeTurn,
    onTerminal: () => void agent.refreshLatestTurn(),
  });
  const activeQuestion =
    events.state.turn_key === agent.activeTurn?.id && events.state.question
      ? events.state.question
      : agent.activeTurn?.question ?? null;

  useEffect(() => setAnsweredQuestionId(null), [activeQuestion?.id]);
  useEffect(() => {
    if (!hasUnsyncedWorkflowDraftRevision(workflowDraft, agent.latestTurn)) {
      return;
    }
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: ["agent-workbench", productId] }),
      queryClient.invalidateQueries({ queryKey: ["workflow-draft", productId, workflowDraft.id] }),
    ]);
  }, [agent.latestTurn?.workflow_draft_revision_id, productId, queryClient, workflowDraft]);
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
  const canSubmitMessage = Boolean(!agent.activeTurn && agent.turns.length > 0);
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
      });
      setComposerText("");
      setComposerAssets([]);
      rotateComposerKey();
    } catch {
      // Mutation state renders the error while preserving the exact draft and key.
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
    } catch (error) {
      if (error instanceof AgentResumeAfterAnswerError) {
        setAnsweredQuestionId(activeQuestion.id);
      }
    }
  };
  const previewSelectedAsset = (asset: GalleryAsset) => {
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
  const composerError = errorDetailOrNull(agent.submitTurnMutation.error);
  const initialError = !agent.turns.length
    ? errorDetailOrNull(agent.initialTurnMutation.error)
    : null;
  const listError = errorDetailOrNull(agent.turnsQuery.error);
  const controlError =
    errorDetailOrNull(agent.cancelTurnMutation.error) ??
    errorDetailOrNull(agent.resumeTurnMutation.error) ??
    (!activeQuestion ? questionError : null) ??
    events.streamError ??
    previewError;
  const reviewDraftRevisionId = reviewDraftAvailable
    ? workflowDraft.current_revision?.id ?? null
    : null;
  const reviewDraftTurnAvailable = Boolean(
    reviewDraftRevisionId && agent.turns.some(
      (turn) => turn.workflow_draft_revision_id === reviewDraftRevisionId,
    ),
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
      className={`flex min-h-0 flex-col overflow-hidden bg-[#f7f8fa] text-zinc-900 dark:bg-[#070b11] dark:text-slate-100 ${className}`}
    >
      <header className="flex min-h-16 shrink-0 items-center gap-3 border-b border-zinc-200 bg-white px-4 py-3 dark:border-slate-800 dark:bg-[#090d13]">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-zinc-950 text-white dark:bg-cyan-400 dark:text-[#071018]">
          <Bot size={18} />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-sm font-semibold text-zinc-950 dark:text-white">{t("agentWorkbench.agent")}</h2>
          <p className="truncate text-xs text-zinc-500 dark:text-slate-400">{productName}</p>
        </div>
        {reviewDraftAvailable && !reviewDraftTurnAvailable && onReviewDraft ? (
          <button
            type="button"
            onClick={onReviewDraft}
            aria-label={t("agentWorkbench.reviewDraft")}
            title={t("agentWorkbench.reviewDraft")}
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-blue-200 bg-blue-50 text-blue-700 hover:border-blue-400 hover:bg-blue-100 dark:border-cyan-400/25 dark:bg-cyan-400/10 dark:text-cyan-200 dark:hover:border-cyan-400/50"
          >
            <ListChecks size={16} />
          </button>
        ) : null}
        {agent.activeTurn ? (
          <>
            <span
              role="status"
              aria-label={connectionLabel}
              title={connectionLabel}
              className={`h-2.5 w-2.5 shrink-0 rounded-full ${
                events.state.terminal_kind
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

      {listError ? (
        <PanelError
          message={listError}
          action={t("agentWorkbench.retry")}
          onAction={() => void agent.turnsQuery.refetch()}
        />
      ) : null}
      {initialError ? (
        <PanelError
          message={initialError}
          action={t("agentWorkbench.retryStart")}
          onAction={agent.retryInitialTurn}
        />
      ) : null}
      {controlError ? <PanelError message={controlError} /> : null}

      <AgentMessageList
        turns={agent.turns}
        activeTurnId={agent.activeTurn?.id ?? null}
        eventState={events.state}
        initialTurnPending={agent.initialTurnMutation.isPending || agent.turnsQuery.isLoading}
        hasOlder={Boolean(agent.turnsQuery.hasNextPage)}
        loadingOlder={agent.turnsQuery.isFetchingNextPage}
        reviewDraftRevisionId={reviewDraftRevisionId}
        onLoadOlder={() => agent.turnsQuery.fetchNextPage()}
        onPreviewAsset={(assetId) => void previewTurnAsset(assetId)}
        onReviewDraft={onReviewDraft}
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
        onChange={(value) => {
          setComposerText(value);
          rotateComposerKey();
        }}
        onOpenAssets={() => setAssetSelectorOpen(true)}
        onRemoveAsset={(assetId) => {
          setComposerAssets((current) => current.filter((asset) => asset.id !== assetId));
          rotateComposerKey();
        }}
        onPreviewAsset={previewSelectedAsset}
        onSubmit={() => void submitMessage()}
        onStop={() => agent.cancelTurnMutation.mutate(agent.activeTurn?.id ?? "")}
      />

      {typeof document === "undefined" ? dialogs : createPortal(dialogs, document.body)}
    </section>
  );
}

export function hasUnsyncedWorkflowDraftRevision(
  workflowDraft: WorkflowDraft,
  latestTurn: AgentTurn | null,
): boolean {
  const projectedRevisionId = latestTurn?.workflow_draft_revision_id;
  return Boolean(
    projectedRevisionId && workflowDraft.current_revision?.id !== projectedRevisionId,
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
