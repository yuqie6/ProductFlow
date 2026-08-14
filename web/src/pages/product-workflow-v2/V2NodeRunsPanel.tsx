import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  CircleDot,
  Clock3,
  Eye,
  FileText,
  Image as ImageIcon,
  Loader2,
  OctagonX,
  RefreshCw,
  RotateCcw,
  Workflow,
} from "lucide-react";

import { ApiError, api } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type {
  CanonicalProductDetail,
  ProductWorkflowV2,
  WorkflowNodeRunV2,
  WorkflowNodeV2,
  WorkflowRunV2,
} from "../../lib/types";
import { statusClass } from "../product-detail/utils";

export interface V2NodeRunsPanelProps {
  product: CanonicalProductDetail;
  workflow: ProductWorkflowV2;
  node: WorkflowNodeV2 | null;
  onPreviewImage: (image: DownloadableImage) => void;
  onWorkflowChanged: () => Promise<unknown>;
}

const ACTIVE_STATUSES = new Set(["queued", "running"]);

export function V2NodeRunsPanel({
  product,
  workflow,
  node,
  onPreviewImage,
  onWorkflowChanged,
}: V2NodeRunsPanelProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const queryKey = ["v2-workflow-runs", product.id, workflow.id] as const;
  const runsQuery = useQuery({
    queryKey,
    queryFn: () => api.listWorkflowRunsV2(product.id, workflow.id),
    refetchInterval: (query) => query.state.data?.items.some((run) => run.status === "running")
      ? 1_200
      : false,
  });
  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey }),
      onWorkflowChanged(),
    ]);
  };
  const cancelMutation = useMutation({
    mutationFn: (runId: string) => api.cancelWorkflowRunV2(product.id, workflow.id, runId),
    onSuccess: refresh,
  });
  const retryMutation = useMutation({
    mutationFn: (runId: string) => api.retryWorkflowRunV2(product.id, workflow.id, runId),
    onSuccess: refresh,
  });
  const previewMutation = useMutation({
    mutationFn: (assetId: string) => api.getGalleryAsset(product.id, assetId),
    onSuccess: (asset) => onPreviewImage({
      previewUrl: api.toApiUrl(asset.preview_url),
      downloadUrl: api.toApiUrl(asset.download_url),
      filename: asset.original_filename,
      alt: asset.display_name,
    }),
  });

  if (runsQuery.isLoading) {
    return <PanelState icon={<Loader2 size={20} className="animate-spin" />} text={t("app.loading")} />;
  }
  if (runsQuery.isError) {
    return (
      <PanelState
        icon={<AlertCircle size={20} />}
        text={errorDetail(runsQuery.error, t("agentWorkbench.nodeEditor.loadFailed"))}
        action={t("agentWorkbench.retry")}
        onAction={() => void runsQuery.refetch()}
      />
    );
  }

  const runs = runsQuery.data?.items ?? [];
  const operationError = cancelMutation.error ?? retryMutation.error ?? previewMutation.error;
  return (
    <div className="space-y-3 pb-4" data-v2-node-runs-panel>
      <div className="flex items-center justify-between gap-2 text-xs text-zinc-500 dark:text-slate-400">
        <span>{t("agentWorkbench.runHistory.workflowCount", { count: runs.length })}</span>
        {runsQuery.isFetching ? <Loader2 size={13} className="animate-spin" /> : null}
      </div>

      {operationError ? (
        <div role="alert" className="rounded-lg border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          {errorDetail(operationError, t("agentWorkbench.nodeEditor.loadFailed"))}
        </div>
      ) : null}

      {runs.length ? runs.map((run) => (
        <WorkflowRunRecord
          key={run.id}
          run={run}
          workflow={runsQuery.data?.workflow ?? workflow}
          selectedNodeId={node?.id ?? null}
          cancelBusy={cancelMutation.isPending && cancelMutation.variables === run.id}
          retryBusy={retryMutation.isPending && retryMutation.variables === run.id}
          previewAssetId={previewMutation.isPending ? previewMutation.variables ?? null : null}
          onCancel={() => cancelMutation.mutate(run.id)}
          onRetry={() => retryMutation.mutate(run.id)}
          onPreview={(assetId) => previewMutation.mutate(assetId)}
        />
      )) : (
        <PanelState icon={<Clock3 size={20} />} text={t("agentWorkbench.runHistory.workflowEmpty")} compact />
      )}
    </div>
  );
}

function WorkflowRunRecord({
  run,
  workflow,
  selectedNodeId,
  cancelBusy,
  retryBusy,
  previewAssetId,
  onCancel,
  onRetry,
  onPreview,
}: {
  run: WorkflowRunV2;
  workflow: ProductWorkflowV2;
  selectedNodeId: string | null;
  cancelBusy: boolean;
  retryBusy: boolean;
  previewAssetId: string | null;
  onCancel: () => void;
  onRetry: () => void;
  onPreview: (assetId: string) => void;
}) {
  const { t } = useI18n();
  const active = run.status === "running";
  const fullRun = run.progress_metadata?.run_scope === "workflow";
  const nodesById = new Map(workflow.nodes.map((item) => [item.id, item]));
  return (
    <article className="config-bubble overflow-hidden rounded-lg shadow-sm" data-workflow-run-id={run.id}>
      <div className="p-3.5">
        <div className="flex min-w-0 items-start gap-2.5">
          <span className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md border ${statusClass(run.status)}`}>
            {active ? <Loader2 size={14} className="animate-spin" /> : <Workflow size={14} />}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs font-semibold text-zinc-900 dark:text-slate-100">
                {t(fullRun ? "agentWorkbench.runHistory.fullWorkflow" : "agentWorkbench.runHistory.singleNode")}
              </span>
              <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${statusClass(run.status)}`}>
                {t(`detail.nodeStatus.${run.status}`)}
              </span>
            </div>
            <div className="mt-1 space-y-0.5 text-[10px] text-zinc-500 dark:text-slate-400">
              <div>{t("agentWorkbench.runHistory.nodeCount", { count: run.node_runs.length })}</div>
              <div>{t("agentWorkbench.runHistory.started", { time: formatDateTime(run.started_at, t.locale) })}</div>
              {run.finished_at ? (
                <div>{t("agentWorkbench.runHistory.finished", { time: formatDateTime(run.finished_at, t.locale) })}</div>
              ) : null}
            </div>
          </div>
          <div className="flex shrink-0 gap-1.5">
            {run.is_retryable ? (
              <IconButton
                label={t("agentWorkbench.runHistory.retryRun")}
                disabled={retryBusy}
                onClick={onRetry}
              >
                {retryBusy ? <Loader2 size={14} className="animate-spin" /> : <RotateCcw size={14} />}
              </IconButton>
            ) : null}
            {run.is_cancelable ? (
              <IconButton
                label={t("agentWorkbench.runHistory.cancel")}
                disabled={cancelBusy}
                danger
                onClick={onCancel}
              >
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
            run={nodeRun}
            title={nodesById.get(nodeRun.node_id)?.title ?? nodeRun.node_id.slice(0, 8)}
            selected={nodeRun.node_id === selectedNodeId}
            previewBusy={previewAssetId === nodeRun.result_asset_id}
            onPreview={() => nodeRun.result_asset_id && onPreview(nodeRun.result_asset_id)}
          />
        ))}
      </div>
    </article>
  );
}

function NodeRunRecord({
  run,
  title,
  selected,
  previewBusy,
  onPreview,
}: {
  run: WorkflowNodeRunV2;
  title: string;
  selected: boolean;
  previewBusy: boolean;
  onPreview: () => void;
}) {
  const { t } = useI18n();
  const active = ACTIVE_STATUSES.has(run.status);
  const provider = [run.provider_name, run.provider_model].filter(Boolean).join(" / ");
  const hasEvidence = Boolean(
    run.requested_spec
    || run.effective_parameters
    || run.actual_media
    || run.compiled_prompt
    || provider,
  );
  return (
    <div className={selected ? "bg-indigo-50/70 dark:bg-violet-500/10" : ""}>
      <div className="flex min-w-0 items-start gap-2.5 px-3.5 py-3">
        <span className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md border ${statusClass(run.status)}`}>
          {active ? <Loader2 size={12} className="animate-spin" /> : <CircleDot size={12} />}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <span className="min-w-0 flex-1 truncate text-xs font-semibold text-zinc-800 dark:text-slate-100">{title}</span>
            <span className={`rounded-full border px-1.5 py-0.5 text-[9px] font-semibold ${statusClass(run.status)}`}>
              {t(`detail.nodeStatus.${run.status}`)}
            </span>
          </div>
          {run.failure_reason ? (
            <div className="mt-1.5 text-[10px] leading-4 text-red-600 dark:text-red-300">{run.failure_reason}</div>
          ) : null}
        </div>
        {run.result_asset_id ? (
          <IconButton label={t("agentWorkbench.runHistory.result")} disabled={previewBusy} onClick={onPreview}>
            {previewBusy ? <Loader2 size={13} className="animate-spin" /> : <Eye size={13} />}
          </IconButton>
        ) : null}
      </div>

      {hasEvidence ? (
        <details className="group border-t border-zinc-100 px-3.5 py-2.5 dark:border-slate-800">
          <summary className="flex cursor-pointer list-none items-center gap-2 text-[10px] font-semibold text-zinc-500 marker:hidden dark:text-slate-400 [&::-webkit-details-marker]:hidden">
            <FileText size={12} />
            <span>{t("agentWorkbench.runHistory.evidence")}</span>
          </summary>
          <div className="mt-2 space-y-2.5">
            {run.requested_spec ? (
              <RunDetail icon={<ImageIcon size={12} />} title={t("agentWorkbench.runHistory.requested")} rows={displayEntries(run.requested_spec)} />
            ) : null}
            {provider ? (
              <RunDetail icon={<RefreshCw size={12} />} title={t("agentWorkbench.runHistory.provider")} rows={[["", provider]]} />
            ) : null}
            {run.effective_parameters ? (
              <RunDetail icon={<RefreshCw size={12} />} title={t("agentWorkbench.runHistory.effective")} rows={displayEntries(run.effective_parameters)} />
            ) : null}
            {run.actual_media ? (
              <RunDetail
                icon={<ImageIcon size={12} />}
                title={t("agentWorkbench.runHistory.actual")}
                rows={[
                  ["type", run.actual_media.mime_type],
                  ["size", `${run.actual_media.width} x ${run.actual_media.height}`],
                  ["bytes", formatBytes(run.actual_media.byte_size)],
                ]}
              />
            ) : null}
            {run.compiled_prompt ? (
              <pre className="max-h-52 overflow-auto whitespace-pre-wrap break-words rounded-md bg-zinc-50 p-3 text-[10px] leading-5 text-zinc-600 dark:bg-[#0b1220] dark:text-slate-300">{run.compiled_prompt}</pre>
            ) : null}
          </div>
        </details>
      ) : null}
    </div>
  );
}

function RunDetail({ icon, title, rows }: { icon: React.ReactNode; title: string; rows: Array<[string, string]> }) {
  return (
    <div>
      <div className="flex items-center gap-1.5 text-[10px] font-semibold text-zinc-600 dark:text-slate-300">
        {icon}<span>{title}</span>
      </div>
      <dl className="mt-1.5 space-y-1">
        {rows.map(([key, value], index) => (
          <div key={`${key}:${index}`} className="grid grid-cols-[minmax(68px,0.35fr)_minmax(0,1fr)] gap-2 text-[10px] leading-4">
            <dt className="break-words text-zinc-400 dark:text-slate-500">{key}</dt>
            <dd className="break-words text-zinc-600 dark:text-slate-300">{value}</dd>
          </div>
        ))}
      </dl>
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
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border disabled:opacity-40 ${
        danger
          ? "border-red-200 text-red-600 hover:bg-red-50 dark:border-red-400/35 dark:text-red-200 dark:hover:bg-red-500/10"
          : "border-zinc-200 text-zinc-600 hover:border-indigo-300 hover:text-indigo-700 dark:border-slate-700 dark:text-slate-300 dark:hover:border-violet-400 dark:hover:text-violet-200"
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
  icon?: React.ReactNode;
  text: string;
  action?: string;
  onAction?: () => void;
  compact?: boolean;
}) {
  return (
    <div className={`flex flex-col items-center justify-center gap-2 px-6 text-center text-xs text-zinc-500 dark:text-slate-400 ${compact ? "min-h-[180px]" : "min-h-[260px]"}`}>
      {icon ? <span className="text-zinc-400 dark:text-slate-500">{icon}</span> : null}
      <span className="max-w-[260px] leading-5">{text}</span>
      {action && onAction ? <button type="button" onClick={onAction} className="mt-1 font-semibold text-indigo-700 hover:underline dark:text-violet-300">{action}</button> : null}
    </div>
  );
}

function displayEntries(value: object): Array<[string, string]> {
  return Object.entries(value).map(([key, item]) => [key, displayValue(item)]);
}

function displayValue(value: unknown): string {
  if (value == null) return "-";
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.detail;
  if (error instanceof Error) return error.message;
  return fallback;
}
