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

const NODE_CHROME = {
  iconBox: "border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-700 dark:bg-slate-800/80 dark:text-slate-300",
  badge: "border-slate-200/80 bg-slate-100 text-slate-600 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300",
};

const KIND_THEMES: Record<WorkflowNodePresentationKind, {
  icon: typeof FileText;
  iconBox: string;
  badge: string;
}> = {
  product_source: { icon: FileText, ...NODE_CHROME },
  image_asset: { icon: ImagePlus, ...NODE_CHROME },
  creative_brief: { icon: Type, ...NODE_CHROME },
  visual_system: { icon: Palette, ...NODE_CHROME },
  prompt_generation: { icon: Braces, ...NODE_CHROME },
  image_generation: { icon: ImageIcon, ...NODE_CHROME },
};

const STATUS_BADGE_CLASSES: Record<WorkflowNodeStatus, string> = {
  idle: "border-slate-200 bg-slate-50 text-slate-500 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-400",
  queued: "border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300",
  running: "border-slate-300 bg-slate-100 text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100",
  succeeded: "border-slate-200 bg-slate-50 text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200",
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
    ? "border-slate-400 ring-1 ring-slate-400/30 shadow-md dark:border-slate-500 dark:ring-slate-500/30"
    : secondarySelected || previewSelected
      ? "border-slate-300 ring-1 ring-slate-300/40 shadow-sm dark:border-slate-600"
      : "border-slate-200 dark:border-slate-700";

  const selected = primarySelected || secondarySelected || previewSelected;

  return (
    <div
      ref={nodeRef}
      data-workflow-node-id={id}
      onClick={onSelect}
      className={`nopan relative w-[248px] cursor-grab touch-none select-none rounded-2xl border bg-white p-3 text-left shadow-sm transition-colors duration-150 active:cursor-grabbing dark:bg-[#0d1424] ${
        revealActive ? "animate-spring-node-in" : ""
      } ${
        dragging ? "cursor-grabbing" : "hover:border-slate-300 dark:hover:border-slate-600"
      } ${selectedClassName}`}
    >
      {selected ? (
        <div
          className={`pointer-events-none absolute right-2.5 top-2.5 z-20 flex h-5 w-5 items-center justify-center rounded-full border bg-white shadow-sm dark:bg-[#111b2d] ${
            primarySelected
              ? "border-slate-300 text-slate-700 dark:border-slate-500 dark:text-slate-200"
              : "border-slate-200 text-slate-500 dark:border-slate-600 dark:text-slate-300"
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
          <div className="relative mb-2 flex h-28 flex-col items-center justify-center overflow-hidden rounded-xl border border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
            <Loader2 size={22} className="animate-spin text-slate-500 opacity-90" />
            <div className="mt-2 rounded-md bg-white/80 px-2.5 py-0.5 text-xs font-semibold tracking-wide shadow-sm backdrop-blur dark:bg-slate-900/80">
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
    <div className="absolute inset-x-2 bottom-2 flex items-center justify-center rounded-lg bg-white/90 px-2 py-1 text-[11px] font-medium text-slate-700 shadow-sm ring-1 ring-slate-200 backdrop-blur dark:bg-slate-950/90 dark:text-slate-200 dark:ring-slate-700">
      <Loader2 size={11} className="mr-1 animate-spin" />
      {label}
    </div>
  );
}
