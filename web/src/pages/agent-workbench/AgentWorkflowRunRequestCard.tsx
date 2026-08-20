import {
  CircleAlert,
  CircleCheck,
  Clock3,
  ExternalLink,
  Loader2,
  Play,
  RotateCcw,
  Square,
  Workflow,
} from "lucide-react";

import { formatDateTime } from "../../lib/format";
import { useI18n } from "../../lib/preferences";
import type { AgentWorkflowRunRequest } from "../../lib/types";

interface AgentWorkflowRunRequestCardProps {
  request: AgentWorkflowRunRequest | null;
  loading: boolean;
  busy: boolean;
  error: string | null;
  targetLabel?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
  onOpenRuns?: () => void;
}

export function AgentWorkflowRunRequestCard({
  request,
  loading,
  busy,
  error,
  targetLabel = null,
  onConfirm,
  onCancel,
  onOpenRuns,
}: AgentWorkflowRunRequestCardProps) {
  const { t } = useI18n();

  if (loading && !request) {
    return (
      <div className="shrink-0 border-t border-zinc-200 bg-white px-4 py-3 text-xs text-zinc-500 dark:border-slate-800 dark:bg-[#090d13] dark:text-slate-400">
        <div className="flex items-center gap-2">
          <Loader2 size={14} className="animate-spin" aria-hidden="true" />
          <span>{t("agentWorkbench.workflowRunRequest.loading")}</span>
        </div>
      </div>
    );
  }

  if (!request) {
    return null;
  }

  const awaitingConfirmation = request.status === "awaiting_confirmation";
  const unknown = request.status === "confirmed" && request.workflow_run_status === "unknown";
  const active = request.status === "confirmed" && !unknown;
  const succeeded = request.status === "succeeded";
  const failed = request.status === "failed";
  const cancelled = request.status === "cancelled";
  const statusKey = request.workflow_run_status && active
    ? request.workflow_run_status
    : request.status;
  const statusLabel = statusKey === "running"
    ? t("agentWorkbench.workflowRunRequest.status.running")
    : statusKey === "succeeded"
      ? t("agentWorkbench.workflowRunRequest.status.succeeded")
      : statusKey === "failed"
        ? t("agentWorkbench.workflowRunRequest.status.failed")
        : statusKey === "cancelled"
          ? t("agentWorkbench.workflowRunRequest.status.cancelled")
          : statusKey === "unknown"
            ? t("agentWorkbench.workflowRunRequest.status.unknown")
          : statusKey === "confirmed"
            ? t("agentWorkbench.workflowRunRequest.status.confirmed")
            : t("agentWorkbench.workflowRunRequest.status.awaitingConfirmation");
  const statusTone = awaitingConfirmation
    ? "border-amber-300/80 bg-amber-50/90 shadow-amber-500/5 dark:border-amber-400/30 dark:bg-amber-400/10"
    : unknown
      ? "border-orange-300/80 bg-orange-50/90 shadow-orange-500/5 dark:border-orange-400/30 dark:bg-orange-400/10"
    : failed
      ? "border-red-300/80 bg-red-50/90 shadow-red-500/5 dark:border-red-400/30 dark:bg-red-400/10"
      : cancelled
        ? "border-zinc-300 bg-zinc-50 dark:border-slate-700 dark:bg-slate-900/70"
        : succeeded
          ? "border-emerald-300/80 bg-emerald-50/90 shadow-emerald-500/5 dark:border-emerald-400/30 dark:bg-emerald-400/10"
          : "border-blue-300/80 bg-blue-50/90 shadow-cyan-500/5 dark:border-cyan-400/30 dark:bg-cyan-400/10";
  const statusIcon = awaitingConfirmation ? (
    <Clock3 size={14} className="text-amber-600 dark:text-amber-400" aria-hidden="true" />
  ) : unknown ? (
    <CircleAlert size={14} className="text-orange-600 dark:text-orange-400" aria-hidden="true" />
  ) : failed ? (
    <CircleAlert size={14} className="text-red-600 dark:text-red-400" aria-hidden="true" />
  ) : succeeded ? (
    <CircleCheck size={14} className="text-emerald-600 dark:text-emerald-400" aria-hidden="true" />
  ) : cancelled ? (
    <Square size={13} className="text-zinc-500 dark:text-slate-400" aria-hidden="true" />
  ) : (
    <Loader2 size={14} className="animate-spin text-blue-600 motion-reduce:animate-none dark:text-cyan-400" aria-hidden="true" />
  );

  return (
    <section
      data-agent-workflow-run-request
      aria-labelledby={`agent-workflow-run-request-${request.id}`}
      className={`shrink-0 border-y px-4 py-4 backdrop-blur-sm transition-all sm:mx-3 sm:my-2 sm:rounded-xl sm:border ${statusTone}`}
    >
      <div className="mx-auto w-full max-w-3xl">
        <div className="flex items-start gap-3.5">
          <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-zinc-950 text-white shadow-sm dark:bg-cyan-400 dark:text-[#071018]">
            <Workflow size={19} aria-hidden="true" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-start gap-2">
              <div className="min-w-0 flex-1">
                <h3 id={`agent-workflow-run-request-${request.id}`} className="text-sm font-semibold tracking-tight text-zinc-950 dark:text-white">
                  {request.source_run_id
                    ? t("agentWorkbench.workflowRunRequest.retryTitle")
                    : t("agentWorkbench.workflowRunRequest.title")}
                </h3>
                <p className="mt-0.5 break-words text-xs text-zinc-700 dark:text-slate-300">
                  {request.workflow_title}
                </p>
                {targetLabel ? (
                  <p className="mt-0.5 break-words text-[11px] text-zinc-600 dark:text-slate-400">
                    {targetLabel}
                  </p>
                ) : null}
              </div>
              <span className={`inline-flex shrink-0 items-center gap-1.5 rounded-full border border-current/20 px-2.5 py-1 text-[11px] font-semibold text-zinc-800 dark:text-slate-100 ${awaitingConfirmation ? "animate-pulse" : ""}`}>
                {statusIcon}
                {statusLabel}
              </span>
            </div>

            <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-zinc-600 dark:text-slate-400">
              <span>{t("agentWorkbench.workflowRunRequest.revision", { version: request.expected_workflow_revision })}</span>
              <span>{t("agentWorkbench.workflowRunRequest.requested", { time: formatDateTime(request.created_at, t.locale) })}</span>
              {request.finished_at ? (
                <span>{t("agentWorkbench.workflowRunRequest.finished", { time: formatDateTime(request.finished_at, t.locale) })}</span>
              ) : null}
            </div>

            {awaitingConfirmation ? (
              <p className="mt-2 text-xs leading-5 text-amber-900 dark:text-amber-100">
                {request.source_run_id
                  ? t("agentWorkbench.workflowRunRequest.confirmRetryDescription")
                  : t("agentWorkbench.workflowRunRequest.confirmDescription")}
              </p>
            ) : null}
            {active ? (
              <p className="mt-2 text-xs leading-5 text-blue-900 dark:text-cyan-100">
                {t("agentWorkbench.workflowRunRequest.runningDescription")}
              </p>
            ) : null}
            {succeeded ? (
              <p className="mt-2 text-xs leading-5 text-emerald-900 dark:text-emerald-100">
                {t("agentWorkbench.workflowRunRequest.succeededDescription")}
              </p>
            ) : null}
            {failed || cancelled ? (
              <p className="mt-2 break-words text-xs leading-5 text-red-800 dark:text-red-200">
                {request.failure_reason ?? t("agentWorkbench.workflowRunRequest.endedDescription")}
              </p>
            ) : null}
            {unknown ? (
              <p className="mt-2 break-words text-xs leading-5 text-orange-800 dark:text-orange-200">
                {request.failure_reason ?? t("agentWorkbench.workflowRunRequest.endedDescription")}
              </p>
            ) : null}
            {error ? <p role="alert" className="mt-2 break-words text-xs leading-5 text-red-700 dark:text-red-200">{error}</p> : null}

            <div className="mt-3 flex flex-wrap gap-2">
              {awaitingConfirmation ? (
                <>
                  <button
                    type="button"
                    onClick={onConfirm}
                    disabled={busy}
                    className="inline-flex min-h-10 items-center gap-2 rounded-md bg-blue-600 px-3 text-xs font-semibold text-white shadow-sm hover:bg-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 disabled:cursor-wait disabled:opacity-50 dark:bg-cyan-400 dark:text-[#071018] dark:hover:bg-cyan-300"
                  >
                    {busy ? <Loader2 size={14} className="animate-spin" /> : <Play size={14} />}
                    {busy
                      ? t("agentWorkbench.workflowRunRequest.confirming")
                      : request.source_run_id
                        ? t("agentWorkbench.workflowRunRequest.confirmRetry")
                        : t("agentWorkbench.workflowRunRequest.confirm")}
                  </button>
                  <button
                    type="button"
                    onClick={onCancel}
                    disabled={busy}
                    className="inline-flex min-h-10 items-center gap-2 rounded-md border border-zinc-300 bg-white px-3 text-xs font-semibold text-zinc-700 hover:border-zinc-500 hover:bg-zinc-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-zinc-500 disabled:opacity-50 dark:border-slate-600 dark:bg-[#111820] dark:text-slate-200 dark:hover:border-slate-400 dark:hover:bg-slate-800"
                  >
                    <RotateCcw size={14} />
                    {t("agentWorkbench.workflowRunRequest.cancel")}
                  </button>
                </>
              ) : null}
              {onOpenRuns && (active || succeeded || failed || cancelled || unknown) ? (
                <button
                  type="button"
                  onClick={onOpenRuns}
                  className="inline-flex min-h-10 items-center gap-2 rounded-md border border-zinc-300 bg-white px-3 text-xs font-semibold text-zinc-700 hover:border-blue-400 hover:text-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 dark:border-slate-600 dark:bg-[#111820] dark:text-slate-200 dark:hover:border-cyan-400 dark:hover:text-cyan-200"
                >
                  <ExternalLink size={14} />
                  {t("agentWorkbench.workflowRunRequest.openRuns")}
                </button>
              ) : null}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
