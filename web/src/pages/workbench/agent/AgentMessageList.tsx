import { Check, ChevronDown, ChevronUp, Copy, Loader2, MessagesSquare, RotateCw } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { api } from "../../../lib/api";
import { formatDateTime } from "../../../lib/format";
import { useI18n } from "../../../lib/preferences";
import type { AgentTurn } from "../../../lib/types";
import {
  selectAgentAssistantText,
  selectAgentToolSteps,
  type AgentTurnEventState,
} from "./agentEventReducer";
import { AgentAssistantMarkdown } from "./AgentAssistantMarkdown";
import { AgentToolStepList } from "./AgentToolStepList";
import { AgentTurnTail } from "./AgentTurnTail";
import { canRetryAgentTurn, groupAgentTurnAttempts } from "./agentTurnRetry";
import { toolStepSignature } from "./toolStepSignature";

interface AgentMessageListProps {
  turns: readonly AgentTurn[];
  activeTurnId: string | null;
  eventState: AgentTurnEventState | null;
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
  renderTurnExtras?: (turn: AgentTurn) => ReactNode;
  emptyLabel?: string;
}

export function AgentMessageList({
  turns,
  activeTurnId,
  eventState,
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
  renderTurnExtras,
  emptyLabel,
}: AgentMessageListProps) {
  const { t } = useI18n();
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const nearBottomRef = useRef(true);
  const [atLatest, setAtLatest] = useState(true);
  const groups = useMemo(() => groupAgentTurnAttempts(turns), [turns]);
  const latestLiveSignature = useMemo(() => {
    const eventTurnId = activeTurnId ?? eventState?.turn_key ?? null;
    const eventTurn = turns.find((turn) => turn.id === eventTurnId);
    if (!eventTurn) {
      return "";
    }
    const text = selectAgentAssistantText(eventTurn, eventState);
    const tools = toolStepSignature(selectAgentToolSteps(eventTurn, eventState));
    return `${text}\u0000${tools}`;
  }, [activeTurnId, eventState, turns]);

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
          const matchingEventState = eventState?.turn_key === latest.id ? eventState : null;
          const assistantText = hideFailedTail ? "" : selectAgentAssistantText(latest, matchingEventState);
          const toolSteps = hideFailedTail ? [] : selectAgentToolSteps(latest, matchingEventState);
          const waitingForAssistant =
            (active && latest.status !== "requires_input" && latest.status !== "awaiting_confirmation") ||
            hideFailedTail;
          const reviewDraft = Boolean(
            reviewDraftRevisionId && latest.workflow_draft_revision_id === reviewDraftRevisionId,
          );
          const canPreviewAssets = Boolean(onPreviewAsset);
          const canRetry = Boolean(onRetryTurn && canRetryAgentTurn({ turn: latest }));
          const retryBusy = Boolean(activeTurnId) || retryPending;
          const showAssistantActions = canRetry || Boolean(assistantText);

          return (
            <article key={root.id} data-agent-turn-id={latest.id} className="group/turn space-y-5">
              <div className="flex justify-end">
                <div className="min-w-0 max-w-[88%] sm:max-w-[34rem]">
                  {root.input_asset_ids.length && canPreviewAssets ? (
                    <div className="mb-2.5 flex flex-wrap justify-end gap-2">
                      {root.input_asset_ids.map((assetId) => (
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
                  <div className="rounded-[20px] border border-blue-200/80 bg-blue-50 px-4 py-3 text-sm leading-6 text-slate-900 shadow-sm dark:border-blue-400/20 dark:bg-blue-400/10 dark:text-slate-100">
                    <div className="whitespace-pre-wrap break-words">{root.input_text}</div>
                  </div>
                  <div className="mt-1.5 flex min-h-7 items-center justify-end gap-1 text-[11px] text-text-muted">
                    <time dateTime={root.created_at}>{formatDateTime(root.created_at, t.locale)}</time>
                    <CopyAction text={root.input_text} label={t("agentWorkbench.copy")} copiedLabel={t("agentWorkbench.copied")} />
                  </div>
                </div>
              </div>

              <div className="min-w-0">
                {assistantText || waitingForAssistant ? (
                  <div aria-live={active || hideFailedTail ? "polite" : undefined} className="min-w-0 text-[15px] leading-7 text-text-primary">
                    {assistantText ? (
                      <AgentAssistantMarkdown text={assistantText} streaming={active} />
                    ) : (
                      <div className="flex h-8 items-center gap-2 text-sm text-text-secondary">
                        <span className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-accent-soft text-accent">
                          <Loader2 size={13} className="animate-spin motion-reduce:animate-none" />
                        </span>
                        {t("agentWorkbench.waitingForAgent")}
                      </div>
                    )}
                  </div>
                ) : null}
                <AgentToolStepList steps={toolSteps} live={active} />
                {showAssistantActions ? (
                  <div data-agent-message-actions className="mt-2 flex min-h-7 items-center gap-1 text-text-muted">
                    {assistantText ? (
                      <CopyAction text={assistantText} label={t("agentWorkbench.copy")} copiedLabel={t("agentWorkbench.copied")} />
                    ) : null}
                    {canRetry ? (
                      <button
                        type="button"
                        data-agent-turn-retry
                        onClick={() => onRetryTurn?.(latest)}
                        disabled={retryBusy}
                        aria-label={t("agentWorkbench.retryStart")}
                        title={t("agentWorkbench.retryStart")}
                        className="inline-flex h-7 w-7 items-center justify-center rounded-md text-text-muted transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
                      >
                        {retryPending ? (
                          <Loader2 size={14} className="animate-spin motion-reduce:animate-none" />
                        ) : (
                          <RotateCw size={14} />
                        )}
                      </button>
                    ) : null}
                  </div>
                ) : null}
                {hideFailedTail ? null : (
                  <AgentTurnTail
                    turn={latest}
                    active={active}
                    reviewDraft={reviewDraft}
                    onReviewDraft={onReviewDraft}
                  />
                )}
                {hideFailedTail ? null : renderTurnExtras?.(latest)}
              </div>
            </article>
          );
        })}

        {!turns.length && initialTurnPending ? (
          <div className="flex min-h-36 flex-col items-center justify-center gap-3 text-sm text-text-secondary">
            <span className="inline-flex h-10 w-10 items-center justify-center rounded-full bg-accent-soft text-accent">
              <Loader2 size={18} className="animate-spin motion-reduce:animate-none" />
            </span>
            {t("agentWorkbench.starting")}
          </div>
        ) : null}
        {!turns.length && !initialTurnPending ? (
          <div className="flex min-h-36 flex-col items-center justify-center gap-2 text-center text-sm text-text-secondary">
            <MessagesSquare size={22} className="text-text-muted" />
            {emptyLabel ?? t("agentWorkbench.emptyConversation")}
          </div>
        ) : null}
      </div>

      {!atLatest && turns.length ? (
        <button
          type="button"
          onClick={scrollToLatest}
          aria-label={t("agentWorkbench.scrollToLatest")}
          title={t("agentWorkbench.scrollToLatest")}
          className="sticky bottom-3 ml-auto mt-3 flex h-9 w-9 items-center justify-center rounded-full border border-border-l2 bg-surface-raised text-text-secondary shadow-lg transition-colors hover:border-accent/50 hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <ChevronDown size={17} />
        </button>
      ) : null}
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
    <button
      type="button"
      onClick={() => void copy()}
      aria-label={copied ? copiedLabel : label}
      title={copied ? copiedLabel : label}
      className="inline-flex h-7 w-7 items-center justify-center rounded-md text-text-muted transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
    >
      {copied ? <Check size={14} /> : <Copy size={14} />}
    </button>
  );
}
