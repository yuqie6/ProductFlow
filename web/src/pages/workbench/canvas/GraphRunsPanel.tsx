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
import type { ReactNode } from "react";

import { ApiError, api } from "../../../lib/api";
import { formatDateTime } from "../../../lib/format";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { sanitizeFilenamePart } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { GraphNodeRun, GraphProjection, GraphRun, WorkflowNodeStatus } from "../../../lib/types";
import { statusClass } from "../chrome/utils";
import {
  graphContextEntries,
  graphNodeRunPreviewAssetId,
  graphRunScopeLabelKey,
} from "./graphRunDisplay";

const ACTIVE_STATUSES = new Set(["queued", "running"]);

export function GraphRunsPanel({
  productId,
  graph,
  selectedNodeId = null,
  structureBusy = false,
  onJump,
  onPreviewImage,
}: {
  productId: string;
  graph: GraphProjection;
  selectedNodeId?: string | null;
  structureBusy?: boolean;
  onJump?: (nodeId: string) => void;
  onPreviewImage?: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const queryKey = ["graph-runs", productId, graph.id] as const;
  const runsQuery = useQuery({
    queryKey,
    queryFn: () => api.listGraphRuns(productId, graph.id),
    refetchInterval: (query) => query.state.data?.items.some((run) => run.status === "running") ? 1200 : false,
  });
  const cancelMutation = useMutation({
    mutationFn: (runId: string) => api.cancelGraphRun(productId, graph.id, runId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  });
  const retryMutation = useMutation({
    mutationFn: (runId: string) => api.retryGraphRun(productId, graph.id, runId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  });

  if (runsQuery.isLoading) {
    return <PanelState icon={<Loader2 size={20} className="animate-spin" />} text={t("app.loading")} />;
  }
  if (runsQuery.isError) {
    return (
      <PanelState
        icon={<AlertCircle size={20} />}
        text={errorDetail(runsQuery.error, t("graph.runs.loadFailed"))}
        action={t("agentWorkbench.retry")}
        onAction={() => void runsQuery.refetch()}
      />
    );
  }

  const runs = runsQuery.data?.items ?? [];
  const operationError = cancelMutation.error ?? retryMutation.error;
  return (
    <div className="space-y-3 pb-4" data-graph-runs-panel>
      <div className="flex items-center justify-between gap-2 px-1 text-xs text-zinc-500 dark:text-slate-400">
        <span>{t("agentWorkbench.runHistory.workflowCount", { count: runs.length })}</span>
        {runsQuery.isFetching ? <Loader2 size={13} className="animate-spin" /> : null}
      </div>
      {operationError ? (
        <div role="alert" className="rounded-lg border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
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
          onCancel={() => cancelMutation.mutate(run.id)}
          onRetry={() => retryMutation.mutate(run.id)}
          onJump={onJump}
          onPreviewImage={onPreviewImage}
        />
      )) : (
        <PanelState icon={<Clock3 size={20} />} text={t("graph.runs.empty")} compact />
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
  onCancel,
  onRetry,
  onJump,
  onPreviewImage,
}: {
  run: GraphRun;
  graph: GraphProjection;
  selectedNodeId: string | null;
  cancelBusy: boolean;
  retryBusy: boolean;
  onCancel: () => void;
  onRetry: () => void;
  onJump?: (nodeId: string) => void;
  onPreviewImage?: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const active = ACTIVE_STATUSES.has(run.status);
  const requested = run.requested_node_id
    ? graph.nodes.find((node) => node.id === run.requested_node_id)
    : null;
  return (
    <article className="config-bubble overflow-hidden rounded-lg shadow-sm" data-graph-run-id={run.id}>
      <div className="p-3.5">
        <div className="flex min-w-0 items-start gap-2.5">
          <span className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md border ${statusClass(run.status as WorkflowNodeStatus)}`}>
            {active ? <Loader2 size={14} className="animate-spin" /> : <Workflow size={14} />}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs font-semibold text-zinc-900 dark:text-slate-100">
                {t(graphRunScopeLabelKey(run.scope))}
              </span>
              <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${statusClass(run.status as WorkflowNodeStatus)}`}>
                {t(`detail.nodeStatus.${run.status}`)}
              </span>
            </div>
            <div className="mt-1 space-y-0.5 text-[10px] text-zinc-500 dark:text-slate-400">
              {requested ? <div>{requested.title}</div> : null}
              <div>{t("agentWorkbench.runHistory.nodeCount", { count: run.node_runs.length })}</div>
              <div>{t("agentWorkbench.runHistory.started", { time: formatDateTime(run.started_at, t.locale) })}</div>
              {run.finished_at ? (
                <div>{t("agentWorkbench.runHistory.finished", { time: formatDateTime(run.finished_at, t.locale) })}</div>
              ) : null}
            </div>
          </div>
          <div className="flex shrink-0 gap-1.5">
            {run.is_retryable ? (
              <IconButton label={t("agentWorkbench.runHistory.retryRun")} disabled={retryBusy} onClick={onRetry}>
                {retryBusy ? <Loader2 size={14} className="animate-spin" /> : <RotateCcw size={14} />}
              </IconButton>
            ) : null}
            {active ? (
              <IconButton label={t("agentWorkbench.runHistory.cancel")} disabled={cancelBusy} danger onClick={onCancel}>
                {cancelBusy ? <Loader2 size={14} className="animate-spin" /> : <OctagonX size={14} />}
              </IconButton>
            ) : null}
          </div>
        </div>
        {run.failure_reason ? (
          <div className="mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-[11px] leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
            {run.failure_reason}
          </div>
        ) : null}
      </div>
      <div className="divide-y divide-zinc-100 border-t border-zinc-100 dark:divide-slate-800 dark:border-slate-800">
        {run.node_runs.map((nodeRun) => (
          <NodeRunRecord
            key={nodeRun.id}
            nodeRun={nodeRun}
            title={graph.nodes.find((node) => node.id === nodeRun.node_id)?.title
              ?? t("graph.runs.deletedNode")}
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
  const active = ACTIVE_STATUSES.has(nodeRun.status);
  const evidence = graphContextEntries(nodeRun.compiled_context);
  return (
    <div className={selected ? "bg-slate-50 dark:bg-slate-800/50" : ""}>
      <div className="flex min-w-0 items-start gap-2.5 px-3.5 py-3">
        <span className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md border ${statusClass(nodeRun.status)}`}>
          {active ? <Loader2 size={12} className="animate-spin" /> : <CircleDot size={12} />}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            {onJump ? (
              <button type="button" onClick={onJump} className="min-w-0 flex-1 truncate text-left text-xs font-semibold text-zinc-800 hover:text-slate-950 dark:text-slate-100 dark:hover:text-white">
                {title}
              </button>
            ) : (
              <span className="min-w-0 flex-1 truncate text-xs font-semibold text-zinc-800 dark:text-slate-100">{title}</span>
            )}
            <span className={`rounded-full border px-1.5 py-0.5 text-[9px] font-semibold ${statusClass(nodeRun.status)}`}>
              {t(`detail.nodeStatus.${nodeRun.status}`)}
            </span>
          </div>
          {nodeRun.failure_reason ? (
            <div className="mt-1.5 text-[10px] leading-4 text-red-600 dark:text-red-300">{nodeRun.failure_reason}</div>
          ) : null}
        </div>
        {previewAssetId && onPreviewImage ? (
          <IconButton
            label={t("agentWorkbench.runHistory.result")}
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
        <details className="group border-t border-zinc-100 px-3.5 py-2.5 dark:border-slate-800">
          <summary className="flex cursor-pointer list-none items-center gap-2 text-[10px] font-semibold text-zinc-500 marker:hidden dark:text-slate-400 [&::-webkit-details-marker]:hidden">
            <FileText size={12} />
            <span>{t("agentWorkbench.runHistory.evidence")}</span>
          </summary>
          <dl className="mt-2 space-y-1">
            {evidence.map((item) => (
              <div key={item.key} className="grid grid-cols-[minmax(88px,0.4fr)_minmax(0,1fr)] gap-2 text-[10px] leading-4">
                <dt className="break-words text-zinc-400 dark:text-slate-500">{t(item.labelKey)}</dt>
                <dd className="break-words text-zinc-600 dark:text-slate-300">{item.value}</dd>
              </div>
            ))}
          </dl>
        </details>
      ) : null}
    </div>
  );
}

function IconButton({
  label,
  disabled,
  danger = false,
  onClick,
  children,
}: {
  label: string;
  disabled?: boolean;
  danger?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border disabled:opacity-40 ${
        danger
          ? "border-red-200 text-red-600 hover:bg-red-50 dark:border-red-400/35 dark:text-red-200 dark:hover:bg-red-500/10"
          : "border-zinc-200 text-zinc-600 hover:border-slate-300 hover:text-slate-900 dark:border-slate-700 dark:text-slate-300 dark:hover:border-slate-500 dark:hover:text-white"
      }`}
      aria-label={label}
      title={label}
    >
      {children}
    </button>
  );
}

function PanelState({
  icon,
  text,
  action,
  onAction,
  compact = false,
}: {
  icon?: ReactNode;
  text: string;
  action?: string;
  onAction?: () => void;
  compact?: boolean;
}) {
  return (
    <div className={`flex flex-col items-center justify-center gap-2 px-6 text-center text-xs text-zinc-500 dark:text-slate-400 ${compact ? "min-h-[180px]" : "min-h-[260px]"}`}>
      {icon ? <span className="text-zinc-400 dark:text-slate-500">{icon}</span> : null}
      <span className="max-w-[260px] leading-5">{text}</span>
      {action && onAction ? (
        <button type="button" onClick={onAction} className="mt-1 font-semibold text-slate-800 hover:underline dark:text-slate-200">
          {action}
        </button>
      ) : null}
    </div>
  );
}

function errorDetail(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.detail : fallback;
}
