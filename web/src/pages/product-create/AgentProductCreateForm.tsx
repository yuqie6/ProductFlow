import { useEffect, useMemo, useState } from "react";
import {
  Bot,
  Eye,
  ImagePlus,
  Loader2,
  Minus,
  Plus,
  RotateCw,
  X,
} from "lucide-react";

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
  options: AgentProductWorkspaceOptions | null;
  name: string;
  selections: readonly AgentImageTypeSelectionDraft[];
  referenceFiles: readonly File[];
  isOptionsLoading: boolean;
  isOptionsError: boolean;
  isSubmitting: boolean;
  error: string;
  onNameChange: (name: string) => void;
  onToggleImageType: (key: AgentProductImageTypeKey, selected: boolean) => void;
  onQuantityChange: (key: AgentProductImageTypeKey, quantity: number) => void;
  onAddReferenceFiles: (files: File[]) => void;
  onRemoveReferenceFile: (index: number) => void;
  onRetryOptions: () => void;
  onCancel: () => void;
  onSubmit: () => void;
}

function formatFileSize(bytes: number, locale: string): string {
  const value = bytes >= 1024 * 1024 ? bytes / (1024 * 1024) : bytes / 1024;
  const unit = bytes >= 1024 * 1024 ? "MB" : "KB";
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value)} ${unit}`;
}

export function AgentProductCreateForm({
  options,
  name,
  selections,
  referenceFiles,
  isOptionsLoading,
  isOptionsError,
  isSubmitting,
  error,
  onNameChange,
  onToggleImageType,
  onQuantityChange,
  onAddReferenceFiles,
  onRemoveReferenceFile,
  onRetryOptions,
  onCancel,
  onSubmit,
}: AgentProductCreateFormProps) {
  const { locale, t } = useI18n();
  const [previewIndex, setPreviewIndex] = useState<number | null>(null);
  const selectedByKey = useMemo(
    () => new Map(selections.map((selection) => [selection.key, selection])),
    [selections],
  );
  const totalImages = agentImageTotal(selections);
  const sortedOptions = useMemo(
    () => [...(options?.image_types ?? [])].sort((left, right) => left.order - right.order),
    [options],
  );
  const previewUrls = useMemo(
    () =>
      referenceFiles.map((file) =>
        typeof URL !== "undefined" && typeof URL.createObjectURL === "function" ? URL.createObjectURL(file) : "",
      ),
    [referenceFiles],
  );

  useEffect(
    () => () => {
      previewUrls.forEach((url) => {
        if (url) {
          URL.revokeObjectURL(url);
        }
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
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
      className="mx-auto w-full max-w-[1320px]"
    >
      <div className="grid items-start gap-8 lg:grid-cols-[minmax(0,1fr)_280px] xl:gap-12">
        <div className="min-w-0">
          <section className="border-b border-zinc-200 pb-8 dark:border-slate-800">
            <label htmlFor="agent-product-name" className="block text-sm font-semibold text-zinc-900 dark:text-white">
              {t("agentCreate.productName")}
            </label>
            <input
              id="agent-product-name"
              type="text"
              maxLength={255}
              aria-required="true"
              disabled={isSubmitting}
              value={name}
              onChange={(event) => onNameChange(event.target.value)}
              placeholder={t("agentCreate.namePlaceholder")}
              className="mt-3 h-12 w-full max-w-2xl rounded-md border border-zinc-300 bg-white px-4 text-base text-zinc-950 outline-none transition-colors placeholder:text-zinc-400 focus:border-blue-600 focus:ring-2 focus:ring-blue-100 disabled:opacity-60 dark:!border-slate-700 dark:!bg-[#0d1117] dark:!text-white dark:placeholder:!text-slate-500 dark:focus:!border-cyan-400 dark:focus:ring-cyan-400/15"
            />
          </section>

          <section className="border-b border-zinc-200 py-8 dark:border-slate-800" aria-labelledby="agent-image-types-title">
            <div className="flex flex-wrap items-end justify-between gap-3">
              <h2 id="agent-image-types-title" className="text-base font-semibold text-zinc-950 dark:text-white">
                {t("agentCreate.imageTypes")}
              </h2>
              <span className="text-sm tabular-nums text-zinc-500 dark:text-slate-400">
                {t("agentCreate.imageTypesMeta", { selected: selections.length, total: totalImages })}
              </span>
            </div>

            {isOptionsLoading ? (
              <div className="mt-5 flex h-36 items-center justify-center border border-zinc-200 bg-white text-sm text-zinc-500 dark:border-slate-800 dark:!bg-[#0d1117] dark:text-slate-400">
                <Loader2 size={18} className="mr-2 animate-spin" />
                {t("agentCreate.optionsLoading")}
              </div>
            ) : null}

            {isOptionsError ? (
              <div className="mt-5 flex min-h-28 flex-col items-center justify-center gap-3 border border-red-200 bg-red-50 px-4 text-sm text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
                <span>{t("agentCreate.optionsFailed")}</span>
                <button
                  type="button"
                  onClick={onRetryOptions}
                  className="inline-flex h-10 items-center gap-2 rounded-md border border-red-300 bg-white px-3 font-medium hover:bg-red-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 dark:border-red-400/40 dark:!bg-[#161018] dark:hover:!bg-red-500/15"
                >
                  <RotateCw size={15} />
                  {t("agentCreate.retryOptions")}
                </button>
              </div>
            ) : null}

            {options ? (
              <div className="mt-5 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                {sortedOptions.map((option) => {
                  const selected = selectedByKey.get(option.key);
                  const translations = AGENT_IMAGE_TYPE_TRANSLATIONS[option.key];
                  const title = translations ? t(translations.title) : option.title;
                  const description = translations ? t(translations.description) : option.description;
                  return (
                    <article
                      key={option.key}
                      data-image-type={option.key}
                      className={`grid min-h-44 grid-rows-[1fr_44px] rounded-md border bg-white transition-colors dark:!bg-[#0d1117] ${
                        selected
                          ? "border-blue-600 shadow-[inset_0_0_0_1px_rgb(37_99_235)] dark:!border-cyan-400 dark:shadow-[inset_0_0_0_1px_rgb(34_211_238)]"
                          : "border-zinc-200 hover:border-zinc-400 dark:!border-slate-800 dark:hover:!border-slate-600"
                      }`}
                    >
                      <label className="flex min-w-0 cursor-pointer items-start gap-3 p-4">
                        <input
                          type="checkbox"
                          checked={Boolean(selected)}
                          disabled={isSubmitting}
                          onChange={(event) => onToggleImageType(option.key, event.target.checked)}
                          className="mt-0.5 h-4 w-4 shrink-0 accent-blue-600"
                        />
                        <span className="min-w-0">
                          <span className="block text-sm font-semibold text-zinc-950 dark:text-white">{title}</span>
                          <span className="mt-1.5 block text-xs leading-5 text-zinc-500 dark:text-slate-400">
                            {description}
                          </span>
                        </span>
                      </label>

                      <div className="flex h-11 items-center justify-between border-t border-zinc-100 px-3 dark:border-slate-800">
                        <span className="text-xs text-zinc-500 dark:text-slate-400">{t("agentCreate.quantity")}</span>
                        {selected ? (
                          <div className="grid h-8 grid-cols-[32px_44px_32px] overflow-hidden rounded-md border border-zinc-300 bg-white dark:border-slate-700 dark:!bg-[#111820]">
                            <button
                              type="button"
                              title={t("agentCreate.decrease", { title })}
                              aria-label={t("agentCreate.decrease", { title })}
                              disabled={
                                isSubmitting || selected.quantity <= options.limits.min_images_per_type
                              }
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
                              disabled={isSubmitting}
                              value={selected.quantity}
                              onChange={(event) => onQuantityChange(option.key, Number(event.target.value))}
                              className="h-full w-full border-0 bg-transparent p-0 text-center text-sm font-semibold tabular-nums outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
                            />
                            <button
                              type="button"
                              title={t("agentCreate.increase", { title })}
                              aria-label={t("agentCreate.increase", { title })}
                              disabled={
                                isSubmitting || selected.quantity >= options.limits.max_images_per_type
                              }
                              onClick={() => onQuantityChange(option.key, selected.quantity + 1)}
                              className="flex items-center justify-center border-l border-zinc-200 text-zinc-600 hover:bg-zinc-100 disabled:opacity-35 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-700"
                            >
                              <Plus size={14} />
                            </button>
                          </div>
                        ) : (
                          <span className="text-sm tabular-nums text-zinc-300 dark:text-slate-700">-</span>
                        )}
                      </div>
                    </article>
                  );
                })}
              </div>
            ) : null}
          </section>

          <section className="py-8" aria-labelledby="agent-reference-images-title">
            <div className="flex flex-wrap items-end justify-between gap-3">
              <h2 id="agent-reference-images-title" className="text-base font-semibold text-zinc-950 dark:text-white">
                {t("agentCreate.references")}
              </h2>
              {options ? (
                <span className="text-sm tabular-nums text-zinc-500 dark:text-slate-400">
                  {t("agentCreate.referencesMeta", {
                    count: referenceFiles.length,
                    max: options.limits.max_reference_images,
                  })}
                </span>
              ) : null}
            </div>

            <div className="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6">
              {referenceFiles.map((file, index) => (
                <article
                  key={`${file.name}:${file.size}:${file.lastModified}:${index}`}
                  className="group min-w-0 overflow-hidden rounded-md border border-zinc-200 bg-white dark:border-slate-800 dark:!bg-[#0d1117]"
                >
                  <button
                    type="button"
                    onClick={() => setPreviewIndex(index)}
                    aria-label={t("agentCreate.preview", { name: file.name })}
                    className="relative block aspect-square w-full overflow-hidden bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blue-600 dark:bg-[#080b10]"
                  >
                    {previewUrls[index] ? (
                      <img src={previewUrls[index]} alt="" className="h-full w-full object-contain" />
                    ) : null}
                    <span className="absolute inset-0 flex items-center justify-center bg-black/0 text-white opacity-0 transition group-hover:bg-black/30 group-hover:opacity-100 group-focus-within:bg-black/30 group-focus-within:opacity-100">
                      <Eye size={20} />
                    </span>
                  </button>
                  <div className="flex h-14 min-w-0 items-center gap-2 px-2.5">
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-xs font-medium text-zinc-800 dark:text-slate-200">{file.name}</div>
                      <div className="mt-0.5 text-[11px] text-zinc-400 dark:text-slate-500">
                        {formatFileSize(file.size, locale)}
                      </div>
                    </div>
                    <button
                      type="button"
                      title={t("agentCreate.remove", { name: file.name })}
                      aria-label={t("agentCreate.remove", { name: file.name })}
                      disabled={isSubmitting}
                      onClick={() => onRemoveReferenceFile(index)}
                      className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-zinc-400 hover:bg-red-50 hover:text-red-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 disabled:opacity-40 dark:hover:bg-red-500/10 dark:hover:text-red-300"
                    >
                      <X size={15} />
                    </button>
                  </div>
                </article>
              ))}

              {options && referenceFiles.length < maxReferences ? (
                <ImageDropZone
                  multiple
                  disabled={isSubmitting}
                  ariaLabel={t("agentCreate.uploadAria")}
                  onFiles={onAddReferenceFiles}
                  className="flex aspect-square min-h-40 cursor-pointer flex-col items-center justify-center rounded-md border border-dashed border-zinc-300 bg-white p-4 text-center text-zinc-500 transition-colors hover:border-amber-500 hover:bg-amber-50/60 dark:!border-slate-700 dark:!bg-[#0d1117] dark:text-slate-400 dark:hover:!border-amber-400 dark:hover:!bg-amber-400/5"
                  activeClassName="!border-amber-500 !bg-amber-50 text-amber-800 dark:!border-amber-400 dark:!bg-amber-400/10 dark:!text-amber-200"
                >
                  {({ isDragging }) => (
                    <>
                      <ImagePlus size={24} className="mb-3 text-amber-600 dark:text-amber-400" />
                      <span className="text-sm font-semibold text-zinc-800 dark:text-slate-200">
                        {isDragging ? t("agentCreate.uploadDrop") : t("agentCreate.uploadTitle")}
                      </span>
                      <span className="mt-1.5 text-xs leading-5 text-zinc-500 dark:text-slate-500">
                        {t("agentCreate.uploadHint", { max: maxReferences })}
                      </span>
                    </>
                  )}
                </ImageDropZone>
              ) : null}
            </div>
          </section>
        </div>

        <aside className="border-t border-zinc-200 pt-6 dark:border-slate-800 lg:sticky lg:top-6 lg:border-l lg:border-t-0 lg:pl-7 lg:pt-0">
          <div className="flex items-center gap-2 text-sm font-semibold text-zinc-950 dark:text-white">
            <Bot size={18} className="text-blue-600 dark:text-cyan-400" />
            {t("agentCreate.summary")}
          </div>
          <dl className="mt-5 divide-y divide-zinc-200 border-y border-zinc-200 text-sm dark:divide-slate-800 dark:border-slate-800">
            <div className="flex items-center justify-between gap-4 py-3">
              <dt className="text-zinc-500 dark:text-slate-400">{t("agentCreate.selectedTypes")}</dt>
              <dd className="font-semibold tabular-nums text-zinc-950 dark:text-white">{selections.length}</dd>
            </div>
            <div className="flex items-center justify-between gap-4 py-3">
              <dt className="text-zinc-500 dark:text-slate-400">{t("agentCreate.plannedImages")}</dt>
              <dd className="font-semibold tabular-nums text-zinc-950 dark:text-white">{totalImages}</dd>
            </div>
            <div className="flex items-center justify-between gap-4 py-3">
              <dt className="text-zinc-500 dark:text-slate-400">{t("agentCreate.referenceImages")}</dt>
              <dd className="font-semibold tabular-nums text-zinc-950 dark:text-white">{referenceFiles.length}</dd>
            </div>
          </dl>

          {error ? (
            <div role="alert" className="mt-4 border-l-2 border-red-500 bg-red-50 px-3 py-2.5 text-sm leading-5 text-red-700 dark:bg-red-500/10 dark:text-red-200">
              {error}
            </div>
          ) : null}

          <button
            type="submit"
            disabled={isSubmitting}
            className="mt-5 inline-flex h-11 w-full items-center justify-center gap-2 rounded-md bg-zinc-950 px-4 text-sm font-semibold text-white transition-colors hover:bg-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-cyan-400 dark:text-[#071018] dark:hover:bg-cyan-300 dark:focus-visible:ring-cyan-400 dark:focus-visible:ring-offset-[#060a12]"
          >
            {isSubmitting ? <Loader2 size={16} className="animate-spin" /> : <Bot size={16} />}
            {isSubmitting ? t("agentCreate.submitting") : t("agentCreate.submit")}
          </button>
          <button
            type="button"
            disabled={isSubmitting}
            onClick={onCancel}
            className="mt-2 h-10 w-full rounded-md text-sm font-medium text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-zinc-500 disabled:opacity-50 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
          >
            {t("agentCreate.cancel")}
          </button>
        </aside>
      </div>

      {previewFile && previewUrl ? (
        <div
          role="dialog"
          aria-modal="true"
          aria-label={t("agentCreate.preview", { name: previewFile.name })}
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) {
              setPreviewIndex(null);
            }
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
              <img src={previewUrl} alt={previewFile.name} className="mx-auto max-h-[calc(92vh-76px)] max-w-full object-contain" />
            </div>
          </div>
        </div>
      ) : null}
    </form>
  );
}
