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
import { Button, buttonVariants } from "../../../components/ui/button";
import { EmptyState as PanelState } from "../../../components/ui/empty-state";
import { PanelSkeleton } from "../../../components/ui/skeleton";
import { formatDateTime } from "../../../lib/format";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type {
  AgentProductImageTypeKey,
  DeliveryRenditionJob,
  ProductImageAsset,
  DeliveryPreset,
  WorkflowDeliverySpec,
} from "../../../lib/types";
import { AGENT_IMAGE_TYPE_TRANSLATIONS } from "../../product-create/imageTypeSelection";
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
      <div className="flex min-w-0 items-start gap-2 border-b border-border-l1 pb-3">
        <div className="min-w-0 flex-1">
          <div className="text-xs font-semibold text-text-primary">
            {t("workbench.rendition.currentSpec")}
          </div>
          <div className="mt-1 text-sm font-semibold text-text-primary">
            {deliverySpecLabel(deliverySpec)}
          </div>
          <div className="mt-1.5 flex flex-wrap gap-1.5 text-[10px] text-text-muted">
            <span className="rounded bg-surface-subtle px-1.5 py-1">
              {t("workbench.rendition.fit")}: {t(`workbench.rendition.fit.${deliverySpec.fit}`)}
            </span>
            <span className="rounded bg-surface-subtle px-1.5 py-1">
              {t("workbench.rendition.maxBytes")}: {formatMaxBytes(deliverySpec.max_byte_size, t("workbench.rendition.noLimit"))}
            </span>
          </div>
        </div>
        <Button
          variant="secondary"
          size="sm"
          className="shrink-0"
          onClick={() => sourceMutation.mutate()}
          busy={sourceMutation.isPending}
        >
          {sourceMutation.isPending ? null : <ArrowUpLeft size={13} aria-hidden="true" />}
          {t("workbench.rendition.viewSource")}
        </Button>
      </div>

      {error ? (
        <div role="alert" className="mt-3 rounded-md border border-state-error/30 bg-state-error-soft px-2.5 py-2 text-xs text-state-error">
          {error}
        </div>
      ) : null}

      {!hasCurrentJob && !jobsQuery.isLoading ? (
        <Button
          variant="primary"
          size="lg"
          className="mt-3 w-full"
          onClick={() => createMutation.mutate(deliverySpec)}
          busy={createMutation.isPending}
        >
          {createMutation.isPending ? null : <WandSparkles size={14} aria-hidden="true" />}
          {t("workbench.rendition.create")}
        </Button>
      ) : null}

      <div className="mt-4 flex items-center justify-between gap-2">
        <h3 className="text-xs font-semibold text-text-primary">
          {t("workbench.rendition.jobs")}
        </h3>
        {jobsQuery.isFetching ? <Loader2 size={13} className="animate-spin text-text-muted" /> : null}
      </div>

      {jobsQuery.isLoading ? (
        <PanelSkeleton compact rows={3} label={t("workbench.rendition.loading")} />
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
    <section className="mb-3 border-b border-border-l1 pb-3" data-delivery-presets>
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-semibold text-text-primary">{t("workbench.rendition.presets")}</h3>
        {presetsQuery.isFetching ? <Loader2 size={13} className="animate-spin text-text-muted" /> : null}
      </div>
      {presetsQuery.isPending ? (
        <PanelSkeleton compact rows={2} label={t("workbench.rendition.presetsLoading")} />
      ) : presetsQuery.error ? (
        <div className="mt-2 flex items-center justify-between gap-2 text-[11px] text-state-error" role="alert">
          <span>{firstError(presetsQuery.error) ?? t("workbench.rendition.presetsLoadFailed")}</span>
          <Button
            variant="secondary"
            size="sm"
            className="shrink-0"
            onClick={() => void presetsQuery.refetch()}
            disabled={presetsQuery.isFetching}
          >
            <RefreshCw size={12} aria-hidden="true" />
            {t("workbench.rendition.presetsRetry")}
          </Button>
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
                    ? "border-accent bg-accent-soft text-accent"
                    : "border-border-l1 bg-surface-raised text-text-secondary hover:border-border-l3"
                    }`}
                >
                  <span className="flex min-w-0 items-center gap-1.5">
                    {applying ? <Loader2 size={11} className="shrink-0 animate-spin" /> : null}
                    <span className="min-w-0 truncate font-semibold">{preset.title}</span>
                  </span>
                  <span className="mt-1 block truncate text-[9px] text-text-muted">
                    {preset.aspect_ratio} · {deliveryPresetImageTypeLabel(preset.applicable_image_type, t)}
                  </span>
                </button>
              );
            })}
          </div>
          {applyError ? (
            <p className="mt-2 text-[10px] leading-4 text-state-error" role="alert">
              {t("workbench.rendition.presetApplyFailed")}: {applyError}
            </p>
          ) : null}
        </>
      ) : null}
    </section>
  );
}

function deliveryPresetImageTypeLabel(
  imageType: string,
  t: ReturnType<typeof useI18n>["t"],
): string {
  const translations = AGENT_IMAGE_TYPE_TRANSLATIONS[imageType as AgentProductImageTypeKey];
  return translations ? t(translations.title) : t("workbench.rendition.imageTypeOther");
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
    <article className="overflow-hidden rounded-md border border-border-l1 bg-surface-raised">
      {job.result_asset ? (
        <button
          type="button"
          onClick={onPreview}
          className="relative block aspect-[16/9] w-full overflow-hidden bg-surface-subtle"
          aria-label={t("workbench.rendition.preview")}
        >
          <img
            src={api.toApiUrl(job.result_asset.thumbnail_url)}
            alt={job.result_asset.display_name}
            className="h-full w-full object-contain"
          />
          <span className="absolute bottom-1.5 right-1.5 inline-flex h-7 w-7 items-center justify-center rounded bg-surface-inverse/80 text-surface-raised">
            <Eye size={13} />
          </span>
        </button>
      ) : null}
      <div className="p-2.5">
        <div className="flex min-w-0 items-center gap-2">
          <span className={`inline-flex h-2 w-2 shrink-0 rounded-full ${statusDotClass(job.status)}`} />
          <span className="min-w-0 flex-1 truncate text-xs font-semibold text-text-primary">
            {deliverySpecLabel(job.delivery_spec)}
          </span>
          {current ? (
            <span className="shrink-0 rounded bg-surface-subtle px-1.5 py-0.5 text-[9px] font-semibold text-text-secondary">
              {t("workbench.rendition.current")}
            </span>
          ) : null}
        </div>
        <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[10px] text-text-muted">
          <span className={statusTextClass(job.status)}>
            {active ? <Loader2 size={10} className="mr-1 inline animate-spin" /> : null}
            {t(`workbench.rendition.status.${job.status}`)}
          </span>
          <span>{t("workbench.rendition.attempts", { count: job.attempts })}</span>
          <span>{formatDateTime(job.updated_at, t.locale)}</span>
        </div>
        {job.failure_reason ? (
          <p className="mt-2 break-words text-[11px] leading-5 text-state-error">
            {job.failure_reason}
          </p>
        ) : null}
        {job.result_asset || (job.status === "failed" && job.is_retryable) ? (
          <div className="mt-2 flex flex-wrap gap-1.5 border-t border-border-l1 pt-2">
            {job.result_asset ? (
              <>
                <Button variant="secondary" size="sm" onClick={onPreview}>
                  <Eye size={12} aria-hidden="true" /> {t("workbench.rendition.preview")}
                </Button>
                <a
                  href={api.toApiUrl(job.result_asset.download_url)}
                  download={job.result_asset.original_filename}
                  target="_blank"
                  rel="noreferrer"
                  className={buttonVariants({ variant: "secondary", size: "sm" })}
                >
                  <Download size={12} aria-hidden="true" /> {t("workbench.rendition.download")}
                </a>
              </>
            ) : null}
            {job.status === "failed" && job.is_retryable ? (
              <Button variant="secondary" size="sm" onClick={onRetry} busy={retrying}>
                {retrying ? null : <RefreshCw size={12} aria-hidden="true" />}
                {t("workbench.rendition.retry")}
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>
    </article>
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
  if (status === "succeeded") return "bg-state-success";
  if (status === "failed") return "bg-state-error";
  if (status === "running") return "bg-accent";
  if (status === "queued") return "bg-state-warning";
  return "bg-text-muted";
}

function statusTextClass(status: DeliveryRenditionJob["status"]): string {
  if (status === "succeeded") return "font-semibold text-state-success";
  if (status === "failed") return "font-semibold text-state-error";
  if (status === "running") return "font-semibold text-accent";
  if (status === "queued") return "font-semibold text-state-warning";
  return "font-semibold text-text-muted";
}
