import { useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Clock3,
  FileText,
  Image as ImageIcon,
  ImagePlus,
  Loader2,
  OctagonX,
  Play,
  Plus,
  Sparkles,
  Trash2,
  Upload,
  XCircle,
} from "lucide-react";

import { ImageDropZone } from "../../components/ImageDropZone";
import { ImageGenerationSettingsPanel } from "../../components/ImageGenerationSettingsPanel";
import { ImageGenerationSettingsTabs, type ImageGenerationSettingsTab } from "../../components/ImageGenerationSettingsTabs";
import { ImageToolControls } from "../../components/ImageToolControls";
import { PromptPreviewDialog, type PromptPreview } from "../../components/PromptPreviewDialog";
import { SelectField } from "../../components/SelectField";
import type { DownloadableImage } from "../../lib/image-downloads";
import type { ImageSizeOption } from "../../lib/imageSizes";
import { formatDateTime, formatPrice } from "../../lib/format";
import type { TranslationKey, TranslationParams } from "../../lib/i18n";
import { useI18n } from "../../lib/preferences";
import type {
  CopyBlock,
  CopyPayloadV2,
  CopySection,
  ImageToolOptionKey,
  ProductDetail,
  ProductWorkflow,
  WorkflowNode,
} from "../../lib/types";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME } from "./constants";
import { DownloadLink } from "./ImageDownloadComponents";
import { getNodeImageDownload } from "./imageDownloads";
import { workflowNodeDisplayLabel, workflowNodeDisplayTitle } from "./nodeDisplay";
import type { NodeConfigDraft, SaveStatus } from "./types";
import {
  type WorkflowNodeRunActionState,
  getWorkflowNodeActiveRunContext,
  outputText,
  statusClass,
  workflowNodeActivityText,
  workflowRetryHintLabel,
  workflowNodeStatusLabel,
  workflowRunQueueText,
} from "./utils";
import { TextArea } from "./TextArea";

type TFunction = (key: TranslationKey, params?: TranslationParams) => string;

const SAVE_STATUS_LABEL_KEYS: Record<SaveStatus, TranslationKey> = {
  idle: "detail.inspector.saveIdle",
  saving: "detail.inspector.saving",
  saved: "detail.inspector.saved",
  failed: "detail.inspector.saveFailed",
};

const SAVE_STATUS_CLASS_NAMES: Record<SaveStatus, string> = {
  idle: "border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300",
  saving: "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-400/35 dark:bg-blue-500/12 dark:text-blue-200",
  saved: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-400/35 dark:bg-emerald-500/12 dark:text-emerald-200",
  failed: "border-red-200 bg-red-50 text-red-700 dark:border-red-400/35 dark:bg-red-500/12 dark:text-red-200",
};

const ADD_COPY_FIELD_BUTTON_CLASS_NAME =
  "copy-add-field inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-semibold";

const REFERENCE_ROLE_OPTIONS: Array<{ value: string; labelKey: TranslationKey }> = [
  { value: "reference", labelKey: "detail.referenceRole.reference" },
  { value: "style", labelKey: "detail.referenceRole.style" },
  { value: "product_angle", labelKey: "detail.referenceRole.productAngle" },
  { value: "main_image", labelKey: "detail.referenceRole.mainImage" },
  { value: "sku_image", labelKey: "detail.referenceRole.skuImage" },
  { value: "model_image", labelKey: "detail.referenceRole.modelImage" },
  { value: "scene_image", labelKey: "detail.referenceRole.sceneImage" },
  { value: "detail_image", labelKey: "detail.referenceRole.detailImage" },
  { value: "campaign_image", labelKey: "detail.referenceRole.campaignImage" },
  { value: "background", labelKey: "detail.referenceRole.background" },
];

interface InspectorPanelProps {
  product: ProductDetail;
  sourceImage: DownloadableImage | null;
  workflow: ProductWorkflow | null;
  node: WorkflowNode;
  draft: NodeConfigDraft;
  imageSizeOptions: ImageSizeOption[];
  imageGenerationMaxDimension: number;
  imageToolAllowedFields: readonly ImageToolOptionKey[];
  onDraftChange: (draft: NodeConfigDraft) => void;
  onPreviewImage: (image: DownloadableImage) => void;
  onRun: () => void;
  onCancelRun: (() => void) | null;
  onUploadImage: (file: File) => void;
  onDelete: () => void;
  busy: boolean;
  cancelBusy: boolean;
  runActionState: WorkflowNodeRunActionState;
  saveStatus: SaveStatus;
}

export function InspectorPanel({
  product,
  sourceImage,
  workflow,
  node,
  draft,
  imageSizeOptions,
  imageGenerationMaxDimension,
  imageToolAllowedFields,
  onDraftChange,
  onPreviewImage,
  onRun,
  onCancelRun,
  onUploadImage,
  onDelete,
  busy,
  cancelBusy,
  runActionState,
  saveStatus,
}: InspectorPanelProps) {
  const { t } = useI18n();
  const [promptPreview, setPromptPreview] = useState<PromptPreview | null>(null);
  const icon = {
    product_context: FileText,
    reference_image: ImagePlus,
    copy_generation: FileText,
    image_generation: ImageIcon,
  }[node.node_type];
  const InspectorIcon = icon;
  const displayTitle = workflowNodeDisplayTitle({ ...node, title: draft.title || node.title }, t);
  const displayLabel = workflowNodeDisplayLabel(node, t);
  const downstreamReferenceCount =
    node.node_type === "image_generation"
      ? new Set(
          workflow?.edges
            .filter((edge) => {
              if (edge.source_node_id !== node.id) {
                return false;
              }
              const target = workflow.nodes.find(
                (item) => item.id === edge.target_node_id,
              );
              return target?.node_type === "reference_image";
            })
            .map((edge) => edge.target_node_id) ?? [],
        ).size
      : 0;
  const hasReferenceImage = Boolean(
    node.node_type === "reference_image" &&
      Array.isArray(node.output_json?.source_asset_ids) &&
      node.output_json.source_asset_ids.length,
  );
  const referenceImage = node.node_type === "reference_image" ? getNodeImageDownload(node, product, t) : null;
  const activeRunContext = getWorkflowNodeActiveRunContext(workflow, node);
  const activeRunQueueText = activeRunContext ? workflowRunQueueText(activeRunContext.run, t) : "";
  const activeRunNodeText = workflowNodeActivityText(
    { ...node, status: activeRunContext?.nodeRun.status ?? node.status },
    t,
  );

  return (
    <div className="space-y-3">
      <section className="config-bubble rounded-2xl p-4 shadow-sm">
        <div className="flex items-start gap-3">
          <span className="rounded-xl border border-indigo-100 bg-indigo-50 p-2 text-indigo-700 dark:border-violet-400/35 dark:bg-violet-500/15 dark:text-violet-100">
            <InspectorIcon size={16} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="truncate text-base font-semibold text-zinc-950 dark:text-white">
              {displayTitle}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-1.5">
              <span className="rounded-full border border-zinc-200 bg-zinc-50 px-2 py-0.5 text-[10px] font-medium text-zinc-600 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300">
                {displayLabel}
              </span>
              <span
                className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusClass(node.status)}`}
              >
                {node.status === "running" || node.status === "queued" ? (
                  <Loader2 size={11} className="mr-1 animate-spin" />
                ) : node.status === "failed" ? (
                  <XCircle size={11} className="mr-1" />
                ) : node.status === "succeeded" ? (
                  <CheckCircle2 size={11} className="mr-1" />
                ) : (
                  <Clock3 size={11} className="mr-1" />
                )}
                {workflowNodeStatusLabel(node, t)}
              </span>
              <span
                className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium ${SAVE_STATUS_CLASS_NAMES[saveStatus]}`}
              >
                {saveStatus === "saving" ? (
                  <Loader2 size={11} className="mr-1 animate-spin" />
                ) : saveStatus === "saved" ? (
                  <CheckCircle2 size={11} className="mr-1" />
                ) : saveStatus === "failed" ? (
                  <XCircle size={11} className="mr-1" />
                ) : null}
                {t(SAVE_STATUS_LABEL_KEYS[saveStatus])}
              </span>
            </div>
            {node.last_run_at ? (
              <div className="mt-2 text-[11px] text-zinc-400 dark:text-slate-400">
                {t("detail.inspector.lastRun", { time: formatDateTime(node.last_run_at, t.locale) })}
              </div>
            ) : null}
          </div>
        </div>

        {activeRunContext ? (
          <div className="mt-4 rounded-xl border border-indigo-100 bg-indigo-50 px-3 py-2.5 text-xs leading-5 text-indigo-700 dark:border-violet-400/30 dark:bg-violet-500/10 dark:text-violet-100">
            <div className="flex items-start gap-2">
              <Loader2 size={14} className="mt-0.5 shrink-0 animate-spin" />
              <div className="min-w-0">
                <div className="font-semibold">
                  {activeRunContext.nodeRun.status === "queued"
                    ? t("detail.inspector.activeRunQueued")
                    : t("detail.inspector.activeRunRunning")}
                </div>
                {activeRunNodeText ? (
                  <div className="mt-0.5 text-indigo-600/85 dark:text-violet-100/80">{activeRunNodeText}</div>
                ) : null}
                {activeRunQueueText ? (
                  <div className="mt-1 text-indigo-600/75 dark:text-violet-100/70">{activeRunQueueText}</div>
                ) : null}
                <div className="mt-1 flex flex-wrap gap-x-2 gap-y-0.5 text-[10px] text-indigo-600/70 dark:text-violet-100/60">
                  <span>{t("detail.nodeRunStarted", { time: formatDateTime(activeRunContext.nodeRun.started_at, t.locale) })}</span>
                  {activeRunContext.run.is_cancelable ? <span>{t("detail.runCancelable")}</span> : null}
                </div>
              </div>
            </div>
          </div>
        ) : null}

        {node.node_type !== "product_context" || onCancelRun ? (
          <div
            className={`mt-4 grid gap-2 ${
              node.node_type === "product_context" ? "grid-cols-1" : onCancelRun ? "grid-cols-3" : "grid-cols-2"
            }`}
          >
            {node.node_type !== "product_context" ? (
              <button
                type="button"
                onClick={onRun}
                disabled={runActionState.disabled}
                className="inline-flex items-center justify-center rounded-xl px-3 py-2.5 text-xs font-semibold btn-primary-spring"
                title={runActionState.title}
              >
                {runActionState.pending ? (
                  <Loader2 size={13} className="mr-1.5 animate-spin" />
                ) : (
                  <Play size={13} className="mr-1.5" />
                )}
                {runActionState.label}
              </button>
            ) : null}
            {onCancelRun ? (
              <button
                type="button"
                onClick={onCancelRun}
                disabled={cancelBusy}
                className="inline-flex items-center justify-center rounded-xl px-3 py-2.5 text-xs font-semibold btn-danger-spring"
                title={t("detail.inspector.cancelCurrentRun")}
              >
                {cancelBusy ? (
                  <Loader2 size={13} className="mr-1.5 animate-spin" />
                ) : (
                  <OctagonX size={13} className="mr-1.5" />
                )}
                {t("detail.cancel")}
              </button>
            ) : null}
            {node.node_type !== "product_context" ? (
              <button
                type="button"
                onClick={onDelete}
                disabled={busy}
                className="inline-flex items-center justify-center rounded-xl px-3 py-2.5 text-xs font-semibold btn-danger-spring"
              >
                <Trash2 size={13} className="mr-1.5" /> {t("detail.delete")}
              </button>
            ) : null}
          </div>
        ) : null}
      </section>

      <section className="config-bubble rounded-2xl p-4 shadow-sm">
        <div className="mb-3 text-[10px] font-semibold uppercase tracking-widest text-zinc-500 dark:text-slate-300">
          {t("detail.inspector.config")}
        </div>
        <label className="mb-3 block">
          <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
            {t("detail.inspector.nodeName")}
          </span>
          <input
            value={draft.title}
            onChange={(event) =>
              onDraftChange({ ...draft, title: event.target.value })
            }
            className="w-full px-3 py-2.5 text-sm outline-none input-premium"
          />
        </label>
        <div className="mb-3 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-xs leading-5 text-slate-600 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300">
          {node.node_type === "image_generation"
            ? t("detail.inspector.description.imageGeneration")
            : node.node_type === "reference_image"
              ? t("detail.inspector.description.referenceImage")
              : node.node_type === "copy_generation"
                ? t("detail.inspector.description.copyGeneration")
                : t("detail.inspector.description.productContext")}
        </div>

        {node.node_type === "product_context" ? (
          <ProductContextInspector
            product={product}
            sourceImage={sourceImage}
            draft={draft}
            onDraftChange={onDraftChange}
            t={t}
          />
        ) : null}
        {node.node_type === "reference_image" ? (
          <ReferenceImageInspector
            draft={draft}
            onDraftChange={onDraftChange}
            onUploadImage={onUploadImage}
            busy={busy}
            hasImage={hasReferenceImage}
            image={referenceImage}
            onPreviewImage={onPreviewImage}
            t={t}
          />
        ) : null}
        {node.node_type === "copy_generation" ? (
          <CopyNodeInspector
            node={node}
            draft={draft}
            onDraftChange={onDraftChange}
            t={t}
          />
        ) : null}
        {node.node_type === "image_generation" ? (
          <ImageGenerationInspector
            node={node}
            draft={draft}
            imageSizeOptions={imageSizeOptions}
            imageGenerationMaxDimension={imageGenerationMaxDimension}
            imageToolAllowedFields={imageToolAllowedFields}
            onDraftChange={onDraftChange}
            downstreamReferenceCount={downstreamReferenceCount}
            onPreviewPrompt={setPromptPreview}
            t={t}
          />
        ) : null}
      </section>
      {node.failure_reason ? (
        <section
          className={`rounded-2xl border p-4 text-xs leading-relaxed shadow-sm ${
            node.status === "cancelled"
              ? "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300"
              : "border-red-200 bg-red-50 text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200"
          }`}
        >
          <AlertCircle size={13} className="mr-1.5 inline" />
          {node.failure_reason}
          {node.status === "failed" && node.is_retryable && node.node_type !== "product_context" ? (
            <div className="mt-2 font-semibold text-red-700 dark:text-red-100">{t("detail.inspector.retryableCurrent")}</div>
          ) : null}
          {node.status === "failed" && !node.is_retryable ? (
            <div className="mt-2 font-semibold text-red-700 dark:text-red-100">{t("detail.notRetryable")}</div>
          ) : null}
          {node.attempt_count > 0 ? (
            <div className="mt-2 text-[11px] text-red-700/85 dark:text-red-100/85">
              {t("detail.nodeAttemptSummary", { attempts: node.attempt_count, retries: node.retry_count })}
            </div>
          ) : null}
          {node.status === "failed" && !node.is_retryable && node.non_retryable_reason ? (
            <div className="mt-1 text-[11px] text-red-700/85 dark:text-red-100/85">
              {t("detail.nonRetryableReason", { reason: node.non_retryable_reason })}
            </div>
          ) : null}
          {node.status === "failed" && node.retry_hint ? (
            <div className="mt-1 text-[11px] text-red-700/85 dark:text-red-100/85">
              {workflowRetryHintLabel(node.retry_hint, t)}
            </div>
          ) : null}
        </section>
      ) : null}
      {promptPreview ? (
        <PromptPreviewDialog preview={promptPreview} onClose={() => setPromptPreview(null)} />
      ) : null}
    </div>
  );
}

function ProductContextInspector({
  product,
  sourceImage,
  draft,
  onDraftChange,
  t,
}: {
  product: ProductDetail;
  sourceImage: DownloadableImage | null;
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
  t: TFunction;
}) {
  return (
    <div className="space-y-3">
      <div
        className={`group relative flex h-40 items-center justify-center overflow-hidden rounded-2xl border border-slate-200 bg-white/50 p-2 shadow-sm transition-all duration-300 ease-out hover:-translate-y-0.5 hover:border-indigo-300 dark:border-slate-800 dark:bg-slate-950/40 dark:hover:border-violet-400/50 hover:shadow-[0_8px_20px_-6px_rgba(99,102,241,0.15)] dark:hover:shadow-[0_8px_20px_-6px_rgba(139,92,246,0.3)] ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
      >
        {sourceImage ? (
          <>
            <img
              src={sourceImage.previewUrl}
              alt={sourceImage.alt}
              className="h-full w-full object-contain transition-transform duration-300 ease-out group-hover:scale-[1.03]"
            />
            <DownloadLink image={sourceImage} variant="overlay" />
          </>
        ) : (
          <div className="text-xs text-zinc-400 dark:text-slate-500">{t("detail.inspector.noSourceImage")}</div>
        )}
      </div>
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
          {t("detail.inspector.productName")}
        </span>
        <input
          value={draft.productName}
          onChange={(event) =>
            onDraftChange({ ...draft, productName: event.target.value })
          }
          className="w-full px-3 py-2 text-xs outline-none input-premium"
        />
      </label>
      <div className="grid grid-cols-2 gap-2">
        <label className="block">
          <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
            {t("detail.inspector.category")}
          </span>
          <input
            value={draft.category}
            onChange={(event) =>
              onDraftChange({ ...draft, category: event.target.value })
            }
            className="w-full px-3 py-2 text-xs outline-none input-premium"
          />
        </label>
        <label className="block">
          <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
            {t("detail.inspector.price")}
          </span>
          <input
            value={draft.price}
            onChange={(event) =>
              onDraftChange({ ...draft, price: event.target.value })
            }
            className="w-full px-3 py-2 text-xs outline-none input-premium"
          />
        </label>
      </div>
      <TextArea
        label={t("detail.inspector.productDescription")}
        value={draft.sourceNote}
        onChange={(value) => onDraftChange({ ...draft, sourceNote: value })}
      />
      <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-xs text-zinc-500 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-400">
        {t("detail.inspector.originalProduct", { name: product.name })}
        {product.category ? ` · ${product.category}` : ""}
        {product.price ? ` · ${formatPrice(product.price)}` : ""}
      </div>
    </div>
  );
}

function ReferenceImageInspector({
  draft,
  onDraftChange,
  onUploadImage,
  busy,
  hasImage,
  image,
  onPreviewImage,
  t,
}: {
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
  onUploadImage: (file: File) => void;
  busy: boolean;
  hasImage: boolean;
  image: DownloadableImage | null;
  onPreviewImage: (image: DownloadableImage) => void;
  t: TFunction;
}) {
  return (
    <div className="space-y-3">
      {image ? (
        <div
          className={`group relative flex aspect-[4/3] min-h-[180px] w-full items-center justify-center overflow-hidden rounded-2xl border border-slate-200 bg-white/50 p-3 shadow-sm transition-all duration-300 ease-out hover:-translate-y-0.5 hover:border-indigo-300 dark:border-slate-800 dark:bg-slate-950/40 dark:hover:border-violet-400/50 hover:shadow-[0_8px_20px_-6px_rgba(99,102,241,0.15)] dark:hover:shadow-[0_8px_20px_-6px_rgba(139,92,246,0.3)] ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
        >
          <button
            type="button"
            onClick={() => onPreviewImage(image)}
            className="flex h-full w-full items-center justify-center rounded-lg focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2"
            aria-label={t("detail.inspector.preview", { alt: image.alt })}
          >
            <img src={image.previewUrl} alt={image.alt} className="h-full w-full object-contain transition-transform duration-300 ease-out group-hover:scale-[1.03]" />
            <span className="pointer-events-none absolute bottom-2 left-2 rounded-md bg-zinc-950/70 px-2 py-1 text-[11px] font-medium text-white opacity-0 transition-opacity group-hover:opacity-100">
              {t("detail.inspector.clickPreview")}
            </span>
          </button>
          <DownloadLink image={image} variant="overlay" />
          <div className="absolute left-2 top-2 inline-flex items-center rounded-full border border-violet-400/60 bg-slate-950/88 px-2.5 py-1 text-[11px] font-semibold text-violet-100 shadow-lg shadow-violet-950/35 ring-1 ring-violet-300/20 backdrop-blur">
            <Sparkles size={12} className="mr-1 text-violet-300" />
            {t("detail.canUseAsReference")}
          </div>
        </div>
      ) : null}
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
          {t("detail.inspector.label")}
        </span>
        <input
          value={draft.label}
          onChange={(event) =>
            onDraftChange({ ...draft, label: event.target.value })
          }
          className="w-full px-3 py-2 text-xs outline-none input-premium"
        />
      </label>
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
          {t("detail.inspector.role")}
        </span>
        <SelectField
          value={draft.role}
          options={REFERENCE_ROLE_OPTIONS.map((option) => ({
            value: option.value,
            label: t(option.labelKey),
          }))}
          onChange={(nextValue) => onDraftChange({ ...draft, role: nextValue })}
          radius="lg"
          visualSize="sm"
        />
      </label>
      <ImageDropZone
        ariaLabel={hasImage ? t("detail.inspector.replaceReference") : t("detail.inspector.uploadReference")}
        disabled={busy}
        className="flex cursor-pointer items-center justify-center rounded-2xl border border-dashed border-slate-300 px-3 py-6 text-xs font-medium text-zinc-600 transition-all duration-300 hover:border-indigo-500 hover:bg-indigo-50/20 hover:text-indigo-600 dark:border-slate-700/80 dark:text-slate-300 dark:hover:border-violet-400 dark:hover:bg-violet-500/5 dark:hover:text-violet-200"
        activeClassName="border-indigo-500 bg-indigo-50/60 text-indigo-700 shadow-[0_0_0_4px_rgba(99,102,241,0.12)] dark:border-violet-400 dark:bg-violet-500/12 dark:text-violet-100 dark:shadow-[0_0_0_4px_rgba(139,92,246,0.18)]"
        focusClassName="focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 dark:focus-visible:ring-violet-400 dark:focus-visible:ring-offset-slate-950"
        onFiles={(files) => {
          const file = files[0];
          if (file) {
            onUploadImage(file);
          }
        }}
      >
        {({ isDragging }) => (
          <div className={`flex h-full w-full items-center justify-center transition-transform duration-200 ${isDragging ? "scale-[1.03] text-indigo-600 dark:text-violet-300" : ""}`}>
            <Upload size={14} className={`mr-2 transition-transform duration-200 ${isDragging ? "-translate-y-0.5 scale-110" : ""}`} />
            {isDragging
              ? t("detail.inspector.dropUpload")
              : hasImage
                ? t("detail.inspector.replaceImage")
                : t("detail.inspector.uploadImage")}
          </div>
        )}
      </ImageDropZone>
    </div>
  );
}

function CopyNodeInspector({
  node,
  draft,
  onDraftChange,
  t,
}: {
  node: WorkflowNode;
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
  t: TFunction;
}) {
  const hasCopy = Boolean(
    node.output_json && outputText(node.output_json, "copy_set_id"),
  );
  const copyPayload = draft.copyStructuredPayload;
  return (
    <div className="space-y-3">
      <TextArea
        label={t("detail.inspector.copyInstruction")}
        value={draft.instruction}
        onChange={(value) => onDraftChange({ ...draft, instruction: value })}
      />
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
          {t("detail.inspector.tone")}
        </span>
        <input
          value={draft.tone}
          onChange={(event) =>
            onDraftChange({ ...draft, tone: event.target.value })
          }
          className="w-full px-3 py-2 text-xs outline-none input-premium"
        />
      </label>
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
          {t("detail.inspector.channel")}
        </span>
        <input
          value={draft.channel}
          onChange={(event) =>
            onDraftChange({ ...draft, channel: event.target.value })
          }
          className="w-full px-3 py-2 text-xs outline-none input-premium"
        />
      </label>
      {hasCopy ? (
        <div className="space-y-3 rounded-2xl border border-zinc-200 bg-zinc-50/80 p-3 dark:border-slate-700 dark:bg-[#0b1220]">
          <div className="text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
            {t("detail.inspector.editCopy")}
          </div>
          {copyPayload ? (
            <StructuredCopyEditor
              payload={copyPayload}
              onChange={(copyStructuredPayload) => onDraftChange({ ...draft, copyStructuredPayload })}
              t={t}
            />
          ) : null}
          <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-[11px] leading-5 text-zinc-500 dark:border-slate-700 dark:bg-[#151f33] dark:text-slate-400">
            {t("detail.inspector.copyAutosave")}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function StructuredCopyEditor({
  payload,
  onChange,
  t,
}: {
  payload: CopyPayloadV2;
  onChange: (payload: CopyPayloadV2) => void;
  t: TFunction;
}) {
  const content = payload.content;
  return (
    <div className="space-y-3">
      <TextArea
        label={t("detail.inspector.summary")}
        value={payload.summary}
        onChange={(summary) => onChange({ ...payload, summary })}
        minRows={1}
        maxRows={6}
      />
      {content.kind === "freeform" ? (
        <TextArea
          label={t("detail.inspector.body")}
          value={content.text}
          onChange={(text) => onChange({ ...payload, content: { kind: "freeform", text } })}
          minRows={3}
          maxRows={18}
        />
      ) : null}
      {content.kind === "blocks" ? (
        <div className="space-y-2">
          {content.blocks.map((block, index) => (
            <CopyBlockEditor
              key={block.id}
              block={block}
              onChange={(nextBlock) => {
                const blocks = [...content.blocks];
                blocks[index] = nextBlock;
                onChange({ ...payload, content: { kind: "blocks", blocks } });
              }}
              t={t}
            />
          ))}
        </div>
      ) : null}
      {content.kind === "layout_brief" ? (
        <div className="space-y-2">
          {content.sections.map((section, index) => (
            <CopySectionEditor
              key={section.id}
              section={section}
              onChange={(nextSection) => {
                const sections = [...content.sections];
                sections[index] = nextSection;
                onChange({ ...payload, content: { kind: "layout_brief", sections } });
              }}
              t={t}
            />
          ))}
        </div>
      ) : null}
      <OptionalTextArea
        label={t("detail.inspector.visualGuidance")}
        value={payload.visual_guidance?.composition_hint ?? ""}
        addLabel={t("detail.inspector.addVisualGuidance")}
        placeholder={t("detail.inspector.visualGuidancePlaceholder")}
        onChange={(composition_hint) =>
          onChange({
            ...payload,
            visual_guidance: {
              main_message: payload.visual_guidance?.main_message ?? "",
              hierarchy: payload.visual_guidance?.hierarchy ?? [],
              composition_hint,
              text_density: payload.visual_guidance?.text_density ?? "medium",
              avoid: payload.visual_guidance?.avoid ?? [],
            },
          })
        }
      />
    </div>
  );
}

function CopyBlockEditor({ block, onChange, t }: { block: CopyBlock; onChange: (block: CopyBlock) => void; t: TFunction }) {
  return (
    <div className="copy-editor-card space-y-3 p-3">
      <OptionalTextInput
        label={t("detail.inspector.label")}
        value={block.label ?? ""}
        addLabel={t("detail.inspector.addLabel")}
        placeholder={t("detail.inspector.label")}
        onChange={(label) => onChange({ ...block, label })}
      />
      <TextArea
        label={t("detail.inspector.body")}
        value={block.text}
        onChange={(text) => onChange({ ...block, text })}
        minRows={1}
        maxRows={12}
      />
      <OptionalTextArea
        label={t("detail.inspector.visualExpression")}
        value={block.visual_hint ?? ""}
        addLabel={t("detail.inspector.addVisualExpression")}
        placeholder={t("detail.inspector.visualExpressionPlaceholder")}
        onChange={(visual_hint) => onChange({ ...block, visual_hint })}
      />
    </div>
  );
}

function CopySectionEditor({ section, onChange, t }: { section: CopySection; onChange: (section: CopySection) => void; t: TFunction }) {
  return (
    <div className="copy-editor-card space-y-3 p-3">
      <OptionalTextInput
        label={t("detail.inspector.sectionTitle")}
        value={section.title ?? ""}
        addLabel={t("detail.inspector.addSectionTitle")}
        placeholder={t("detail.inspector.sectionTitle")}
        onChange={(title) => onChange({ ...section, title })}
      />
      <OptionalTextArea
        label={t("detail.inspector.description")}
        value={section.body ?? ""}
        addLabel={t("detail.inspector.addDescription")}
        placeholder={t("detail.inspector.sectionDescriptionPlaceholder")}
        onChange={(body) => onChange({ ...section, body })}
      />
      {section.items.length ? (
        <div className="space-y-1.5">
          <div className="text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
            {t("detail.inspector.items")}
          </div>
          <div className="space-y-1.5">
            {section.items.map((item, index) => (
              <CopySectionItemEditor
                key={item.id}
                block={item}
                onChange={(nextItem) => {
                  const items = [...section.items];
                  items[index] = nextItem;
                  onChange({ ...section, items });
                }}
                t={t}
              />
            ))}
          </div>
        </div>
      ) : null}
      <OptionalTextArea
        label={t("detail.inspector.visualGuidance")}
        value={section.visual_hint ?? ""}
        addLabel={t("detail.inspector.addVisualGuidance")}
        placeholder={t("detail.inspector.sectionVisualPlaceholder")}
        onChange={(visual_hint) => onChange({ ...section, visual_hint })}
      />
    </div>
  );
}

function CopySectionItemEditor({ block, onChange, t }: { block: CopyBlock; onChange: (block: CopyBlock) => void; t: TFunction }) {
  return (
    <div className="copy-editor-item space-y-2">
      <OptionalTextInput
        label={t("detail.inspector.label")}
        value={block.label ?? ""}
        addLabel={t("detail.inspector.addLabel")}
        placeholder={t("detail.inspector.label")}
        onChange={(label) => onChange({ ...block, label })}
      />
      <TextArea
        label={t("detail.inspector.body")}
        value={block.text}
        onChange={(text) => onChange({ ...block, text })}
        minRows={1}
        maxRows={8}
      />
      <OptionalTextArea
        label={t("detail.inspector.visualExpression")}
        value={block.visual_hint ?? ""}
        addLabel={t("detail.inspector.addVisualExpression")}
        placeholder={t("detail.inspector.itemVisualPlaceholder")}
        onChange={(visual_hint) => onChange({ ...block, visual_hint })}
      />
    </div>
  );
}

function OptionalTextInput({
  label,
  value,
  addLabel,
  placeholder,
  onChange,
}: {
  label: string;
  value: string;
  addLabel: string;
  placeholder?: string;
  onChange: (value: string) => void;
}) {
  const [isEditing, setIsEditing] = useState(hasText(value));
  const shouldShowInput = isEditing || hasText(value);

  if (!shouldShowInput) {
    return (
      <button
        type="button"
        className={ADD_COPY_FIELD_BUTTON_CLASS_NAME}
        onClick={() => setIsEditing(true)}
      >
        <Plus size={12} />
        {addLabel}
      </button>
    );
  }

  return (
    <label className="block">
      <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
        {label}
      </span>
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onBlur={() => {
          if (!hasText(value)) {
            setIsEditing(false);
          }
        }}
        placeholder={placeholder}
        className="w-full px-3 py-2 text-xs outline-none input-premium"
      />
    </label>
  );
}

function OptionalTextArea({
  label,
  value,
  addLabel,
  placeholder,
  onChange,
}: {
  label: string;
  value: string;
  addLabel: string;
  placeholder?: string;
  onChange: (value: string) => void;
}) {
  const [isEditing, setIsEditing] = useState(hasText(value));
  const shouldShowTextArea = isEditing || hasText(value);

  if (!shouldShowTextArea) {
    return (
      <button
        type="button"
        className={ADD_COPY_FIELD_BUTTON_CLASS_NAME}
        onClick={() => setIsEditing(true)}
      >
        <Plus size={12} />
        {addLabel}
      </button>
    );
  }

  return (
    <TextArea
      label={label}
      value={value}
      onChange={onChange}
      onBlur={() => {
        if (!hasText(value)) {
          setIsEditing(false);
        }
      }}
      minRows={1}
      maxRows={12}
      placeholder={placeholder}
    />
  );
}

function hasText(value: string | null | undefined): boolean {
  return Boolean(value?.trim());
}

function ImageGenerationInspector({
  node,
  draft,
  imageSizeOptions,
  imageGenerationMaxDimension,
  imageToolAllowedFields,
  onDraftChange,
  downstreamReferenceCount,
  onPreviewPrompt,
  t,
}: {
  node: WorkflowNode;
  draft: NodeConfigDraft;
  imageSizeOptions: ImageSizeOption[];
  imageGenerationMaxDimension: number;
  imageToolAllowedFields: readonly ImageToolOptionKey[];
  onDraftChange: (draft: NodeConfigDraft) => void;
  downstreamReferenceCount: number;
  onPreviewPrompt: (preview: PromptPreview) => void;
  t: TFunction;
}) {
  const [settingsTab, setSettingsTab] = useState<ImageGenerationSettingsTab>("basic");
  const savedInstruction = node.output_json ? outputText(node.output_json, "instruction") : "";
  const previewText = savedInstruction || draft.instruction;
  const promptMeta = savedInstruction ? t("detail.inspector.savedPromptMeta") : t("detail.inspector.currentDraft");

  return (
    <div className="space-y-3">
      {downstreamReferenceCount === 0 ? (
        <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-400/35 dark:bg-amber-500/10 dark:text-amber-200">
          {t("detail.inspector.connectImageNodeFirst")}
        </div>
      ) : null}
      <ImageGenerationSettingsTabs
        value={settingsTab}
        onChange={setSettingsTab}
        basic={
          <div className="space-y-3">
            <div className="rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 dark:border-slate-700 dark:bg-[#0b1220]">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <div className="text-xs font-semibold text-slate-700 dark:text-slate-200">{t("detail.inspector.generationCount")}</div>
                  <div className="mt-1 text-[11px] leading-5 text-slate-500 dark:text-slate-400">
                    {t("detail.inspector.downstreamImageCount", { count: downstreamReferenceCount })}
                  </div>
                </div>
                <span className="shrink-0 rounded-full border border-slate-200 bg-white px-2.5 py-1 text-xs font-semibold text-slate-700 dark:border-slate-700 dark:bg-[#151f33] dark:text-slate-200">
                  {t("detail.inspector.imageCount", { count: downstreamReferenceCount })}
                </span>
              </div>
            </div>
            <TextArea
              label={t("detail.inspector.imageDescription")}
              value={draft.instruction}
              onChange={(value) => onDraftChange({ ...draft, instruction: value })}
            />
            {previewText.trim() ? (
              <button
                type="button"
                onClick={() =>
                  onPreviewPrompt({
                    title: t("detail.inspector.imagePrompt"),
                    text: previewText,
                    meta: promptMeta,
                  })
                }
                className="inline-flex w-full items-center justify-center rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs font-medium text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-950 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:border-violet-400/45 dark:hover:bg-violet-500/12 dark:hover:text-white"
              >
                <FileText size={13} className="mr-1.5" />
                {t("detail.inspector.reviewPrompt")}
              </button>
            ) : null}
            <ImageGenerationSettingsPanel
              surface="plain"
              size={draft.size}
              sizeOptions={imageSizeOptions}
              maxDimension={imageGenerationMaxDimension}
              toolOptions={draft.toolOptions}
              allowedToolFields={imageToolAllowedFields}
              onSizeChange={(size) => onDraftChange({ ...draft, size })}
              onToolOptionsChange={(toolOptions) => onDraftChange({ ...draft, toolOptions })}
              showToolOptions={false}
            />
          </div>
        }
        advanced={
          <ImageToolControls
            surface="plain"
            value={draft.toolOptions}
            allowedFields={imageToolAllowedFields}
            onChange={(toolOptions) => onDraftChange({ ...draft, toolOptions })}
          />
        }
      />
    </div>
  );
}
