/**
 * 图片成果工作视图：按分组组织展示，每项对应一个生图/证据节点。
 */

import {
  AlertCircle,
  History,
  Images,
  Loader2,
  LocateFixed,
  Pencil,
  Play,
  RefreshCw,
} from "lucide-react";
import { useEffect, useMemo, useRef, type MutableRefObject } from "react";

import { IconButton } from "../../../components/ui/icon-button";
import { Tooltip } from "../../../components/ui/tooltip";
import { ApiError, api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { TranslationKey } from "../../../lib/i18n";
import type { GraphPlannedAction, GraphRunSubmitInput, WorkflowNodeDisplayStatus } from "../../../lib/types";
import { AGENT_IMAGE_TYPE_TRANSLATIONS } from "../../product-create/imageTypeSelection";
import type { AgentProductImageTypeKey } from "../../../lib/types";
import { dominantPlannedAction, plannedActionClassName, runPreviewPointerHandlers } from "./graphRunPreview";
import type { GraphResultItem, GraphResultSection } from "./resultProjection";

export interface GraphResultsViewProps {
  sections: readonly GraphResultSection[];
  runsLoading?: boolean;
  runsFetching?: boolean;
  runsError?: unknown;
  operationError?: unknown;
  onRetryRuns?: () => void;
  busy: boolean;
  runningNodeId: string | null;
  selectedNodeIds: readonly string[];
  plannedActions?: Readonly<Record<string, GraphPlannedAction>>;
  blockedReasons?: Readonly<Record<string, string>>;
  onSelectItem: (item: GraphResultItem) => void;
  onLocateItem: (item: GraphResultItem) => void;
  onOpenHistory: (item: GraphResultItem) => void;
  onRunItem: (item: GraphResultItem) => void;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
  onOpenLocalEdit?: (item: GraphResultItem) => void;
  onPreviewImage?: (item: GraphResultItem) => void;
  onBindEvidence?: (item: GraphResultItem) => void;
}

export function GraphResultsView({
  sections,
  runsLoading = false,
  runsFetching = false,
  runsError = null,
  operationError = null,
  onRetryRuns,
  busy,
  runningNodeId,
  selectedNodeIds,
  plannedActions,
  blockedReasons,
  onSelectItem,
  onLocateItem,
  onOpenHistory,
  onRunItem,
  onPreviewRun,
  onHideRunPreview,
  onOpenLocalEdit,
  onPreviewImage,
  onBindEvidence,
}: GraphResultsViewProps) {
  const { t } = useI18n();
  const error = operationError ?? runsError;
  const itemCount = useMemo(
    () => sections.reduce((count, section) => count + section.items.length, 0),
    [sections],
  );
  const selectedSet = useMemo(() => new Set(selectedNodeIds), [selectedNodeIds]);
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const cardRefs = useRef(new Map<string, HTMLElement>());

  useEffect(() => {
    const selectedId = selectedNodeIds.find((nodeId) => cardRefs.current.has(nodeId));
    if (!selectedId) return;
    const card = cardRefs.current.get(selectedId);
    if (!card || typeof card.scrollIntoView !== "function") return;
    const reducedMotion = typeof window !== "undefined"
      && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    card.scrollIntoView({ block: "nearest", inline: "nearest", behavior: reducedMotion ? "auto" : "smooth" });
  }, [selectedNodeIds]);

  return (
    <section
      data-graph-results-view
      aria-label={t("graph.results.title")}
      className="absolute inset-0 z-10 flex min-h-0 flex-col bg-surface-base"
    >
      <header className="flex min-h-11 shrink-0 items-center gap-2 border-b border-border-l1 px-3 py-2 sm:px-4">
        <Images size={16} className="shrink-0 text-text-muted" aria-hidden="true" />
        <h2 className="min-w-0 flex-1 truncate text-sm font-semibold text-text-primary">
          {t("graph.results.title")}
        </h2>
        <span className="shrink-0 text-[11px] font-medium text-text-muted">
          {t("graph.results.count", { count: itemCount })}
        </span>
        {runsFetching ? (
          <Loader2 size={14} className="shrink-0 animate-spin text-text-muted motion-reduce:animate-none" aria-hidden="true" />
        ) : null}
      </header>
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
        data-graph-results-scroll
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-3 py-3 sm:px-4"
      >
        {runsLoading && !itemCount ? (
          <div className="flex min-h-40 items-center justify-center text-text-muted" aria-label={t("app.loading")}>
            <Loader2 size={18} className="animate-spin motion-reduce:animate-none" aria-hidden="true" />
          </div>
        ) : null}
        {!runsLoading && !itemCount ? (
          <p className="rounded-control border border-dashed border-border-l1 bg-surface-subtle px-4 py-8 text-center text-sm text-text-muted">
            {t("graph.results.empty")}
          </p>
        ) : null}
        <div className="flex flex-col gap-5">
          {sections.map((section) => (
            <ResultSection
              key={section.key}
              section={section}
              selectedSet={selectedSet}
              busy={busy}
              runningNodeId={runningNodeId}
              plannedActions={plannedActions}
              blockedReasons={blockedReasons}
              cardRefs={cardRefs}
              onSelectItem={onSelectItem}
              onLocateItem={onLocateItem}
              onOpenHistory={onOpenHistory}
              onRunItem={onRunItem}
              onPreviewRun={onPreviewRun}
              onHideRunPreview={onHideRunPreview}
              onOpenLocalEdit={onOpenLocalEdit}
              onPreviewImage={onPreviewImage}
              onBindEvidence={onBindEvidence}
            />
          ))}
        </div>
      </div>
    </section>
  );
}

function ResultSection({
  section,
  selectedSet,
  busy,
  runningNodeId,
  plannedActions,
  blockedReasons,
  cardRefs,
  onSelectItem,
  onLocateItem,
  onOpenHistory,
  onRunItem,
  onPreviewRun,
  onHideRunPreview,
  onOpenLocalEdit,
  onPreviewImage,
  onBindEvidence,
}: {
  section: GraphResultSection;
  selectedSet: ReadonlySet<string>;
  busy: boolean;
  runningNodeId: string | null;
  plannedActions?: Readonly<Record<string, GraphPlannedAction>>;
  blockedReasons?: Readonly<Record<string, string>>;
  cardRefs: MutableRefObject<Map<string, HTMLElement>>;
  onSelectItem: (item: GraphResultItem) => void;
  onLocateItem: (item: GraphResultItem) => void;
  onOpenHistory: (item: GraphResultItem) => void;
  onRunItem: (item: GraphResultItem) => void;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
  onOpenLocalEdit?: (item: GraphResultItem) => void;
  onPreviewImage?: (item: GraphResultItem) => void;
  onBindEvidence?: (item: GraphResultItem) => void;
}) {
  const { t } = useI18n();
  const title = section.kind === "group"
    ? section.title
    : section.kind === "evidence"
      ? t("graph.results.evidenceSection")
      : t("graph.results.ungroupedSection");

  return (
    <section
      data-graph-results-section={section.key}
      data-graph-results-section-kind={section.kind}
      aria-label={title}
      className="min-w-0"
    >
      <h3 className="mb-2 truncate text-xs font-semibold tracking-wide text-text-secondary">
        {title}
      </h3>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5">
        {section.items.map((item) => (
          <ResultCard
            key={item.nodeId}
            item={item}
            selected={selectedSet.has(item.nodeId)}
            busy={busy}
            running={runningNodeId === item.nodeId}
            plannedAction={dominantPlannedAction([plannedActions?.[item.nodeId]])}
            runBlockedReason={blockedReasons?.[item.nodeId]}
            cardRef={(element) => {
              if (element) cardRefs.current.set(item.nodeId, element);
              else cardRefs.current.delete(item.nodeId);
            }}
            onSelectItem={onSelectItem}
            onLocateItem={onLocateItem}
            onOpenHistory={onOpenHistory}
            onRunItem={onRunItem}
            onPreviewRun={onPreviewRun}
            onHideRunPreview={onHideRunPreview}
            onOpenLocalEdit={onOpenLocalEdit}
            onPreviewImage={onPreviewImage}
            onBindEvidence={onBindEvidence}
          />
        ))}
      </div>
    </section>
  );
}

function ResultCard({
  item,
  selected,
  busy,
  running,
  plannedAction,
  runBlockedReason,
  cardRef,
  onSelectItem,
  onLocateItem,
  onOpenHistory,
  onRunItem,
  onPreviewRun,
  onHideRunPreview,
  onOpenLocalEdit,
  onPreviewImage,
  onBindEvidence,
}: {
  item: GraphResultItem;
  selected: boolean;
  busy: boolean;
  running: boolean;
  plannedAction?: GraphPlannedAction | null;
  runBlockedReason?: string;
  cardRef: (element: HTMLElement | null) => void;
  onSelectItem: (item: GraphResultItem) => void;
  onLocateItem: (item: GraphResultItem) => void;
  onOpenHistory: (item: GraphResultItem) => void;
  onRunItem: (item: GraphResultItem) => void;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
  onOpenLocalEdit?: (item: GraphResultItem) => void;
  onPreviewImage?: (item: GraphResultItem) => void;
  onBindEvidence?: (item: GraphResultItem) => void;
}) {
  const { t } = useI18n();
  const failed = item.status === "failed" || item.status === "unknown";
  const frozen = plannedAction === "frozen" && (item.status === "idle" || item.status === "skipped");
  const statusLabel = frozen ? t("graph.preview.frozen") : t(statusKey(item.status));
  const purpose = purposeLabel(item, t);
  const focusLabel = purpose ? `${item.title} · ${purpose} · ${statusLabel}` : `${item.title} · ${statusLabel}`;
  const failureReason = failed ? item.failureReason ?? t("graph.shots.failureUnknown") : null;
  const canEdit = Boolean(item.currentAssetId && item.kind === "generation" && onOpenLocalEdit);
  const canPreview = Boolean(item.currentAssetId && onPreviewImage);
  const canBind = item.kind === "evidence" && Boolean(onBindEvidence);

  return (
    <article
      ref={cardRef}
      data-graph-result-item={item.nodeId}
      data-graph-result-kind={item.kind}
      data-graph-result-status={item.status}
      data-graph-result-stale={item.showingStaleCurrent ? "true" : "false"}
      data-graph-planned-action={plannedAction ?? undefined}
      className={`group relative flex min-w-0 flex-col overflow-hidden rounded-control border bg-surface-raised ${plannedActionClassName(plannedAction)} ${
        failed
          ? "border-state-error/60"
          : selected
            ? "border-accent"
            : "border-border-l1"
      }`}
    >
      <Tooltip content={failureReason ?? focusLabel}>
        <button
          type="button"
          data-graph-result-select
          aria-pressed={selected}
          aria-label={focusLabel}
          title={failureReason ?? undefined}
          onClick={() => onSelectItem(item)}
          onDoubleClick={() => {
            if (canPreview) onPreviewImage?.(item);
          }}
          className="relative aspect-square w-full overflow-hidden bg-surface-subtle text-text-muted outline-none focus-visible:ring-2 focus-visible:ring-focus-ring focus-visible:ring-inset"
        >
          {item.currentAssetId ? (
            <img
              src={api.getProductImageAssetMediaUrl(item.currentAssetId, "thumbnail")}
              alt=""
              className="h-full w-full object-cover"
            />
          ) : (
            <span className="flex h-full w-full items-center justify-center">
              <Images size={22} aria-hidden="true" />
            </span>
          )}
          <span
            className={`absolute right-1.5 top-1.5 h-2.5 w-2.5 rounded-full border-2 border-surface-raised ${statusColor(item.status)} ${running ? "animate-pulse motion-reduce:animate-none" : ""}`}
            aria-hidden="true"
          />
          {item.showingStaleCurrent ? (
            <span className="absolute bottom-1 left-1 rounded bg-surface-inverse/80 px-1 text-[9px] font-semibold text-surface-raised">
              {t("graph.results.staleCurrent")}
            </span>
          ) : null}
        </button>
      </Tooltip>
      <div className="flex min-w-0 flex-col gap-1 px-2 py-1.5">
        <span className="truncate text-[11px] font-semibold text-text-primary" title={item.title}>
          {item.title}
        </span>
        {purpose ? (
          <span className="truncate text-[10px] text-text-muted" title={purpose}>{purpose}</span>
        ) : null}
        <div className="flex items-center justify-between gap-1">
          <span className="min-w-0 truncate text-[10px] font-medium text-text-secondary">{statusLabel}</span>
          <div className="flex shrink-0 items-center gap-0.5">
            <IconButton
              label={t("graph.results.locate")}
              size="sm"
              data-graph-result-locate
              className="!h-7 !w-7"
              onClick={() => onLocateItem(item)}
            >
              <LocateFixed size={12} aria-hidden="true" />
            </IconButton>
            <IconButton
              label={t("graph.results.history")}
              size="sm"
              data-graph-result-history
              className="!h-7 !w-7"
              onClick={() => onOpenHistory(item)}
            >
              <History size={12} aria-hidden="true" />
            </IconButton>
            {canEdit ? (
              <IconButton
                label={t("localEdit.open")}
                size="sm"
                data-graph-result-edit
                className="!h-7 !w-7"
                onClick={() => onOpenLocalEdit?.(item)}
              >
                <Pencil size={12} aria-hidden="true" />
              </IconButton>
            ) : null}
            {canBind ? (
              <IconButton
                label={t("graph.inspector.bind")}
                size="sm"
                data-graph-result-bind
                className="!h-7 !w-7"
                onClick={() => onBindEvidence?.(item)}
              >
                <Images size={12} aria-hidden="true" />
              </IconButton>
            ) : null}
            {item.runnable ? (
              <Tooltip content={runBlockedReason ?? t("graph.canvas.runNode")}>
                <span>
                  <IconButton
                    label={runBlockedReason ?? t("graph.canvas.runNode")}
                    size="sm"
                    variant="primary"
                    busy={running}
                    disabled={busy || Boolean(runBlockedReason)}
                    data-graph-result-run
                    className="!h-7 !w-7"
                    {...runPreviewPointerHandlers(
                      { scope: "node", node_id: item.nodeId },
                      onPreviewRun,
                      onHideRunPreview,
                    )}
                    onClick={() => onRunItem(item)}
                  >
                    <Play size={12} aria-hidden="true" />
                  </IconButton>
                </span>
              </Tooltip>
            ) : null}
          </div>
        </div>
      </div>
    </article>
  );
}

function purposeLabel(item: GraphResultItem, t: (key: TranslationKey) => string): string | null {
  if (item.kind === "evidence") return t("graph.inspector.role.evidence");
  const key = item.imageTypeKey;
  if (!key) return null;
  const translation = AGENT_IMAGE_TYPE_TRANSLATIONS[key as AgentProductImageTypeKey];
  return translation ? t(translation.title) : key;
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
