import { Check, ChevronDown, ChevronUp, Copy, Loader2, MessagesSquare, RotateCw } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { IconButton } from "../../../components/ui/icon-button";
import { api } from "../../../lib/api";
import { formatDateTime } from "../../../lib/format";
import { useI18n } from "../../../lib/preferences";
import type { AgentQuestion, AgentTurn } from "../../../lib/types";
import {
  isAgentTurnTerminal,
  selectAgentAssistantText,
  selectAgentToolSteps,
  selectAgentTurnBlocks,
  type AgentTurnEventState,
} from "./agentEventReducer";
import { assembleTurnNodes, echoMatchesTurn, type PendingUserEcho } from "./conversation/types";
import { AgentTurnTail } from "./AgentTurnTail";
import { AgentTurnTimeline } from "./AgentTurnTimeline";
import { canRetryAgentTurn, excludeQuestionContinuationTurns, groupAgentTurnAttempts } from "./agentTurnRetry";
import { toolStepSignature } from "./toolStepSignature";

interface AgentMessageListProps {
  turns: readonly AgentTurn[];
  activeTurnId: string | null;
  eventState?: AgentTurnEventState | null;
  eventStates?: Readonly<Record<string, AgentTurnEventState>>;
  initialTurnPending: boolean;
  hasOlder?: boolean;
  loadingOlder?: boolean;
  reviewDraftRevisionId?: string | null;
  onLoadOlder?: () => Promise<unknown>;
  onPreviewAsset?: (assetId: string) => void;
  getAssetThumbnailUrl?: (assetId: string) => string;
  onReviewDraft?: () => void;
  onRetryTurn?: (turn: AgentTurn) => void;
  retryingTurnId?: string | null;
  pendingEcho?: PendingUserEcho | null;
  renderTurnExtras?: (turn: AgentTurn) => ReactNode;
  emptyLabel?: string;
  onCanvasFocus?: (nodeIds: string[]) => void;
}

export function AgentMessageList({
  turns,
  activeTurnId,
  eventState = null,
  eventStates,
  initialTurnPending,
  hasOlder = false,
  loadingOlder = false,
  reviewDraftRevisionId = null,
  onLoadOlder,
  onPreviewAsset,
  getAssetThumbnailUrl = (assetId) => api.getProductImageAssetMediaUrl(assetId, "thumbnail"),
  onReviewDraft,
  onRetryTurn,
  retryingTurnId = null,
  pendingEcho = null,
  renderTurnExtras,
  emptyLabel,
  onCanvasFocus,
}: AgentMessageListProps) {
  const { t } = useI18n();
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const nearBottomRef = useRef(true);
  const [atLatest, setAtLatest] = useState(true);
  const groups = useMemo(
    () => groupAgentTurnAttempts(excludeQuestionContinuationTurns(turns)),
    [turns],
  );
  const latestLiveSignature = useMemo(() => {
    return groups.map(({ latest }) => {
      const matching = eventStateForTurn(latest.id, eventStates, eventState);
      const text = selectAgentAssistantText(latest, matching);
      const blocks = selectAgentTurnBlocks(latest, matching);
      const thinking = blocks
        .flatMap((block) => (block.type === "thinking" ? [block.text] : []))
        .join("\u0001");
      const tools = toolStepSignature(selectAgentToolSteps(latest, matching));
      return `${latest.id}\u0000${text}\u0000${thinking}\u0000${tools}`;
    }).join("\u0002");
  }, [eventState, eventStates, groups]);

  useEffect(() => {
    const element = scrollRef.current;
    if (element && nearBottomRef.current) {
      element.scrollTop = element.scrollHeight;
    }
  }, [latestLiveSignature, groups.length]);

  const loadOlder = async () => {
    if (!onLoadOlder) {
      return;
    }
    const element = scrollRef.current;
    const previousHeight = element?.scrollHeight ?? 0;
    await onLoadOlder();
    if (element) {
      requestAnimationFrame(() => {
        element.scrollTop += element.scrollHeight - previousHeight;
      });
    }
  };

  const scrollToLatest = () => {
    const element = scrollRef.current;
    if (!element) {
      return;
    }
    element.scrollTo({ top: element.scrollHeight, behavior: "smooth" });
    nearBottomRef.current = true;
    setAtLatest(true);
  };

  return (
    <div
      ref={scrollRef}
      data-agent-message-list
      onScroll={(event) => {
        const element = event.currentTarget;
        const nextAtLatest = element.scrollHeight - element.scrollTop - element.clientHeight < 96;
        nearBottomRef.current = nextAtLatest;
        setAtLatest((current) => (current === nextAtLatest ? current : nextAtLatest));
      }}
      className="relative min-h-0 flex-1 overflow-y-auto overscroll-contain bg-surface-base px-4 py-7 sm:px-6 sm:py-9"
    >
      <div className="mx-auto w-full max-w-[47rem] space-y-10">
        {hasOlder && onLoadOlder ? (
          <button
            type="button"
            onClick={() => void loadOlder()}
            disabled={loadingOlder}
            className="mx-auto flex h-9 items-center gap-2 rounded-full border border-border-l2 bg-surface-raised px-3.5 text-xs font-medium text-text-secondary shadow-sm transition-colors hover:border-accent/40 hover:text-text-primary disabled:cursor-wait disabled:opacity-50"
          >
            {loadingOlder ? <Loader2 size={14} className="animate-spin motion-reduce:animate-none" /> : <ChevronUp size={14} />}
            {t("agentWorkbench.loadEarlier")}
          </button>
        ) : null}

        {groups.map((group) => {
          const { root, attempts, latest } = group;
          const active = latest.id === activeTurnId;
          const retryPending = Boolean(retryingTurnId) && attempts.some((turn) => turn.id === retryingTurnId);
          const hideFailedTail = retryPending && !active;
          const matchingEventState = eventStateForTurn(latest.id, eventStates, eventState);
          const assistantText = hideFailedTail ? "" : selectAgentAssistantText(latest, matchingEventState);
          const toolSteps = hideFailedTail ? [] : selectAgentToolSteps(latest, matchingEventState);
          const blocks = hideFailedTail ? [] : selectAgentTurnBlocks(latest, matchingEventState);
          const nodes = hideFailedTail
            ? []
            : assembleTurnNodes({
              turn: latest,
              eventState: matchingEventState,
              blocks,
              live: active,
            });
          const questionNode = nodes.find((node) => node.type === "question");
          const waitingForAssistant =
            (active && latest.status !== "requires_input" && latest.status !== "awaiting_confirmation") ||
            hideFailedTail;
          const hasThinkingOrText = blocks.some(
            (block) => block.type === "thinking" || (block.type === "text" && block.text.trim()),
          );
          const showWaitingSpinner = waitingForAssistant && !hasThinkingOrText;
          const foldProcess = isAgentTurnTerminal(latest.status) && !hideFailedTail;
          const reviewDraft = Boolean(
            reviewDraftRevisionId && latest.library_organization_draft_revision_id === reviewDraftRevisionId,
          );
          const canPreviewAssets = Boolean(onPreviewAsset);
          const canRetry = Boolean(onRetryTurn && canRetryAgentTurn({ turn: latest }));
          const retryBusy = Boolean(activeTurnId) || retryPending;
          const showAssistantActions = canRetry || Boolean(assistantText);

          return (
            <article key={root.id} data-agent-turn-id={latest.id} className="group/turn space-y-5">
              <div className="flex justify-end" data-agent-conversation-node="user">
                <UserTurnBubble
                  text={root.input_text}
                  assetIds={root.input_asset_ids}
                  createdAt={root.created_at}
                  canPreviewAssets={canPreviewAssets}
                  onPreviewAsset={onPreviewAsset}
                  getAssetThumbnailUrl={getAssetThumbnailUrl}
                />
              </div>

              <div className="min-w-0">
                {blocks.length || showWaitingSpinner ? (
                  <div aria-live={active || hideFailedTail ? "polite" : undefined} className="min-w-0 text-[15px] leading-7 text-text-primary">
                    {blocks.length ? (
                      <AgentTurnTimeline
                        blocks={blocks}
                        toolSteps={toolSteps}
                        live={active}
                        fold={foldProcess}
                        textSettled={Boolean(matchingEventState?.text_settled) || isAgentTurnTerminal(latest.status)}
                        onCanvasFocus={onCanvasFocus}
                      />
                    ) : null}
                    {questionNode && questionNode.type === "question" ? (
                      <TurnQuestionNode question={questionNode.question} />
                    ) : null}
                    {showWaitingSpinner ? (
                      <div className="flex h-8 items-center gap-2 text-sm text-text-secondary">
                        <span className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-accent-soft text-accent">
                          <Loader2 size={13} className="animate-spin motion-reduce:animate-none" />
                        </span>
                        {t("agentWorkbench.waitingForAgent")}
                      </div>
                    ) : null}
                  </div>
                ) : null}
                {showAssistantActions ? (
                  <div data-agent-message-actions className="mt-2 flex min-h-7 items-center gap-1 text-text-muted">
                    {assistantText ? (
                      <CopyAction text={assistantText} label={t("agentWorkbench.copy")} copiedLabel={t("agentWorkbench.copied")} />
                    ) : null}
                    {canRetry ? (
                      <IconButton
                        label={t("agentWorkbench.retryStart")}
                        size="toolbar"
                        data-agent-turn-retry
                        disabled={retryBusy}
                        busy={retryPending}
                        onClick={() => onRetryTurn?.(latest)}
                      >
                        <RotateCw size={14} />
                      </IconButton>
                    ) : null}
                  </div>
                ) : null}
                {hideFailedTail ? null : (
                  <div data-agent-conversation-node="turn-tail">
                    <AgentTurnTail
                      turn={latest}
                      active={active}
                      reviewDraft={reviewDraft}
                      onReviewDraft={onReviewDraft}
                    />
                  </div>
                )}
                {hideFailedTail ? null : renderTurnExtras?.(latest)}
              </div>
            </article>
          );
        })}

        {pendingEcho && !turns.some((item) => echoMatchesTurn(pendingEcho, item)) ? (
          <article data-agent-pending-echo data-agent-conversation-node="user" className="flex justify-end">
            <UserTurnBubble
              text={pendingEcho.text}
              assetIds={pendingEcho.assetIds}
              createdAt={pendingEcho.createdAt}
              canPreviewAssets={Boolean(onPreviewAsset)}
              onPreviewAsset={onPreviewAsset}
              getAssetThumbnailUrl={getAssetThumbnailUrl}
              pending
            />
          </article>
        ) : null}

        {!turns.length && !pendingEcho && initialTurnPending ? (
          <div className="flex min-h-36 flex-col items-center justify-center gap-3 text-sm text-text-secondary">
            <span className="inline-flex h-10 w-10 items-center justify-center rounded-full bg-accent-soft text-accent">
              <Loader2 size={18} className="animate-spin motion-reduce:animate-none" />
            </span>
            {t("agentWorkbench.starting")}
          </div>
        ) : null}
        {!turns.length && !pendingEcho && !initialTurnPending ? (
          <div className="flex min-h-36 flex-col items-center justify-center gap-2 text-center text-sm text-text-secondary">
            <MessagesSquare size={22} className="text-text-muted" />
            {emptyLabel ?? t("agentWorkbench.emptyConversation")}
          </div>
        ) : null}
      </div>

      {!atLatest && (turns.length || pendingEcho) ? (
        <IconButton
          label={t("agentWorkbench.scrollToLatest")}
          variant="secondary"
          size="toolbar"
          className="sticky bottom-3 ml-auto mt-3 rounded-full shadow-lg"
          onClick={scrollToLatest}
        >
          <ChevronDown size={17} />
        </IconButton>
      ) : null}
    </div>
  );
}

function UserTurnBubble({
  text,
  assetIds,
  createdAt,
  canPreviewAssets,
  onPreviewAsset,
  getAssetThumbnailUrl,
  pending = false,
}: {
  text: string;
  assetIds: readonly string[];
  createdAt: string;
  canPreviewAssets: boolean;
  onPreviewAsset?: (assetId: string) => void;
  getAssetThumbnailUrl: (assetId: string) => string;
  pending?: boolean;
}) {
  const { t } = useI18n();
  return (
    <div className="min-w-0 max-w-[88%] sm:max-w-[34rem]">
      {assetIds.length && canPreviewAssets ? (
        <div className="mb-2.5 flex flex-wrap justify-end gap-2">
          {assetIds.map((assetId) => (
            <button
              key={assetId}
              type="button"
              onClick={() => onPreviewAsset?.(assetId)}
              aria-label={t("agentWorkbench.previewTurnAsset")}
              className="h-16 w-16 overflow-hidden rounded-xl border border-border-l2 bg-surface-subtle shadow-sm transition-transform hover:-translate-y-0.5 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            >
              <img
                src={getAssetThumbnailUrl(assetId)}
                alt=""
                className="h-full w-full object-cover"
              />
            </button>
          ))}
        </div>
      ) : null}
      <div className="rounded-[20px] border border-accent/25 bg-accent-soft px-4 py-3 text-sm leading-6 text-text-primary shadow-sm">
        <div className="whitespace-pre-wrap break-words">{text}</div>
      </div>
      <div className="mt-1.5 flex min-h-7 items-center justify-end gap-1 text-[11px] text-text-muted">
        <time dateTime={createdAt}>{formatDateTime(createdAt, t.locale)}</time>
        {pending ? <span>{t("agentWorkbench.starting")}</span> : null}
        <CopyAction text={text} label={t("agentWorkbench.copy")} copiedLabel={t("agentWorkbench.copied")} />
      </div>
    </div>
  );
}

function TurnQuestionNode({ question }: { question: AgentQuestion }) {
  return (
    <div
      data-agent-conversation-node="question"
      data-agent-question-node
      className="rounded-xl border border-border-l2 bg-surface-subtle px-3 py-2.5"
    >
      <div className="text-[11px] font-semibold uppercase tracking-wide text-accent">{question.header}</div>
      <p className="mt-1 text-sm font-medium leading-6 text-text-primary">{question.question}</p>
    </div>
  );
}

function CopyAction({
  text,
  label,
  copiedLabel,
}: {
  text: string;
  label: string;
  copiedLabel: string;
}) {
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => () => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
    }
  }, []);

  const copy = async () => {
    if (!text || typeof navigator === "undefined") {
      return;
    }
    try {
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(text);
      } else {
        const fallback = document.createElement("textarea");
        fallback.value = text;
        fallback.setAttribute("readonly", "");
        fallback.style.position = "fixed";
        fallback.style.opacity = "0";
        document.body.appendChild(fallback);
        fallback.select();
        const copied = document.execCommand("copy");
        fallback.remove();
        if (!copied) {
          throw new Error("Clipboard is unavailable");
        }
      }
      setCopied(true);
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
      timerRef.current = setTimeout(() => setCopied(false), 1_500);
    } catch {
      setCopied(false);
    }
  };

  return (
    <IconButton
      label={copied ? copiedLabel : label}
      size="toolbar"
      onClick={() => void copy()}
    >
      {copied ? <Check size={14} /> : <Copy size={14} />}
    </IconButton>
  );
}

function eventStateForTurn(
  turnId: string,
  eventStates: Readonly<Record<string, AgentTurnEventState>> | undefined,
  eventState: AgentTurnEventState | null,
): AgentTurnEventState | null {
  return eventStates?.[turnId] ?? (eventState?.turn_key === turnId ? eventState : null);
}
