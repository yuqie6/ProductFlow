import { AlertCircle, ExternalLink, Images, Loader2, PencilLine, Play } from "lucide-react";

import { ApiError, api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { TranslationKey } from "../../../lib/i18n";
import type { WorkflowNodeDisplayStatus } from "../../../lib/types";
import type { LocalImageEditOpenRequest } from "../local-edit/LocalImageEditController";
import { statusClass } from "../chrome/utils";
import type { GraphShotProjection } from "./shotProjection";

export interface GraphShotListProps {
  shots: readonly GraphShotProjection[];
  runsLoading?: boolean;
  runsFetching?: boolean;
  runsError?: unknown;
  operationError?: unknown;
  notice?: string | null;
  onRetryRuns?: () => void;
  busy: boolean;
  runningGroupId: string | null;
  onOpenNode: (nodeId: string) => void;
  onOpenLocalEdit?: (request: LocalImageEditOpenRequest) => void;
  onRunShot: (groupId: string) => void;
  onRunAll: () => void;
  runAllDisabled?: boolean;
  blockedGroupIds?: ReadonlySet<string>;
  runBlockedReason?: string;
}

export function GraphShotList({
  shots,
  runsLoading = false,
  runsFetching = false,
  runsError = null,
  operationError = null,
  notice = null,
  onRetryRuns,
  busy,
  runningGroupId,
  onOpenNode,
  onOpenLocalEdit,
  onRunShot,
  onRunAll,
  runAllDisabled = false,
  blockedGroupIds,
  runBlockedReason,
}: GraphShotListProps) {
  const { t } = useI18n();

  return (
    <section
      data-graph-shot-list
      aria-label={t("graph.shots.title")}
      className="flex h-full min-h-0 flex-col overflow-hidden bg-surface-raised text-text-primary"
    >
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b border-border-l1 px-3 py-3 sm:px-5">
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-2">
            <h2 className="truncate text-sm font-semibold text-text-primary">{t("graph.shots.title")}</h2>
            {runsFetching ? <Loader2 size={14} className="shrink-0 animate-spin text-text-muted" aria-hidden="true" /> : null}
          </div>
          <p className="mt-1 text-[11px] text-text-muted">{t("graph.shots.count", { count: shots.length })}</p>
        </div>
        <button
          type="button"
          data-graph-shot-run-all
          disabled={busy || runningGroupId !== null || runAllDisabled}
          onClick={onRunAll}
          className="inline-flex min-h-9 shrink-0 items-center justify-center gap-1.5 rounded-lg bg-accent px-3 text-xs font-semibold text-accent-fg hover:bg-accent-strong disabled:cursor-not-allowed disabled:opacity-45"
        >
          {busy || runningGroupId !== null ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Play size={14} aria-hidden="true" />}
          <span>{t("graph.shots.generateSet")}</span>
        </button>
      </header>

      {operationError ? (
        <div
          role="alert"
          className="mx-3 mt-3 flex shrink-0 items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200 sm:mx-5"
        >
          <AlertCircle size={15} className="mt-0.5 shrink-0" aria-hidden="true" />
          <span className="min-w-0">{errorDetail(operationError, t("workbench.error.run"))}</span>
        </div>
      ) : null}
      {notice ? (
        <div
          role="status"
          className="mx-3 mt-3 shrink-0 rounded-lg border border-border-l1 bg-surface-subtle px-3 py-2.5 text-xs leading-5 text-text-secondary sm:mx-5"
        >
          {notice}
        </div>
      ) : null}
      {runsError ? (
        <div
          role="alert"
          className="mx-3 mt-3 flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200 sm:mx-5"
        >
          <AlertCircle size={15} className="mt-0.5 shrink-0" aria-hidden="true" />
          <span className="min-w-0 flex-1">{errorDetail(runsError, t("graph.runs.loadFailed"))}</span>
          {onRetryRuns ? (
            <button type="button" onClick={onRetryRuns} className="shrink-0 font-semibold underline underline-offset-2">
              {t("agentWorkbench.retry")}
            </button>
          ) : null}
        </div>
      ) : null}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {runsLoading && !shots.length ? (
          <div className="flex min-h-40 items-center justify-center gap-2 px-4 text-xs text-text-muted">
            <Loader2 size={16} className="animate-spin" aria-hidden="true" />
            <span>{t("app.loading")}</span>
          </div>
        ) : shots.length ? (
          <div role="list">
            {shots.map((shot) => (
              <GraphShotRow
                key={shot.groupId}
                shot={shot}
                busy={busy}
                running={runningGroupId === shot.groupId}
                anotherShotRunning={runningGroupId !== null && runningGroupId !== shot.groupId}
                runBlocked={blockedGroupIds?.has(shot.groupId) ?? false}
                runBlockedReason={runBlockedReason}
                onOpenNode={onOpenNode}
                onOpenLocalEdit={onOpenLocalEdit}
                onRunShot={onRunShot}
              />
            ))}
          </div>
        ) : (
          <div className="flex min-h-40 items-center justify-center px-4 text-center text-xs text-text-muted">
            {t("graph.shots.empty")}
          </div>
        )}
      </div>
    </section>
  );
}

function GraphShotRow({
  shot,
  busy,
  running,
  anotherShotRunning,
  runBlocked,
  runBlockedReason,
  onOpenNode,
  onOpenLocalEdit,
  onRunShot,
}: {
  shot: GraphShotProjection;
  busy: boolean;
  running: boolean;
  anotherShotRunning: boolean;
  runBlocked: boolean;
  runBlockedReason?: string;
  onOpenNode: (nodeId: string) => void;
  onOpenLocalEdit?: (request: LocalImageEditOpenRequest) => void;
  onRunShot: (groupId: string) => void;
}) {
  const { t } = useI18n();
  const statusLabel = t(statusKey(shot.latestNodeStatus));
  const failureReason = shot.latestNodeStatus === "failed"
    ? shot.latestFailureReason ?? t("graph.shots.failureUnknown")
    : null;
  const previewAssetId = shot.primaryImageAssetId;

  return (
    <article
      role="listitem"
      data-graph-shot-id={shot.groupId}
      className="grid grid-cols-[minmax(0,1fr)_auto] gap-3 border-b border-border-l1 px-3 py-3.5 sm:gap-4 sm:px-5 sm:py-4"
    >
      <div className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-start gap-2">
          <button
            type="button"
            data-graph-shot-open
            onClick={() => onOpenNode(shot.primaryImageNodeId)}
            className="min-w-0 max-w-full text-left text-sm font-semibold text-text-primary underline-offset-2 hover:text-accent hover:underline"
            title={t("graph.shots.openNode")}
          >
            <span className="break-words">{shot.title}</span>
          </button>
          {onOpenLocalEdit && previewAssetId ? (
            <button
              type="button"
              data-graph-shot-local-edit
              onClick={() => onOpenLocalEdit({ sourceAssetId: previewAssetId, targetNodeId: shot.primaryImageNodeId })}
              className="inline-flex min-h-8 items-center gap-1 rounded-lg border border-border-l1 px-2.5 text-[11px] font-semibold text-text-secondary hover:bg-surface-subtle hover:text-text-primary"
              title={t("localEdit.open")}
            >
              <PencilLine size={13} aria-hidden="true" />
              <span>{t("localEdit.open")}</span>
            </button>
          ) : null}
          <span className={`inline-flex shrink-0 items-center rounded-full border px-2 py-0.5 text-[10px] font-semibold ${statusClass(shot.latestNodeStatus)}`}>
            {statusLabel}
          </span>
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-text-muted">
          <span>{t("detail.inspector.imageCount", { count: shot.imageNodeCount })}</span>
          <span>{t("graph.shots.completedCount", { count: shot.completedImageCount })}</span>
          <span className="inline-flex items-center gap-1">
            <span>{t("graph.shots.latestStatus")}</span>
            <span className="font-medium text-text-secondary">{statusLabel}</span>
          </span>
        </div>
        {failureReason ? (
          <p role="alert" className="mt-2 break-words text-xs leading-5 text-red-700 dark:text-red-200">
            {failureReason}
          </p>
        ) : null}
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <button
            type="button"
            data-graph-shot-open-node
            onClick={() => onOpenNode(shot.primaryImageNodeId)}
            className="inline-flex min-h-8 items-center gap-1 rounded-lg border border-border-l1 px-2.5 text-[11px] font-semibold text-text-secondary hover:bg-surface-subtle hover:text-text-primary"
          >
            <ExternalLink size={13} aria-hidden="true" />
            <span>{t("graph.shots.openNode")}</span>
          </button>
          <button
            type="button"
            data-graph-shot-run
            data-graph-shot-running={running ? "true" : undefined}
            aria-busy={running}
            disabled={busy || running || anotherShotRunning || runBlocked}
            onClick={() => onRunShot(shot.groupId)}
            title={runBlocked ? runBlockedReason : undefined}
            className="inline-flex min-h-8 items-center gap-1 rounded-lg bg-slate-900 px-2.5 text-[11px] font-semibold text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-45 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
          >
            {running ? <Loader2 size={13} className="animate-spin" aria-hidden="true" /> : <Play size={13} aria-hidden="true" />}
            <span>{running ? t("detail.runAction.submitting") : t("graph.canvas.runShot")}</span>
          </button>
        </div>
      </div>
      {previewAssetId ? (
        <button
          type="button"
          data-graph-shot-preview
          onClick={() => onOpenNode(shot.primaryImageNodeId)}
          className="relative h-16 w-16 shrink-0 overflow-hidden rounded-lg border border-border-l1 bg-surface-subtle text-left sm:h-20 sm:w-20"
          aria-label={t("graph.shots.openNode")}
        >
          <img
            src={api.getProductImageAssetMediaUrl(previewAssetId, "thumbnail")}
            alt={shot.title}
            className="h-full w-full object-cover"
          />
          {shot.currentResultAssetIds.length > 1 ? (
            <span className="absolute bottom-1 right-1 rounded bg-slate-950/75 px-1 text-[10px] font-semibold text-white">
              +{shot.currentResultAssetIds.length - 1}
            </span>
          ) : null}
        </button>
      ) : (
        <div className="flex h-16 w-16 shrink-0 flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-border-l1 bg-surface-subtle text-[10px] text-text-muted sm:h-20 sm:w-20">
          <Images size={17} aria-hidden="true" />
          <span>{t("graph.inspector.noPreview")}</span>
        </div>
      )}
    </article>
  );
}

function statusKey(status: WorkflowNodeDisplayStatus): TranslationKey {
  const keys: Record<WorkflowNodeDisplayStatus, TranslationKey> = {
    idle: "detail.nodeStatus.idle",
    queued: "detail.nodeStatus.queued",
    running: "detail.nodeStatus.running",
    succeeded: "detail.nodeStatus.succeeded",
    failed: "detail.nodeStatus.failed",
    cancelled: "detail.nodeStatus.cancelled",
    skipped: "detail.nodeStatus.skipped",
    unknown: "detail.nodeStatus.unknown",
  };
  return keys[status];
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.detail) return error.detail;
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}
