import { CircleAlert, Loader2, Play, RotateCw } from "lucide-react";
import { type ReactNode } from "react";
import { createPortal } from "react-dom";

import { useI18n } from "../../../lib/preferences";
import type { AgentAttachment, AgentTurn } from "../../../lib/types";
import type { AgentTurnEventState } from "./agentEventReducer";
import { AgentComposer } from "./AgentComposer";
import { AgentMessageList } from "./AgentMessageList";
import type { ConversationChrome } from "./conversation/useConversationChrome";
import { errorDetailOrNull } from "./conversation/helpers";

interface ConversationPanelProps {
  variant: "product" | "global";
  className?: string;
  children?: ReactNode;
  header?: ReactNode;
  top?: ReactNode;
  notices?: ReactNode;
  messages?: ReactNode;
  approval?: ReactNode;
  extraApproval?: ReactNode;
  resume?: ReactNode;
  composer?: ReactNode;
  dialogs?: ReactNode;
}

/** Product 与 global 对话共享布局；scope 特有内容通过插槽注入。弹层 portal 到 document.body。 */
export function ConversationPanel({
  variant,
  className = "",
  children,
  header,
  top,
  notices,
  messages,
  approval,
  extraApproval,
  resume,
  composer,
  dialogs,
}: ConversationPanelProps) {
  const portal = dialogs
    ? (typeof document === "undefined" ? dialogs : createPortal(dialogs, document.body))
    : null;
  return (
    <section
      data-agent-conversation-panel={variant === "product" ? "" : undefined}
      data-global-agent-conversation={variant === "global" ? "" : undefined}
      className={`flex min-h-0 flex-col overflow-hidden bg-surface-base text-text-primary ${className}`}
    >
      {children !== undefined ? children : (
        <>
          {header}
          {top}
          {notices}
          {messages}
          {approval}
          {extraApproval}
          {resume}
          {composer}
          {portal}
        </>
      )}
    </section>
  );
}

export function PanelError({
  message,
  action,
  onAction,
}: {
  message: string;
  action?: string;
  onAction?: () => void;
}) {
  return (
    <div role="alert" className="flex items-start gap-2 border-b border-state-error/20 bg-state-error/10 px-4 py-2.5 text-xs leading-5 text-state-error">
      <CircleAlert size={15} className="mt-0.5 shrink-0" />
      <span className="min-w-0 flex-1">{message}</span>
      {action && onAction ? (
        <button type="button" onClick={onAction} className="inline-flex h-11 shrink-0 items-center gap-1.5 rounded-md px-2 font-semibold hover:bg-state-error/15 lg:h-8">
          <RotateCw size={13} />
          {action}
        </button>
      ) : null}
    </div>
  );
}

interface ConversationWorkbenchAgent {
  turns: AgentTurn[];
  activeTurn: AgentTurn | null;
  turnsQuery: {
    isLoading: boolean;
    isFetchingNextPage: boolean;
    hasNextPage: boolean;
    fetchNextPage: () => Promise<unknown>;
    error: unknown;
  };
  submitTurnMutation: { isPending: boolean; error: unknown };
  cancelTurnMutation: { isPending: boolean; error: unknown; mutate: (id: string) => void };
  resumeTurnMutation: { isPending: boolean; error: unknown; mutate: (id: string) => void };
  answerQuestionMutation: { isPending: boolean; error: unknown };
}

interface ConversationWorkbenchProps<TAsset extends AgentAttachment> {
  variant: "product" | "global";
  className?: string;
  header: ReactNode;
  top?: ReactNode;
  notices?: ReactNode;
  approval?: ReactNode;
  extraApproval?: ReactNode;
  dialogs?: ReactNode;
  chrome: ConversationChrome<TAsset>;
  agent: ConversationWorkbenchAgent;
  eventStates: Readonly<Record<string, AgentTurnEventState>>;
  onCanvasFocus?: (nodeIds: string[]) => void;
  renderTurnExtras?: (turn: AgentTurn) => ReactNode;
  emptyLabel?: string;
  getAssetThumbnailUrl?: (assetId: string) => string;
  showOlder?: boolean;
  composerPlaceholder?: string;
  showAssetPicker?: boolean;
  assetPickerLabel?: string;
  selectedAssetsCountLabel?: string;
  composerError?: string | null;
  onOpenAssets: () => void;
  onUploadFiles?: (files: File[]) => void;
  isUploading?: boolean;
}

export function ConversationWorkbench<TAsset extends AgentAttachment>({
  variant,
  className = "",
  header,
  top,
  notices,
  approval,
  extraApproval,
  dialogs,
  chrome,
  agent,
  eventStates,
  onCanvasFocus,
  renderTurnExtras,
  emptyLabel,
  getAssetThumbnailUrl,
  showOlder = false,
  composerPlaceholder,
  showAssetPicker,
  assetPickerLabel,
  selectedAssetsCountLabel,
  composerError,
  onOpenAssets,
  onUploadFiles,
  isUploading,
}: ConversationWorkbenchProps<TAsset>) {
  const { t } = useI18n();
  const questionError = errorDetailOrNull(agent.answerQuestionMutation.error);
  return (
    <ConversationPanel
      variant={variant}
      className={className}
      header={header}
      top={top}
      notices={notices}
      messages={(
        <AgentMessageList
          turns={agent.turns}
          activeTurnId={agent.activeTurn?.id ?? null}
          eventStates={eventStates}
          initialTurnPending={agent.turnsQuery.isLoading}
          pendingEcho={chrome.pendingEcho}
          hasOlder={showOlder ? Boolean(agent.turnsQuery.hasNextPage) : undefined}
          loadingOlder={showOlder ? agent.turnsQuery.isFetchingNextPage : undefined}
          onLoadOlder={showOlder ? () => agent.turnsQuery.fetchNextPage() : undefined}
          onPreviewAsset={(assetId) => void chrome.previewTurnAsset(assetId)}
          onRetryTurn={(turn) => void chrome.retryTurn(turn)}
          retryingTurnId={chrome.retryingTurnId}
          onCanvasFocus={onCanvasFocus}
          renderTurnExtras={renderTurnExtras}
          emptyLabel={emptyLabel}
          getAssetThumbnailUrl={getAssetThumbnailUrl}
        />
      )}
      approval={approval}
      extraApproval={extraApproval}
      resume={agent.activeTurn?.resume_required && !chrome.activeQuestion ? (
        <div className="flex items-center gap-3 border-t border-border-l1 bg-accent-soft px-4 py-3">
          <CircleAlert size={16} className="shrink-0 text-accent" />
          <span className="min-w-0 flex-1 text-xs text-text-primary">{t("agentWorkbench.resumeRequired")}</span>
          <button
            type="button"
            onClick={() => agent.resumeTurnMutation.mutate(agent.activeTurn?.id ?? "")}
            disabled={agent.resumeTurnMutation.isPending}
            className="inline-flex h-10 items-center gap-2 rounded-md bg-accent px-3 text-xs font-semibold text-accent-fg transition-colors hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
          >
            {agent.resumeTurnMutation.isPending ? <Loader2 size={14} className="animate-spin" /> : <Play size={14} />}
            {t("agentWorkbench.resume")}
          </button>
        </div>
      ) : null}
      composer={(
        <AgentComposer
          value={chrome.composerText}
          selectedAssets={chrome.composerAssets}
          isSubmitting={agent.submitTurnMutation.isPending}
          canSubmit={chrome.canSubmitMessage}
          stopAvailable={chrome.stopAvailable}
          isStopping={chrome.isStopping}
          error={composerError ?? null}
          placeholder={composerPlaceholder}
          showAssetPicker={showAssetPicker}
          assetPickerLabel={assetPickerLabel}
          selectedAssetsCountLabel={selectedAssetsCountLabel}
          question={chrome.activeQuestion && !chrome.questionAnswered ? chrome.activeQuestion : null}
          questionBusy={agent.answerQuestionMutation.isPending || agent.resumeTurnMutation.isPending}
          questionError={questionError}
          onAnswerQuestion={(answer) => void chrome.answerQuestion(answer)}
          onChange={chrome.setComposerText}
          onOpenAssets={onOpenAssets}
          onRemoveAsset={chrome.removeComposerAsset}
          onPreviewAsset={chrome.previewSelectedAsset}
          onSubmit={() => void chrome.submitMessage()}
          onStop={() => agent.cancelTurnMutation.mutate(agent.activeTurn?.id ?? "")}
          onUploadFiles={onUploadFiles}
          isUploading={isUploading}
        />
      )}
      dialogs={dialogs}
    />
  );
}
