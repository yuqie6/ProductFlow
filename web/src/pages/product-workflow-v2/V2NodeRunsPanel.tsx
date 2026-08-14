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
} from "lucide-react";

import { ApiError, api } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type {
  CanonicalProductDetail,
  WorkflowNodeRunV2,
  WorkflowNodeV2,
} from "../../lib/types";
import { statusClass } from "../product-detail/utils";

export interface V2NodeRunsPanelProps {
  product: CanonicalProductDetail;
  node: WorkflowNodeV2 | null;
  onPreviewImage: (image: DownloadableImage) => void;
  onWorkflowChanged: () => Promise<unknown>;
}

const ACTIVE_STATUSES = new Set(["queued", "running"]);

export function V2NodeRunsPanel({
  product,
  node,
  onPreviewImage,
  onWorkflowChanged,
}: V2NodeRunsPanelProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const queryKey = ["v2-workflow-node-runs", node?.id] as const;
  const runsQuery = useQuery({
    queryKey,
    queryFn: () => api.listWorkflowNodeRunsV2(node!.id),
    enabled: Boolean(node),
    refetchInterval: (query) => query.state.data?.items.some((run) => ACTIVE_STATUSES.has(run.status))
      ? 1_200
      : false,
  });
  const cancelMutation = useMutation({
    mutationFn: (runId: string) => api.cancelWorkflowNodeRunV2(runId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey }),
        onWorkflowChanged(),
      ]);
    },
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

  if (!node) {
    return <PanelState icon={<CircleDot size={20} />} text={t("agentWorkbench.runHistory.select")} />;
  }
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
  return (
    <div className="space-y-3 pb-4" data-v2-node-runs-panel>
      <div className="flex items-center justify-between gap-2 text-xs text-zinc-500 dark:text-slate-400">
        <span>{t("agentWorkbench.runHistory.count", { count: runs.length })}</span>
        {runsQuery.isFetching ? <Loader2 size={13} className="animate-spin" /> : null}
      </div>

      {cancelMutation.error || previewMutation.error ? (
        <div role="alert" className="rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          {errorDetail(cancelMutation.error ?? previewMutation.error, t("agentWorkbench.nodeEditor.loadFailed"))}
        </div>
      ) : null}

      {runs.length ? runs.map((run) => (
        <RunRecord
          key={run.id}
          run={run}
          cancelBusy={cancelMutation.isPending && cancelMutation.variables === run.id}
          previewBusy={previewMutation.isPending && previewMutation.variables === run.result_asset_id}
          onCancel={() => cancelMutation.mutate(run.id)}
          onPreview={() => run.result_asset_id && previewMutation.mutate(run.result_asset_id)}
        />
      )) : (
        <PanelState icon={<Clock3 size={20} />} text={t("agentWorkbench.runHistory.empty")} compact />
      )}
    </div>
  );
}

function RunRecord({
  run,
  cancelBusy,
  previewBusy,
  onCancel,
  onPreview,
}: {
  run: WorkflowNodeRunV2;
  cancelBusy: boolean;
  previewBusy: boolean;
  onCancel: () => void;
  onPreview: () => void;
}) {
  const { t } = useI18n();
  const active = ACTIVE_STATUSES.has(run.status);
  const provider = [run.provider_name, run.provider_model].filter(Boolean).join(" / ");
  return (
    <article className="config-bubble overflow-hidden rounded-2xl shadow-sm">
      <div className="p-3.5">
        <div className="flex min-w-0 items-start gap-2.5">
          <span className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border ${statusClass(run.status)}`}>
            {active ? <Loader2 size={13} className="animate-spin" /> : <CircleDot size={13} />}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${statusClass(run.status)}`}>
                {t(`detail.nodeStatus.${run.status}`)}
              </span>
              {run.prompt_artifact_version_id ? (
                <span className="max-w-full truncate rounded bg-zinc-100 px-1.5 py-0.5 font-mono text-[9px] text-zinc-500 dark:bg-slate-800 dark:text-slate-300" title={run.prompt_artifact_version_id}>
                  {run.prompt_artifact_version_id.slice(0, 8)}
                </span>
              ) : null}
            </div>
            <div className="mt-1.5 space-y-0.5 text-[10px] text-zinc-500 dark:text-slate-400">
              <div>{t("agentWorkbench.runHistory.started", { time: formatDateTime(run.started_at, t.locale) })}</div>
              {run.finished_at ? <div>{t("agentWorkbench.runHistory.finished", { time: formatDateTime(run.finished_at, t.locale) })}</div> : null}
            </div>
          </div>
          {active ? (
            <button
              type="button"
              onClick={onCancel}
              disabled={cancelBusy}
              className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-red-200 text-red-600 hover:bg-red-50 disabled:opacity-40 dark:border-red-400/35 dark:text-red-200 dark:hover:bg-red-500/10"
              aria-label={t("agentWorkbench.runHistory.cancel")}
              title={t("agentWorkbench.runHistory.cancel")}
            >
              {cancelBusy ? <Loader2 size={14} className="animate-spin" /> : <OctagonX size={14} />}
            </button>
          ) : null}
        </div>

        {run.failure_reason ? (
          <div className="mt-3 rounded-xl border border-red-200 bg-red-50 px-3 py-2 text-[11px] leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
            {run.failure_reason}
          </div>
        ) : null}

        {run.result_asset_id ? (
          <button
            type="button"
            onClick={onPreview}
            disabled={previewBusy}
            className="mt-3 inline-flex h-9 w-full items-center justify-center gap-2 rounded-xl border border-zinc-200 bg-white text-xs font-semibold text-zinc-700 hover:border-indigo-300 hover:text-indigo-700 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:border-violet-400 dark:hover:text-violet-200"
          >
            {previewBusy ? <Loader2 size={14} className="animate-spin" /> : <Eye size={14} />}
            {t("agentWorkbench.runHistory.result")}
          </button>
        ) : null}
      </div>

      <div className="divide-y divide-zinc-100 border-t border-zinc-100 dark:divide-slate-800 dark:border-slate-800">
        {run.requested_spec ? (
          <RunDetail
            icon={<ImageIcon size={13} />}
            title={t("agentWorkbench.runHistory.requested")}
            rows={displayEntries(run.requested_spec)}
          />
        ) : null}
        {provider ? (
          <RunDetail icon={<RefreshCw size={13} />} title={t("agentWorkbench.runHistory.provider")} rows={[["", provider]]} />
        ) : null}
        {run.effective_parameters ? (
          <RunDetail
            icon={<RefreshCw size={13} />}
            title={t("agentWorkbench.runHistory.effective")}
            rows={displayEntries(run.effective_parameters)}
          />
        ) : null}
        {run.actual_media ? (
          <RunDetail
            icon={<ImageIcon size={13} />}
            title={t("agentWorkbench.runHistory.actual")}
            rows={[
              ["type", run.actual_media.mime_type],
              ["size", `${run.actual_media.width} x ${run.actual_media.height}`],
              ["bytes", formatBytes(run.actual_media.byte_size)],
            ]}
          />
        ) : null}
        {run.compiled_prompt ? (
          <details className="group px-3.5 py-3">
            <summary className="flex cursor-pointer list-none items-center gap-2 text-xs font-semibold text-zinc-700 marker:hidden dark:text-slate-200 [&::-webkit-details-marker]:hidden">
              <FileText size={13} />
              <span className="min-w-0 flex-1">{t("agentWorkbench.runHistory.compiledPrompt")}</span>
            </summary>
            <pre className="mt-2 max-h-52 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-zinc-50 p-3 text-[10px] leading-5 text-zinc-600 dark:bg-[#0b1220] dark:text-slate-300">{run.compiled_prompt}</pre>
          </details>
        ) : null}
      </div>
    </article>
  );
}

function RunDetail({ icon, title, rows }: { icon: React.ReactNode; title: string; rows: Array<[string, string]> }) {
  return (
    <details className="group px-3.5 py-3">
      <summary className="flex cursor-pointer list-none items-center gap-2 text-xs font-semibold text-zinc-700 marker:hidden dark:text-slate-200 [&::-webkit-details-marker]:hidden">
        {icon}<span className="min-w-0 flex-1">{title}</span>
      </summary>
      <dl className="mt-2 space-y-1.5">
        {rows.map(([key, value], index) => (
          <div key={`${key}:${index}`} className="grid grid-cols-[minmax(72px,0.4fr)_minmax(0,1fr)] gap-2 text-[10px] leading-4">
            <dt className="break-words text-zinc-400 dark:text-slate-500">{key}</dt>
            <dd className="break-words text-zinc-600 dark:text-slate-300">{value}</dd>
          </div>
        ))}
      </dl>
    </details>
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
