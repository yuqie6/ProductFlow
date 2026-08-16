import { useEffect, useMemo, useState } from "react";
import { Eye, ImagePlus, Loader2, Minus, Plus, RotateCw, Sparkles, X } from "lucide-react";

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
  const maxReferences = options?.limits.max_reference_images ?? 0;

  return (
    <form
      data-agent-product-intake-form
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
      className="mt-6 min-w-0"
    >
      <div>
        <label
          htmlFor="agent-product-name"
          className="block text-sm font-semibold text-zinc-950 dark:text-white"
        >
          {t("agentCreate.productName")}
        </label>
        <input
          id="agent-product-name"
          autoFocus={!isProductNameReadOnly}
          autoComplete="off"
          value={productName}
          readOnly={isProductNameReadOnly}
          disabled={isSubmitting || editingLocked}
          onChange={(event) => onProductNameChange(event.target.value)}
          placeholder={t("agentCreate.namePlaceholder")}
          className="mt-2 h-11 w-full rounded-md border border-zinc-300 bg-white px-3 text-sm text-zinc-950 outline-none transition-colors placeholder:text-zinc-400 focus:border-blue-600 focus:ring-2 focus:ring-blue-600/20 disabled:cursor-not-allowed disabled:opacity-60 read-only:bg-zinc-100 read-only:text-zinc-600 dark:border-slate-700 dark:!bg-[#0d1117] dark:text-white dark:placeholder:text-slate-500 dark:read-only:!bg-slate-800 dark:read-only:text-slate-300"
        />
      </div>

      <section className="mt-7" aria-labelledby="agent-image-types-title">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id="agent-image-types-title" className="text-sm font-semibold text-zinc-950 dark:text-white">
            {t("agentCreate.imageTypes")}
          </h2>
          <span className="text-xs tabular-nums text-zinc-500 dark:text-slate-400">
            {t("agentCreate.imageTypesMeta", { selected: selections.length, total: totalImages })}
          </span>
        </div>

        {isOptionsLoading ? (
          <div className="mt-3 flex min-h-28 items-center justify-center border-y border-zinc-200 text-sm text-zinc-500 dark:border-slate-800 dark:text-slate-400">
            <Loader2 size={17} className="mr-2 animate-spin" />
            {t("agentCreate.optionsLoading")}
          </div>
        ) : null}

        {isOptionsError ? (
          <div className="mt-3 flex min-h-28 flex-col items-center justify-center gap-3 border-y border-red-200 bg-red-50/70 px-4 text-sm text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
            <span>{t("agentCreate.optionsFailed")}</span>
            <button
              type="button"
              onClick={onRetryOptions}
              className="inline-flex h-9 items-center gap-2 rounded-md border border-red-300 bg-white px-3 font-medium hover:bg-red-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 dark:border-red-400/40 dark:!bg-[#161018]"
            >
              <RotateCw size={14} />
              {t("agentCreate.retryOptions")}
            </button>
          </div>
        ) : null}

        {options ? (
          <div className="mt-3 grid overflow-hidden border-y border-zinc-200 bg-white md:grid-cols-2 dark:border-slate-800 dark:!bg-[#0d1117]">
            {sortedOptions.map((option) => {
              const selected = selectedByKey.get(option.key);
              const translations = AGENT_IMAGE_TYPE_TRANSLATIONS[option.key];
              const title = translations ? t(translations.title) : option.title;
              const description = translations ? t(translations.description) : option.description;
              return (
                <div
                  key={option.key}
                  data-image-type={option.key}
                  className={`grid min-h-21 grid-cols-[minmax(0,1fr)_112px] items-center gap-3 border-b border-zinc-100 px-3 py-2.5 transition-colors md:odd:border-r dark:border-slate-800 ${
                    selected ? "bg-blue-50/60 dark:bg-cyan-400/5" : "hover:bg-zinc-50 dark:hover:bg-slate-800/45"
                  }`}
                >
                  <label className="flex min-w-0 cursor-pointer items-start gap-3">
                    <input
                      type="checkbox"
                      checked={Boolean(selected)}
                      disabled={isSubmitting || editingLocked}
                      onChange={(event) => onToggleImageType(option.key, event.target.checked)}
                      className="mt-0.5 h-4 w-4 shrink-0 accent-blue-600"
                    />
                    <span className="min-w-0">
                      <span className="block text-sm font-semibold leading-5 text-zinc-950 dark:text-white">
                        {title}
                      </span>
                      <span className="mt-0.5 block text-xs leading-4 text-zinc-500 dark:text-slate-400">
                        {description}
                      </span>
                    </span>
                  </label>

                  {selected ? (
                    <div className="grid h-9 w-28 grid-cols-[34px_44px_34px] overflow-hidden rounded-md border border-zinc-300 bg-white dark:border-slate-700 dark:!bg-[#111820]">
                      <button
                        type="button"
                        title={t("agentCreate.decrease", { title })}
                        aria-label={t("agentCreate.decrease", { title })}
                        disabled={isSubmitting || editingLocked || selected.quantity <= options.limits.min_images_per_type}
                        onClick={() => onQuantityChange(option.key, selected.quantity - 1)}
                        className="flex items-center justify-center border-r border-zinc-200 text-zinc-600 hover:bg-zinc-100 disabled:opacity-35 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-700"
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
                        onChange={(event) => onQuantityChange(option.key, Number(event.target.value))}
                        className="h-full w-full border-0 bg-transparent p-0 text-center text-sm font-semibold tabular-nums outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
                      />
                      <button
                        type="button"
                        title={t("agentCreate.increase", { title })}
                        aria-label={t("agentCreate.increase", { title })}
                        disabled={isSubmitting || editingLocked || selected.quantity >= options.limits.max_images_per_type}
                        onClick={() => onQuantityChange(option.key, selected.quantity + 1)}
                        className="flex items-center justify-center border-l border-zinc-200 text-zinc-600 hover:bg-zinc-100 disabled:opacity-35 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-700"
                      >
                        <Plus size={14} />
                      </button>
                    </div>
                  ) : (
                    <span className="w-28 text-center text-sm text-zinc-300 dark:text-slate-700">-</span>
                  )}
                </div>
              );
            })}
          </div>
        ) : null}
      </section>

      <section className="mt-7" aria-labelledby="agent-reference-images-title">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id="agent-reference-images-title" className="text-sm font-semibold text-zinc-950 dark:text-white">
            {t("agentCreate.references")}
          </h2>
          {options ? (
            <span className="text-xs tabular-nums text-zinc-500 dark:text-slate-400">
              {t("agentCreate.referencesMeta", {
                count: referenceFiles.length,
                max: options.limits.max_reference_images,
              })}
            </span>
          ) : null}
        </div>

        <div className="mt-3 flex min-h-27 gap-2 overflow-x-auto pb-2">
          {referenceFiles.map((file, index) => (
            <article
              key={`${file.name}:${file.size}:${file.lastModified}:${index}`}
              className="group relative w-24 shrink-0 overflow-hidden rounded-md border border-zinc-200 bg-white dark:border-slate-800 dark:!bg-[#0d1117]"
            >
              <button
                type="button"
                onClick={() => setPreviewIndex(index)}
                aria-label={t("agentCreate.preview", { name: file.name })}
                className="relative block h-20 w-full overflow-hidden bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blue-600 dark:bg-[#080b10]"
              >
                {previewUrls[index] ? (
                  <img src={previewUrls[index]} alt="" className="h-full w-full object-contain" />
                ) : null}
                <span className="absolute inset-0 flex items-center justify-center bg-black/0 text-white opacity-0 transition group-hover:bg-black/30 group-hover:opacity-100 group-focus-within:bg-black/30 group-focus-within:opacity-100">
                  <Eye size={18} />
                </span>
              </button>
              <div className="min-w-0 px-2 py-1.5">
                <div className="truncate text-[11px] font-medium text-zinc-800 dark:text-slate-200">{file.name}</div>
                <div className="text-[10px] text-zinc-400 dark:text-slate-500">
                  {formatFileSize(file.size, locale)}
                </div>
              </div>
              <button
                type="button"
                title={t("agentCreate.remove", { name: file.name })}
                aria-label={t("agentCreate.remove", { name: file.name })}
                disabled={isSubmitting || editingLocked}
                onClick={() => onRemoveReferenceFile(index)}
                className="absolute right-1 top-1 flex h-7 w-7 items-center justify-center rounded-md bg-black/65 text-white opacity-0 transition-opacity hover:bg-red-600 focus:opacity-100 group-hover:opacity-100 disabled:opacity-40"
              >
                <X size={13} />
              </button>
            </article>
          ))}

          {options && referenceFiles.length < maxReferences ? (
            <ImageDropZone
              multiple
              disabled={isSubmitting || editingLocked}
              ariaLabel={t("agentCreate.uploadAria")}
              onFiles={onAddReferenceFiles}
              className="flex h-27 w-36 shrink-0 cursor-pointer flex-col items-center justify-center rounded-md border border-dashed border-zinc-300 bg-zinc-50 px-3 text-center text-zinc-500 transition-colors hover:border-amber-500 hover:bg-amber-50/60 dark:!border-slate-700 dark:!bg-[#0d1117] dark:text-slate-400 dark:hover:!border-amber-400 dark:hover:!bg-amber-400/5"
              activeClassName="!border-amber-500 !bg-amber-50 text-amber-800 dark:!border-amber-400 dark:!bg-amber-400/10 dark:!text-amber-200"
            >
              {({ isDragging }) => (
                <>
                  <ImagePlus size={20} className="mb-2 text-amber-600 dark:text-amber-400" />
                  <span className="text-xs font-semibold text-zinc-800 dark:text-slate-200">
                    {isDragging ? t("agentCreate.uploadDrop") : t("agentCreate.uploadTitle")}
                  </span>
                  <span className="mt-1 text-[10px] leading-4 text-zinc-500 dark:text-slate-500">
                    {t("agentCreate.uploadHint", { max: maxReferences })}
                  </span>
                </>
              )}
            </ImageDropZone>
          ) : null}
        </div>
      </section>

      {error ? (
        <div role="alert" className="mt-5 border-l-2 border-red-500 bg-red-50 px-3 py-2.5 text-sm leading-5 text-red-700 dark:bg-red-500/10 dark:text-red-200">
          {error}
        </div>
      ) : null}

      <div className="mt-6 flex flex-col-reverse gap-3 border-t border-zinc-200 pt-5 sm:flex-row sm:items-center sm:justify-between dark:border-slate-800">
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-zinc-500 dark:text-slate-400">
          <span>{t("agentCreate.selectedTypes")}: <strong className="font-semibold text-zinc-800 dark:text-slate-200">{selections.length}</strong></span>
          <span>{t("agentCreate.plannedImages")}: <strong className="font-semibold text-zinc-800 dark:text-slate-200">{totalImages}</strong></span>
          <span>{t("agentCreate.referenceImages")}: <strong className="font-semibold text-zinc-800 dark:text-slate-200">{referenceFiles.length}</strong></span>
        </div>
        <button
          type="submit"
          disabled={isSubmitting}
          className="inline-flex h-11 shrink-0 items-center justify-center gap-2 rounded-md bg-zinc-950 px-5 text-sm font-semibold text-white transition-colors hover:bg-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-cyan-400 dark:text-[#071018] dark:hover:bg-cyan-300"
        >
          {isSubmitting ? <Loader2 size={16} className="animate-spin" /> : <Sparkles size={16} />}
          {isSubmitting ? t("agentCreate.submitting") : primaryActionLabel ?? t("agentCreate.submit")}
        </button>
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
          <div className="flex max-h-[92vh] w-full max-w-5xl flex-col overflow-hidden rounded-md bg-white shadow-2xl dark:bg-[#0d1117]">
            <div className="flex h-13 items-center justify-between gap-3 border-b border-zinc-200 px-4 dark:border-slate-800">
              <span className="min-w-0 truncate text-sm font-medium text-zinc-900 dark:text-white">{previewFile.name}</span>
              <button
                type="button"
                title={t("agentCreate.closePreview")}
                aria-label={t("agentCreate.closePreview")}
                onClick={() => setPreviewIndex(null)}
                className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-zinc-500 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 dark:text-slate-400 dark:hover:bg-slate-800"
              >
                <X size={18} />
              </button>
            </div>
            <div className="min-h-0 flex-1 bg-zinc-100 p-3 dark:bg-[#070a0e]">
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
