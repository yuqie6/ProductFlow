import { Bot, ChevronUp, Image, Loader2, User } from "lucide-react";
import { useEffect, useMemo, useRef } from "react";

import { api } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import { useI18n } from "../../lib/preferences";
import type { AgentTurn } from "../../lib/types";
import {
  selectAgentAssistantText,
  selectAgentToolSteps,
  type AgentTurnEventState,
} from "./agentEventReducer";
import { AgentToolStepList } from "./AgentToolStepList";
import { AgentTurnTail } from "./AgentTurnTail";
import { toolStepSignature } from "./toolStepSignature";

interface AgentMessageListProps {
  turns: readonly AgentTurn[];
  activeTurnId: string | null;
  eventState: AgentTurnEventState | null;
  initialTurnPending: boolean;
  hasOlder: boolean;
  loadingOlder: boolean;
  reviewDraftRevisionId?: string | null;
  onLoadOlder: () => Promise<unknown>;
  onPreviewAsset: (assetId: string) => void;
  onReviewDraft?: () => void;
}

export function AgentMessageList({
  turns,
  activeTurnId,
  eventState,
  initialTurnPending,
  hasOlder,
  loadingOlder,
  reviewDraftRevisionId = null,
  onLoadOlder,
  onPreviewAsset,
  onReviewDraft,
}: AgentMessageListProps) {
  const { t } = useI18n();
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const nearBottomRef = useRef(true);
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
  }, [latestLiveSignature, turns.length]);

  const loadOlder = async () => {
    const element = scrollRef.current;
    const previousHeight = element?.scrollHeight ?? 0;
    await onLoadOlder();
    if (element) {
      requestAnimationFrame(() => {
        element.scrollTop += element.scrollHeight - previousHeight;
      });
    }
  };

  return (
    <div
      ref={scrollRef}
      data-agent-message-list
      onScroll={(event) => {
        const element = event.currentTarget;
        nearBottomRef.current = element.scrollHeight - element.scrollTop - element.clientHeight < 96;
      }}
      className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-3 py-4 sm:px-5"
    >
      <div className="mx-auto w-full max-w-3xl space-y-5">
        {hasOlder ? (
          <button
            type="button"
            onClick={() => void loadOlder()}
            disabled={loadingOlder}
            className="mx-auto flex h-10 items-center gap-2 rounded-md px-3 text-xs font-semibold text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900 disabled:opacity-50 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
          >
            {loadingOlder ? <Loader2 size={14} className="animate-spin" /> : <ChevronUp size={14} />}
            {t("agentWorkbench.loadEarlier")}
          </button>
        ) : null}

        {turns.map((turn) => {
          const active = turn.id === activeTurnId;
          const matchingEventState = eventState?.turn_key === turn.id ? eventState : null;
          const assistantText = selectAgentAssistantText(turn, matchingEventState);
          const toolSteps = selectAgentToolSteps(turn, matchingEventState);
          const waitingForAssistant =
            active && turn.status !== "requires_input" && turn.status !== "awaiting_confirmation";
          const reviewDraft = Boolean(
            reviewDraftRevisionId && turn.workflow_draft_revision_id === reviewDraftRevisionId,
          );
          return (
            <div key={turn.id} data-agent-turn-id={turn.id} className="space-y-3">
              <div className="flex justify-end gap-2.5">
                <div className="min-w-0 max-w-[88%]">
                  {turn.input_asset_ids.length ? (
                    <div className="mb-2 flex flex-wrap justify-end gap-2">
                      {turn.input_asset_ids.map((assetId) => (
                        <button
                          key={assetId}
                          type="button"
                          onClick={() => onPreviewAsset(assetId)}
                          aria-label={t("agentWorkbench.previewTurnAsset")}
                          className="flex h-16 w-16 items-center justify-center overflow-hidden rounded-md border border-zinc-200 bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 dark:border-slate-700 dark:bg-slate-900"
                        >
                          <img
                            src={api.getProductImageAssetMediaUrl(assetId, "thumbnail")}
                            alt=""
                            className="h-full w-full object-cover"
                          />
                        </button>
                      ))}
                    </div>
                  ) : null}
                  <div className="rounded-md bg-blue-600 px-3.5 py-2.5 text-sm leading-6 text-white shadow-sm dark:bg-cyan-400 dark:text-[#071018]">
                    <div className="whitespace-pre-wrap break-words">{turn.input_text}</div>
                  </div>
                  <div className="mt-1 text-right text-[10px] text-zinc-400 dark:text-slate-500">
                    {formatDateTime(turn.created_at, t.locale)}
                  </div>
                </div>
                <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-zinc-200 text-zinc-600 dark:bg-slate-800 dark:text-slate-300">
                  <User size={15} />
                </span>
              </div>

              <div className="flex gap-2.5">
                <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-zinc-950 text-white dark:bg-cyan-400 dark:text-[#071018]">
                  <Bot size={15} />
                </span>
                <div className="min-w-0 max-w-[88%] flex-1">
                  {assistantText || waitingForAssistant ? (
                    <div
                      aria-live={active ? "polite" : undefined}
                      className="border-l-2 border-border-l3 pl-3 text-sm leading-6 text-text-primary"
                    >
                      {assistantText ? (
                        <div className="whitespace-pre-wrap break-words">{assistantText}</div>
                      ) : (
                        <div className="flex h-8 items-center gap-2 text-text-secondary">
                          <Loader2 size={14} className="animate-spin motion-reduce:animate-none" />
                          {t("agentWorkbench.waitingForAgent")}
                        </div>
                      )}
                    </div>
                  ) : null}
                  <AgentToolStepList steps={toolSteps} live={active} />
                  <AgentTurnTail
                    turn={turn}
                    active={active}
                    reviewDraft={reviewDraft}
                    onReviewDraft={onReviewDraft}
                  />
                </div>
              </div>
            </div>
          );
        })}

        {!turns.length && initialTurnPending ? (
          <div className="flex min-h-36 flex-col items-center justify-center gap-3 text-sm text-zinc-500 dark:text-slate-400">
            <Loader2 size={20} className="animate-spin" />
            {t("agentWorkbench.starting")}
          </div>
        ) : null}
        {!turns.length && !initialTurnPending ? (
          <div className="flex min-h-36 flex-col items-center justify-center gap-2 text-center text-sm text-zinc-500 dark:text-slate-400">
            <Image size={20} />
            {t("agentWorkbench.emptyConversation")}
          </div>
        ) : null}
      </div>
    </div>
  );
}
