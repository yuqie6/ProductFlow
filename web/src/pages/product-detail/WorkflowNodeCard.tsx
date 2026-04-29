import type { PointerEvent as ReactPointerEvent } from "react";
import {
  FileText,
  Image as ImageIcon,
  ImagePlus,
  Loader2,
  Play,
  Trash2,
} from "lucide-react";

import { formatDateTime } from "../../lib/format";
import type { DownloadableImage } from "../../lib/image-downloads";
import type { WorkflowNode } from "../../lib/types";
import { DownloadLink } from "./ImageDownloadComponents";
import {
  IMAGE_PREVIEW_SURFACE_CLASS_NAME,
  NODE_LABELS,
} from "./constants";
import type { CanvasPoint } from "./types";
import {
  imageWorkflowNodeWaitingLabel,
  isImageWorkflowNodeWaiting,
  statusClass,
  workflowNodeStatusLabel,
} from "./utils";

interface WorkflowNodeCardProps {
  node: WorkflowNode;
  nodeRef: (element: HTMLDivElement | null) => void;
  position: CanvasPoint;
  image: DownloadableImage | null;
  selected: boolean;
  dragging: boolean;
  onSelect: () => void;
  onStartDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onMoveDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onEndDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onCancelDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onStartConnection: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onMoveConnection: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onEndConnection: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onRun: () => void;
  onDelete: () => void;
  busy: boolean;
  runBusy: boolean;
}

export function WorkflowNodeCard({
  node,
  nodeRef,
  position,
  image,
  selected,
  dragging,
  onSelect,
  onStartDrag,
  onMoveDrag,
  onEndDrag,
  onCancelDrag,
  onStartConnection,
  onMoveConnection,
  onEndConnection,
  onRun,
  onDelete,
  busy,
  runBusy,
}: WorkflowNodeCardProps) {
  const icon = {
    product_context: FileText,
    reference_image: ImagePlus,
    copy_generation: FileText,
    image_generation: ImageIcon,
  }[node.node_type];
  const Icon = icon;
  const imageWaiting = isImageWorkflowNodeWaiting(node);
  const waitingLabel = imageWorkflowNodeWaitingLabel(node);

  return (
    <div
      ref={nodeRef}
      data-workflow-node-id={node.id}
      className={`absolute w-[248px] touch-none select-none rounded-2xl border bg-white/95 p-3 text-left shadow-sm backdrop-blur ${
        dragging ? "cursor-grabbing" : "transition-[border-color,box-shadow] hover:shadow-md"
      } ${
        selected ? "border-indigo-300 shadow-lg shadow-indigo-950/10 ring-2 ring-indigo-200/70" : "border-slate-200"
      }`}
      style={{
        left: 0,
        top: 0,
        transform: `translate3d(${position.x}px, ${position.y}px, 0)`,
        willChange: dragging ? "transform" : undefined,
      }}
      onPointerDown={onStartDrag}
      onPointerMove={onMoveDrag}
      onPointerUp={onEndDrag}
      onPointerCancel={onCancelDrag}
      onLostPointerCapture={onCancelDrag}
    >
      <button
        type="button"
        data-node-action
        data-workflow-target-node-id={node.id}
        onClick={onSelect}
        className="absolute left-[-9px] top-[47px] z-20 h-[18px] w-[18px] rounded-full border border-slate-300 bg-white shadow-sm hover:border-indigo-400 hover:ring-4 hover:ring-indigo-100"
        title="输入 handle"
        aria-label={`${node.title} 输入 handle`}
      />
      <button
        type="button"
        data-node-action
        onPointerDown={onStartConnection}
        onPointerMove={onMoveConnection}
        onPointerUp={onEndConnection}
        onPointerCancel={onEndConnection}
        className="absolute right-[-10px] top-[46px] z-20 h-5 w-5 rounded-full border-2 border-indigo-500 bg-white shadow-sm hover:bg-indigo-50 hover:ring-4 hover:ring-indigo-100"
        title="拖拽连接输出"
        aria-label={`${node.title} 输出 handle`}
      />
      <div onClick={onSelect} className="cursor-grab active:cursor-grabbing">
        <div className="mb-3 flex items-start justify-between gap-2">
          <div className="flex min-w-0 gap-2">
            <span className="mt-0.5 rounded-xl border border-slate-200 bg-slate-50 p-1.5 text-slate-500">
              <Icon size={14} />
            </span>
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold text-zinc-900">
                {node.title}
              </div>
              <div className="mt-0.5 text-[10px] uppercase tracking-wider text-zinc-400">
                {NODE_LABELS[node.node_type]}
              </div>
            </div>
          </div>
          <span
            className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusClass(node.status)}`}
          >
            {workflowNodeStatusLabel(node)}
          </span>
        </div>
        {image ? (
          <div
            className={`relative mb-2 flex h-28 items-center justify-center overflow-hidden rounded-xl border border-zinc-100 p-2 ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
          >
            <img
              src={image.previewUrl}
              alt={image.alt}
              className="h-full w-full object-contain"
            />
            <DownloadLink image={image} variant="overlay" />
            {imageWaiting ? (
              <div className="absolute inset-x-2 bottom-2 flex items-center justify-center rounded-lg bg-white/90 px-2 py-1 text-[11px] font-medium text-indigo-700 shadow-sm ring-1 ring-indigo-100 backdrop-blur">
                <Loader2 size={11} className="mr-1 animate-spin" />
                {waitingLabel}
              </div>
            ) : null}
          </div>
        ) : imageWaiting ? (
          <div className="mb-2 flex h-28 flex-col items-center justify-center rounded-xl border border-indigo-100 bg-indigo-50 text-indigo-700">
            <Loader2 size={18} className="animate-spin" />
            <div className="mt-2 text-xs font-medium">{waitingLabel}</div>
          </div>
        ) : null}
        {node.failure_reason ? (
          <div className="line-clamp-2 rounded-lg border border-red-100 bg-red-50 px-2.5 py-1.5 text-xs leading-relaxed text-red-700">
            {node.failure_reason}
          </div>
        ) : null}
      </div>
      <div className="mt-3 flex items-center justify-between text-[10px] text-zinc-400">
        <span>{node.last_run_at ? `最近 ${formatDateTime(node.last_run_at)}` : NODE_LABELS[node.node_type]}</span>
        {node.node_type !== "product_context" ? (
          <div className="flex items-center gap-1.5">
            <button
              type="button"
              data-node-action
              onClick={onDelete}
              disabled={busy}
              className="inline-flex items-center rounded border border-zinc-200 px-2 py-1 text-[11px] font-medium text-red-500 hover:border-red-300 hover:bg-red-50 disabled:opacity-50"
            >
              <Trash2 size={11} className="mr-1" /> 删除
            </button>
            <button
              type="button"
              data-node-action
              onClick={onRun}
              disabled={runBusy}
              className="inline-flex items-center rounded border border-zinc-200 px-2 py-1 text-[11px] font-medium text-zinc-600 hover:border-zinc-900 hover:text-zinc-900 disabled:opacity-50"
            >
              {runBusy ? (
                <Loader2 size={11} className="mr-1 animate-spin" />
              ) : (
                <Play size={11} className="mr-1" />
              )}
              运行
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
