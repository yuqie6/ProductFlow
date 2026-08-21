import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  BadgeCheck,
  BookOpen,
  Boxes,
  Check,
  CircleAlert,
  CircleHelp,
  Eye,
  Factory,
  ImagePlus,
  Images,
  Layers,
  ListChecks,
  Loader2,
  Minus,
  Package,
  Plus,
  RotateCw,
  Ruler,
  ShieldCheck,
  Sparkles,
  Truck,
  X,
  ZoomIn,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { ImageDropZone } from "../../components/ImageDropZone";
import { useI18n } from "../../lib/preferences";
import type {
  AgentProductImageTypeKey,
  AgentProductWorkspaceOptions,
} from "../../lib/types";
import {
  AGENT_IMAGE_TYPE_TRANSLATIONS,
  agentImageTotal,
  type AgentImageTypeSelectionDraft,
} from "./imageTypeSelection";

interface AgentProductCreateFormProps {
  productName: string;
  isProductNameReadOnly: boolean;
  options: AgentProductWorkspaceOptions | null;
  selections: readonly AgentImageTypeSelectionDraft[];
  referenceFiles: readonly File[];
  isOptionsLoading: boolean;
  isOptionsError: boolean;
  isSubmitting: boolean;
  editingLocked: boolean;
  primaryActionLabel?: string;
  error: string;
  onProductNameChange: (name: string) => void;
  onToggleImageType: (key: AgentProductImageTypeKey, selected: boolean) => void;
  onQuantityChange: (key: AgentProductImageTypeKey, quantity: number) => void;
  onAddReferenceFiles: (files: File[]) => void;
  onRemoveReferenceFile: (index: number) => void;
  onRetryOptions: () => void;
  onSubmit: () => void;
  onDirectCreate?: () => void;
  isDirectCreating?: boolean;
}

const IMAGE_TYPE_ICONS: Partial<Record<AgentProductImageTypeKey, LucideIcon>> = {
  hero: Sparkles,
  selling_point: BadgeCheck,
  scene: Layers,
  detail: ZoomIn,
  sku: Boxes,
  dimensions: Ruler,
  specifications: ListChecks,
  after_sales: ShieldCheck,
  brand_story: BookOpen,
  precautions: CircleAlert,
  certification: BadgeCheck,
  faq: CircleHelp,
  factory: Factory,
  packaging: Package,
  shipping: Truck,
};

const STAGE_ICONS: [LucideIcon, LucideIcon, LucideIcon] = [Package, Images, ImagePlus];

const stepClass =
  "flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-[#6366f1] to-[#8b5cf6] text-[11px] font-bold text-white shadow-[0_3px_8px_rgb(99_102_241/0.3)]";

function cardShellClass(isActive: boolean): string {
  const base =
    "relative rounded-2xl border bg-surface-raised p-5 shadow-sm transition-[border-color,box-shadow] duration-200 sm:p-6";
  if (isActive) {
    return `${base} border-accent/40 shadow-[0_12px_44px_-16px_rgb(99_102_241/0.35)] ring-1 ring-accent/15`;
  }
  return `${base} border-border-l1 hover:border-border-l3`;
}

function formatFileSize(bytes: number, locale: string): string {
  const value = bytes >= 1024 * 1024 ? bytes / (1024 * 1024) : bytes / 1024;
  const unit = bytes >= 1024 * 1024 ? "MB" : "KB";
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value)} ${unit}`;
}

export function AgentProductCreateForm({
  productName,
  isProductNameReadOnly,
  options,
  selections,
  referenceFiles,
  isOptionsLoading,
  isOptionsError,
  isSubmitting,
  editingLocked,
  primaryActionLabel,
  error,
  onProductNameChange,
  onToggleImageType,
  onQuantityChange,
  onAddReferenceFiles,
  onRemoveReferenceFile,
  onRetryOptions,
  onSubmit,
  onDirectCreate,
  isDirectCreating = false,
}: AgentProductCreateFormProps) {
  const { locale, t } = useI18n();
  const [previewIndex, setPreviewIndex] = useState<number | null>(null);
  const selectedByKey = useMemo(
    () => new Map(selections.map((selection) => [selection.key, selection])),
    [selections],
  );
  const sortedOptions = useMemo(
    () => [...(options?.image_types ?? [])].sort((left, right) => left.order - right.order),
    [options],
  );
  const totalImages = agentImageTotal(selections);
  const previewUrls = useMemo(
    () =>
      referenceFiles.map((file) =>
        typeof URL !== "undefined" && typeof URL.createObjectURL === "function"
          ? URL.createObjectURL(file)
          : "",
      ),
    [referenceFiles],
  );

  useEffect(
    () => () => {
      previewUrls.forEach((url) => {
        if (url) URL.revokeObjectURL(url);
      });
    },
    [previewUrls],
  );

  useEffect(() => {
    if (previewIndex !== null && previewIndex >= referenceFiles.length) {
      setPreviewIndex(null);
    }
  }, [previewIndex, referenceFiles.length]);

  const previewFile = previewIndex === null ? null : referenceFiles[previewIndex] ?? null;
  const previewUrl = previewIndex === null ? "" : previewUrls[previewIndex] ?? "";
  const minReferences = options?.limits.min_reference_images ?? 0;
  const maxReferences = options?.limits.max_reference_images ?? 0;
  const nameReady = productName.trim().length > 0;
  const planReady = selections.length > 0;
  const referenceReady = referenceFiles.length >= minReferences;

  const stageCard = (stage: 1 | 2 | 3, meta: string, isActive: boolean, body: ReactNode) => {
    const Icon = STAGE_ICONS[stage - 1];
    return (
      <section aria-labelledby={`agent-stage-${stage}-title`} className={cardShellClass(isActive)}>
        <div className="mb-4 flex items-start gap-3 border-b border-border-l2 pb-4">
          <span className={stepClass} aria-hidden="true">
            {stage}
          </span>
          <div className="min-w-0 flex-1">
            <h2
              id={`agent-stage-${stage}-title`}
              className="flex items-center gap-2 text-sm font-semibold text-text-primary"
            >
              <Icon size={16} className="text-accent" aria-hidden="true" />
              {stage === 1 ? t("agentCreate.productName") : stage === 2 ? t("agentCreate.imageTypes") : t("agentCreate.references")}
            </h2>
            <p className="mt-0.5 text-xs leading-4 text-text-muted">{meta}</p>
          </div>
          {isActive ? (
            <span className="mt-0.5 hidden shrink-0 items-center gap-1 rounded-full border border-accent/30 bg-accent-soft px-2.5 py-1 text-[11px] font-semibold text-accent-strong sm:flex">
              <Check size={12} aria-hidden="true" />
              {t("agentCreate.stageReady")}
            </span>
          ) : null}
        </div>
        {body}
      </section>
    );
  };

  const stepper = (key: AgentProductImageTypeKey, title: string) => {
    if (!options) return null;
    const selected = selectedByKey.get(key);
    if (!selected) {
      return (
        <span className="inline-flex h-8 items-center gap-1 rounded-full border border-accent/70 bg-accent/20 px-3.5 text-xs font-semibold text-accent-strong transition-colors group-hover:border-accent group-hover:bg-accent/25">
          <Plus size={12} aria-hidden="true" />
          {t("agentCreate.add")}
        </span>
      );
    }
    return (
      <div className="grid h-9 w-28 grid-cols-[34px_44px_34px] overflow-hidden rounded-full border border-accent/35 bg-surface-raised shadow-sm">
        <button
          type="button"
          title={t("agentCreate.decrease", { title })}
          aria-label={t("agentCreate.decrease", { title })}
          disabled={isSubmitting || editingLocked || selected.quantity <= options.limits.min_images_per_type}
          onClick={() => onQuantityChange(key, selected.quantity - 1)}
          className="flex items-center justify-center rounded-l-full text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary disabled:opacity-35"
        >
          <Minus size={14} />
        </button>
        <input
          aria-label={`${title} ${t("agentCreate.quantity")}`}
          type="number"
          min={options.limits.min_images_per_type}
          max={options.limits.max_images_per_type}
          step={1}
          disabled={isSubmitting || editingLocked}
          value={selected.quantity}
          onChange={(event) => onQuantityChange(key, Number(event.target.value))}
          className="h-full w-full border-x border-accent/25 bg-transparent text-center text-sm font-semibold tabular-nums text-text-primary outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
        />
        <button
          type="button"
          title={t("agentCreate.increase", { title })}
          aria-label={t("agentCreate.increase", { title })}
          disabled={isSubmitting || editingLocked || selected.quantity >= options.limits.max_images_per_type}
          onClick={() => onQuantityChange(key, selected.quantity + 1)}
          className="flex items-center justify-center rounded-r-full text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary disabled:opacity-35"
        >
          <Plus size={14} />
        </button>
      </div>
    );
  };

  return (
    <form
      data-agent-product-intake-form
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
      className="mt-6 min-w-0 space-y-4 pb-40"
    >
      {stageCard(1, t("agentCreate.nameHint"), nameReady, (
        <label htmlFor="agent-product-name" className="block">
          <span className="sr-only">{t("agentCreate.productName")}</span>
          <input
            id="agent-product-name"
            autoFocus={!isProductNameReadOnly}
            autoComplete="off"
            value={productName}
            readOnly={isProductNameReadOnly}
            disabled={isSubmitting || editingLocked}
            onChange={(event) => onProductNameChange(event.target.value)}
            placeholder={t("agentCreate.namePlaceholder")}
            className="input-premium h-12 w-full px-4 text-[15px] font-medium text-text-primary read-only:cursor-not-allowed read-only:bg-surface-subtle/70 read-only:text-text-secondary disabled:cursor-not-allowed disabled:opacity-60"
          />
        </label>
      ))}

      {stageCard(2, t("agentCreate.imageTypesMeta", { selected: selections.length, total: totalImages }), planReady, (
        <div className="space-y-3">
          {isOptionsLoading ? (
            <div className="flex min-h-28 items-center justify-center text-sm text-text-muted">
              <Loader2 size={17} className="mr-2 animate-spin text-accent" />
              {t("agentCreate.optionsLoading")}
            </div>
          ) : null}

          {isOptionsError ? (
            <div className="flex min-h-28 flex-col items-center justify-center gap-3 rounded-xl border border-state-error/30 bg-state-error/10 px-4 py-5 text-sm text-text-primary">
              <span className="text-text-secondary">{t("agentCreate.optionsFailed")}</span>
              <button
                type="button"
                onClick={onRetryOptions}
                className="inline-flex h-9 items-center gap-2 rounded-lg border border-border-l3 bg-surface-raised px-3 font-medium text-text-secondary transition-colors hover:border-accent/50 hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                <RotateCw size={14} />
                {t("agentCreate.retryOptions")}
              </button>
            </div>
          ) : null}

          {options ? (
            <div className="grid gap-2.5 sm:grid-cols-2 xl:grid-cols-3">
              {sortedOptions.map((option) => {
                const selected = selectedByKey.get(option.key);
                const translations = AGENT_IMAGE_TYPE_TRANSLATIONS[option.key];
                const title = translations ? t(translations.title) : option.title;
                const description = translations ? t(translations.description) : option.description;
                const Icon = IMAGE_TYPE_ICONS[option.key] ?? Images;
                const iconColor = selected ? "text-accent" : "text-text-muted";
                return (
                  <label
                    key={option.key}
                    data-image-type={option.key}
                    className={`group flex min-h-32 cursor-pointer flex-col rounded-xl border p-4 transition-[border-color,background-color,box-shadow] duration-200 ${
                      selected
                        ? "border-accent/60 bg-accent-soft/60 shadow-[0_8px_24px_-14px_rgb(99_102_241/0.45)] dark:bg-accent/10"
                        : "border-border-l1 bg-surface-base/60 hover:border-border-l3 hover:bg-surface-raised"
                    }`}
                  >
                    <span className="flex min-h-10 items-start gap-2.5">
                      <input
                        type="checkbox"
                        checked={Boolean(selected)}
                        disabled={isSubmitting || editingLocked}
                        onChange={(event) => onToggleImageType(option.key, event.target.checked)}
                        className="peer sr-only"
                      />
                      <span
                        aria-hidden="true"
                        className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-md border border-slate-400/80 bg-surface-raised transition-colors group-hover:border-accent/60 peer-focus-visible:ring-2 peer-focus-visible:ring-accent peer-focus-visible:ring-offset-2 peer-disabled:opacity-50 peer-checked:hidden dark:border-slate-500 dark:peer-focus-visible:ring-offset-surface-raised"
                      />
                      <span
                        aria-hidden="true"
                        className="mt-0.5 hidden h-5 w-5 shrink-0 items-center justify-center rounded-md bg-gradient-to-br from-[#6366f1] to-[#8b5cf6] text-white shadow-[0_2px_6px_rgb(99_102_241/0.4)] transition-colors peer-focus-visible:ring-2 peer-focus-visible:ring-accent peer-focus-visible:ring-offset-2 peer-disabled:opacity-50 peer-checked:flex dark:peer-focus-visible:ring-offset-surface-raised"
                      >
                        <Check size={12} strokeWidth={3} aria-hidden="true" />
                      </span>
                      <span className="flex min-w-0 items-start gap-1.5 text-sm font-semibold leading-5 text-text-primary">
                        <Icon size={15} className={`${iconColor} mt-0.5 shrink-0`} aria-hidden="true" />
                        <span className="min-w-0 line-clamp-2">{title}</span>
                      </span>
                    </span>
                    <span className="mt-1.5 block text-xs leading-5 text-text-muted">{description}</span>
                    <span className="mt-auto flex justify-end pt-2.5">{stepper(option.key, title)}</span>
                  </label>
                );
              })}
            </div>
          ) : null}
        </div>
      ))}

      {stageCard(3, t("agentCreate.referencesMeta", { count: referenceFiles.length, max: maxReferences }) || t("agentCreate.references"), referenceReady, (
        <div className="space-y-3">
          {options && referenceFiles.length < maxReferences ? (
            <ImageDropZone
              multiple
              disabled={isSubmitting || editingLocked}
              ariaLabel={t("agentCreate.uploadAria")}
              onFiles={onAddReferenceFiles}
              className="glass-empty-state flex min-h-27 cursor-pointer flex-col items-center justify-center gap-1 px-4 text-center"
              activeClassName="!border-accent !bg-accent-soft text-accent-strong"
            >
              {({ isDragging }) => (
                <>
                  <span className="flex h-10 w-10 items-center justify-center rounded-full bg-accent/10 text-accent">
                    <ImagePlus size={20} aria-hidden="true" />
                  </span>
                  <span className="mt-1 text-sm font-semibold text-text-primary">
                    {isDragging ? t("agentCreate.uploadDrop") : t("agentCreate.uploadTitle")}
                  </span>
                  <span className="text-xs leading-4 text-text-muted">
                    {t("agentCreate.uploadHint", { max: maxReferences })}
                  </span>
                </>
              )}
            </ImageDropZone>
          ) : null}

          {referenceFiles.length > 0 ? (
            <div className="flex gap-2.5 overflow-x-auto pb-2">
              {referenceFiles.map((file, index) => (
                <article
                  key={`${file.name}:${file.size}:${file.lastModified}:${index}`}
                  className="group relative w-25 shrink-0 overflow-hidden rounded-xl border border-border-l1 bg-surface-raised shadow-sm"
                >
                  <button
                    type="button"
                    onClick={() => setPreviewIndex(index)}
                    aria-label={t("agentCreate.preview", { name: file.name })}
                    className="relative block h-20 w-full overflow-hidden bg-surface-subtle focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent"
                  >
                    {previewUrls[index] ? (
                      <img src={previewUrls[index]} alt="" className="h-full w-full object-contain" />
                    ) : null}
                    <span className="absolute inset-0 flex items-center justify-center bg-black/0 text-white opacity-0 transition group-hover:bg-black/30 group-hover:opacity-100 group-focus-within:bg-black/30 group-focus-within:opacity-100">
                      <Eye size={18} />
                    </span>
                  </button>
                  <div className="min-w-0 px-2 py-1.5">
                    <div className="truncate text-[11px] font-medium text-text-primary">{file.name}</div>
                    <div className="text-[10px] text-text-muted">{formatFileSize(file.size, locale)}</div>
                  </div>
                  <button
                    type="button"
                    title={t("agentCreate.remove", { name: file.name })}
                    aria-label={t("agentCreate.remove", { name: file.name })}
                    disabled={isSubmitting || editingLocked}
                    onClick={() => onRemoveReferenceFile(index)}
                    className="absolute right-1.5 top-1.5 flex h-7 w-7 items-center justify-center rounded-full bg-surface-inverse/70 text-surface-inverse-fg backdrop-blur-sm transition-opacity hover:bg-state-error hover:text-white focus:opacity-100 group-hover:opacity-100 disabled:opacity-40 sm:opacity-0"
                  >
                    <X size={13} />
                  </button>
                </article>
              ))}
            </div>
          ) : null}
        </div>
      ))}

      {error ? (
        <div
          role="alert"
          className="animate-spring-slide-in rounded-xl border border-state-error/30 bg-state-error/10 px-4 py-3 text-sm leading-5 text-state-error"
        >
          {error}
        </div>
      ) : null}

      <div className="fixed inset-x-0 bottom-0 z-30 border-t border-border-l1 bg-surface-raised shadow-[0_-10px_30px_-12px_rgb(2_6_23/0.25)] dark:border-slate-700 dark:bg-[#0b1424] dark:shadow-[0_-14px_36px_rgb(0_0_0/0.45)]">
        <div className="mx-auto flex w-full max-w-[920px] flex-col-reverse gap-3 px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-6">
          <div className="flex flex-wrap gap-x-5 gap-y-1.5 text-xs text-text-secondary">
            <span>
              {t("agentCreate.selectedTypes")}:{" "}
              <strong className="font-semibold tabular-nums text-text-primary">{selections.length}</strong>
            </span>
            <span>
              {t("agentCreate.plannedImages")}:{" "}
              <strong className="font-semibold tabular-nums text-text-primary">{totalImages}</strong>
            </span>
            <span>
              {t("agentCreate.referenceImages")}:{" "}
              <strong className="font-semibold tabular-nums text-text-primary">{referenceFiles.length}</strong>
            </span>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            {onDirectCreate ? (
              <button
                type="button"
                disabled={isSubmitting || isDirectCreating}
                onClick={onDirectCreate}
                className="inline-flex h-11 shrink-0 items-center justify-center gap-2 rounded-xl border border-indigo-200 px-6 text-sm font-semibold text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2"
              >
                {isDirectCreating ? <Loader2 size={16} className="animate-spin" /> : null}
                {t("agentCreate.submitDirect")}
              </button>
            ) : null}
            <button
              type="submit"
              disabled={isSubmitting || isDirectCreating}
              className="btn-primary-spring inline-flex h-11 shrink-0 items-center justify-center gap-2 rounded-xl px-6 text-sm font-semibold focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2"
            >
              {isSubmitting ? (
                <Loader2 size={16} className="animate-spin" />
              ) : (
                <Sparkles size={16} aria-hidden="true" />
              )}
              {isSubmitting ? t("agentCreate.submitting") : primaryActionLabel ?? t("agentCreate.submit")}
            </button>
          </div>
        </div>
      </div>

      {previewFile && previewUrl ? (
        <div
          role="dialog"
          aria-modal="true"
          aria-label={t("agentCreate.preview", { name: previewFile.name })}
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) setPreviewIndex(null);
          }}
        >
          <div className="flex max-h-[92vh] w-full max-w-5xl animate-spring-pop-in flex-col overflow-hidden rounded-2xl border border-border-l1 bg-surface-raised shadow-2xl">
            <div className="flex h-13 shrink-0 items-center justify-between gap-3 border-b border-border-l1 px-4">
              <span className="min-w-0 truncate text-sm font-medium text-text-primary">{previewFile.name}</span>
              <button
                type="button"
                title={t("agentCreate.closePreview")}
                aria-label={t("agentCreate.closePreview")}
                onClick={() => setPreviewIndex(null)}
                className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                <X size={18} />
              </button>
            </div>
            <div className="min-h-0 flex-1 bg-surface-subtle p-3">
              <img
                src={previewUrl}
                alt={previewFile.name}
                className="mx-auto max-h-[calc(92vh-76px)] max-w-full object-contain"
              />
            </div>
          </div>
        </div>
      ) : null}
    </form>
  );
}
