import type { MouseEvent as ReactMouseEvent } from "react";
import {
  Braces,
  Check,
  Clock,
  FileText,
  Image as ImageIcon,
  ImagePlus,
  Loader2,
} from "lucide-react";

import { formatDateTime } from "../../../lib/format";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { WorkflowNodeStatus, WorkflowNodeTypeV2 } from "../../../lib/types";
import { DownloadLink } from "./ImageDownloadComponents";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME } from "./constants";

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

const KIND_THEMES: Record<WorkflowNodePresentationKind, {
  icon: typeof FileText;
  iconBox: string;
  badge: string;
}> = {
  product_context: {
    icon: FileText,
    iconBox: "border-purple-200/80 bg-purple-50 text-purple-700 dark:border-purple-500/30 dark:bg-purple-950/60 dark:text-purple-300",
    badge: "text-purple-700 bg-purple-100/70 border-purple-200/60 dark:text-purple-300 dark:bg-purple-950/60 dark:border-purple-800/50",
  },
  reference_image: {
    icon: ImagePlus,
    iconBox: "border-indigo-200/80 bg-indigo-50 text-indigo-700 dark:border-indigo-500/30 dark:bg-indigo-950/60 dark:text-indigo-300",
    badge: "text-indigo-700 bg-indigo-100/70 border-indigo-200/60 dark:text-indigo-300 dark:bg-indigo-950/60 dark:border-indigo-800/50",
  },
  prompt_generation: {
    icon: Braces,
    iconBox: "border-amber-200/80 bg-amber-50 text-amber-700 dark:border-amber-500/30 dark:bg-amber-950/60 dark:text-amber-300",
    badge: "text-amber-700 bg-amber-100/70 border-amber-200/60 dark:text-amber-300 dark:bg-amber-950/60 dark:border-amber-800/50",
  },
  image_generation: {
    icon: ImageIcon,
    iconBox: "border-cyan-200/80 bg-cyan-50 text-cyan-700 dark:border-cyan-500/30 dark:bg-cyan-950/60 dark:text-cyan-300",
    badge: "text-cyan-700 bg-cyan-100/70 border-cyan-200/60 dark:text-cyan-300 dark:bg-cyan-950/60 dark:border-cyan-800/50",
  },
};

const STATUS_BADGE_CLASSES: Record<WorkflowNodeStatus, string> = {
  idle: "border-slate-200 bg-slate-100 text-slate-700 dark:border-slate-700 dark:bg-slate-800/80 dark:text-slate-300",
  queued: "border-amber-300 bg-amber-100/80 text-amber-900 dark:border-amber-700/50 dark:bg-amber-950/60 dark:text-amber-200 animate-pulse",
  running: "border-blue-300 bg-blue-100/90 text-blue-900 dark:border-cyan-700/60 dark:bg-cyan-950/80 dark:text-cyan-200",
  succeeded: "border-emerald-300 bg-emerald-100/80 text-emerald-900 dark:border-emerald-700/50 dark:bg-emerald-950/60 dark:text-emerald-200",
  failed: "border-red-300 bg-red-100/80 text-red-900 dark:border-red-700/50 dark:bg-red-950/60 dark:text-red-200",
  cancelled: "border-zinc-200 bg-zinc-100 text-zinc-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400",
  unknown: "border-orange-300 bg-orange-100/80 text-orange-900 dark:border-orange-700/50 dark:bg-orange-950/60 dark:text-orange-200",
};

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
  const theme = KIND_THEMES[kind] ?? KIND_THEMES.product_context;
  const Icon = theme.icon;

  const selectedClassName = primarySelected
    ? "border-indigo-400 ring-2 ring-indigo-400/40 shadow-xl shadow-indigo-950/15 dark:border-cyan-400 dark:ring-cyan-400/40 dark:shadow-cyan-950/40"
    : secondarySelected || previewSelected
      ? "border-sky-300 ring-2 ring-sky-300/30 shadow-md shadow-sky-950/10 dark:border-sky-400 dark:ring-sky-400/30"
      : "border-slate-200/90 dark:border-slate-700/80";

  const selected = primarySelected || secondarySelected || previewSelected;

  return (
    <div
      ref={nodeRef}
      data-workflow-node-id={id}
      className={`nopan relative w-[248px] touch-none select-none rounded-2xl border bg-white/95 p-3 text-left shadow-sm backdrop-blur-md transition-all duration-200 dark:bg-[#0d1424]/95 dark:shadow-[0_18px_42px_rgba(0,0,0,0.36)] ${
        revealActive ? "animate-spring-node-in" : ""
      } ${
        dragging
          ? "cursor-grabbing"
          : "hover:-translate-y-1 hover:shadow-lg dark:hover:border-slate-500 dark:hover:shadow-[0_22px_48px_rgba(0,0,0,0.5)]"
      } ${selectedClassName} ${
        status === "running" ? "animate-running-glow" : status === "queued" ? "animate-queued-glow" : ""
      }`}
    >
      {selected ? (
        <div
          className={`pointer-events-none absolute right-2.5 top-2.5 z-20 flex h-5 w-5 items-center justify-center rounded-full border bg-white shadow-sm dark:bg-[#111b2d] ${
            primarySelected
              ? "border-indigo-300 text-indigo-600 dark:border-cyan-400 dark:text-cyan-300"
              : "border-sky-200 text-sky-600 dark:border-sky-300 dark:text-sky-300"
          }`}
          aria-hidden="true"
        >
          <Check size={12} strokeWidth={2.5} />
        </div>
      ) : null}

      <div onClick={onSelect} className="cursor-grab active:cursor-grabbing">
        {/* 卡片头部：图标、标题与类型胶囊 */}
        <div className="mb-2 flex items-start justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border shadow-sm ${theme.iconBox}`}>
              <Icon size={15} />
            </span>
            <div className="min-w-0">
              <div className="truncate text-xs font-bold tracking-tight text-zinc-950 dark:text-slate-100" title={title}>
                {title}
              </div>
              <span className={`inline-block mt-0.5 truncate rounded px-1.5 py-0.2 text-[9px] font-semibold uppercase tracking-wider ${theme.badge}`}>
                {label}
              </span>
            </div>
          </div>
          <span className={`inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-semibold ${STATUS_BADGE_CLASSES[status]}`}>
            {status === "running" ? <Loader2 size={10} className="animate-spin motion-reduce:animate-none" /> : null}
            {statusLabel}
          </span>
        </div>

        {/* 图片预览区 */}
        {image ? (
          <div
            className={`relative mb-2 flex h-28 items-center justify-center overflow-hidden rounded-xl border border-zinc-200/80 bg-zinc-900/5 p-1.5 transition-all dark:border-slate-800 dark:bg-black/30 ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
          >
            <img src={image.previewUrl} alt={image.alt} className="h-full w-full rounded-lg object-contain" draggable={false} />
            <DownloadLink image={image} variant="overlay" />
            {imageWaiting ? <WaitingBadge label={waitingLabel ?? statusLabel} /> : null}
          </div>
        ) : imageWaiting ? (
          <div className="relative mb-2 flex h-28 flex-col items-center justify-center overflow-hidden rounded-xl border border-cyan-300/50 bg-gradient-to-br from-cyan-50/70 to-blue-50/70 text-cyan-800 shadow-inner dark:border-cyan-500/25 dark:from-cyan-950/30 dark:to-blue-950/30 dark:text-cyan-200">
            <Loader2 size={22} className="animate-spin text-cyan-600 opacity-90 dark:text-cyan-300" />
            <div className="mt-2 rounded-md bg-white/80 px-2.5 py-0.5 text-xs font-semibold tracking-wide shadow-sm backdrop-blur dark:bg-slate-900/80">
              {waitingLabel ?? statusLabel}
            </div>
          </div>
        ) : null}

        {/* 活动执行摘要 */}
        {activityText && !imageWaiting ? (
          <div className="mb-2 flex items-start gap-2 rounded-lg border border-blue-200/80 bg-blue-50/70 px-2.5 py-1.5 text-xs leading-5 text-blue-900 dark:border-cyan-800/40 dark:bg-cyan-950/30 dark:text-cyan-100">
            <Loader2 size={12} className="mt-1 shrink-0 animate-spin" />
            <div className="min-w-0">
              <div className="font-semibold text-xs">{activityText}</div>
              {lastRunAt ? (
                <div className="text-[10px] text-blue-700/80 dark:text-cyan-300/70">
                  {t("detail.recent", { time: formatDateTime(lastRunAt, t.locale) })}
                </div>
              ) : null}
            </div>
          </div>
        ) : null}

        {/* 失败与重试分析 */}
        {failureReason ? (
          <div
            className={`rounded-lg border px-2.5 py-1.5 text-xs leading-relaxed ${
              status === "cancelled"
                ? "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300"
                : "border-red-200 bg-red-50 text-red-800 dark:border-red-800/40 dark:bg-red-950/30 dark:text-red-200"
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

      {/* 底部时间线 */}
      <div className="mt-2.5 flex items-center gap-1.5 border-t border-border-l1/60 pt-1.5 text-[10px] text-text-tertiary">
        <Clock size={11} className="shrink-0 opacity-70" />
        <span className="min-w-0 flex-1 truncate text-left leading-tight">
          {lastRunAt ? t("detail.recent", { time: formatDateTime(lastRunAt, t.locale) }) : label}
        </span>
      </div>
    </div>
  );
}

function WaitingBadge({ label }: { label: string }) {
  return (
    <div className="absolute inset-x-2 bottom-2 flex items-center justify-center rounded-lg bg-white/90 px-2 py-1 text-[11px] font-medium text-cyan-800 shadow-sm ring-1 ring-cyan-200 backdrop-blur dark:bg-slate-950/90 dark:text-cyan-200 dark:ring-cyan-500/30">
      <Loader2 size={11} className="mr-1 animate-spin" />
      {label}
    </div>
  );
}
