import { AlertTriangle, Bot, ChevronUp, Image, Loader2, User } from "lucide-react";
import { useEffect, useMemo, useRef } from "react";

import { api } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import type { TranslationKey } from "../../lib/i18n";
import { useI18n } from "../../lib/preferences";
import type { AgentTurn, AgentTurnStatus } from "../../lib/types";
import {
  isAgentTurnTerminal,
  selectAgentAssistantText,
  type AgentTurnEventState,
} from "./agentEventReducer";

const STATUS_KEYS: Record<AgentTurnStatus, TranslationKey> = {
  queued: "agentWorkbench.status.queued",
  running: "agentWorkbench.status.running",
  requires_input: "agentWorkbench.status.requiresInput",
  awaiting_confirmation: "agentWorkbench.status.awaitingConfirmation",
  succeeded: "agentWorkbench.status.succeeded",
  failed: "agentWorkbench.status.failed",
  cancel_requested: "agentWorkbench.status.cancelRequested",
  canceled: "agentWorkbench.status.canceled",
  unknown: "agentWorkbench.status.unknown",
};

interface AgentMessageListProps {
  turns: readonly AgentTurn[];
  activeTurnId: string | null;
  eventState: AgentTurnEventState | null;
  initialTurnPending: boolean;
  hasOlder: boolean;
  loadingOlder: boolean;
  onLoadOlder: () => Promise<unknown>;
  onPreviewAsset: (assetId: string) => void;
}

export function AgentMessageList({
  turns,
  activeTurnId,
  eventState,
  initialTurnPending,
  hasOlder,
  loadingOlder,
  onLoadOlder,
  onPreviewAsset,
}: AgentMessageListProps) {
  const { t } = useI18n();
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const nearBottomRef = useRef(true);
  const latestLiveText = useMemo(() => {
    const active = turns.find((turn) => turn.id === activeTurnId);
    return active ? selectAgentAssistantText(active, eventState) : "";
  }, [activeTurnId, eventState, turns]);

  useEffect(() => {
    const element = scrollRef.current;
    if (element && nearBottomRef.current) {
      element.scrollTop = element.scrollHeight;
    }
  }, [latestLiveText, turns.length]);

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
          const assistantText = selectAgentAssistantText(turn, active ? eventState : null);
          const waitingForAssistant =
            active && turn.status !== "requires_input" && turn.status !== "awaiting_confirmation";
          const showAssistant = Boolean(
            assistantText || waitingForAssistant || turn.error_text || turn.sync_error,
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

              {showAssistant ? (
                <div className="flex gap-2.5">
                  <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-zinc-950 text-white dark:bg-cyan-400 dark:text-[#071018]">
                    <Bot size={15} />
                  </span>
                  <div className="min-w-0 max-w-[88%] flex-1">
                    <div
                      aria-live={active ? "polite" : undefined}
                      className="border-l-2 border-zinc-300 pl-3 text-sm leading-6 text-zinc-800 dark:border-slate-700 dark:text-slate-200"
                    >
                      {assistantText ? (
                        <div className="whitespace-pre-wrap break-words">{assistantText}</div>
                      ) : waitingForAssistant ? (
                        <div className="flex h-8 items-center gap-2 text-zinc-500 dark:text-slate-400">
                          <Loader2 size={14} className="animate-spin" />
                          {t("agentWorkbench.waitingForAgent")}
                        </div>
                      ) : null}
                      {turn.error_text || turn.sync_error ? (
                        <div className="mt-2 flex items-start gap-2 border-l-2 border-red-500 bg-red-50 px-3 py-2 text-xs leading-5 text-red-700 dark:bg-red-500/10 dark:text-red-200">
                          <AlertTriangle size={14} className="mt-0.5 shrink-0" />
                          <span>{turn.error_text ?? turn.sync_error}</span>
                        </div>
                      ) : null}
                    </div>
                    <div className="mt-1 flex items-center gap-1.5 text-[10px] text-zinc-400 dark:text-slate-500">
                      <span>{t(STATUS_KEYS[turn.status])}</span>
                      {active && !isAgentTurnTerminal(turn.status) ? (
                        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-600 dark:bg-cyan-400" />
                      ) : null}
                    </div>
                  </div>
                </div>
              ) : null}
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
