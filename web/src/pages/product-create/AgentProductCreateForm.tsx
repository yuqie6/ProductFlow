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
  Palette,
  Plus,
  RotateCw,
  Ruler,
  ShieldCheck,
  Sparkles,
  Type,
  Truck,
  X,
  ZoomIn,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { ImageDropZone } from "../../components/ImageDropZone";
import { Dialog, DialogContent } from "../../components/ui/dialog";
import { IMAGE_TYPE_FAMILY_ORDER, imageTypeFamily, isEvidenceImageType } from "../../lib/imageTypeFamilies";
import type { ImageTypeFamily } from "../../lib/imageTypeFamilies";
import { useI18n } from "../../lib/preferences";
import type {
  AgentProductImageTypeKey,
  DeliveryPresetCatalog,
  AgentProductWorkspaceOptions,
} from "../../lib/types";
import {
  AGENT_IMAGE_TYPE_TRANSLATIONS,
  applyRecommendedImageSet,
  agentImageTotal,
  aspectRatioForSelection,
  selectionNeedsConversionShot,
  type AgentImageTypeSelectionDraft,
} from "./imageTypeSelection";
import { CreateAspectRatioChips } from "./CreateAspectRatioChips";
import { CreateSourceNoteEditor } from "./CreateSourceNoteEditor";
import {
  CREATE_DEFAULT_TEXT_LANGUAGE,
  CREATE_TEXT_LANGUAGE_OPTIONS,
  CREATE_TEXT_POLICIES,
  createOutputSummary,
  hasCreateImagePlan,
  isCreateCanvasReady,
  isCreateOutputReady,
  type CreateOutputDraft,
} from "./createIntake";
import { formatSourceNote, isCreateSourceNoteReady, type CreateSourceNoteDraft } from "./sourceNote";

interface AgentProductCreateFormProps {
  productName: string;
  isProductNameReadOnly: boolean;
  options: AgentProductWorkspaceOptions | null;
  selections: readonly AgentImageTypeSelectionDraft[];
  referenceFiles: readonly File[];
  deliveryPresetCatalog: DeliveryPresetCatalog | null;
  deliveryPresetKey: string | null;
  isDeliveryPresetLoading: boolean;
  isDeliveryPresetError: boolean;
  isOptionsLoading: boolean;
  isOptionsError: boolean;
  isSubmitting: boolean;
  editingLocked: boolean;
  primaryActionLabel?: string;
  error: string;
  onProductNameChange: (name: string) => void;
  onToggleImageType: (key: AgentProductImageTypeKey, selected: boolean) => void;
  onQuantityChange: (key: AgentProductImageTypeKey, quantity: number) => void;
  onAspectRatioChange: (key: AgentProductImageTypeKey, aspectRatio: string) => void;
  onAddReferenceFiles: (files: File[]) => void;
  onRemoveReferenceFile: (index: number) => void;
  onDeliveryPresetChange: (key: string | null) => void;
  onRetryDeliveryPresets: () => void;
  sourceNote: CreateSourceNoteDraft;
  outputDraft: CreateOutputDraft;
  onSourceNoteChange: (value: CreateSourceNoteDraft) => void;
  onGenerateSourceNote: () => void;
  isGeneratingSourceNote?: boolean;
  sourceNoteError?: string;
  onOutputChange: (value: CreateOutputDraft) => void;
  onRetryOptions: () => void;
  onApplyRecommendedSet: () => void;
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

const STAGE_ICONS: [LucideIcon, LucideIcon, LucideIcon, LucideIcon, LucideIcon] = [
  Package,
  ImagePlus,
  Type,
  Images,
  Palette,
];

const TEXT_POLICY_LABELS = {
  required: "agentCreate.textPolicy.required",
  none: "agentCreate.textPolicy.none",
} as const;

function familyTitleKey(family: ImageTypeFamily): "agentCreate.family.photography" | "agentCreate.family.infographic" | "agentCreate.family.evidence" {
  if (family === "infographic") return "agentCreate.family.infographic";
  if (family === "evidence") return "agentCreate.family.evidence";
  return "agentCreate.family.photography";
}

function familyHintKey(family: ImageTypeFamily): "agentCreate.family.photographyHint" | "agentCreate.family.infographicHint" | "agentCreate.family.evidenceHint" {
  if (family === "infographic") return "agentCreate.family.infographicHint";
  if (family === "evidence") return "agentCreate.family.evidenceHint";
  return "agentCreate.family.photographyHint";
}

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
  deliveryPresetCatalog,
  deliveryPresetKey,
  isDeliveryPresetLoading,
  isDeliveryPresetError,
  isOptionsLoading,
  isOptionsError,
  isSubmitting,
  editingLocked,
  primaryActionLabel,
  error,
  onProductNameChange,
  onToggleImageType,
  onQuantityChange,
  onAspectRatioChange,
  onAddReferenceFiles,
  onRemoveReferenceFile,
  onDeliveryPresetChange,
  onRetryDeliveryPresets,
  sourceNote,
  outputDraft,
  onSourceNoteChange,
  onGenerateSourceNote,
  isGeneratingSourceNote = false,
  sourceNoteError = "",
  onOutputChange,
  onRetryOptions,
  onApplyRecommendedSet,
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
  const recommendedSet = options
    ? applyRecommendedImageSet({
        current: selections,
        catalogKeys: sortedOptions.map((option) => option.key),
        limits: options.limits,
      })
    : null;
  const recommendedSetDisabled = isSubmitting || editingLocked || !recommendedSet?.ok;
  const needsConversionShot = selectionNeedsConversionShot(selections);
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
  const briefReady = isCreateSourceNoteReady(sourceNote);
  const planReady = selections.length > 0;
  const referenceReady = referenceFiles.length >= minReferences;
  const outputReady = isCreateOutputReady(outputDraft);
  const canvasReady = isCreateCanvasReady({
    name: productName,
    brief: formatSourceNote(sourceNote),
    selections,
    referenceImageCount: referenceFiles.length,
    limits: options?.limits ?? null,
    outputDraft,
  });
  const createOutcome = canvasReady
    ? "full-canvas"
    : hasCreateImagePlan({ selections, referenceImageCount: referenceFiles.length })
      ? "need-plan"
      : "conversation";
  const outcomeCopyKey = createOutcome === "full-canvas"
    ? "agentCreate.outcome.canvas"
    : createOutcome === "need-plan"
      ? "agentCreate.outcome.needPlan"
      : "agentCreate.outcome.conversation";
  const outputSummary = createOutputSummary(outputDraft);
  const selectedRatios = [...new Set(selections.map((item) => aspectRatioForSelection(item)))];
  const languageLabel =
    CREATE_TEXT_LANGUAGE_OPTIONS.find((option) => option.value === outputDraft.textLanguage)?.label
    ?? outputDraft.textLanguage;

  const stageTitle = (stage: 1 | 2 | 3 | 4 | 5): string => {
    if (stage === 1) return t("agentCreate.productInfo");
    if (stage === 2) return t("agentCreate.references");
    if (stage === 3) return t("agentCreate.brief");
    if (stage === 4) return t("agentCreate.imageTypes");
    return t("agentCreate.output");
  };

  const updateOutput = (patch: Partial<CreateOutputDraft>) => {
    const nextPolicy = patch.textPolicy ?? outputDraft.textPolicy;
    const nextLanguage = patch.textLanguage ?? outputDraft.textLanguage;
    onOutputChange({
      textPolicy: nextPolicy,
      textLanguage:
        nextPolicy === "none"
          ? nextLanguage
          : nextLanguage.trim() || CREATE_DEFAULT_TEXT_LANGUAGE,
    });
  };

  const stageCard = (stage: 1 | 2 | 3 | 4 | 5, meta: string, isActive: boolean, body: ReactNode) => {
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
              {stageTitle(stage)}
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
      className="mt-6 min-w-0 space-y-4 pb-56"
    >
      {stageCard(1, t("agentCreate.productInfoHint"), nameReady, (
        <div className="space-y-4">
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
          <div data-delivery-preset-control className="border-t border-border-l2 pt-4">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between sm:gap-4">
              <label htmlFor="agent-create-delivery-preset" className="block min-w-0 flex-1">
                <span className="mb-1.5 block text-xs font-medium text-text-secondary">
                  {t("agentCreate.deliveryPreset.label")}
                </span>
                <select
                  id="agent-create-delivery-preset"
                  value={deliveryPresetKey ?? ""}
                  disabled={isSubmitting || editingLocked || isDeliveryPresetLoading || isDeliveryPresetError}
                  onChange={(event) => onDeliveryPresetChange(event.target.value || null)}
                  className="input-premium h-11 w-full px-3 text-sm text-text-primary disabled:cursor-not-allowed disabled:opacity-60"
                >
                  <option value="">{t("agentCreate.deliveryPreset.none")}</option>
                  {(deliveryPresetCatalog?.items ?? []).map((preset) => (
                    <option key={preset.key} value={preset.key}>
                      {preset.title} · {preset.aspect_ratio}
                    </option>
                  ))}
                </select>
              </label>
              {isDeliveryPresetLoading ? (
                <span className="inline-flex h-8 shrink-0 items-center gap-1.5 text-xs text-text-muted">
                  <Loader2 size={13} className="animate-spin" aria-hidden="true" />
                  {t("agentCreate.deliveryPreset.loading")}
                </span>
              ) : null}
            </div>
            {isDeliveryPresetError ? (
              <div
                role="alert"
                className="mt-2 flex flex-wrap items-center justify-between gap-2 text-xs leading-5 text-state-error"
              >
                <span>{t("agentCreate.deliveryPreset.loadFailed")}</span>
                <button
                  type="button"
                  onClick={onRetryDeliveryPresets}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-state-error/30 px-2.5 font-medium text-state-error transition-colors hover:border-state-error/60 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                >
                  <RotateCw size={13} aria-hidden="true" />
                  {t("agentCreate.deliveryPreset.retry")}
                </button>
              </div>
            ) : null}
          </div>
        </div>
      ))}

      {stageCard(2, t("agentCreate.referencesMeta", { count: referenceFiles.length, max: maxReferences }) || t("agentCreate.references"), referenceReady, (
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

      {stageCard(3, t("agentCreate.briefHint"), briefReady, (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-xs font-medium text-text-secondary">{t("agentCreate.brief")}</span>
            <button
              type="button"
              data-create-generate-brief
              disabled={
                isSubmitting
                || editingLocked
                || isGeneratingSourceNote
                || referenceFiles.length < 1
              }
              title={referenceFiles.length < 1 ? t("agentCreate.briefGenerateHint") : t("agentCreate.briefGenerate")}
              onClick={onGenerateSourceNote}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-accent/35 bg-accent-soft/60 px-2.5 text-xs font-semibold text-accent-strong transition-colors hover:border-accent/60 hover:bg-accent-soft disabled:cursor-not-allowed disabled:opacity-45"
            >
              {isGeneratingSourceNote ? (
                <Loader2 size={13} className="animate-spin" aria-hidden="true" />
              ) : (
                <Sparkles size={13} aria-hidden="true" />
              )}
              {isGeneratingSourceNote ? t("agentCreate.briefGenerating") : t("agentCreate.briefGenerate")}
            </button>
          </div>
          {sourceNoteError ? (
            <p role="alert" className="text-xs leading-5 text-state-error">{sourceNoteError}</p>
          ) : null}
          <CreateSourceNoteEditor
            value={sourceNote}
            disabled={isSubmitting || editingLocked || isGeneratingSourceNote}
            onChange={onSourceNoteChange}
          />
        </div>
      ))}

      {stageCard(4, t("agentCreate.imageTypesMeta", { selected: selections.length, total: totalImages }), planReady, (
        <div className="space-y-3">
          {options ? (
            <div className="flex flex-col gap-2">
              <div className="flex justify-end">
              <button
                type="button"
                data-agent-apply-recommended-set
                disabled={recommendedSetDisabled}
                onClick={onApplyRecommendedSet}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-accent/35 bg-accent-soft/60 px-2.5 text-xs font-semibold text-accent-strong transition-colors hover:border-accent/60 hover:bg-accent-soft disabled:cursor-not-allowed disabled:opacity-45"
              >
                <Sparkles size={13} aria-hidden="true" />
                {t("agentCreate.applyRecommendedSet")}
              </button>
              </div>
              {needsConversionShot ? (
                <p className="text-xs leading-5 text-text-secondary">{t("agentCreate.missingConversionShot")}</p>
              ) : null}
            </div>
          ) : null}
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
            <div className="space-y-5">
              {IMAGE_TYPE_FAMILY_ORDER.map((family) => {
                const familyOptions = sortedOptions.filter((option) => imageTypeFamily(option.key) === family);
                if (!familyOptions.length) return null;
                return (
                  <div key={family} data-image-type-family={family}>
                    <h3 className="text-xs font-semibold text-text-secondary">{t(familyTitleKey(family))}</h3>
                    <p className="mt-0.5 text-xs leading-5 text-text-muted">{t(familyHintKey(family))}</p>
                    <div className="mt-2 grid gap-2.5 sm:grid-cols-2 xl:grid-cols-3">
                      {familyOptions.map((option) => {
                        const selected = selectedByKey.get(option.key);
                        const translations = AGENT_IMAGE_TYPE_TRANSLATIONS[option.key];
                        const title = translations ? t(translations.title) : option.title;
                        const description = translations ? t(translations.description) : option.description;
                        const Icon = IMAGE_TYPE_ICONS[option.key] ?? Images;
                        const iconColor = selected ? "text-accent" : "text-text-muted";
                        const evidence = isEvidenceImageType(option.key);
                        return (
                          <label
                            key={option.key}
                            data-image-type={option.key}
                            className={`group flex min-h-32 cursor-pointer flex-col rounded-xl border p-4 transition-[border-color,background-color,box-shadow] duration-200 ${selected
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
                            <span className="mt-auto flex justify-end pt-2.5">
                              {evidence ? (
                                <span className="text-xs leading-5 text-text-muted">{t("agentCreate.evidenceBind")}</span>
                              ) : (
                                stepper(option.key, title)
                              )}
                            </span>
                          </label>
                        );
                      })}
                    </div>
                  </div>
                );
              })}
            </div>
          ) : null}
        </div>
      ))}

      {stageCard(5, t("agentCreate.outputHint"), outputReady, (
        <div className="space-y-4" data-create-output>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-stretch">
            <div role="radiogroup" aria-label={t("agentCreate.textPolicy")} className="grid min-w-0 flex-1 grid-cols-2 gap-1.5">
              {CREATE_TEXT_POLICIES.map((policy) => {
                const selected = outputDraft.textPolicy === policy;
                return (
                  <button
                    key={policy}
                    type="button"
                    role="radio"
                    aria-checked={selected}
                    disabled={isSubmitting || editingLocked}
                    onClick={() => updateOutput({ textPolicy: policy })}
                    className={`flex h-11 items-center justify-center rounded-lg border px-2 text-sm font-semibold transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60 ${selected
                        ? "border-accent/60 bg-accent-soft/60 text-accent-strong"
                        : "border-border-l1 bg-surface-base/60 text-text-secondary hover:border-border-l3 hover:bg-surface-raised hover:text-text-primary"
                      }`}
                  >
                    {t(TEXT_POLICY_LABELS[policy])}
                  </button>
                );
              })}
            </div>
            {outputDraft.textPolicy === "required" ? <label htmlFor="agent-create-text-language" className="block shrink-0 sm:w-40">
              <span className="sr-only">{t("agentCreate.textLanguage")}</span>
              <select
                id="agent-create-text-language"
                value={outputDraft.textLanguage}
                disabled={isSubmitting || editingLocked}
                onChange={(event) => updateOutput({ textLanguage: event.target.value })}
                className="input-premium h-11 w-full px-3 text-sm text-text-primary disabled:cursor-not-allowed disabled:opacity-60"
              >
                {CREATE_TEXT_LANGUAGE_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label> : null}
          </div>
          <div>
            <div id="agent-create-type-ratios" className="mb-1 text-xs font-medium text-text-secondary">
              {t("agentCreate.aspectRatio")}
            </div>
            {selections.length === 0 ? (
              <p className="text-xs leading-5 text-text-muted">{t("agentCreate.aspectRatioEmpty")}</p>
            ) : (
              <ul className="divide-y divide-border-l2">
                {selections.map((item) => {
                  const translations = AGENT_IMAGE_TYPE_TRANSLATIONS[item.key];
                  const title = translations ? t(translations.title) : item.key;
                  const headingId = `agent-create-ratio-${item.key}`;
                  const Icon = IMAGE_TYPE_ICONS[item.key] ?? Images;
                  return (
                    <li
                      key={item.key}
                      data-image-type-aspect={item.key}
                      className="flex flex-col gap-2 py-2.5 first:pt-1 last:pb-0 sm:flex-row sm:items-center sm:justify-between sm:gap-4"
                    >
                      <div id={headingId} className="flex min-w-0 items-center gap-1.5 text-sm font-medium text-text-primary">
                        <Icon size={14} className="shrink-0 text-accent" aria-hidden="true" />
                        <span className="truncate">{title}</span>
                      </div>
                      <CreateAspectRatioChips
                        value={aspectRatioForSelection(item)}
                        disabled={isSubmitting || editingLocked}
                        labelledBy={headingId}
                        className="sm:justify-end"
                        onChange={(aspectRatio) => onAspectRatioChange(item.key, aspectRatio)}
                      />
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>
      ))}

      {error ? (
        <div
          role="alert"
          className="animate-node-reveal rounded-control border border-state-error/30 bg-state-error/10 px-4 py-3 text-sm leading-5 text-state-error motion-reduce:animate-none"
        >
          {error}
        </div>
      ) : null}

      <div className="fixed inset-x-0 bottom-0 z-30 border-t border-border-l1 bg-surface-raised shadow-[0_-10px_30px_-12px_rgb(2_6_23/0.25)] dark:border-slate-700 dark:bg-[#0b1424] dark:shadow-[0_-14px_36px_rgb(0_0_0/0.45)]">
        <div className="mx-auto flex w-full max-w-[920px] flex-col-reverse gap-3 px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-6">
          <div className="flex flex-wrap gap-x-5 gap-y-1.5 text-xs text-text-secondary">
            <span data-create-outcome={createOutcome} className="w-full text-text-primary">
              <strong className="font-semibold">{t(outcomeCopyKey)}</strong>
            </span>
            {createOutcome === "full-canvas" ? (
              <span className="w-full text-text-primary">
                <strong className="font-semibold">{t("agentCreate.outcome.canvasOnly")}</strong>
              </span>
            ) : null}
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
              <strong
                data-create-reference-count={referenceFiles.length}
                className="font-semibold tabular-nums text-text-primary"
              >
                {referenceFiles.length}
              </strong>
            </span>
            <span>
              {t("agentCreate.textPolicy")}:{" "}
              <strong className="font-semibold text-text-primary">{t(TEXT_POLICY_LABELS[outputSummary.textPolicy])}</strong>
            </span>
            {outputSummary.textLanguage ? (
              <span>
                {t("agentCreate.textLanguage")}:{" "}
                <strong className="font-semibold text-text-primary">{languageLabel}</strong>
              </span>
            ) : null}
            {selectedRatios.length > 0 ? (
              <span>
                {t("agentCreate.aspectRatio")}:{" "}
                <strong className="font-semibold tabular-nums text-text-primary">{selectedRatios.join(" · ")}</strong>
              </span>
            ) : null}
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            {onDirectCreate ? (
              <button
                type="button"
                disabled={isSubmitting || isDirectCreating || !canvasReady}
                title={canvasReady ? t("agentCreate.submitDirect") : t("agentCreate.submitDirectNeedPlan")}
                aria-label={t("agentCreate.submitDirect")}
                data-create-direct
                data-create-direct-ready={canvasReady ? "true" : "false"}
                onClick={onDirectCreate}
                className="inline-flex h-11 shrink-0 items-center justify-center gap-2 rounded-xl border border-indigo-200 px-6 text-sm font-semibold text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-40"
              >
                {isDirectCreating ? <Loader2 size={16} className="animate-spin" /> : null}
                {t("agentCreate.submitDirect")}
              </button>
            ) : null}
            <button
              type="submit"
              disabled={isSubmitting || isDirectCreating}
              className="inline-flex h-11 shrink-0 items-center justify-center gap-2 rounded-control bg-accent px-6 text-sm font-semibold text-accent-fg shadow-elev-1 transition-[background-color,box-shadow] duration-fast hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-focus-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-45"
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

      <div data-create-form-bottom-spacer="" className="h-28 shrink-0" aria-hidden="true" />

      <Dialog open={Boolean(previewFile && previewUrl)} onOpenChange={(open) => !open && setPreviewIndex(null)}>
        {previewFile && previewUrl ? (
          <DialogContent
            title={t("agentCreate.preview", { name: previewFile.name })}
            size="xl"
            className="max-h-[92vh] max-w-5xl"
            closeLabel={t("agentCreate.closePreview")}
            onClose={() => setPreviewIndex(null)}
            bodyClassName="min-h-0 bg-surface-subtle p-3"
          >
              <img
                src={previewUrl}
                alt={previewFile.name}
                className="mx-auto max-h-[calc(92vh-76px)] max-w-full object-contain"
              />
          </DialogContent>
        ) : null}
      </Dialog>
    </form>
  );
}
