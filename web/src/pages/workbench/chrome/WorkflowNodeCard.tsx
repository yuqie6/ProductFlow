import type { MouseEvent as ReactMouseEvent } from "react";
import {
  Braces,
  Clock,
  FileText,
  Image as ImageIcon,
  ImagePlus,
  Palette,
  Type,
} from "lucide-react";

import { StatusBadge, type StatusBadgeStatus } from "../../../components/ui/status-badge";
import { formatDateTime } from "../../../lib/format";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { GraphNodeType } from "../../../lib/types";
import { DownloadLink } from "./ImageDownloadComponents";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME } from "./constants";

export type WorkflowNodePresentationKind = GraphNodeType;

export interface WorkflowNodePresentationCardProps {
  id: string;
  kind: WorkflowNodePresentationKind;
  title: string;
  label: string;
  status: StatusBadgeStatus;
  statusLabel: string;
  image: DownloadableImage | null;
  imageWaiting?: boolean;
  waitingLabel?: string;
  activityText?: string | null;
  failureReason?: string | null;
  blockedReason?: string | null;
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
    iconBox: "border-kind-product-border bg-kind-product-soft text-kind-product",
    badge: "border-kind-product-border bg-kind-product-soft text-kind-product",
  },
  image_asset: {
    icon: ImagePlus,
    iconBox: "border-kind-asset-border bg-kind-asset-soft text-kind-asset",
    badge: "border-kind-asset-border bg-kind-asset-soft text-kind-asset",
  },
  creative_brief: {
    icon: Type,
    iconBox: "border-kind-brief-border bg-kind-brief-soft text-kind-brief",
    badge: "border-kind-brief-border bg-kind-brief-soft text-kind-brief",
  },
  visual_system: {
    icon: Palette,
    iconBox: "border-kind-visual-border bg-kind-visual-soft text-kind-visual",
    badge: "border-kind-visual-border bg-kind-visual-soft text-kind-visual",
  },
  image_prompt: {
    icon: Braces,
    iconBox: "border-kind-prompt-border bg-kind-prompt-soft text-kind-prompt",
    badge: "border-kind-prompt-border bg-kind-prompt-soft text-kind-prompt",
  },
  image_generation: {
    icon: ImageIcon,
    iconBox: "border-kind-image-border bg-kind-image-soft text-kind-image",
    badge: "border-kind-image-border bg-kind-image-soft text-kind-image",
  },
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
  blockedReason,
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
    ? "border-accent ring-2 ring-accent/40 shadow-elev-2"
    : secondarySelected || previewSelected
      ? "border-accent/50 ring-1 ring-accent/25 shadow-elev-1"
      : "border-border-l1";
  const motionClassName = status === "running"
    ? "border-accent/60 animate-status-pulse motion-reduce:animate-none"
    : status === "queued"
      ? "border-state-warning/60 animate-status-pulse motion-reduce:animate-none"
      : "";

  return (
    <div
      ref={nodeRef}
      data-workflow-node-id={id}
      data-node-kind={kind}
      onClick={onSelect}
      className={`nopan relative w-[248px] cursor-grab touch-none select-none rounded-surface border bg-surface-raised p-3 text-left shadow-elev-1 transition-[border-color,transform,box-shadow] duration-fast active:cursor-grabbing ${revealActive ? "animate-node-reveal motion-reduce:animate-none" : ""
        } ${dragging ? "cursor-grabbing" : "hover:-translate-y-0.5 hover:border-border-l3 hover:shadow-elev-2 motion-reduce:hover:translate-y-0"
        } ${selectedClassName} ${motionClassName}`}
    >

      <div>
        <div className="mb-2 flex items-start justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-panel border shadow-elev-1 ${theme.iconBox}`}>
              <Icon size={15} />
            </span>
            <div className="min-w-0">
              <div className="truncate text-xs font-bold tracking-tight text-text-primary" title={title}>
                {title}
              </div>
              <span className={`mt-0.5 inline-block truncate rounded px-1.5 text-[9px] font-semibold ${theme.badge}`}>
                {label}
              </span>
            </div>
          </div>
          <StatusBadge status={status} spinning={status === "running"}>
            {statusLabel}
          </StatusBadge>
        </div>

        {image ? (
          <div
            className={`relative mb-2 flex h-28 items-center justify-center overflow-hidden rounded-panel border border-border-l1 bg-surface-subtle p-1.5 ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
          >
            <img src={image.previewUrl} alt={image.alt} className="h-full w-full rounded-control object-contain" draggable={false} />
            <DownloadLink image={image} variant="overlay" />
            {imageWaiting ? <WaitingBadge label={waitingLabel ?? statusLabel} /> : null}
          </div>
        ) : imageWaiting ? (
          <div className="relative mb-2 flex h-28 flex-col items-center justify-center overflow-hidden rounded-panel border border-kind-image-border bg-kind-image-soft text-kind-image">
            <div className="h-8 w-8 rounded-full border-2 border-kind-image/30 border-t-kind-image motion-safe:animate-spin" aria-hidden="true" />
            <div className="relative z-10 mt-2 rounded-control bg-surface-raised/90 px-2.5 py-0.5 text-xs font-semibold tracking-wide shadow-elev-1">
              {waitingLabel ?? statusLabel}
            </div>
          </div>
        ) : null}

        {activityText && !imageWaiting ? (
          <div className="mb-2 flex items-start gap-2 rounded-control border border-border-l1 bg-surface-subtle px-2.5 py-1.5 text-xs leading-5 text-text-secondary">
            <div className="mt-1 h-2.5 w-2.5 shrink-0 rounded-full bg-accent motion-safe:animate-pulse" aria-hidden="true" />
            <div className="min-w-0">
              <div className="text-xs font-semibold text-text-primary">{activityText}</div>
              {lastRunAt ? (
                <div className="text-[10px] text-text-muted">
                  {t("detail.recent", { time: formatDateTime(lastRunAt, t.locale) })}
                </div>
              ) : null}
            </div>
          </div>
        ) : null}

        {failureReason ? (
          <div
            className={`rounded-control border px-2.5 py-1.5 text-xs leading-relaxed ${status === "cancelled"
                ? "border-border-l1 bg-surface-subtle text-text-secondary"
                : "border-state-error/30 bg-state-error-soft text-state-error"
              }`}
          >
            <div className="line-clamp-2">{failureReason}</div>
            {status === "failed" && retryable === true ? (
              <div className="mt-1 text-[10px] font-medium">{t("detail.retryable")}</div>
            ) : null}
            {status === "failed" && retryable === false ? (
              <div className="mt-1 text-[10px] font-medium">{t("detail.notRetryable")}</div>
            ) : null}
            {attemptCount > 0 ? (
              <div className="mt-1 text-[10px] opacity-80">
                {t("detail.nodeAttemptSummary", { attempts: attemptCount, retries: retryCount })}
              </div>
            ) : null}
            {status === "failed" && retryable === false && nonRetryableReason ? (
              <div className="mt-1 line-clamp-2 text-[10px] opacity-80">
                {t("detail.nonRetryableReason", { reason: nonRetryableReason })}
              </div>
            ) : null}
            {status === "failed" && retryHintLabel ? (
              <div className="mt-1 text-[10px] opacity-80">{retryHintLabel}</div>
            ) : null}
          </div>
        ) : null}

        {!failureReason && blockedReason ? (
          <div className="rounded-control border border-state-warning/35 bg-state-warning-soft px-2.5 py-1.5 text-xs leading-relaxed text-state-warning">
            {blockedReason}
          </div>
        ) : null}
      </div>

      {lastRunAt ? <div className="mt-2.5 flex items-center gap-1.5 border-t border-border-l1/60 pt-1.5 text-[10px] text-text-muted">
        <Clock size={11} className="shrink-0 opacity-70" />
        <span className="min-w-0 flex-1 truncate text-left leading-tight">
          {t("detail.recent", { time: formatDateTime(lastRunAt, t.locale) })}
        </span>
      </div> : null}
    </div>
  );
}

export function workflowNodeKindTheme(kind: WorkflowNodePresentationKind) {
  return KIND_THEMES[kind];
}

function WaitingBadge({ label }: { label: string }) {
  return (
    <div className="absolute inset-x-2 bottom-2 z-10 flex items-center justify-center rounded-control bg-surface-raised/90 px-2 py-1 text-[11px] font-medium text-kind-image shadow-elev-1 ring-1 ring-kind-image-border">
      <span className="mr-1 h-2 w-2 rounded-full bg-kind-image motion-safe:animate-pulse" aria-hidden="true" />
      {label}
    </div>
  );
}
