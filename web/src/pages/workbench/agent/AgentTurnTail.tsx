import {
  CircleCheck,
  CircleHelp,
  CircleSlash,
  CircleX,
  ClipboardCheck,
  Clock3,
  ListChecks,
  LoaderCircle,
  MessageCircleQuestion,
} from "lucide-react";
import type { ReactNode } from "react";

import type { TranslationKey } from "../../../lib/i18n";
import { useI18n } from "../../../lib/preferences";
import type { AgentTurn, AgentTurnStatus } from "../../../lib/types";

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

const STATUS_ICON: Record<AgentTurnStatus, ReactNode> = {
  queued: <Clock3 size={14} />,
  running: <LoaderCircle size={14} />,
  requires_input: <MessageCircleQuestion size={14} />,
  awaiting_confirmation: <ClipboardCheck size={14} />,
  succeeded: <CircleCheck size={14} />,
  failed: <CircleX size={14} />,
  cancel_requested: <LoaderCircle size={14} />,
  canceled: <CircleSlash size={14} />,
  unknown: <CircleHelp size={14} />,
};

const STATUS_TONE: Record<AgentTurnStatus, string> = {
  queued: "text-text-muted",
  running: "text-accent",
  requires_input: "text-state-warning",
  awaiting_confirmation: "text-accent",
  succeeded: "text-state-success",
  failed: "text-state-error",
  cancel_requested: "text-state-warning",
  canceled: "text-text-muted",
  unknown: "text-state-warning",
};

interface AgentTurnTailProps {
  turn: AgentTurn;
  active: boolean;
  reviewDraft: boolean;
  onReviewDraft?: () => void;
}

export function shouldShowAgentTurnTail({
  turn,
  reviewDraft,
}: {
  turn: Pick<AgentTurn, "status" | "error_text" | "sync_error">;
  reviewDraft: boolean;
}): boolean {
  if (turn.error_text || turn.sync_error || reviewDraft) {
    return true;
  }
  return turn.status !== "succeeded"
    && turn.status !== "queued"
    && turn.status !== "running"
    && turn.status !== "cancel_requested";
}

export function AgentTurnTail({
  turn,
  active,
  reviewDraft,
  onReviewDraft,
}: AgentTurnTailProps) {
  const { t } = useI18n();
  const error = turn.error_text;
  const syncWarning = turn.sync_error;
  const animate = active && (turn.status === "running" || turn.status === "cancel_requested");
  const terminalReason = turn.status === "unknown" ? turn.terminal_reason_code : undefined;
  const statusLabelKey = terminalReason
    ? (`agentWorkbench.terminal.${terminalReason}` as TranslationKey)
    : STATUS_KEYS[turn.status];

  if (!shouldShowAgentTurnTail({ turn, reviewDraft })) {
    return null;
  }

  return (
    <div
      data-agent-turn-tail
      data-agent-turn-status={turn.status}
      data-agent-turn-reason={terminalReason ?? ""}
      className="mt-1.5"
    >
      <div className="flex min-h-8 items-center gap-2">
        <span
          aria-hidden
          className={`inline-flex h-5 w-5 shrink-0 items-center justify-center ${STATUS_TONE[turn.status]} ${animate ? "animate-spin motion-reduce:animate-none" : ""}`}
        >
          {STATUS_ICON[turn.status]}
        </span>
        <span
          role={active ? "status" : undefined}
          className="min-w-0 flex-1 text-xs font-medium text-text-secondary"
        >
          {t(statusLabelKey)}
        </span>
        {reviewDraft && onReviewDraft ? (
          <button
            type="button"
            onClick={onReviewDraft}
            className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg bg-accent-soft px-2.5 text-xs font-semibold text-accent-strong transition-colors hover:bg-accent-soft/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <ListChecks size={14} />
            {t("agentWorkbench.reviewDraft")}
          </button>
        ) : null}
      </div>
      {error ? (
        <div
          role="alert"
          className="mt-1.5 flex items-start gap-2 border-l-2 border-state-error bg-state-error/5 px-2.5 py-2 text-xs leading-5 text-state-error"
        >
          <CircleX size={14} className="mt-0.5 shrink-0" />
          <span className="min-w-0 break-words">{error}</span>
        </div>
      ) : null}
      {syncWarning ? (
        <div
          role="status"
          className="mt-1.5 flex items-start gap-2 border-l-2 border-state-warning bg-state-warning/5 px-2.5 py-2 text-xs leading-5 text-state-warning"
        >
          <LoaderCircle size={14} className={`mt-0.5 shrink-0 ${active ? "animate-spin motion-reduce:animate-none" : ""}`} />
          <span className="min-w-0 break-words">
            <span className="font-medium">{t("agentWorkbench.connection.syncing")}</span>
            <span className="ml-1.5">{syncWarning}</span>
          </span>
        </div>
      ) : null}
    </div>
  );
}
