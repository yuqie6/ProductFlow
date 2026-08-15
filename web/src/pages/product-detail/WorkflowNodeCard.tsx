import type { MouseEvent as ReactMouseEvent } from "react";
import {
  Braces,
  Check,
  FileText,
  Image as ImageIcon,
  ImagePlus,
  Loader2,
} from "lucide-react";

import { formatDateTime } from "../../lib/format";
import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type { WorkflowNodeStatus, WorkflowNodeTypeV2 } from "../../lib/types";
import { DownloadLink } from "./ImageDownloadComponents";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME } from "./constants";
import { statusClass } from "./utils";

export type WorkflowNodePresentationKind = WorkflowNodeTypeV2;

export interface WorkflowNodePresentationCardProps {
  id: string;
  kind: WorkflowNodePresentationKind;
  title: string;
  label: string;
  status: WorkflowNodeStatus;
  statusLabel: string;
  image: DownloadableImage | null;
  imageWaiting?: boolean;
  waitingLabel?: string;
  activityText?: string | null;
  failureReason?: string | null;
  lastRunAt?: string | null;
  retryable?: boolean;
  attemptCount?: number;
  retryCount?: number;
  nonRetryableReason?: string | null;
  retryHintLabel?: string | null;
  primarySelected: boolean;
  secondarySelected?: boolean;
  previewSelected?: boolean;
  dragging: boolean;
  revealActive?: boolean;
  nodeRef?: (element: HTMLDivElement | null) => void;
  onSelect: (event: ReactMouseEvent<HTMLElement>) => void;
}

export function WorkflowNodePresentationCard({
  id,
  kind,
  title,
  label,
  status,
  statusLabel,
  image,
  imageWaiting = false,
  waitingLabel,
  activityText,
  failureReason,
  lastRunAt,
  retryable,
  attemptCount = 0,
  retryCount = 0,
  nonRetryableReason,
  retryHintLabel,
  primarySelected,
  secondarySelected = false,
  previewSelected = false,
  dragging,
  revealActive = false,
  nodeRef,
  onSelect,
}: WorkflowNodePresentationCardProps) {
  const { t } = useI18n();
  const Icon = {
    product_context: FileText,
    reference_image: ImagePlus,
    prompt_generation: Braces,
    image_generation: ImageIcon,
  }[kind];
  const selectedClassName = primarySelected
    ? "border-indigo-300 shadow-lg shadow-indigo-950/10 ring-2 ring-indigo-200/70 dark:border-violet-400 dark:shadow-indigo-950/30 dark:ring-violet-300/60"
    : secondarySelected || previewSelected
      ? "border-sky-300 shadow-md shadow-sky-950/5 ring-2 ring-sky-100 dark:border-sky-400 dark:shadow-sky-950/25 dark:ring-sky-300/45"
      : "border-slate-200 dark:border-slate-500/85 dark:ring-1 dark:ring-slate-200/10";
  const selected = primarySelected || secondarySelected || previewSelected;

  return (
    <div
      ref={nodeRef}
      data-workflow-node-id={id}
      className={`nopan relative w-[248px] touch-none select-none rounded-2xl border bg-white/95 p-3 text-left shadow-sm backdrop-blur transition-[border-color,box-shadow,transform] transition-spring dark:bg-[#1c2940]/96 dark:shadow-[0_18px_42px_rgba(0,0,0,0.34)] ${
        revealActive ? "animate-spring-node-in" : ""
      } ${
        dragging
          ? "cursor-grabbing"
          : "hover:-translate-y-0.5 hover:shadow-md dark:hover:border-slate-400/85 dark:hover:shadow-[0_20px_46px_rgba(0,0,0,0.42)]"
      } ${selectedClassName} ${
        status === "running" ? "animate-running-glow" : status === "queued" ? "animate-queued-glow" : ""
      }`}
    >
      {selected ? (
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

      <div onClick={onSelect} className="cursor-grab active:cursor-grabbing">
        <div className="mb-3 flex items-start justify-between gap-2">
          <div className="flex min-w-0 gap-2">
            <span className="mt-0.5 rounded-xl border border-slate-200 bg-slate-50 p-1.5 text-slate-500 dark:border-slate-500/70 dark:bg-[#111b2d] dark:text-slate-100">
              <Icon size={14} />
            </span>
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold text-zinc-900 dark:text-white" title={title}>
                {title}
              </div>
              <div className="mt-0.5 truncate text-[10px] uppercase tracking-wider text-zinc-400 dark:text-slate-400">
                {label}
              </div>
            </div>
          </div>
          <span className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusClass(status)}`}>
            {statusLabel}
          </span>
        </div>

        {image ? (
          <div
            className={`relative mb-2 flex h-28 items-center justify-center overflow-hidden rounded-xl border border-zinc-100 p-2 ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
          >
            <img src={image.previewUrl} alt={image.alt} className="h-full w-full object-contain" draggable={false} />
            <DownloadLink image={image} variant="overlay" />
            {imageWaiting ? <WaitingBadge label={waitingLabel ?? statusLabel} /> : null}
          </div>
        ) : imageWaiting ? (
          <div className="relative mb-2 flex h-28 flex-col items-center justify-center overflow-hidden rounded-xl border border-indigo-200/50 bg-indigo-50/70 text-indigo-700 shadow-inner dark:border-indigo-400/20 dark:bg-slate-950/30 dark:text-indigo-100">
            <Loader2 size={20} className="animate-spin text-indigo-500 opacity-80 dark:text-indigo-300" />
            <div className="mt-2 rounded-md bg-white/70 px-2 py-0.5 text-xs font-semibold tracking-wide shadow-sm backdrop-blur dark:bg-slate-900/60">
              {waitingLabel ?? statusLabel}
            </div>
          </div>
        ) : null}

        {activityText && !imageWaiting ? (
          <div className="mb-2 flex items-start gap-2 rounded-lg border border-indigo-100 bg-indigo-50 px-2.5 py-2 text-xs leading-5 text-indigo-700 dark:border-violet-400/30 dark:bg-violet-500/10 dark:text-violet-100">
            <Loader2 size={13} className="mt-0.5 shrink-0 animate-spin" />
            <div className="min-w-0">
              <div className="font-semibold">{activityText}</div>
              {lastRunAt ? (
                <div className="mt-0.5 text-[10px] text-indigo-600/70 dark:text-violet-100/70">
                  {t("detail.recent", { time: formatDateTime(lastRunAt, t.locale) })}
                </div>
              ) : null}
            </div>
          </div>
        ) : null}

        {failureReason ? (
          <div
            className={`rounded-lg border px-2.5 py-1.5 text-xs leading-relaxed ${
              status === "cancelled"
                ? "border-zinc-100 bg-zinc-50 text-zinc-600 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300"
                : "border-red-100 bg-red-50 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200"
            }`}
          >
            <div className="line-clamp-2">{failureReason}</div>
            {status === "failed" && retryable === true ? (
              <div className="mt-1 text-[10px] font-medium text-red-600 dark:text-red-200">{t("detail.retryable")}</div>
            ) : null}
            {status === "failed" && retryable === false ? (
              <div className="mt-1 text-[10px] font-medium text-red-600 dark:text-red-200">{t("detail.notRetryable")}</div>
            ) : null}
            {attemptCount > 0 ? (
              <div className="mt-1 text-[10px] text-red-600/80 dark:text-red-200/80">
                {t("detail.nodeAttemptSummary", { attempts: attemptCount, retries: retryCount })}
              </div>
            ) : null}
            {status === "failed" && retryable === false && nonRetryableReason ? (
              <div className="mt-1 line-clamp-2 text-[10px] text-red-600/80 dark:text-red-200/80">
                {t("detail.nonRetryableReason", { reason: nonRetryableReason })}
              </div>
            ) : null}
            {status === "failed" && retryHintLabel ? (
              <div className="mt-1 text-[10px] text-red-600/80 dark:text-red-200/80">{retryHintLabel}</div>
            ) : null}
          </div>
        ) : null}
      </div>

      <div className="mt-3 flex items-center gap-2 text-[10px] text-zinc-400 dark:text-slate-300">
        <span className="min-w-0 flex-1 truncate text-left leading-tight">
          {lastRunAt ? t("detail.recent", { time: formatDateTime(lastRunAt, t.locale) }) : label}
        </span>
      </div>
    </div>
  );
}

function WaitingBadge({ label }: { label: string }) {
  return (
    <div className="absolute inset-x-2 bottom-2 flex items-center justify-center rounded-lg bg-white/90 px-2 py-1 text-[11px] font-medium text-indigo-700 shadow-sm ring-1 ring-indigo-100 backdrop-blur dark:bg-slate-950/90 dark:text-indigo-100 dark:ring-indigo-400/30">
      <Loader2 size={11} className="mr-1 animate-spin" />
      {label}
    </div>
  );
}
