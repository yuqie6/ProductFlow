import {
  AlertCircle,
  CheckCircle2,
  Clock3,
  FileText,
  Image as ImageIcon,
  ImagePlus,
  Loader2,
  Play,
  Trash2,
  Upload,
  XCircle,
} from "lucide-react";

import { ImageDropZone } from "../../components/ImageDropZone";
import { ImageSizePicker } from "../../components/ImageSizePicker";
import type { DownloadableImage } from "../../lib/image-downloads";
import type { ImageSizeOption } from "../../lib/imageSizes";
import { formatDateTime, formatPrice } from "../../lib/format";
import type { ProductDetail, ProductWorkflow, WorkflowNode } from "../../lib/types";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME, NODE_LABELS, NODE_STATUS_LABELS } from "./constants";
import { DownloadLink } from "./ImageDownloadComponents";
import { getNodeImageDownload } from "./imageDownloads";
import type { NodeConfigDraft, SaveStatus } from "./types";
import { outputText, statusClass } from "./utils";
import { TextArea } from "./TextArea";

const SAVE_STATUS_LABELS: Record<SaveStatus, string> = {
  idle: "自动保存",
  saving: "保存中",
  saved: "已保存",
  failed: "保存失败",
};

const SAVE_STATUS_CLASS_NAMES: Record<SaveStatus, string> = {
  idle: "border-zinc-200 bg-zinc-50 text-zinc-500",
  saving: "border-blue-200 bg-blue-50 text-blue-700",
  saved: "border-emerald-200 bg-emerald-50 text-emerald-700",
  failed: "border-red-200 bg-red-50 text-red-700",
};

interface InspectorPanelProps {
  product: ProductDetail;
  sourceImage: DownloadableImage | null;
  workflow: ProductWorkflow | null;
  node: WorkflowNode;
  draft: NodeConfigDraft;
  imageSizeOptions: ImageSizeOption[];
  imageGenerationMaxDimension: number;
  onDraftChange: (draft: NodeConfigDraft) => void;
  onPreviewImage: (image: DownloadableImage) => void;
  onRun: () => void;
  onUploadImage: (file: File) => void;
  onDelete: () => void;
  busy: boolean;
  runBusy: boolean;
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
  onDraftChange,
  onPreviewImage,
  onRun,
  onUploadImage,
  onDelete,
  busy,
  runBusy,
  saveStatus,
}: InspectorPanelProps) {
  const icon = {
    product_context: FileText,
    reference_image: ImagePlus,
    copy_generation: FileText,
    image_generation: ImageIcon,
  }[node.node_type];
  const InspectorIcon = icon;
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
  const referenceImage = node.node_type === "reference_image" ? getNodeImageDownload(node, product) : null;

  return (
    <div className="space-y-3">
      <section className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm shadow-slate-200/50">
        <div className="flex items-start gap-3">
          <span className="rounded-xl border border-indigo-100 bg-indigo-50 p-2 text-indigo-700">
            <InspectorIcon size={16} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="truncate text-base font-semibold text-zinc-950">
              {draft.title || node.title}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-1.5">
              <span className="rounded-full border border-zinc-200 bg-zinc-50 px-2 py-0.5 text-[10px] font-medium text-zinc-600">
                {NODE_LABELS[node.node_type]}
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
                {NODE_STATUS_LABELS[node.status]}
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
                {SAVE_STATUS_LABELS[saveStatus]}
              </span>
            </div>
            {node.last_run_at ? (
              <div className="mt-2 text-[11px] text-zinc-400">
                最近 {formatDateTime(node.last_run_at)}
              </div>
            ) : null}
          </div>
        </div>

        {node.node_type !== "product_context" ? (
          <div className="mt-4 grid grid-cols-2 gap-2">
            <button
              type="button"
              onClick={onRun}
              disabled={runBusy}
              className="inline-flex items-center justify-center rounded-xl bg-indigo-600 px-3 py-2 text-xs font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
            >
              {runBusy ? (
                <Loader2 size={13} className="mr-1.5 animate-spin" />
              ) : (
                <Play size={13} className="mr-1.5" />
              )}
              运行
            </button>
            <button
              type="button"
              onClick={onDelete}
              disabled={busy}
              className="inline-flex items-center justify-center rounded-lg border border-red-200 bg-white px-3 py-2 text-xs font-medium text-red-600 hover:bg-red-50 disabled:opacity-50"
            >
              <Trash2 size={13} className="mr-1.5" /> 删除
            </button>
          </div>
        ) : null}
      </section>

      <section className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm shadow-slate-200/50">
        <div className="mb-3 text-[10px] font-semibold uppercase tracking-widest text-zinc-500">
          配置
        </div>
        <label className="mb-3 block">
          <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
            节点名称
          </span>
          <input
            value={draft.title}
            onChange={(event) =>
              onDraftChange({ ...draft, title: event.target.value })
            }
            className="w-full rounded-lg border border-zinc-200 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
          />
        </label>

        {node.node_type === "product_context" ? (
          <ProductContextInspector
            product={product}
            sourceImage={sourceImage}
            draft={draft}
            onDraftChange={onDraftChange}
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
          />
        ) : null}
        {node.node_type === "copy_generation" ? (
          <CopyNodeInspector
            node={node}
            draft={draft}
            onDraftChange={onDraftChange}
          />
        ) : null}
        {node.node_type === "image_generation" ? (
            <ImageGenerationInspector
              draft={draft}
              imageSizeOptions={imageSizeOptions}
              imageGenerationMaxDimension={imageGenerationMaxDimension}
              onDraftChange={onDraftChange}
              downstreamReferenceCount={downstreamReferenceCount}
            />
        ) : null}
      </section>
      {node.failure_reason ? (
        <section className="rounded-2xl border border-red-200 bg-red-50 p-4 text-xs leading-relaxed text-red-700 shadow-sm">
          <AlertCircle size={13} className="mr-1.5 inline" />
          {node.failure_reason}
        </section>
      ) : null}
    </div>
  );
}

function ProductContextInspector({
  product,
  sourceImage,
  draft,
  onDraftChange,
}: {
  product: ProductDetail;
  sourceImage: DownloadableImage | null;
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
}) {
  return (
    <div className="space-y-3">
      <div
        className={`relative flex h-40 items-center justify-center overflow-hidden rounded-xl border border-zinc-200 p-2 ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
      >
        {sourceImage ? (
          <>
            <img
              src={sourceImage.previewUrl}
              alt={sourceImage.alt}
              className="h-full w-full object-contain"
            />
            <DownloadLink image={sourceImage} variant="overlay" />
          </>
        ) : (
          <div className="text-xs text-zinc-400">暂无商品源图</div>
        )}
      </div>
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          商品名称
        </span>
        <input
          value={draft.productName}
          onChange={(event) =>
            onDraftChange({ ...draft, productName: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        />
      </label>
      <div className="grid grid-cols-2 gap-2">
        <label className="block">
          <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
            类目
          </span>
          <input
            value={draft.category}
            onChange={(event) =>
              onDraftChange({ ...draft, category: event.target.value })
            }
            className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
          />
        </label>
        <label className="block">
          <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
            价格
          </span>
          <input
            value={draft.price}
            onChange={(event) =>
              onDraftChange({ ...draft, price: event.target.value })
            }
            className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
          />
        </label>
      </div>
      <TextArea
        label="商品描述"
        value={draft.sourceNote}
        onChange={(value) => onDraftChange({ ...draft, sourceNote: value })}
      />
      <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-xs text-zinc-500">
        原始商品：{product.name}
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
}: {
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
  onUploadImage: (file: File) => void;
  busy: boolean;
  hasImage: boolean;
  image: DownloadableImage | null;
  onPreviewImage: (image: DownloadableImage) => void;
}) {
  return (
    <div className="space-y-3">
      {image ? (
        <div
          className={`group relative flex aspect-[4/3] min-h-[180px] w-full items-center justify-center overflow-hidden rounded-xl border border-zinc-200 p-3 transition-colors hover:border-indigo-300 ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
        >
          <button
            type="button"
            onClick={() => onPreviewImage(image)}
            className="flex h-full w-full items-center justify-center rounded-lg focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2"
            aria-label={`预览 ${image.alt}`}
          >
            <img src={image.previewUrl} alt={image.alt} className="h-full w-full object-contain" />
            <span className="pointer-events-none absolute bottom-2 left-2 rounded-md bg-zinc-950/70 px-2 py-1 text-[11px] font-medium text-white opacity-0 transition-opacity group-hover:opacity-100">
              点击预览
            </span>
          </button>
          <DownloadLink image={image} variant="overlay" />
        </div>
      ) : null}
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          标签
        </span>
        <input
          value={draft.label}
          onChange={(event) =>
            onDraftChange({ ...draft, label: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        />
      </label>
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          角色
        </span>
        <select
          value={draft.role}
          onChange={(event) =>
            onDraftChange({ ...draft, role: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        >
          <option value="reference">参考图</option>
          <option value="style">风格图</option>
          <option value="product_angle">商品角度</option>
        </select>
      </label>
      <ImageDropZone
        ariaLabel={hasImage ? "替换参考图" : "上传参考图"}
        disabled={busy}
        className="flex cursor-pointer items-center justify-center rounded-md border border-dashed border-zinc-300 px-3 py-6 text-xs font-medium text-zinc-600 transition-colors hover:bg-zinc-50"
        onFiles={(files) => {
          const file = files[0];
          if (file) {
            onUploadImage(file);
          }
        }}
      >
        {({ isDragging }) => (
          <>
            <Upload size={14} className="mr-2" />
            {isDragging ? "松开以上传图片" : hasImage ? "拖拽或点击替换图片" : "拖拽或点击上传图片"}
          </>
        )}
      </ImageDropZone>
    </div>
  );
}

function CopyNodeInspector({
  node,
  draft,
  onDraftChange,
}: {
  node: WorkflowNode;
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
}) {
  const hasCopy = Boolean(
    node.output_json && outputText(node.output_json, "copy_set_id"),
  );
  return (
    <div className="space-y-3">
      <TextArea
        label="文案指令"
        value={draft.instruction}
        onChange={(value) => onDraftChange({ ...draft, instruction: value })}
      />
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          语气
        </span>
        <input
          value={draft.tone}
          onChange={(event) =>
            onDraftChange({ ...draft, tone: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        />
      </label>
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          渠道
        </span>
        <input
          value={draft.channel}
          onChange={(event) =>
            onDraftChange({ ...draft, channel: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        />
      </label>
      {hasCopy ? (
        <div className="space-y-3 rounded-md border border-zinc-200 bg-zinc-50 p-3">
          <div className="text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
            编辑文案
          </div>
          <label className="block">
            <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
              标题
            </span>
            <input
              value={draft.copyTitle}
              onChange={(event) =>
                onDraftChange({ ...draft, copyTitle: event.target.value })
              }
              className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
            />
          </label>
          <TextArea
            label="卖点"
            value={draft.copySellingPoints}
            onChange={(value) =>
              onDraftChange({ ...draft, copySellingPoints: value })
            }
          />
          <label className="block">
            <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
              海报标题
            </span>
            <input
              value={draft.copyPosterHeadline}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  copyPosterHeadline: event.target.value,
                })
              }
              className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
            />
          </label>
          <label className="block">
            <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
              CTA
            </span>
            <input
              value={draft.copyCta}
              onChange={(event) =>
                onDraftChange({ ...draft, copyCta: event.target.value })
              }
              className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
            />
          </label>
          <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-[11px] leading-5 text-zinc-500">
            文案编辑会自动保存；运行前也会先同步当前草稿。
          </div>
        </div>
      ) : null}
    </div>
  );
}

function ImageGenerationInspector({
  draft,
  imageSizeOptions,
  imageGenerationMaxDimension,
  onDraftChange,
  downstreamReferenceCount,
}: {
  draft: NodeConfigDraft;
  imageSizeOptions: ImageSizeOption[];
  imageGenerationMaxDimension: number;
  onDraftChange: (draft: NodeConfigDraft) => void;
  downstreamReferenceCount: number;
}) {
  return (
    <div className="space-y-3">
      {downstreamReferenceCount === 0 ? (
        <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800">
          请先连接一个参考图节点，生成结果会写入该节点。
        </div>
      ) : null}
      <TextArea
        label="生图"
        value={draft.instruction}
        onChange={(value) => onDraftChange({ ...draft, instruction: value })}
      />
      <div>
        <div className="mb-1.5 text-[10px] font-semibold uppercase tracking-widest text-zinc-400">尺寸</div>
        <ImageSizePicker
          value={draft.size}
          presets={imageSizeOptions}
          maxDimension={imageGenerationMaxDimension}
          onChange={(size) => onDraftChange({ ...draft, size })}
        />
      </div>
    </div>
  );
}
