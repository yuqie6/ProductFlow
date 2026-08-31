import { AlertCircle, Images, Loader2, Play, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { IconButton } from "../../../components/ui/icon-button";
import { Tooltip } from "../../../components/ui/tooltip";
import { ApiError, api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { TranslationKey } from "../../../lib/i18n";
import type { GraphPlannedAction, GraphRunSubmitInput, WorkflowNodeDisplayStatus } from "../../../lib/types";
import { dominantPlannedAction, plannedActionClassName, runPreviewPointerHandlers } from "./graphRunPreview";
import type { GraphShotProjection } from "./shotProjection";

export interface GraphShotFilmstripProps {
  shots: readonly GraphShotProjection[];
  runsLoading?: boolean;
  runsFetching?: boolean;
  runsError?: unknown;
  operationError?: unknown;
  onRetryRuns?: () => void;
  busy: boolean;
  runningGroupId: string | null;
  selectedNodeIds: readonly string[];
  onFocusShot: (shot: GraphShotProjection) => void;
  onRunShot: (groupId: string) => void;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
  plannedActions?: Readonly<Record<string, GraphPlannedAction>>;
  blockedReasons?: Readonly<Record<string, string>>;
  visible?: boolean;
}

export function GraphShotFilmstrip({
  shots,
  runsLoading = false,
  runsFetching = false,
  runsError = null,
  operationError = null,
  onRetryRuns,
  busy,
  runningGroupId,
  selectedNodeIds,
  onFocusShot,
  onRunShot,
  onPreviewRun,
  onHideRunPreview,
  plannedActions,
  blockedReasons,
  visible = true,
}: GraphShotFilmstripProps) {
  const { t } = useI18n();
  const error = operationError ?? runsError;
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const tileRefs = useRef(new Map<string, HTMLElement>());
  const [overflow, setOverflow] = useState({ left: false, right: false });
  const selectedShotId = useMemo(
    () => shots.find((shot) => shot.imageNodeIds.some((nodeId) => selectedNodeIds.includes(nodeId)))?.groupId ?? null,
    [selectedNodeIds, shots],
  );
  const updateOverflow = useCallback(() => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    setOverflow({
      left: scroller.scrollLeft > 1,
      right: scroller.scrollLeft + scroller.clientWidth < scroller.scrollWidth - 1,
    });
  }, []);
  const revealTile = useCallback((groupId: string, forceAuto = false) => {
    const tile = tileRefs.current.get(groupId);
    if (!tile || typeof tile.scrollIntoView !== "function") return;
    const reducedMotion = forceAuto
      || (typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches);
    tile.scrollIntoView({ block: "nearest", inline: "nearest", behavior: reducedMotion ? "auto" : "smooth" });
  }, []);

  useEffect(() => {
    if (selectedShotId) revealTile(selectedShotId);
  }, [revealTile, selectedShotId]);

  useEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    updateOverflow();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(updateOverflow);
    observer?.observe(scroller);
    return () => observer?.disconnect();
  }, [shots, updateOverflow, visible]);

  return (
    <section
      data-graph-shot-filmstrip
      data-visible={visible ? "true" : "false"}
      aria-hidden={!visible || undefined}
      inert={!visible || undefined}
      aria-label={t("graph.shots.title")}
      className={`graph-shot-filmstrip absolute inset-x-0 bottom-0 z-20 backdrop-blur-xl transition-[transform,opacity] duration-200 ease-out motion-reduce:transition-none ${visible ? "translate-y-0 opacity-100" : "pointer-events-none translate-y-full opacity-0"}`}
    >
      {error ? (
        <div role="alert" className="flex min-h-8 items-center gap-2 border-b border-state-error/25 bg-state-error-soft px-3 text-[11px] text-state-error">
          <AlertCircle size={13} className="shrink-0" aria-hidden="true" />
          <span className="min-w-0 flex-1 truncate">{errorDetail(error, runsError ? t("graph.runs.loadFailed") : t("workbench.error.run"))}</span>
          {runsError && onRetryRuns ? (
            <IconButton label={t("agentWorkbench.retry")} size="sm" onClick={onRetryRuns}>
              <RefreshCw size={13} aria-hidden="true" />
            </IconButton>
          ) : null}
        </div>
      ) : null}
      <div
        ref={scrollerRef}
        data-graph-shot-filmstrip-scroll
        className="flex h-[116px] min-w-0 items-stretch gap-2 overflow-x-auto overscroll-x-contain px-3 py-2 sm:h-[124px] sm:px-4"
        onScroll={updateOverflow}
      >
        {runsLoading && !shots.length ? (
          <div className="flex min-w-28 items-center justify-center text-text-muted" aria-label={t("app.loading")}>
            <Loader2 size={16} className="animate-spin motion-reduce:animate-none" aria-hidden="true" />
          </div>
        ) : null}
        {shots.map((shot) => (
          <GraphShotTile
            key={shot.groupId}
            shot={shot}
            selected={shot.imageNodeIds.some((nodeId) => selectedNodeIds.includes(nodeId))}
            busy={busy}
            running={runningGroupId === shot.groupId}
            anotherShotRunning={runningGroupId !== null && runningGroupId !== shot.groupId}
            runBlockedReason={blockedReasons?.[shot.groupId]}
            onFocusShot={onFocusShot}
            onRunShot={onRunShot}
            onPreviewRun={onPreviewRun}
            onHideRunPreview={onHideRunPreview}
            plannedAction={dominantPlannedAction(shot.imageNodeIds.map((nodeId) => plannedActions?.[nodeId]))}
            tileRef={(element) => {
              if (element) tileRefs.current.set(shot.groupId, element);
              else tileRefs.current.delete(shot.groupId);
            }}
            onReveal={() => revealTile(shot.groupId, true)}
          />
        ))}
        {runsFetching ? (
          <div className="flex w-8 shrink-0 items-center justify-center text-text-muted" aria-label={t("app.loading")}>
            <Loader2 size={14} className="animate-spin motion-reduce:animate-none" aria-hidden="true" />
          </div>
        ) : null}
      </div>
      {overflow.left ? (
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-y-0 left-0 w-7"
          style={{ background: "linear-gradient(to right, var(--color-surface-raised), transparent)" }}
        />
      ) : null}
      {overflow.right ? (
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-y-0 right-0 w-7"
          style={{ background: "linear-gradient(to left, var(--color-surface-raised), transparent)" }}
        />
      ) : null}
    </section>
  );
}

function GraphShotTile({
  shot,
  selected,
  busy,
  running,
  anotherShotRunning,
  runBlockedReason,
  onFocusShot,
  onRunShot,
  onPreviewRun,
  onHideRunPreview,
  plannedAction,
  tileRef,
  onReveal,
}: {
  shot: GraphShotProjection;
  selected: boolean;
  busy: boolean;
  running: boolean;
  anotherShotRunning: boolean;
  runBlockedReason?: string;
  onFocusShot: (shot: GraphShotProjection) => void;
  onRunShot: (groupId: string) => void;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
  plannedAction?: GraphPlannedAction | null;
  tileRef: (element: HTMLElement | null) => void;
  onReveal: () => void;
}) {
  const { t } = useI18n();
  const previewAssetId = shot.primaryImageAssetId;
  const failed = shot.latestNodeStatus === "failed";
  const frozen = plannedAction === "frozen" && (shot.latestNodeStatus === "idle" || shot.latestNodeStatus === "skipped");
  const statusLabel = frozen ? t("graph.preview.frozen") : t(statusKey(shot.latestNodeStatus));
  const failureReason = failed ? shot.latestFailureReason ?? t("graph.shots.failureUnknown") : null;
  const focusLabel = `${shot.title} · ${statusLabel}`;

  return (
    <article
      data-graph-shot-id={shot.groupId}
      data-graph-shot-primary-node-id={shot.primaryImageNodeId}
      data-graph-shot-status={shot.latestNodeStatus}
      data-graph-planned-action={plannedAction ?? undefined}
      ref={tileRef}
      onFocusCapture={onReveal}
      className={`group relative w-[84px] shrink-0 sm:w-[92px] ${plannedActionClassName(plannedAction)}`}
    >
      <Tooltip content={failureReason ?? focusLabel}>
        <button
          type="button"
          data-graph-shot-focus
          aria-pressed={selected}
          aria-label={focusLabel}
          title={failureReason ?? undefined}
          onClick={() => onFocusShot(shot)}
          className={`relative block aspect-square w-full overflow-hidden rounded-control border-2 bg-surface-subtle text-text-muted outline-none transition-colors focus-visible:ring-2 focus-visible:ring-focus-ring ${
            failed
              ? "border-state-error"
              : selected ? "border-accent" : "border-border-l1 hover:border-border-l2"
          }`}
        >
          {previewAssetId ? (
            <img src={api.getProductImageAssetMediaUrl(previewAssetId, "thumbnail")} alt="" className="h-full w-full object-cover" />
          ) : (
            <span className="flex h-full w-full items-center justify-center"><Images size={18} aria-hidden="true" /></span>
          )}
          <span className={`absolute right-1 top-1 h-2.5 w-2.5 rounded-full border-2 border-surface-raised ${statusColor(shot.latestNodeStatus)} ${running ? "animate-pulse motion-reduce:animate-none" : ""}`} aria-hidden="true" />
          {shot.currentResultAssetIds.length > 1 ? (
            <span className="absolute bottom-1 left-1 rounded bg-surface-inverse/80 px-1 text-[9px] font-semibold text-surface-raised">+{shot.currentResultAssetIds.length - 1}</span>
          ) : null}
        </button>
      </Tooltip>
      <span className="mt-1 block truncate text-[10px] font-medium text-text-secondary">{shot.title}</span>
      <Tooltip content={runBlockedReason ?? t("graph.canvas.runShot")}>
        <span className="absolute bottom-5 right-1 z-10">
          <IconButton
            label={runBlockedReason ?? t("graph.canvas.runShot")}
            size="sm"
            variant="primary"
            busy={running}
            disabled={busy || anotherShotRunning || Boolean(runBlockedReason)}
            data-graph-shot-run
            className="!h-7 !w-7 opacity-100 sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100"
            {...runPreviewPointerHandlers(
              shot.imageNodeIds.length ? { scope: "selection", node_ids: shot.imageNodeIds } : null,
              onPreviewRun,
              onHideRunPreview,
            )}
            onClick={() => onRunShot(shot.groupId)}
          >
            <Play size={12} aria-hidden="true" />
          </IconButton>
        </span>
      </Tooltip>
    </article>
  );
}

function statusKey(status: WorkflowNodeDisplayStatus): TranslationKey {
  const keys: Record<WorkflowNodeDisplayStatus, TranslationKey> = {
    idle: "detail.nodeStatus.idle",
    queued: "detail.nodeStatus.queued",
    running: "detail.nodeStatus.running",
    succeeded: "detail.nodeStatus.succeeded",
    failed: "detail.nodeStatus.failed",
    cancelled: "detail.nodeStatus.cancelled",
    skipped: "detail.nodeStatus.skipped",
    unknown: "detail.nodeStatus.unknown",
  };
  return keys[status];
}

function statusColor(status: WorkflowNodeDisplayStatus): string {
  if (status === "failed" || status === "unknown") return "bg-state-error";
  if (status === "running" || status === "queued") return "bg-accent";
  if (status === "succeeded") return "bg-state-success";
  if (status === "cancelled") return "bg-state-warning";
  return "bg-text-muted";
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.detail) return error.detail;
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}
