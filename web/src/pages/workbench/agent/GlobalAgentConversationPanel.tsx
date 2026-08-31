import { Bot } from "lucide-react";
import { useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";

import { GalleryImagePreviewDialog } from "../../../components/GalleryImagePreviewDialog";
import { api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type {
  AgentPageContextSnapshotInput,
  AgentTaskStatus,
  AgentTurn,
  MediaLibraryAsset,
} from "../../../lib/types";
import { AGENT_COMPOSER_MAX_ASSETS } from "./AgentComposer";
import { AgentMediaLibraryPicker } from "./AgentMediaLibraryPicker";
import { AgentWorkflowRunRequestCard } from "./AgentWorkflowRunRequestCard";
import { ConversationWorkbench } from "./ConversationPanel";
import {
  errorDetailOrNull,
  mergeWorkflowRunRequest,
  workflowRequestFromTurn,
} from "./conversation/helpers";
import { useConversationChrome } from "./conversation/useConversationChrome";
import { GlobalLibraryOrganizationDraftCard } from "./GlobalLibraryOrganizationDraftCard";
import { useGlobalAgentConversation } from "./useGlobalAgentConversation";
import { useAgentTurnEventMap, useAgentTurnEvents } from "./useAgentTurnEvents";

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
    onArtifactProposed: () => void agent.workflowRunRequestQuery.refetch(),
    onTerminal: () => {
      void agent.refreshLatestTurn();
      void agent.workflowRunRequestQuery.refetch();
    },
  });
  const eventStates = useAgentTurnEventMap({
    getEventsUrl: (turnId, after) => api.getGlobalAgentTurnEventsUrl(conversationId ?? "", turnId, after),
    turns: agent.turns,
    enabled: Boolean(conversationId),
  });
  const chrome = useConversationChrome<MediaLibraryAsset>({
    conversationKey: `${conversationId ?? ""}:${taskId ?? ""}`,
    resetComposerOnKeyChange: true,
    agent,
    events,
    pageContext,
    submitTaskId: taskId,
    loadPreviewAsset: (assetId) => api.getMediaLibraryAsset(assetId),
    previewFailedLabel: t("agentWorkbench.previewFailed"),
  });
  useEffect(() => {
    confirmationKeyRef.current = null;
  }, [conversationId, taskId]);

  const pendingRequest = mergeWorkflowRunRequest(
    agent.workflowRunRequestQuery.data,
    workflowRequestFromTurn(agent.latestTurn, eventStates),
  );
  const confirmWorkflowRunRequest = () => {
    const requestId = mergeWorkflowRunRequest(
      agent.workflowRunRequestQuery.data,
      workflowRequestFromTurn(agent.latestTurn, eventStates),
    )?.id;
    if (requestId) {
      agent.confirmWorkflowRunRequestMutation.mutate(requestId);
    }
  };
  const cancelWorkflowRunRequest = () => {
    const requestId = mergeWorkflowRunRequest(
      agent.workflowRunRequestQuery.data,
      workflowRequestFromTurn(agent.latestTurn, eventStates),
    )?.id;
    if (requestId) {
      agent.cancelWorkflowRunRequestMutation.mutate(requestId);
    }
  };
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
  const renderTurnExtras = (turn: AgentTurn) => (
    <>
      {turn.library_organization_draft_revision_id &&
        turn.library_organization_draft_revision_id ===
        agent.libraryOrganizationDraftQuery.data?.current_revision?.id ? (
        <GlobalLibraryOrganizationDraftCard
          draft={agent.libraryOrganizationDraftQuery.data ?? null}
          loading={agent.libraryOrganizationDraftQuery.isLoading}
          error={errorDetailOrNull(
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
  const error = errorDetailOrNull(
    agent.turnsQuery.error ??
    agent.submitTurnMutation.error ??
    agent.cancelTurnMutation.error ??
    agent.resumeTurnMutation.error ??
    agent.answerQuestionMutation.error ??
    agent.workflowRunRequestQuery.error ??
    agent.confirmWorkflowRunRequestMutation.error ??
    agent.cancelWorkflowRunRequestMutation.error ??
    events.streamError ??
    chrome.previewError,
    t("globalAgent.requestFailed"),
  );

  const dialogs = (
    <>
      {chrome.assetPickerOpen ? (
        <AgentMediaLibraryPicker
          selectedAssets={chrome.composerAssets}
          onClose={() => chrome.setAssetPickerOpen(false)}
          onConfirm={(assets) => {
            chrome.setComposerAssets(assets);
            chrome.rotateComposerKey();
            chrome.setAssetPickerOpen(false);
          }}
          onPreview={chrome.previewSelectedAsset}
        />
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

  if (!conversationId) {
    return (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-5 text-center text-text-muted">
        <Bot size={24} />
        <p className="text-sm">{t("globalAgent.noGlobalConversation")}</p>
      </div>
    );
  }

  return (
    <ConversationWorkbench
      variant="global"
      className="flex-1"
      header={(
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
              aria-label={agent.activeTurn
                ? (events.connectionState === "open"
                  ? t("agentWorkbench.connection.open")
                  : t("agentWorkbench.connection.reconnecting"))
                : t("agentWorkbench.status.succeeded")}
              className={`h-2.5 w-2.5 shrink-0 rounded-full ${agent.activeTurn
                ? events.connectionState === "open"
                  ? "animate-pulse bg-accent"
                  : "animate-pulse bg-state-warning"
                : "bg-state-success"}`}
            />
          </div>
          <div className="mt-2 flex min-w-0 items-center gap-1.5 text-[11px] text-text-muted">
            <span className="shrink-0">{t("globalAgent.currentPage")}</span>
            <span className="truncate" title={pageContext.route}>{pageContext.route}</span>
          </div>
        </header>
      )}
      notices={error ? <p role="alert" className="shrink-0 border-b border-state-error/20 bg-state-error/10 px-4 py-2 text-xs leading-5 text-state-error">{error}</p> : null}
      approval={(
        <AgentWorkflowRunRequestCard
          request={pendingRequest}
          loading={agent.workflowRunRequestQuery.isLoading}
          busy={
            agent.confirmWorkflowRunRequestMutation.isPending ||
            agent.cancelWorkflowRunRequestMutation.isPending
          }
          error={errorDetailOrNull(
            agent.workflowRunRequestQuery.error ??
            agent.confirmWorkflowRunRequestMutation.error ??
            agent.cancelWorkflowRunRequestMutation.error,
            t("globalAgent.requestFailed"),
          )}
          targetLabel={
            pendingRequest?.product_name || pendingRequest?.product_id || null
          }
          onConfirm={confirmWorkflowRunRequest}
          onCancel={cancelWorkflowRunRequest}
          onOpenRuns={() => {
            const request = pendingRequest;
            if (request?.product_id) {
              navigate(`/products/${encodeURIComponent(request.product_id)}`);
            }
          }}
        />
      )}
      chrome={chrome}
      agent={agent}
      eventStates={eventStates}
      renderTurnExtras={renderTurnExtras}
      emptyLabel={t("globalAgent.emptyChat")}
      getAssetThumbnailUrl={(assetId) => api.getMediaLibraryAssetMediaUrl(assetId, "thumbnail")}
      composerPlaceholder={
        agent.activeTurn?.status === "awaiting_confirmation"
          ? t("agentWorkbench.composer.blockedConfirmation")
          : t("globalAgent.chatPlaceholder")
      }
      assetPickerLabel={t("globalAgent.attachImage")}
      selectedAssetsCountLabel={t("globalAgent.assetPicker.selected", {
        count: chrome.composerAssets.length,
        maximum: AGENT_COMPOSER_MAX_ASSETS,
      })}
      composerError={errorDetailOrNull(agent.submitTurnMutation.error, t("globalAgent.requestFailed"))}
      onOpenAssets={() => chrome.setAssetPickerOpen(true)}
      dialogs={dialogs}
    />
  );
}
