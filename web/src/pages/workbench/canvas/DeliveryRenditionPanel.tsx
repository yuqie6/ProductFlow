import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowUpLeft,
  Download,
  Eye,
  FileImage,
  Loader2,
  RefreshCw,
  WandSparkles,
} from "lucide-react";
import { useState } from "react";

import { api, ApiError } from "../../../lib/api";
import { formatDateTime } from "../../../lib/format";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type {
  DeliveryRenditionJob,
  ProductImageAsset,
  DeliveryPreset,
  WorkflowDeliverySpec,
} from "../../../lib/types";
import {
  deliverySpecKey,
  deliverySpecLabel,
  parseWorkflowDeliverySpec,
} from "./deliveryRenditions";

interface DeliveryRenditionPanelProps {
  productId: string;
  sourceAssetId?: string | null;
  deliverySpec?: unknown;
  onPreviewImage: (image: DownloadableImage) => void;
  onApplyDeliverySpec?: (deliverySpec: WorkflowDeliverySpec) => Promise<void>;
  applyDisabled?: boolean;
}

export function DeliveryRenditionPanel({
  productId,
  sourceAssetId = null,
  deliverySpec: rawDeliverySpec,
  onPreviewImage,
  onApplyDeliverySpec,
  applyDisabled = false,
}: DeliveryRenditionPanelProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const deliverySpec = parseWorkflowDeliverySpec(rawDeliverySpec);
  const deliverySpecInvalid = rawDeliverySpec !== undefined && rawDeliverySpec !== null && !deliverySpec;
  const configuredSpecKey = deliverySpec ? deliverySpecKey(deliverySpec) : null;
  const presetSection = (
    <DeliveryPresetSection
      currentSpecKey={configuredSpecKey}
      disabled={applyDisabled}
      onApply={onApplyDeliverySpec}
    />
  );
  const queryKey = ["delivery-renditions", sourceAssetId] as const;
  const jobsQuery = useQuery({
    queryKey,
    queryFn: () => api.listDeliveryRenditions(sourceAssetId!),
    enabled: Boolean(sourceAssetId),
    refetchInterval: (query) => query.state.data?.items.some(isActiveJob) ? 1200 : false,
  });

  const invalidateRenditionViews = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey }),
      queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] }),
      queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] }),
      queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] }),
    ]);
  };
  const createMutation = useMutation({
    mutationFn: (spec: WorkflowDeliverySpec) => api.createDeliveryRendition(sourceAssetId!, spec),
    onSuccess: invalidateRenditionViews,
    onError: () => queryClient.invalidateQueries({ queryKey }),
  });
  const retryMutation = useMutation({
    mutationFn: (jobId: string) => api.retryDeliveryRenditionJob(jobId),
    onSuccess: invalidateRenditionViews,
    onError: () => queryClient.invalidateQueries({ queryKey }),
  });
  const sourceMutation = useMutation({
    mutationFn: () => api.getGalleryAsset(productId, sourceAssetId!),
    onSuccess: (asset) => onPreviewImage(toDownloadableImage(asset)),
  });

  if (!sourceAssetId) {
    return (
      <div className="min-w-0 p-3" data-delivery-rendition-panel>
        {presetSection}
        <PanelState icon={<FileImage size={20} />} text={t("workbench.rendition.runImageNode")} />
      </div>
    );
  }
  if (deliverySpecInvalid) {
    return (
      <div className="min-w-0 p-3" data-delivery-rendition-panel>
        {presetSection}
        <PanelState text={t("workbench.rendition.invalidSpec")} />
      </div>
    );
  }
  if (!deliverySpec) {
    return (
      <div className="min-w-0 p-3" data-delivery-rendition-panel>
        {presetSection}
        <PanelState icon={<FileImage size={20} />} text={t("workbench.rendition.noSpec")} />
      </div>
    );
  }

  const jobs = jobsQuery.data?.items ?? [];
  const hasCurrentJob = jobs.some((job) => deliverySpecKey(job.delivery_spec) === configuredSpecKey);
  const error = firstError(jobsQuery.error, createMutation.error, retryMutation.error, sourceMutation.error);

  return (
    <div className="min-w-0 p-3" data-delivery-rendition-panel>
      {presetSection}
      <div className="flex min-w-0 items-start gap-2 border-b border-slate-200 pb-3 dark:border-slate-800">
        <div className="min-w-0 flex-1">
          <div className="text-xs font-semibold text-slate-950 dark:text-slate-100">
            {t("workbench.rendition.currentSpec")}
          </div>
          <div className="mt-1 text-sm font-semibold text-slate-800 dark:text-slate-100">
            {deliverySpecLabel(deliverySpec)}
          </div>
          <div className="mt-1.5 flex flex-wrap gap-1.5 text-[10px] text-slate-500 dark:text-slate-400">
            <span className="rounded bg-slate-200 px-1.5 py-1 dark:bg-slate-800">
              {t("workbench.rendition.fit")}: {t(`workbench.rendition.fit.${deliverySpec.fit}`)}
            </span>
            <span className="rounded bg-slate-200 px-1.5 py-1 dark:bg-slate-800">
              {t("workbench.rendition.maxBytes")}: {formatMaxBytes(deliverySpec.max_byte_size, t("workbench.rendition.noLimit"))}
            </span>
          </div>
        </div>
        <button
          type="button"
          onClick={() => sourceMutation.mutate()}
          disabled={sourceMutation.isPending}
          className="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-md border border-slate-200 bg-white px-2.5 text-[11px] font-semibold text-slate-600 hover:border-slate-400 hover:text-slate-950 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:text-white"
          title={t("workbench.rendition.viewSource")}
        >
          {sourceMutation.isPending ? <Loader2 size={13} className="animate-spin" /> : <ArrowUpLeft size={13} />}
          {t("workbench.rendition.viewSource")}
        </button>
      </div>

      {error ? (
        <div role="alert" className="mt-3 rounded-md border border-red-200 bg-red-50 px-2.5 py-2 text-xs text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          {error}
        </div>
      ) : null}

      {!hasCurrentJob && !jobsQuery.isLoading ? (
        <button
          type="button"
          onClick={() => createMutation.mutate(deliverySpec)}
          disabled={createMutation.isPending}
          className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-md bg-slate-950 px-3 text-xs font-semibold text-white hover:bg-emerald-700 disabled:opacity-50 dark:bg-emerald-600 dark:hover:bg-emerald-500"
        >
          {createMutation.isPending ? <Loader2 size={14} className="animate-spin" /> : <WandSparkles size={14} />}
          {t("workbench.rendition.create")}
        </button>
      ) : null}

      <div className="mt-4 flex items-center justify-between gap-2">
        <h3 className="text-xs font-semibold text-slate-800 dark:text-slate-200">
          {t("workbench.rendition.jobs")}
        </h3>
        {jobsQuery.isFetching ? <Loader2 size={13} className="animate-spin text-slate-400" /> : null}
      </div>

      {jobsQuery.isLoading ? (
        <PanelState compact icon={<Loader2 size={17} className="animate-spin" />} text={t("workbench.rendition.loading")} />
      ) : jobs.length === 0 ? (
        <PanelState compact text={t("workbench.rendition.empty")} />
      ) : (
        <div className="mt-2 space-y-2">
          {jobs.map((job) => (
            <RenditionJobRow
              key={job.id}
              job={job}
              current={deliverySpecKey(job.delivery_spec) === configuredSpecKey}
              retrying={retryMutation.isPending && retryMutation.variables === job.id}
              onPreview={() => job.result_asset && onPreviewImage(toDownloadableImage(job.result_asset))}
              onRetry={() => retryMutation.mutate(job.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function DeliveryPresetSection({
  currentSpecKey,
  disabled,
  onApply,
}: {
  currentSpecKey: string | null;
  disabled: boolean;
  onApply?: (deliverySpec: WorkflowDeliverySpec) => Promise<void>;
}) {
  const { t } = useI18n();
  const [applyingKey, setApplyingKey] = useState<string | null>(null);
  const [applyError, setApplyError] = useState<string | null>(null);
  const presetsQuery = useQuery({
    queryKey: ["delivery-presets"],
    queryFn: () => api.getDeliveryPresets(),
    staleTime: 60_000,
  });
  const selectedPreset = presetsQuery.data?.items.find((item) => (
    currentSpecKey !== null && deliverySpecKey(item.delivery_spec) === currentSpecKey
  )) ?? null;
  const metadataPreset = selectedPreset ?? presetsQuery.data?.items[0] ?? null;

  const applyPreset = async (preset: DeliveryPreset) => {
    if (!onApply || disabled || applyingKey || currentSpecKey === deliverySpecKey(preset.delivery_spec)) return;
    setApplyingKey(preset.key);
    setApplyError(null);
    try {
      await onApply(preset.delivery_spec);
    } catch (error) {
      setApplyError(firstError(error));
    } finally {
      setApplyingKey(null);
    }
  };

  return (
    <section className="mb-3 border-b border-slate-200 pb-3 dark:border-slate-800" data-delivery-presets>
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-semibold text-slate-900 dark:text-slate-100">{t("workbench.rendition.presets")}</h3>
        {presetsQuery.isFetching ? <Loader2 size={13} className="animate-spin text-slate-400" /> : null}
      </div>
      {presetsQuery.isPending ? (
        <p className="mt-2 text-[11px] text-slate-500 dark:text-slate-400">{t("workbench.rendition.presetsLoading")}</p>
      ) : presetsQuery.error ? (
        <div className="mt-2 flex items-center justify-between gap-2 text-[11px] text-red-600 dark:text-red-300" role="alert">
          <span>{firstError(presetsQuery.error) ?? t("workbench.rendition.presetsLoadFailed")}</span>
          <button
            type="button"
            onClick={() => void presetsQuery.refetch()}
            disabled={presetsQuery.isFetching}
            className="inline-flex h-7 shrink-0 items-center gap-1 rounded-md border border-red-200 px-2 font-semibold hover:bg-red-50 disabled:opacity-50 dark:border-red-400/30 dark:hover:bg-red-500/10"
          >
            <RefreshCw size={12} />
            {t("workbench.rendition.presetsRetry")}
          </button>
        </div>
      ) : presetsQuery.data ? (
        <>
          <div className="mt-2 grid grid-cols-2 gap-1.5" role="listbox" aria-label={t("workbench.rendition.presets")}>
            {presetsQuery.data.items.map((preset) => {
              const selected = selectedPreset?.key === preset.key;
              const applying = applyingKey === preset.key;
              return (
                <button
                  key={preset.key}
                  type="button"
                  role="option"
                  aria-selected={selected}
                  data-delivery-preset-key={preset.key}
                  onClick={() => void applyPreset(preset)}
                  disabled={disabled || Boolean(applyingKey) || selected || !onApply}
                  className={`min-w-0 rounded-md border px-2 py-2 text-left text-[10px] transition-colors disabled:cursor-default disabled:opacity-60 ${selected
                    ? "border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-500/50 dark:bg-emerald-500/10 dark:text-emerald-200"
                    : "border-slate-200 bg-white text-slate-700 hover:border-slate-400 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:border-slate-500"
                    }`}
                >
                  <span className="flex min-w-0 items-center gap-1.5">
                    {applying ? <Loader2 size={11} className="shrink-0 animate-spin" /> : null}
                    <span className="min-w-0 truncate font-semibold">{preset.title}</span>
                  </span>
                  <span className="mt-1 block truncate text-[9px] text-slate-500 dark:text-slate-400">
                    {preset.aspect_ratio} · {preset.applicable_image_type}
                  </span>
                </button>
              );
            })}
          </div>
          <div className="mt-2 space-y-1 text-[10px] leading-4 text-slate-500 dark:text-slate-400" data-delivery-preset-meta>
            <div className="flex flex-wrap gap-x-2 gap-y-0.5">
              <span className="font-semibold text-slate-700 dark:text-slate-200">{t("workbench.rendition.presetsCustom")}</span>
              <span>{t("workbench.rendition.presetsCustomHint")}</span>
            </div>
            {metadataPreset ? (
              <>
                <div>{t("workbench.rendition.presetReviewedAt", { date: metadataPreset.reviewed_at })}</div>
                <div className="truncate" title={metadataPreset.source}>{t("workbench.rendition.presetSource", { source: metadataPreset.source })}</div>
                <p>{metadataPreset.disclaimer}</p>
              </>
            ) : (
              <p>{t("workbench.rendition.presetDisclaimer")}</p>
            )}
            {applyError ? <p className="text-red-600 dark:text-red-300" role="alert">{t("workbench.rendition.presetApplyFailed")}: {applyError}</p> : null}
          </div>
        </>
      ) : null}
    </section>
  );
}

function RenditionJobRow({
  job,
  current,
  retrying,
  onPreview,
  onRetry,
}: {
  job: DeliveryRenditionJob;
  current: boolean;
  retrying: boolean;
  onPreview: () => void;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  const active = isActiveJob(job);
  return (
    <article className="overflow-hidden rounded-md border border-slate-200 bg-white dark:border-slate-700 dark:bg-slate-950/45">
      {job.result_asset ? (
        <button
          type="button"
          onClick={onPreview}
          className="relative block aspect-[16/9] w-full overflow-hidden bg-slate-100 dark:bg-slate-900"
          aria-label={t("workbench.rendition.preview")}
        >
          <img
            src={api.toApiUrl(job.result_asset.thumbnail_url)}
            alt={job.result_asset.display_name}
            className="h-full w-full object-contain"
          />
          <span className="absolute bottom-1.5 right-1.5 inline-flex h-7 w-7 items-center justify-center rounded bg-slate-950/80 text-white">
            <Eye size={13} />
          </span>
        </button>
      ) : null}
      <div className="p-2.5">
        <div className="flex min-w-0 items-center gap-2">
          <span className={`inline-flex h-2 w-2 shrink-0 rounded-full ${statusDotClass(job.status)}`} />
          <span className="min-w-0 flex-1 truncate text-xs font-semibold text-slate-900 dark:text-slate-100">
            {deliverySpecLabel(job.delivery_spec)}
          </span>
          {current ? (
            <span className="shrink-0 rounded bg-slate-100 px-1.5 py-0.5 text-[9px] font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">
              {t("workbench.rendition.current")}
            </span>
          ) : null}
        </div>
        <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[10px] text-slate-500 dark:text-slate-400">
          <span className={statusTextClass(job.status)}>
            {active ? <Loader2 size={10} className="mr-1 inline animate-spin" /> : null}
            {t(`workbench.rendition.status.${job.status}`)}
          </span>
          <span>{t("workbench.rendition.attempts", { count: job.attempts })}</span>
          <span>{formatDateTime(job.updated_at, t.locale)}</span>
        </div>
        {job.failure_reason ? (
          <p className="mt-2 break-words text-[11px] leading-5 text-red-600 dark:text-red-300">
            {job.failure_reason}
          </p>
        ) : null}
        {job.result_asset || (job.status === "failed" && job.is_retryable) ? (
          <div className="mt-2 flex flex-wrap gap-1.5 border-t border-slate-100 pt-2 dark:border-slate-800">
            {job.result_asset ? (
              <>
                <button type="button" onClick={onPreview} className="inline-flex h-8 items-center gap-1.5 rounded-md border border-slate-200 px-2 text-[11px] font-semibold text-slate-600 hover:text-slate-950 dark:border-slate-700 dark:text-slate-300 dark:hover:text-white">
                  <Eye size={12} /> {t("workbench.rendition.preview")}
                </button>
                <a href={api.toApiUrl(job.result_asset.download_url)} download={job.result_asset.original_filename} target="_blank" rel="noreferrer" className="inline-flex h-8 items-center gap-1.5 rounded-md border border-slate-200 px-2 text-[11px] font-semibold text-slate-600 hover:text-slate-950 dark:border-slate-700 dark:text-slate-300 dark:hover:text-white">
                  <Download size={12} /> {t("workbench.rendition.download")}
                </a>
              </>
            ) : null}
            {job.status === "failed" && job.is_retryable ? (
              <button type="button" onClick={onRetry} disabled={retrying} className="inline-flex h-8 items-center gap-1.5 rounded-md bg-amber-500 px-2 text-[11px] font-semibold text-slate-950 hover:bg-amber-400 disabled:opacity-50">
                {retrying ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
                {t("workbench.rendition.retry")}
              </button>
            ) : null}
          </div>
        ) : null}
      </div>
    </article>
  );
}

function PanelState({
  icon,
  text,
  compact = false,
}: {
  icon?: React.ReactNode;
  text: string;
  compact?: boolean;
}) {
  return (
    <div className={`flex flex-col items-center justify-center gap-2 px-5 text-center text-xs text-slate-500 dark:text-slate-400 ${compact ? "min-h-28" : "min-h-[260px]"}`}>
      {icon ? <span className="text-slate-400 dark:text-slate-500">{icon}</span> : null}
      <span className="max-w-[260px] leading-5">{text}</span>
    </div>
  );
}

function toDownloadableImage(asset: ProductImageAsset): DownloadableImage {
  return {
    previewUrl: api.toApiUrl(asset.preview_url),
    downloadUrl: api.toApiUrl(asset.download_url),
    filename: asset.original_filename,
    alt: asset.display_name,
  };
}

function isActiveJob(job: DeliveryRenditionJob): boolean {
  return job.status === "queued" || job.status === "running";
}

function firstError(...errors: unknown[]): string | null {
  const error = errors.find(Boolean);
  if (error instanceof ApiError) return error.detail;
  if (error instanceof Error) return error.message;
  return error ? String(error) : null;
}

function formatMaxBytes(value: number | null | undefined, noLimit: string): string {
  if (value == null) return noLimit;
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}

function statusDotClass(status: DeliveryRenditionJob["status"]): string {
  if (status === "succeeded") return "bg-emerald-500";
  if (status === "failed") return "bg-red-500";
  if (status === "running") return "bg-blue-500";
  if (status === "queued") return "bg-amber-500";
  return "bg-slate-400";
}

function statusTextClass(status: DeliveryRenditionJob["status"]): string {
  if (status === "succeeded") return "font-semibold text-emerald-700 dark:text-emerald-300";
  if (status === "failed") return "font-semibold text-red-600 dark:text-red-300";
  if (status === "running") return "font-semibold text-blue-700 dark:text-blue-300";
  if (status === "queued") return "font-semibold text-amber-700 dark:text-amber-300";
  return "font-semibold text-slate-500";
}
