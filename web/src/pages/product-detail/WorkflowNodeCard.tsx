import type { MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from "react";
import {
  Check,
  FileText,
  Image as ImageIcon,
  ImagePlus,
  Loader2,
  Play,
  Trash2,
} from "lucide-react";

import { formatDateTime } from "../../lib/format";
import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type { WorkflowNode } from "../../lib/types";
import { DownloadLink } from "./ImageDownloadComponents";
import {
  IMAGE_PREVIEW_SURFACE_CLASS_NAME,
} from "./constants";
import { workflowNodeDisplayLabel, workflowNodeDisplayTitle } from "./nodeDisplay";
import type { CanvasPoint } from "./types";
import {
  type WorkflowNodeRunActionState,
  imageWorkflowNodeWaitingLabel,
  isImageWorkflowNodeWaiting,
  statusClass,
  workflowNodeActivityText,
  workflowNodeStatusLabel,
} from "./utils";

interface WorkflowNodeCardProps {
  node: WorkflowNode;
  nodeRef: (element: HTMLDivElement | null) => void;
  position: CanvasPoint;
  image: DownloadableImage | null;
  primarySelected: boolean;
  secondarySelected: boolean;
  previewSelected: boolean;
  dragging: boolean;
  onSelect: (event: ReactMouseEvent<HTMLElement>) => void;
  onStartDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onMoveDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onEndDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onCancelDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onStartConnection: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onMoveConnection: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onEndConnection: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onCancelConnection: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onRun: () => void;
  onDelete: () => void;
  busy: boolean;
  runActionState: WorkflowNodeRunActionState;
}

export function WorkflowNodeCard({
  node,
  nodeRef,
  position,
  image,
  primarySelected,
  secondarySelected,
  previewSelected,
  dragging,
  onSelect,
  onStartDrag,
  onMoveDrag,
  onEndDrag,
  onCancelDrag,
  onStartConnection,
  onMoveConnection,
  onEndConnection,
  onCancelConnection,
  onRun,
  onDelete,
  busy,
  runActionState,
}: WorkflowNodeCardProps) {
  const { t } = useI18n();
  const icon = {
    product_context: FileText,
    reference_image: ImagePlus,
    copy_generation: FileText,
    image_generation: ImageIcon,
  }[node.node_type];
  const Icon = icon;
  const displayTitle = workflowNodeDisplayTitle(node, t);
  const displayLabel = workflowNodeDisplayLabel(node, t);
  const imageWaiting = isImageWorkflowNodeWaiting(node);
  const waitingLabel = imageWorkflowNodeWaitingLabel(node, t);
  const activityText = workflowNodeActivityText(node, t);
  const selectedClassName = primarySelected
    ? "border-indigo-300 shadow-lg shadow-indigo-950/10 ring-2 ring-indigo-200/70 dark:border-violet-400 dark:shadow-indigo-950/30 dark:ring-violet-300/60"
    : secondarySelected || previewSelected
      ? "border-sky-300 shadow-md shadow-sky-950/5 ring-2 ring-sky-100 dark:border-sky-400 dark:shadow-sky-950/25 dark:ring-sky-300/45"
      : "border-slate-200 dark:border-slate-500/85 dark:ring-1 dark:ring-slate-200/10";

  return (
    <div
      ref={nodeRef}
      data-workflow-node-id={node.id}
      className={`absolute w-[248px] touch-none select-none rounded-2xl border bg-white/95 p-3 text-left shadow-sm backdrop-blur dark:bg-[#1c2940]/96 dark:shadow-[0_18px_42px_rgba(0,0,0,0.34)] ${
        dragging ? "cursor-grabbing" : "transition-[border-color,box-shadow] hover:shadow-md dark:hover:border-slate-400/85 dark:hover:shadow-[0_20px_46px_rgba(0,0,0,0.42)]"
      } ${selectedClassName}`}
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
      {primarySelected || secondarySelected || previewSelected ? (
        <div
          className={`pointer-events-none absolute right-2 top-2 z-20 flex h-5 w-5 items-center justify-center rounded-full border bg-white shadow-sm dark:bg-[#111b2d] ${
            primarySelected
              ? "border-indigo-200 text-indigo-600 dark:border-indigo-300 dark:text-indigo-200"
              : "border-sky-200 text-sky-600 dark:border-sky-300 dark:text-sky-200"
          }`}
          aria-hidden="true"
        >
          <Check size={12} strokeWidth={2.5} />
        </div>
      ) : null}
      <button
        type="button"
        data-node-action
        data-workflow-target-node-id={node.id}
        onClick={onSelect}
        className="absolute left-[-22px] top-[34px] z-20 h-11 w-11 rounded-full border border-transparent bg-transparent before:absolute before:left-1/2 before:top-1/2 before:h-[18px] before:w-[18px] before:-translate-x-1/2 before:-translate-y-1/2 before:rounded-full before:border before:border-slate-300 before:bg-white before:shadow-sm hover:before:border-indigo-400 hover:before:ring-4 hover:before:ring-indigo-100 dark:before:border-slate-400/90 dark:before:bg-[#111b2d] dark:before:shadow-black/30 dark:hover:before:border-violet-300 dark:hover:before:ring-violet-400/20 lg:left-[-9px] lg:top-[47px] lg:h-[18px] lg:w-[18px]"
        title={t("detail.inputHandle")}
        aria-label={`${displayTitle} ${t("detail.inputHandle")}`}
      />
      <button
        type="button"
        data-node-action
        onPointerDown={onStartConnection}
        onPointerMove={onMoveConnection}
        onPointerUp={onEndConnection}
        onPointerCancel={onCancelConnection}
        onLostPointerCapture={onCancelConnection}
        className="absolute right-[-22px] top-[34px] z-20 h-11 w-11 rounded-full border border-transparent bg-transparent before:absolute before:left-1/2 before:top-1/2 before:h-5 before:w-5 before:-translate-x-1/2 before:-translate-y-1/2 before:rounded-full before:border-2 before:border-indigo-500 before:bg-white before:shadow-sm hover:before:bg-indigo-50 hover:before:ring-4 hover:before:ring-indigo-100 dark:before:border-violet-300 dark:before:bg-[#111b2d] dark:before:shadow-black/30 dark:hover:before:bg-violet-500/20 dark:hover:before:ring-violet-400/25 lg:right-[-10px] lg:top-[46px] lg:h-5 lg:w-5"
        title={t("detail.dragOutput")}
        aria-label={`${displayTitle} ${t("detail.outputHandle")}`}
      />
      <div onClick={onSelect} className="cursor-grab active:cursor-grabbing">
        <div className="mb-3 flex items-start justify-between gap-2">
          <div className="flex min-w-0 gap-2">
            <span className="mt-0.5 rounded-xl border border-slate-200 bg-slate-50 p-1.5 text-slate-500 dark:border-slate-500/70 dark:bg-[#111b2d] dark:text-slate-100">
              <Icon size={14} />
            </span>
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold text-zinc-900 dark:text-white">
                {displayTitle}
              </div>
              <div className="mt-0.5 text-[10px] uppercase tracking-wider text-zinc-400 dark:text-slate-400">
                {displayLabel}
              </div>
            </div>
          </div>
          <span
            className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusClass(node.status)}`}
          >
            {workflowNodeStatusLabel(node, t)}
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
              <div className="absolute inset-x-2 bottom-2 flex items-center justify-center rounded-lg bg-white/90 px-2 py-1 text-[11px] font-medium text-indigo-700 shadow-sm ring-1 ring-indigo-100 backdrop-blur dark:bg-slate-950/90 dark:text-indigo-100 dark:ring-indigo-400/30">
                <Loader2 size={11} className="mr-1 animate-spin" />
                {waitingLabel}
              </div>
            ) : null}
          </div>
        ) : imageWaiting ? (
          <div className="mb-2 flex h-28 flex-col items-center justify-center rounded-xl border border-indigo-100 bg-indigo-50 text-indigo-700 dark:border-indigo-400/30 dark:bg-indigo-500/10 dark:text-indigo-100">
            <Loader2 size={18} className="animate-spin" />
            <div className="mt-2 text-xs font-medium">{waitingLabel}</div>
          </div>
        ) : null}
        {activityText && !imageWaiting ? (
          <div className="mb-2 flex items-start gap-2 rounded-lg border border-indigo-100 bg-indigo-50 px-2.5 py-2 text-xs leading-5 text-indigo-700 dark:border-violet-400/30 dark:bg-violet-500/10 dark:text-violet-100">
            <Loader2 size={13} className="mt-0.5 shrink-0 animate-spin" />
            <div className="min-w-0">
              <div className="font-semibold">{activityText}</div>
              {node.last_run_at ? (
                <div className="mt-0.5 text-[10px] text-indigo-600/70 dark:text-violet-100/70">
                  {t("detail.recent", { time: formatDateTime(node.last_run_at) })}
                </div>
              ) : null}
            </div>
          </div>
        ) : null}
        {node.failure_reason ? (
          <div className="rounded-lg border border-red-100 bg-red-50 px-2.5 py-1.5 text-xs leading-relaxed text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
            <div className="line-clamp-2">{node.failure_reason}</div>
            {!runActionState.disabled ? (
              <div className="mt-1 text-[10px] font-medium text-red-600 dark:text-red-200">{t("detail.retryable")}</div>
            ) : null}
          </div>
        ) : null}
      </div>
      <div className="mt-3 flex items-center gap-2 text-[10px] text-zinc-400 dark:text-slate-300">
        <span className="min-w-0 max-w-[5.5rem] flex-1 truncate text-left leading-tight lg:max-w-[6.25rem]">
          {node.last_run_at ? t("detail.recent", { time: formatDateTime(node.last_run_at) }) : displayLabel}
        </span>
        {node.node_type !== "product_context" ? (
          <div className="ml-auto flex shrink-0 items-center gap-1.5">
            <button
              type="button"
              data-node-action
              onClick={onDelete}
              disabled={busy}
              className="inline-flex min-h-11 min-w-[4rem] items-center justify-center rounded border border-zinc-200 px-3 text-[11px] font-medium text-red-500 hover:border-red-300 hover:bg-red-50 disabled:opacity-50 dark:border-slate-700 dark:text-red-300 dark:hover:border-red-400/60 dark:hover:bg-red-500/10 lg:min-h-0 lg:min-w-[3.25rem] lg:px-2 lg:py-1"
            >
              <Trash2 size={11} className="mr-1" /> {t("detail.delete")}
            </button>
            <button
              type="button"
              data-node-action
              onClick={onRun}
              disabled={runActionState.disabled}
              className="inline-flex min-h-11 min-w-[4rem] items-center justify-center rounded border border-zinc-200 px-3 text-[11px] font-medium text-zinc-600 hover:border-zinc-900 hover:text-zinc-900 disabled:opacity-50 dark:border-slate-700 dark:text-slate-100 dark:hover:border-slate-400 dark:hover:text-white lg:min-h-0 lg:min-w-[3.25rem] lg:px-2 lg:py-1"
              title={runActionState.title}
              aria-label={`${displayTitle} ${runActionState.label}`}
            >
              {runActionState.pending ? (
                <Loader2 size={11} className="mr-1 animate-spin" />
              ) : (
                <Play size={11} className="mr-1" />
              )}
              {runActionState.label}
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
