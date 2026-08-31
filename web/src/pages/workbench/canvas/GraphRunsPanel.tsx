import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  CircleDot,
  Clock3,
  Eye,
  FileText,
  Loader2,
  OctagonX,
  RotateCcw,
  Workflow,
} from "lucide-react";
import { useCallback } from "react";

import { IconButton } from "../../../components/ui/icon-button";
import { StatusBadge, statusBadgeClass } from "../../../components/ui/status-badge";
import { EmptyState } from "../../../components/ui/empty-state";
import { PanelSkeleton } from "../../../components/ui/skeleton";
import { ApiError, api } from "../../../lib/api";
import { formatDateTime } from "../../../lib/format";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { sanitizeFilenamePart } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { GraphNodeRun, GraphProjection, GraphRun, GraphRunSubmitInput, WorkflowNodeDisplayStatus } from "../../../lib/types";
import { graphEdgeRoleLabelKey } from "./graphCatalog";
import {
  graphContextEntries,
  graphNodeRunPreviewAssetId,
  graphProgressPhaseLabelKey,
  graphRunInputTraceEntries,
  graphRunScopeLabelKey,
  formatElapsed,
  LIVE_RUN_STATUSES,
} from "./graphRunDisplay";
import { withGraphRunSubmit } from "./graphRunLock";
import { failedNodesRunInput, runPreviewPointerHandlers } from "./graphRunPreview";

export function GraphRunsPanel({
  productId,
  graph,
  selectedNodeId = null,
  structureBusy = false,
  onBeforeRun,
  onJump,
  onPreviewImage,
  onPreviewRun,
  onHideRunPreview,
}: {
  productId: string;
  graph: GraphProjection;
  selectedNodeId?: string | null;
  structureBusy?: boolean;
  onBeforeRun?: () => Promise<void>;
  onJump?: (nodeId: string) => void;
  onPreviewImage?: (image: DownloadableImage) => void;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const queryKey = ["graph-runs", productId, graph.id] as const;
  const runsQuery = useQuery({
    queryKey,
    queryFn: () => api.listGraphRuns(productId, graph.id),
    // GraphCanvasPanel owns the shared run SSE and updates this query cache.
    refetchInterval: false,
  });
  const cancelMutation = useMutation({
    mutationFn: (runId: string) => api.cancelGraphRun(productId, graph.id, runId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  });
  const retryMutation = useMutation({
    mutationFn: (runId: string) => api.retryGraphRun(productId, graph.id, runId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  });
  const selectionMutation = useMutation({
    mutationFn: (input: GraphRunSubmitInput) => api.submitGraphRun(productId, graph.id, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  });
  const retryRun = useCallback(async (runId: string) => {
    try {
      await onBeforeRun?.();
    } catch {
      return;
    }
    retryMutation.mutate(runId);
  }, [onBeforeRun, retryMutation]);
  const retryFailedNodes = useCallback(async (run: GraphRun) => {
    const input = failedNodesRunInput(run);
    if (!input) return;
    try {
      await onBeforeRun?.();
    } catch {
      return;
    }
    onHideRunPreview?.();
    await withGraphRunSubmit(() => selectionMutation.mutateAsync(input));
  }, [onBeforeRun, onHideRunPreview, selectionMutation]);

  if (runsQuery.isLoading) {
    return <PanelSkeleton rows={5} label={t("app.loading")} />;
  }
  if (runsQuery.isError) {
    return (
      <EmptyState
        icon={<AlertCircle size={20} />}
        text={errorDetail(runsQuery.error, t("graph.runs.loadFailed"))}
        action={t("agentWorkbench.retry")}
        onAction={() => void runsQuery.refetch()}
      />
    );
  }

  const runs = runsQuery.data?.items ?? [];
  const operationError = cancelMutation.error ?? retryMutation.error ?? selectionMutation.error;
  return (
    <div className="space-y-3 pb-4" data-graph-runs-panel>
      <div className="flex items-center justify-between gap-2 px-1 text-xs text-text-muted">
        <span>{t("agentWorkbench.runHistory.workflowCount", { count: runs.length })}</span>
        {runsQuery.isFetching ? <Loader2 size={13} className="animate-spin" /> : null}
      </div>
      {operationError ? (
        <div role="alert" className="rounded-lg border border-state-error/30 bg-state-error-soft px-3 py-2.5 text-xs leading-5 text-state-error">
          {errorDetail(operationError, t("graph.runs.loadFailed"))}
        </div>
      ) : null}
      {runs.length ? runs.map((run) => (
        <GraphRunRecord
          key={run.id}
          run={run}
          graph={graph}
          selectedNodeId={selectedNodeId}
          cancelBusy={structureBusy || (cancelMutation.isPending && cancelMutation.variables === run.id)}
          retryBusy={structureBusy || (retryMutation.isPending && retryMutation.variables === run.id)}
          retryFailedBusy={structureBusy || selectionMutation.isPending}
          onCancel={() => cancelMutation.mutate(run.id)}
          onRetry={() => void retryRun(run.id)}
          onRetryFailed={() => void retryFailedNodes(run)}
          onPreviewFailed={failedNodesRunInput(run) ?? undefined}
          onPreviewRun={onPreviewRun}
          onHideRunPreview={onHideRunPreview}
          onJump={onJump}
          onPreviewImage={onPreviewImage}
        />
      )) : (
        <EmptyState icon={<Clock3 size={20} />} text={t("graph.runs.empty")} compact />
      )}
    </div>
  );
}

function GraphRunRecord({
  run,
  graph,
  selectedNodeId,
  cancelBusy,
  retryBusy,
  retryFailedBusy,
  onCancel,
  onRetry,
  onRetryFailed,
  onPreviewFailed,
  onPreviewRun,
  onHideRunPreview,
  onJump,
  onPreviewImage,
}: {
  run: GraphRun;
  graph: GraphProjection;
  selectedNodeId: string | null;
  cancelBusy: boolean;
  retryBusy: boolean;
  retryFailedBusy: boolean;
  onCancel: () => void;
  onRetry: () => void;
  onRetryFailed: () => void;
  onPreviewFailed?: GraphRunSubmitInput;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
  onJump?: (nodeId: string) => void;
  onPreviewImage?: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const active = LIVE_RUN_STATUSES.has(run.status);
  const requested = run.requested_node_id
    ? graph.nodes.find((node) => node.id === run.requested_node_id)
    : null;
  return (
    <article
      className="config-bubble overflow-hidden rounded-lg shadow-sm"
      data-graph-run-id={run.id}
      data-graph-run-status={run.status}
    >
      <div className="p-3.5">
        <div className="flex min-w-0 items-start gap-2.5">
          <span className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md border ${statusBadgeClass(run.status as WorkflowNodeDisplayStatus)}`}>
            {active ? <Loader2 size={14} className="animate-spin" /> : <Workflow size={14} />}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs font-semibold text-text-primary">
                {t(graphRunScopeLabelKey(run.scope))}
              </span>
              <StatusBadge status={run.status as WorkflowNodeDisplayStatus}>
                {t(`detail.nodeStatus.${run.status}`)}
              </StatusBadge>
            </div>
            <div className="mt-1 space-y-0.5 text-[10px] text-text-muted">
              {requested ? <div>{requested.title}</div> : null}
              <div>{t("agentWorkbench.runHistory.nodeCount", { count: run.node_runs.length })}</div>
              <div>{t("agentWorkbench.runHistory.started", { time: formatDateTime(run.started_at, t.locale) })}</div>
              {run.finished_at ? (
                <div>{t("agentWorkbench.runHistory.finished", { time: formatDateTime(run.finished_at, t.locale) })}</div>
              ) : null}
            </div>
          </div>
          <div className="flex shrink-0 gap-1.5">
            {run.node_runs.some((nodeRun) => nodeRun.status === "failed" && nodeRun.node_id) && !active ? (
              <IconButton
                label={t("graph.runs.retryFailed")}
                disabled={retryFailedBusy}
                variant="secondary"
                size="sm"
                onClick={onRetryFailed}
                {...runPreviewPointerHandlers(onPreviewFailed, onPreviewRun, onHideRunPreview)}
              >
                {retryFailedBusy ? <Loader2 size={14} className="animate-spin" /> : <RotateCcw size={14} />}
              </IconButton>
            ) : null}
            {run.status === "failed" && run.is_retryable ? (
              <IconButton label={t("agentWorkbench.runHistory.retryRun")} disabled={retryBusy} variant="secondary" size="sm" onClick={onRetry}>
                {retryBusy ? <Loader2 size={14} className="animate-spin" /> : <RotateCcw size={14} />}
              </IconButton>
            ) : null}
            {active ? (
              <IconButton label={t("agentWorkbench.runHistory.cancel")} disabled={cancelBusy} variant="danger" size="sm" onClick={onCancel}>
                {cancelBusy ? <Loader2 size={14} className="animate-spin" /> : <OctagonX size={14} />}
              </IconButton>
            ) : null}
          </div>
        </div>
        {run.failure_reason ? (
          <div className="mt-3 rounded-md border border-state-error/30 bg-state-error-soft px-3 py-2 text-[11px] leading-5 text-state-error">
            {run.failure_reason}
          </div>
        ) : null}
      </div>
      <div className="divide-y divide-border-l2 border-t border-border-l2">
        {run.node_runs.map((nodeRun) => (
          <NodeRunRecord
            key={nodeRun.id}
            nodeRun={nodeRun}
            title={nodeRun.node_title
              || graph.nodes.find((node) => node.id === nodeRun.node_id)?.title
              || t("graph.runs.deletedNode")}
            selected={Boolean(nodeRun.node_id && nodeRun.node_id === selectedNodeId)}
            previewAssetId={graphNodeRunPreviewAssetId(nodeRun, graph)}
            onJump={onJump && nodeRun.node_id ? () => onJump(nodeRun.node_id as string) : undefined}
            onPreviewImage={onPreviewImage}
          />
        ))}
      </div>
    </article>
  );
}

function NodeRunRecord({
  nodeRun,
  title,
  selected,
  previewAssetId,
  onJump,
  onPreviewImage,
}: {
  nodeRun: GraphNodeRun;
  title: string;
  selected: boolean;
  previewAssetId: string | null;
  onJump?: () => void;
  onPreviewImage?: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const active = LIVE_RUN_STATUSES.has(nodeRun.status);
  const sources = graphRunInputTraceEntries(nodeRun);
  const evidence = graphContextEntries(nodeRun.compiled_context);
  const phaseKey = graphProgressPhaseLabelKey(nodeRun.progress_phase);
  const elapsed = formatElapsed(nodeRun.started_at, nodeRun.finished_at, nodeRun.status);
  return (
    <div className={selected ? "bg-surface-subtle" : ""}>
      <div className="flex min-w-0 items-start gap-2.5 px-3.5 py-3">
        <span className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md border ${statusBadgeClass(nodeRun.status)}`}>
          {active ? <Loader2 size={12} className="animate-spin" /> : <CircleDot size={12} />}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            {onJump ? (
              <button type="button" onClick={onJump} className="min-w-0 flex-1 truncate text-left text-xs font-semibold text-text-primary hover:text-accent">
                {title}
              </button>
            ) : (
              <span className="min-w-0 flex-1 truncate text-xs font-semibold text-text-primary">{title}</span>
            )}
            <StatusBadge status={nodeRun.status} className="px-1.5 py-0.5 text-[9px]">
              {t(`detail.nodeStatus.${nodeRun.status}`)}
            </StatusBadge>
          </div>
          {phaseKey || elapsed || nodeRun.attempt_count > 1 ? (
            <div className="mt-1 space-y-0.5 text-[10px] text-text-muted">
              {phaseKey ? <div>{t(phaseKey)}</div> : null}
              {elapsed ? <div>{elapsed}</div> : null}
              {nodeRun.attempt_count > 1 ? (
                <div>{t("detail.nodeAttemptSummary", { attempts: nodeRun.attempt_count, retries: Math.max(0, nodeRun.attempt_count - 1) })}</div>
              ) : null}
            </div>
          ) : null}
          {sources.length ? (
            <ul data-graph-run-inputs className="mt-1.5 space-y-0.5">
              {sources.map((item) => {
                const roleKey = graphEdgeRoleLabelKey(item.role);
                return (
                  <li key={item.id} className="flex min-w-0 items-center justify-between gap-2 text-[10px] text-text-muted">
                    <span className="min-w-0 truncate">{item.title || t("graph.runs.deletedNode")}</span>
                    <span>{roleKey ? t(roleKey) : t("graph.edgeRole.unknown")}</span>
                  </li>
                );
              })}
            </ul>
          ) : null}
          {nodeRun.failure_reason ? (
            <div className="mt-1.5 text-[10px] leading-4 text-state-error">{nodeRun.failure_reason}</div>
          ) : null}
        </div>
        {previewAssetId && onPreviewImage ? (
          <IconButton
            label={t("agentWorkbench.runHistory.result")}
            variant="secondary"
            size="sm"
            onClick={() => onPreviewImage({
              previewUrl: api.getProductImageAssetMediaUrl(previewAssetId, "thumbnail"),
              downloadUrl: api.getProductImageAssetMediaUrl(previewAssetId),
              filename: `${sanitizeFilenamePart(title, "workflow-image")}.png`,
              alt: title,
            })}
          >
            <Eye size={13} />
          </IconButton>
        ) : null}
      </div>
      {evidence.length ? (
        <details data-graph-run-inputs-technical className="group border-t border-border-l2 px-3.5 py-2.5">
          <summary className="flex cursor-pointer list-none items-center gap-2 text-[10px] font-semibold text-text-muted marker:hidden [&::-webkit-details-marker]:hidden">
            <FileText size={12} />
            <span>{t("agentWorkbench.runHistory.evidence")}</span>
          </summary>
          <dl className="mt-2 space-y-1">
            {evidence.map((item) => (
              <div key={item.key} className="grid grid-cols-[minmax(88px,0.4fr)_minmax(0,1fr)] gap-2 text-[10px] leading-4">
                <dt className="break-words text-text-muted">{t(item.labelKey)}</dt>
                <dd className="break-words text-text-secondary">{item.value}</dd>
              </div>
            ))}
          </dl>
        </details>
      ) : null}
    </div>
  );
}

function errorDetail(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.detail : fallback;
}
