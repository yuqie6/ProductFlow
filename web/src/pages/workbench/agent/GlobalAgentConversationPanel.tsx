import { Bot } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";

import { GalleryImagePreviewDialog } from "../../../components/GalleryImagePreviewDialog";
import { ApiError, api } from "../../../lib/api";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type {
  AgentAttachment,
  AgentPageContextSnapshotInput,
  AgentQuestionAnswer,
  AgentTaskStatus,
  AgentTurn,
  MediaLibraryAsset,
} from "../../../lib/types";
import { AgentComposer, AGENT_COMPOSER_MAX_ASSETS } from "./AgentComposer";
import { AgentMediaLibraryPicker } from "./AgentMediaLibraryPicker";
import { AgentMessageList } from "./AgentMessageList";
import { AgentQuestionPrompt } from "./AgentQuestionPrompt";
import { AgentWorkflowRunRequestCard } from "./AgentWorkflowRunRequestCard";
import { agentTurnRetrySubmitInput, canRetryAgentTurn, retryIdempotencyKey } from "./agentTurnRetry";
import { GlobalLibraryOrganizationDraftCard } from "./GlobalLibraryOrganizationDraftCard";
import { useGlobalAgentConversation } from "./useGlobalAgentConversation";
import { useAgentTurnEvents } from "./useAgentTurnEvents";

interface GlobalAgentConversationPanelProps {
  conversationId: string | null;
  sessionTitle: string;
  taskTitle?: string | null;
  taskId?: string | null;
  taskGoal?: string | null;
  taskStatus?: AgentTaskStatus | null;
  pageContext: AgentPageContextSnapshotInput;
}

export function GlobalAgentConversationPanel({
  conversationId,
  sessionTitle,
  taskTitle = null,
  taskId = null,
  taskGoal = null,
  taskStatus = null,
  pageContext,
}: GlobalAgentConversationPanelProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [composerText, setComposerText] = useState("");
  const [composerAssets, setComposerAssets] = useState<MediaLibraryAsset[]>([]);
  const [assetPickerOpen, setAssetPickerOpen] = useState(false);
  const [preview, setPreview] = useState<DownloadableImage | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [answeredQuestionId, setAnsweredQuestionId] = useState<string | null>(null);
  const composerKeyRef = useRef(globalThis.crypto.randomUUID());
  const retryKeysRef = useRef(new Map<string, string>());
  const [retryingTurnId, setRetryingTurnId] = useState<string | null>(null);
  const confirmationKeyRef = useRef<{ draftId: string; version: number; key: string } | null>(null);
  const agent = useGlobalAgentConversation({
    conversationId: conversationId ?? "",
    taskId,
    taskGoal,
    taskStatus,
    pageContext,
    enabled: Boolean(conversationId),
  });
  const events = useAgentTurnEvents({
    getEventsUrl: (turnId, after) => api.getGlobalAgentTurnEventsUrl(conversationId ?? "", turnId, after),
    runId: agent.activeTurn?.harness_run_id ?? null,
    turn: agent.activeTurn,
    enabled: Boolean(conversationId),
    onTerminal: () => void agent.refreshLatestTurn(),
  });
  const activeQuestion =
    events.state.turn_key === agent.activeTurn?.id && events.state.question
      ? events.state.question
      : agent.activeTurn?.question ?? null;

  useEffect(() => {
    setComposerText("");
    setComposerAssets([]);
    setAssetPickerOpen(false);
    setPreview(null);
    setPreviewError(null);
    setAnsweredQuestionId(null);
    setRetryingTurnId(null);
    composerKeyRef.current = globalThis.crypto.randomUUID();
    retryKeysRef.current = new Map();
    confirmationKeyRef.current = null;
  }, [conversationId, taskId]);
  useEffect(() => setAnsweredQuestionId(null), [activeQuestion?.id]);
  useEffect(() => {
    if (!assetPickerOpen && !preview) {
      return;
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        if (preview) {
          setPreview(null);
        } else {
          setAssetPickerOpen(false);
        }
      }
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [assetPickerOpen, preview]);

  const rotateComposerKey = () => {
    composerKeyRef.current = globalThis.crypto.randomUUID();
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
      previewSelectedAsset(await api.getMediaLibraryAsset(assetId));
    } catch (error) {
      setPreviewError(errorDetail(error, t("agentWorkbench.previewFailed")));
    }
  };

  const canSubmit = Boolean(conversationId && composerText.trim() && !agent.activeTurn);
  const submit = async () => {
    const input = composerText.trim();
    if (!input || !canSubmit || agent.submitTurnMutation.isPending) {
      return;
    }
    try {
      await agent.submitTurnMutation.mutateAsync({
        input_text: input,
        asset_ids: composerAssets.map((asset) => asset.id),
        idempotency_key: composerKeyRef.current,
        task_id: taskId,
        page_context: {
          ...pageContext,
          selected_asset_ids: composerAssets.map((asset) => asset.id),
          captured_at: new Date().toISOString(),
        },
      });
      setComposerText("");
      setComposerAssets([]);
      rotateComposerKey();
    } catch {
      // 保留文本和幂等键，失败请求可以安全重试
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
      // mutation 界面仍可通过同一答案与续跑键重试
    }
  };
  const retryTurn = async (turn: AgentTurn) => {
    if (
      !conversationId ||
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
          taskId: turn.task_id ?? taskId,
          pageContext: {
            ...pageContext,
            selected_asset_ids: turn.input_asset_ids,
            captured_at: new Date().toISOString(),
          },
        }),
      );
      retryKeysRef.current.delete(turn.id);
    } catch {
      // 失败请求沿用同一续跑键
    } finally {
      setRetryingTurnId(null);
    }
  };
  const error = errorDetail(
    agent.turnsQuery.error ??
    agent.submitTurnMutation.error ??
    agent.cancelTurnMutation.error ??
    agent.resumeTurnMutation.error ??
    agent.answerQuestionMutation.error ??
    agent.workflowRunRequestQuery.error ??
    agent.confirmWorkflowRunRequestMutation.error ??
    agent.cancelWorkflowRunRequestMutation.error ??
    events.streamError ??
    previewError,
    t("globalAgent.requestFailed"),
  );
  const questionAnswered = Boolean(
    activeQuestion && (answeredQuestionId === activeQuestion.id || agent.activeTurn?.resume_required),
  );
  const confirmDraft = () => {
    const draft = agent.libraryOrganizationDraftQuery.data;
    const revision = draft?.current_revision;
    if (!draft || !revision || draft.status !== "awaiting_confirmation") {
      return;
    }
    const cached = confirmationKeyRef.current;
    const key =
      cached?.draftId === draft.id && cached.version === revision.version
        ? cached.key
        : globalThis.crypto.randomUUID();
    confirmationKeyRef.current = { draftId: draft.id, version: revision.version, key };
    agent.confirmLibraryOrganizationDraftMutation.mutate({
      expectedDraftVersion: revision.version,
      idempotencyKey: key,
    });
  };
  const confirmWorkflowRunRequest = () => {
    const request = agent.workflowRunRequestQuery.data;
    if (request) {
      agent.confirmWorkflowRunRequestMutation.mutate(request.id);
    }
  };
  const cancelWorkflowRunRequest = () => {
    const request = agent.workflowRunRequestQuery.data;
    if (request) {
      agent.cancelWorkflowRunRequestMutation.mutate(request.id);
    }
  };
  const renderTurnExtras = (turn: AgentTurn) => (
    <>
      {turn.library_organization_draft_revision_id &&
        turn.library_organization_draft_revision_id ===
        agent.libraryOrganizationDraftQuery.data?.current_revision?.id ? (
        <GlobalLibraryOrganizationDraftCard
          draft={agent.libraryOrganizationDraftQuery.data ?? null}
          loading={agent.libraryOrganizationDraftQuery.isLoading}
          error={errorDetail(
            agent.libraryOrganizationDraftQuery.error ??
            agent.confirmLibraryOrganizationDraftMutation.error,
            t("globalAgent.draft.loadFailed"),
          )}
          busy={agent.confirmLibraryOrganizationDraftMutation.isPending}
          onConfirm={confirmDraft}
        />
      ) : null}
    </>
  );

  const dialogs = (
    <>
      {assetPickerOpen ? (
        <AgentMediaLibraryPicker
          selectedAssets={composerAssets}
          onClose={() => setAssetPickerOpen(false)}
          onConfirm={(assets) => {
            setComposerAssets(assets);
            rotateComposerKey();
            setAssetPickerOpen(false);
          }}
          onPreview={previewSelectedAsset}
        />
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

  if (!conversationId) {
    return (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-5 text-center text-text-muted">
        <Bot size={24} />
        <p className="text-sm">{t("globalAgent.noGlobalConversation")}</p>
      </div>
    );
  }

  return (
    <section data-global-agent-conversation className="flex min-h-0 flex-1 flex-col bg-surface-base text-text-primary">
      <header className="shrink-0 border-b border-border-l1 bg-surface-raised/90 px-4 py-3 backdrop-blur">
        <div className="flex min-w-0 items-center gap-3">
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent">
            <Bot size={16} aria-hidden="true" />
          </span>
          <div className="min-w-0 flex-1">
            <h3 className="truncate text-sm font-semibold">{taskTitle ?? sessionTitle}</h3>
            <p className="truncate text-[11px] text-text-secondary" title={taskGoal ?? undefined}>
              {taskGoal ? `${t("globalAgent.taskContext")}: ${taskGoal}` : t("globalAgent.globalScope")}
            </p>
          </div>
          <span
            role="status"
            aria-label={agent.activeTurn ? t("agentWorkbench.connection.open") : t("agentWorkbench.status.succeeded")}
            className={`h-2.5 w-2.5 shrink-0 rounded-full ${agent.activeTurn ? "animate-pulse bg-accent" : "bg-state-success"}`}
          />
        </div>
        <div className="mt-2 flex min-w-0 items-center gap-1.5 text-[11px] text-text-muted">
          <span className="shrink-0">{t("globalAgent.currentPage")}</span>
          <span className="truncate" title={pageContext.route}>{pageContext.route}</span>
        </div>
      </header>

      {error ? <p role="alert" className="shrink-0 border-b border-state-error/20 bg-state-error/10 px-4 py-2 text-xs leading-5 text-state-error">{error}</p> : null}

      <AgentMessageList
        turns={agent.turns}
        activeTurnId={agent.activeTurn?.id ?? null}
        eventState={events.state}
        initialTurnPending={agent.turnsQuery.isLoading}
        onPreviewAsset={(assetId) => void previewTurnAsset(assetId)}
        getAssetThumbnailUrl={(assetId) => api.getMediaLibraryAssetMediaUrl(assetId, "thumbnail")}
        onRetryTurn={(turn) => void retryTurn(turn)}
        retryingTurnId={retryingTurnId}
        renderTurnExtras={renderTurnExtras}
        emptyLabel={t("globalAgent.emptyChat")}
      />

      <AgentWorkflowRunRequestCard
        request={agent.workflowRunRequestQuery.data ?? null}
        loading={agent.workflowRunRequestQuery.isLoading}
        busy={
          agent.confirmWorkflowRunRequestMutation.isPending ||
          agent.cancelWorkflowRunRequestMutation.isPending
        }
        error={errorDetail(
          agent.workflowRunRequestQuery.error ??
          agent.confirmWorkflowRunRequestMutation.error ??
          agent.cancelWorkflowRunRequestMutation.error,
          t("globalAgent.requestFailed"),
        )}
        targetLabel={
          agent.workflowRunRequestQuery.data?.product_name ??
          agent.workflowRunRequestQuery.data?.product_id
        }
        onConfirm={confirmWorkflowRunRequest}
        onCancel={cancelWorkflowRunRequest}
        onOpenRuns={() => {
          const request = agent.workflowRunRequestQuery.data;
          if (request) {
            navigate(`/products/${encodeURIComponent(request.product_id)}`);
          }
        }}
      />

      {activeQuestion && agent.activeTurn ? (
        <AgentQuestionPrompt
          question={activeQuestion}
          answered={questionAnswered}
          resumeRequired={Boolean(agent.activeTurn.resume_required)}
          busy={agent.answerQuestionMutation.isPending || agent.resumeTurnMutation.isPending}
          error={errorDetail(agent.answerQuestionMutation.error, t("globalAgent.requestFailed"))}
          onAnswer={(answer) => void answerQuestion(answer)}
          onResume={() => agent.resumeTurnMutation.mutate(agent.activeTurn?.id ?? "")}
        />
      ) : null}

      {!activeQuestion ? (
        <AgentComposer
          value={composerText}
          selectedAssets={composerAssets}
          isSubmitting={agent.submitTurnMutation.isPending}
          canSubmit={canSubmit}
          stopAvailable={Boolean(agent.activeTurn)}
          isStopping={
            agent.cancelTurnMutation.isPending ||
            agent.activeTurn?.status === "cancel_requested" ||
            Boolean(events.state.terminal_kind)
          }
          error={errorDetail(agent.submitTurnMutation.error, t("globalAgent.requestFailed"))}
          placeholder={t("globalAgent.chatPlaceholder")}
          assetPickerLabel={t("globalAgent.attachImage")}
          selectedAssetsCountLabel={t("globalAgent.assetPicker.selected", {
            count: composerAssets.length,
            maximum: AGENT_COMPOSER_MAX_ASSETS,
          })}
          onChange={setComposerText}
          onOpenAssets={() => setAssetPickerOpen(true)}
          onRemoveAsset={(assetId) => {
            setComposerAssets((current) => current.filter((asset) => asset.id !== assetId));
            rotateComposerKey();
          }}
          onPreviewAsset={previewSelectedAsset}
          onSubmit={() => void submit()}
          onStop={() => agent.cancelTurnMutation.mutate(agent.activeTurn?.id ?? "")}
        />
      ) : null}

      {typeof document === "undefined" ? dialogs : createPortal(dialogs, document.body)}
    </section>
  );
}

function errorDetail(error: unknown, fallback: string): string | null {
  if (!error) {
    return null;
  }
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}
