import type { MouseEvent as ReactMouseEvent } from "react";
import {
  Braces,
  Check,
  Clock,
  FileText,
  Image as ImageIcon,
  ImagePlus,
  Loader2,
  Palette,
  Type,
} from "lucide-react";

import { formatDateTime } from "../../../lib/format";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { GraphNodeType, WorkflowNodeStatus } from "../../../lib/types";
import { DownloadLink } from "./ImageDownloadComponents";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME } from "./constants";

export type WorkflowNodePresentationKind = GraphNodeType;

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
  product_source: {
    icon: FileText,
    iconBox: "border-purple-200/80 bg-purple-50 text-purple-700 dark:border-purple-500/30 dark:bg-purple-950/60 dark:text-purple-300",
    badge: "border-purple-200/60 bg-purple-100/70 text-purple-700 dark:border-purple-800/50 dark:bg-purple-950/60 dark:text-purple-300",
  },
  image_asset: {
    icon: ImagePlus,
    iconBox: "border-indigo-200/80 bg-indigo-50 text-indigo-700 dark:border-indigo-500/30 dark:bg-indigo-950/60 dark:text-indigo-300",
    badge: "border-indigo-200/60 bg-indigo-100/70 text-indigo-700 dark:border-indigo-800/50 dark:bg-indigo-950/60 dark:text-indigo-300",
  },
  creative_brief: {
    icon: Type,
    iconBox: "border-rose-200/80 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-950/60 dark:text-rose-300",
    badge: "border-rose-200/60 bg-rose-100/70 text-rose-700 dark:border-rose-800/50 dark:bg-rose-950/60 dark:text-rose-300",
  },
  visual_system: {
    icon: Palette,
    iconBox: "border-violet-200/80 bg-violet-50 text-violet-700 dark:border-violet-500/30 dark:bg-violet-950/60 dark:text-violet-300",
    badge: "border-violet-200/60 bg-violet-100/70 text-violet-700 dark:border-violet-800/50 dark:bg-violet-950/60 dark:text-violet-300",
  },
  prompt_generation: {
    icon: Braces,
    iconBox: "border-amber-200/80 bg-amber-50 text-amber-700 dark:border-amber-500/30 dark:bg-amber-950/60 dark:text-amber-300",
    badge: "border-amber-200/60 bg-amber-100/70 text-amber-700 dark:border-amber-800/50 dark:bg-amber-950/60 dark:text-amber-300",
  },
  image_generation: {
    icon: ImageIcon,
    iconBox: "border-cyan-200/80 bg-cyan-50 text-cyan-700 dark:border-cyan-500/30 dark:bg-cyan-950/60 dark:text-cyan-300",
    badge: "border-cyan-200/60 bg-cyan-100/70 text-cyan-700 dark:border-cyan-800/50 dark:bg-cyan-950/60 dark:text-cyan-300",
  },
};

const STATUS_BADGE_CLASSES: Record<WorkflowNodeStatus, string> = {
  idle: "border-slate-200 bg-slate-50 text-slate-500 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-400",
  queued: "animate-pulse border-amber-300 bg-amber-100/80 text-amber-900 dark:border-amber-700/50 dark:bg-amber-950/60 dark:text-amber-200",
  running: "border-blue-300 bg-blue-100/90 text-blue-900 dark:border-cyan-700/60 dark:bg-cyan-950/80 dark:text-cyan-200",
  succeeded: "border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-800/50 dark:bg-emerald-950/40 dark:text-emerald-200",
  failed: "border-red-200 bg-red-50 text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-200",
  cancelled: "border-slate-200 bg-slate-50 text-slate-500 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-400",
  unknown: "border-slate-200 bg-slate-50 text-slate-500 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-400",
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
  const theme = KIND_THEMES[kind] ?? KIND_THEMES.product_source;
  const Icon = theme.icon;

  const selectedClassName = primarySelected
    ? "border-accent ring-2 ring-accent/40 shadow-md"
    : secondarySelected || previewSelected
      ? "border-accent/50 ring-1 ring-accent/25 shadow-sm"
      : "border-border-l1";
  const motionClassName = status === "running"
    ? "animate-running-glow motion-reduce:animate-none"
    : status === "queued"
      ? "animate-queued-glow motion-reduce:animate-none"
      : "";

  const selected = primarySelected || secondarySelected || previewSelected;

  return (
    <div
      ref={nodeRef}
      data-workflow-node-id={id}
      data-node-kind={kind}
      onClick={onSelect}
      className={`nopan relative w-[248px] cursor-grab touch-none select-none rounded-2xl border bg-surface-raised p-3 text-left shadow-sm transition-[border-color,transform,box-shadow] duration-150 active:cursor-grabbing ${
        revealActive ? "animate-spring-node-in" : ""
      } ${
        dragging ? "cursor-grabbing" : "hover:-translate-y-0.5 hover:border-border-l3 hover:shadow-md motion-reduce:hover:translate-y-0"
      } ${selectedClassName} ${motionClassName}`}
    >
      {selected ? (
        <div
          className={`pointer-events-none absolute right-2.5 top-2.5 z-20 flex h-5 w-5 items-center justify-center rounded-full border bg-surface-raised shadow-sm ${
            primarySelected
              ? "border-accent text-accent"
              : "border-accent/40 text-accent"
          }`}
          aria-hidden="true"
        >
          <Check size={12} strokeWidth={2.5} />
        </div>
      ) : null}

      <div>
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
          <div className="relative mb-2 flex h-28 flex-col items-center justify-center overflow-hidden rounded-xl border border-cyan-300/50 bg-cyan-50/70 text-cyan-800 shadow-inner dark:border-cyan-500/25 dark:bg-cyan-950/30 dark:text-cyan-200">
            <WaitingParticles />
            <Loader2 size={22} className="relative z-10 animate-spin text-cyan-600 opacity-90 motion-reduce:animate-none dark:text-cyan-300" />
            <div className="relative z-10 mt-2 rounded-md bg-white/80 px-2.5 py-0.5 text-xs font-semibold tracking-wide shadow-sm backdrop-blur dark:bg-slate-900/80">
              {waitingLabel ?? statusLabel}
            </div>
          </div>
        ) : null}

        {/* 活动执行摘要 */}
        {activityText && !imageWaiting ? (
          <div className="mb-2 flex items-start gap-2 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1.5 text-xs leading-5 text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200">
            <Loader2 size={12} className="mt-1 shrink-0 animate-spin" />
            <div className="min-w-0">
              <div className="font-semibold text-xs">{activityText}</div>
              {lastRunAt ? (
                <div className="text-[10px] text-slate-500 dark:text-slate-400">
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

export function workflowNodeKindTheme(kind: WorkflowNodePresentationKind) {
  return KIND_THEMES[kind];
}

function WaitingBadge({ label }: { label: string }) {
  return (
    <div className="absolute inset-x-2 bottom-2 z-10 flex items-center justify-center rounded-lg bg-white/90 px-2 py-1 text-[11px] font-medium text-cyan-800 shadow-sm ring-1 ring-cyan-200 backdrop-blur dark:bg-slate-950/90 dark:text-cyan-200 dark:ring-cyan-500/30">
      <Loader2 size={11} className="mr-1 animate-spin motion-reduce:animate-none" />
      {label}
    </div>
  );
}

function WaitingParticles() {
  return (
    <div className="pointer-events-none absolute inset-0 motion-reduce:hidden" aria-hidden="true">
      <div className="absolute bottom-0 left-[35%] h-3 w-3 rounded-full bg-cyan-400/60 blur-[2px] animate-particle-1" />
      <div className="absolute bottom-0 left-[50%] h-2.5 w-2.5 rounded-full bg-blue-400/50 blur-[1px] animate-particle-2" />
      <div className="absolute bottom-0 left-[22%] h-2 w-2 rounded-full bg-indigo-400/40 blur-[1px] animate-particle-3" />
      <div className="absolute bottom-0 left-[62%] h-3.5 w-3.5 rounded-full bg-cyan-300/50 blur-[2px] animate-particle-4" />
      <div className="absolute bottom-0 left-[38%] h-3.5 w-3.5 rounded-full bg-indigo-300/50 blur-[2px] animate-particle-5" />
      <div className="absolute bottom-0 left-[48%] h-3 w-3 rounded-full bg-blue-300/60 blur-[2px] animate-particle-6" />
    </div>
  );
}
