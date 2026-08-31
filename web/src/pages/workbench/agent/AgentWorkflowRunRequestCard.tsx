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

import { formatDateTime } from "../../../lib/format";
import { useI18n } from "../../../lib/preferences";
import type { AgentWorkflowRunRequestView } from "./conversation/helpers";

interface AgentWorkflowRunRequestCardProps {
  request: AgentWorkflowRunRequestView | null;
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
      <div className="shrink-0 border-t border-border-l1 bg-surface-raised px-4 py-3 text-xs text-text-muted">
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
    ? "border-state-warning/40 bg-state-warning/10"
    : unknown
      ? "border-state-warning/50 bg-state-warning/10"
      : failed
        ? "border-state-error/40 bg-state-error/10"
        : cancelled
          ? "border-border-l2 bg-surface-subtle"
          : succeeded
            ? "border-state-success/40 bg-state-success/10"
            : "border-accent/30 bg-accent-soft";
  const statusIcon = awaitingConfirmation ? (
    <Clock3 size={14} className="text-state-warning" aria-hidden="true" />
  ) : unknown ? (
    <CircleAlert size={14} className="text-state-warning" aria-hidden="true" />
  ) : failed ? (
    <CircleAlert size={14} className="text-state-error" aria-hidden="true" />
  ) : succeeded ? (
    <CircleCheck size={14} className="text-state-success" aria-hidden="true" />
  ) : cancelled ? (
    <Square size={13} className="text-text-muted" aria-hidden="true" />
  ) : (
    <Loader2 size={14} className="animate-spin text-accent motion-reduce:animate-none" aria-hidden="true" />
  );
  const documentActionLabel = request.document_action === "complete"
    ? t("graph.inspector.complete")
    : request.document_action === "rewrite"
      ? t("graph.inspector.rewrite")
      : request.document_action === "replace"
        ? t("graph.inspector.replace")
        : null;

  return (
    <section
      data-agent-workflow-run-request
      aria-labelledby={`agent-workflow-run-request-${request.id}`}
      className={`shrink-0 border-y px-4 py-4 backdrop-blur-sm transition-[border-color,background-color] duration-fast sm:mx-3 sm:my-2 sm:rounded-panel sm:border ${statusTone}`}
    >
      <div className="mx-auto w-full max-w-3xl">
        <div className="flex items-start gap-3.5">
          <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-accent text-accent-fg shadow-sm">
            <Workflow size={19} aria-hidden="true" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-start gap-2">
              <div className="min-w-0 flex-1">
                <h3 id={`agent-workflow-run-request-${request.id}`} className="text-sm font-semibold tracking-tight text-text-primary">
                  {request.source_run_id
                    ? t("agentWorkbench.workflowRunRequest.retryTitle")
                    : t("agentWorkbench.workflowRunRequest.title")}
                </h3>
                <p className="mt-0.5 break-words text-xs text-text-secondary">
                  {request.workflow_title}
                </p>
                {targetLabel ? (
                  <p className="mt-0.5 break-words text-[11px] text-text-muted">
                    {targetLabel}
                  </p>
                ) : null}
              </div>
              <span className={`inline-flex shrink-0 items-center gap-1.5 rounded-full border border-current/20 px-2.5 py-1 text-[11px] font-semibold text-text-primary ${awaitingConfirmation ? "animate-pulse" : ""}`}>
                {statusIcon}
                {statusLabel}
              </span>
            </div>

            <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-text-muted">
              {request.expected_workflow_revision != null ? (
                <span>{t("agentWorkbench.workflowRunRequest.revision", { version: request.expected_workflow_revision })}</span>
              ) : null}
              <span>{t("agentWorkbench.workflowRunRequest.requested", { time: formatDateTime(request.created_at, t.locale) })}</span>
              {documentActionLabel ? (
                <span>{t("agentWorkbench.workflowRunRequest.documentAction", { action: documentActionLabel })}</span>
              ) : null}
              {request.finished_at ? (
                <span>{t("agentWorkbench.workflowRunRequest.finished", { time: formatDateTime(request.finished_at, t.locale) })}</span>
              ) : null}
            </div>

            {awaitingConfirmation ? (
              <p className="mt-2 text-xs leading-5 text-text-primary">
                {request.source_run_id
                  ? t("agentWorkbench.workflowRunRequest.confirmRetryDescription")
                  : t("agentWorkbench.workflowRunRequest.confirmDescription")}
              </p>
            ) : null}
            {active ? (
              <p className="mt-2 text-xs leading-5 text-accent-strong">
                {t("agentWorkbench.workflowRunRequest.runningDescription")}
              </p>
            ) : null}
            {succeeded ? (
              <p className="mt-2 text-xs leading-5 text-state-success">
                {t("agentWorkbench.workflowRunRequest.succeededDescription")}
              </p>
            ) : null}
            {failed || cancelled ? (
              <p className="mt-2 break-words text-xs leading-5 text-state-error">
                {request.failure_reason ?? t("agentWorkbench.workflowRunRequest.endedDescription")}
              </p>
            ) : null}
            {unknown ? (
              <p className="mt-2 break-words text-xs leading-5 text-state-warning">
                {request.failure_reason ?? t("agentWorkbench.workflowRunRequest.endedDescription")}
              </p>
            ) : null}
            {error ? <p role="alert" className="mt-2 break-words text-xs leading-5 text-state-error">{error}</p> : null}

            <div className="mt-3 flex flex-wrap gap-2">
              {awaitingConfirmation ? (
                <>
                  <button
                    type="button"
                    onClick={onConfirm}
                    disabled={busy}
                    className="inline-flex min-h-10 items-center gap-2 rounded-md bg-accent px-3 text-xs font-semibold text-accent-fg shadow-sm transition-colors hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-wait disabled:opacity-50"
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
                    className="inline-flex min-h-10 items-center gap-2 rounded-md border border-border-l2 bg-surface-raised px-3 text-xs font-semibold text-text-secondary transition-colors hover:border-border-l3 hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
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
                  className="inline-flex min-h-10 items-center gap-2 rounded-md border border-border-l2 bg-surface-raised px-3 text-xs font-semibold text-text-secondary transition-colors hover:border-accent/40 hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
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
