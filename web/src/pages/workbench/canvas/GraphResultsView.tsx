/**
 * 图片成果工作视图：按分组组织展示，每项对应一个生图/证据节点。
 */

import {
  AlertCircle,
  Check,
  Eye,
  History,
  Images,
  Loader2,
  LocateFixed,
  Package,
  Pencil,
  Play,
  RefreshCw,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type MutableRefObject, type ReactNode } from "react";

import { Dialog, DialogContent } from "../../../components/ui/dialog";
import { IconButton } from "../../../components/ui/icon-button";
import { Tooltip } from "../../../components/ui/tooltip";
import { ApiError, api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { TranslationKey, TranslationParams } from "../../../lib/i18n";
import type {
  DeliveryAdoptionSlot,
  GraphPlannedAction,
  GraphRunSubmitInput,
  WorkflowNodeDisplayStatus,
} from "../../../lib/types";
import { AGENT_IMAGE_TYPE_TRANSLATIONS } from "../../product-create/imageTypeSelection";
import type { AgentProductImageTypeKey } from "../../../lib/types";
import {
  adoptionQualityIssueMessageKey,
  adoptionQualityStatusMessageKey,
  deliveryAdoptionFreshnessIssueMessageKey,
  type AdoptionQualityAssessment,
  type DeliveryAdoptionFreshnessAssessment,
  type DeliveryAdoptionFreshnessIssue,
} from "./deliveryAdoption";
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
  /** 当前产物的有限质量检查；服务端返回的采用槽位质量优先。 */
  adoptionQualityByNodeId?: ReadonlyMap<string, AdoptionQualityAssessment>;
  adoptedSlotByNodeId?: ReadonlyMap<string, DeliveryAdoptionSlot>;
  deliveryAdoptionFreshness?: DeliveryAdoptionFreshnessAssessment | null;
  adoptingNodeId?: string | null;
  exportingAdoption?: boolean;
  onAdoptItem?: (item: GraphResultItem) => void;
  onExportAdoption?: () => void;
  /** 成果态下收纳视图切换等，避免被画布右上浮动工具条叠住。 */
  headerActions?: ReactNode;
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
  adoptionQualityByNodeId,
  adoptedSlotByNodeId,
  deliveryAdoptionFreshness = null,
  adoptingNodeId = null,
  exportingAdoption = false,
  onAdoptItem,
  onExportAdoption,
  headerActions,
}: GraphResultsViewProps) {
  const { t } = useI18n();
  const [sectionKey, setSectionKey] = useState<string | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const [compact, setCompact] = useState(false);
  const regionRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = regionRef.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => setCompact(entry.contentRect.width < 960));
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const activeSection = sections.find(section => section.key === sectionKey);
  const visibleSections = activeSection ? [activeSection] : sections;
  const detailNodeId = selectedNodeIds.find(id => visibleSections.some(section => section.items.some(item => item.nodeId === id)));
  const selectItem = (item: GraphResultItem) => { onSelectItem(item); setDetailOpen(true); };
  const error = operationError ?? runsError;
  const itemCount = useMemo(
    () => sections.reduce((count, section) => count + section.items.length, 0),
    [sections],
  );
  const selectedSet = useMemo(() => new Set(selectedNodeIds), [selectedNodeIds]);
  const resultTitleByNodeId = useMemo(
    () => new Map(sections.flatMap((section) => section.items.map((item) => [item.nodeId, item.title] as const))),
    [sections],
  );
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
      className="absolute inset-0 z-10 flex min-h-0 flex-col bg-surface-base @container/results"
    >
      <header className="flex shrink-0 flex-wrap items-center gap-3 border-b border-border-l1 bg-surface-raised px-4 py-4">
        <Images size={16} className="shrink-0 text-text-muted" aria-hidden="true" />
        <h2 className="min-w-0 flex-1 truncate text-lg font-semibold text-text-primary">
          {t("graph.results.title")}
        </h2>
        <span className="shrink-0 text-[11px] font-medium text-text-muted">
          {t("graph.results.count", { count: itemCount })}
        </span>
        <div className="relative z-20 flex shrink-0 items-center gap-1">
          {headerActions}
          {onExportAdoption ? (
            <IconButton
              label={t("graph.results.exportAdoption")}
              size="sm"
              data-graph-results-export-adoption
              variant="primary"
              className="!w-auto gap-2 px-3"
              disabled={busy || exportingAdoption}
              onClick={onExportAdoption}
            >
              {exportingAdoption
                ? <Loader2 size={13} className="animate-spin motion-reduce:animate-none" aria-hidden="true" />
                : <Package size={13} aria-hidden="true" />}
              <span className="text-xs">{t("graph.results.exportAdoption")}</span>
            </IconButton>
          ) : null}
          {runsFetching ? (
            <Loader2 size={14} className="shrink-0 animate-spin text-text-muted motion-reduce:animate-none" aria-hidden="true" />
          ) : null}
        </div>
      </header>
      {deliveryAdoptionFreshness?.isStale ? (
        <div
          role="status"
          data-graph-results-adoption-stale
          className="flex min-w-0 shrink-0 items-start gap-2 border-b border-state-warning/30 bg-state-warning-soft px-3 py-2 text-[11px] leading-4 text-state-warning sm:px-4"
        >
          <AlertCircle size={14} className="mt-0.5 shrink-0" aria-hidden="true" />
          <div className="min-w-0 flex-1">
            <p className="break-words font-semibold">{t("graph.results.adoptionSnapshotStale")}</p>
            {deliveryAdoptionFreshness.issues.length ? (
              <ul tabIndex={0} aria-label={t("graph.results.adoptionSnapshotStale")} className="mt-1 max-h-24 overflow-y-auto overscroll-contain list-disc space-y-0.5 break-words pl-4 outline-none focus-visible:ring-2 focus-visible:ring-focus-ring">
                {deliveryAdoptionFreshness.issues.map((issue, index) => (
                  <li key={`${issue.code}-${issue.nodeId ?? "version"}-${index}`} data-graph-results-adoption-issue={issue.code}>
                    {adoptionFreshnessIssueText(issue, resultTitleByNodeId, t)}
                  </li>
                ))}
              </ul>
            ) : null}
          </div>
        </div>
      ) : null}
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
      <div ref={regionRef} className="relative flex min-h-0 flex-1 flex-col @min-[960px]/results:flex-row">
        <nav aria-label={t("graph.results.title")} className="flex shrink-0 gap-1 overflow-auto border-b border-border-l1 bg-surface-raised p-2 @min-[960px]/results:w-40 @min-[960px]/results:flex-col @min-[960px]/results:border-r @min-[960px]/results:border-b-0 @min-[960px]/results:py-5">
          {[{key: null, title: t("graph.results.title"), count: itemCount}, ...sections.map(section => ({key: section.key, title: section.kind === "group" ? section.title : t(section.kind === "evidence" ? "graph.results.evidenceSection" : "graph.results.ungroupedSection"), count: section.items.length}))].map(entry => (
            <button key={entry.key ?? "all"} type="button" aria-pressed={(activeSection?.key ?? null) === entry.key} onClick={() => {setSectionKey(entry.key); setDetailOpen(false);}} className={`flex min-h-11 shrink-0 items-center justify-between gap-3 rounded-control px-3 py-2 text-left text-xs outline-none focus-visible:ring-2 focus-visible:ring-focus-ring ${(activeSection?.key ?? null) === entry.key ? "bg-accent-soft text-accent" : "text-text-secondary hover:bg-surface-subtle"}`}>
              <span className="max-w-40 truncate" title={entry.title}>{entry.title}</span><span className="tabular-nums text-text-muted">{entry.count}</span>
            </button>
          ))}
        </nav>
      <div
        ref={scrollerRef}
        data-graph-results-scroll
        className={`min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain p-4 ${detailNodeId && !compact ? "pr-[320px]" : ""}`}
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
          {visibleSections.map((section) => (
            <ResultSection
              key={section.key}
              section={section}
              compact={compact}
              detailNodeId={detailNodeId}
              detailOpen={detailOpen}
              onCloseDetail={() => setDetailOpen(false)}
              selectedSet={selectedSet}
              busy={busy}
              runningNodeId={runningNodeId}
              plannedActions={plannedActions}
              blockedReasons={blockedReasons}
              cardRefs={cardRefs}
              onSelectItem={selectItem}
              onLocateItem={onLocateItem}
              onOpenHistory={onOpenHistory}
              onRunItem={onRunItem}
              onPreviewRun={onPreviewRun}
              onHideRunPreview={onHideRunPreview}
              onOpenLocalEdit={onOpenLocalEdit}
              onPreviewImage={onPreviewImage}
              onBindEvidence={onBindEvidence}
              adoptionQualityByNodeId={adoptionQualityByNodeId}
              adoptedSlotByNodeId={adoptedSlotByNodeId}
              deliveryAdoptionFreshness={deliveryAdoptionFreshness}
              adoptingNodeId={adoptingNodeId}
              onAdoptItem={onAdoptItem}
            />
          ))}
        </div>
      </div>
      </div>
    </section>
  );
}

function ResultSection({
  section,
  compact, detailNodeId, detailOpen, onCloseDetail,
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
  adoptionQualityByNodeId,
  adoptedSlotByNodeId,
  deliveryAdoptionFreshness,
  adoptingNodeId,
  onAdoptItem,
}: {
  section: GraphResultSection;
  compact: boolean;
  detailNodeId?: string;
  detailOpen: boolean;
  onCloseDetail: () => void;
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
  adoptionQualityByNodeId?: ReadonlyMap<string, AdoptionQualityAssessment>;
  adoptedSlotByNodeId?: ReadonlyMap<string, DeliveryAdoptionSlot>;
  deliveryAdoptionFreshness?: DeliveryAdoptionFreshnessAssessment | null;
  adoptingNodeId: string | null;
  onAdoptItem?: (item: GraphResultItem) => void;
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
      <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,160px),1fr))] gap-4 @min-[600px]/results:grid-cols-[repeat(auto-fill,minmax(min(100%,210px),1fr))]">
        {section.items.map((item) => (
          <ResultCard
            key={item.nodeId}
            item={item}
            showDetail={detailNodeId === item.nodeId}
            compact={compact}
            detailOpen={detailOpen}
            onCloseDetail={onCloseDetail}
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
            adoptionQuality={adoptionQualityByNodeId?.get(item.nodeId) ?? null}
            adoptedSlot={adoptedSlotByNodeId?.get(item.nodeId) ?? null}
            deliveryAdoptionFreshness={deliveryAdoptionFreshness}
            adopting={adoptingNodeId === item.nodeId}
            onAdoptItem={onAdoptItem}
          />
        ))}
      </div>
    </section>
  );
}

function ResultCard({
  item,
  showDetail, compact, detailOpen, onCloseDetail,
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
  adoptionQuality,
  adoptedSlot,
  deliveryAdoptionFreshness,
  adopting,
  onAdoptItem,
}: {
  item: GraphResultItem;
  showDetail: boolean;
  compact: boolean;
  detailOpen: boolean;
  onCloseDetail: () => void;
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
  adoptionQuality: AdoptionQualityAssessment | null;
  adoptedSlot: DeliveryAdoptionSlot | null;
  deliveryAdoptionFreshness?: DeliveryAdoptionFreshnessAssessment | null;
  adopting: boolean;
  onAdoptItem?: (item: GraphResultItem) => void;
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
  const adoptionDiffersForItem = Boolean(
    deliveryAdoptionFreshness?.issues.some((issue) => (
      issue.code === "different_graph" || issue.nodeId === item.nodeId
    )),
  );
  const isAdopted = Boolean(
    adoptedSlot?.source_asset_id
    && item.currentAssetId
    && adoptedSlot.source_asset_id === item.currentAssetId
    && (adoptedSlot.source_node_id == null || adoptedSlot.source_node_id === item.nodeId)
    && !adoptionDiffersForItem,
  );
  const canAdopt = Boolean(
    item.kind === "generation"
    && item.currentAssetId
    && onAdoptItem
    && !isAdopted,
  );
  const adoptedQuality = isAdopted ? adoptedSlot : null;
  const qualityStatus = adoptedQuality?.quality_status ?? adoptionQuality?.status ?? null;
  const qualityDetail = adoptedQuality?.quality_detail
    ?? adoptionQuality?.issueCodes.map((code) => t(adoptionQualityIssueMessageKey(code))).join(" · ")
    ?? null;

  const detailBody = (
    <div className="space-y-5">
      {showDetail && item.currentAssetId ? <img src={api.getProductImageAssetMediaUrl(item.currentAssetId, "preview")} alt={item.title} className="aspect-square w-full rounded-panel bg-surface-subtle object-contain" /> : null}
      <div><h3 className="break-words text-sm font-semibold">{item.title}</h3>{purpose ? <p className="mt-1 text-xs text-text-muted">{purpose}</p> : null}</div>
      <div className="flex flex-wrap gap-2 text-xs"><span>{statusLabel}</span>{isAdopted ? <span className="text-accent">{t("graph.results.adopted")}</span> : null}</div>
      {failureReason ? <p role="alert" className="break-words text-xs text-state-error">{failureReason}</p> : null}
      {item.showingStaleCurrent ? <p className="text-xs text-state-warning">{t("graph.results.staleCurrent")}</p> : null}
          <div className="grid grid-cols-2 gap-2">
            <IconButton
              label={t("graph.results.locate")}
              size="sm"
              data-graph-result-locate
              className="!h-11 !w-full justify-start gap-2 px-3"
              onClick={() => { onCloseDetail(); onLocateItem(item); }}
            >
              <LocateFixed size={14} aria-hidden="true" /><span className="text-xs">{t("graph.results.locate")}</span>
            </IconButton>
            <IconButton
              label={t("graph.results.history")}
              size="sm"
              data-graph-result-history
              className="!h-11 !w-full justify-start gap-2 px-3"
              onClick={() => { onCloseDetail(); onOpenHistory(item); }}
            >
              <History size={14} aria-hidden="true" /><span className="text-xs">{t("graph.results.history")}</span>
            </IconButton>
            {canAdopt ? (
              <IconButton
                label={t("graph.results.adopt")}
                size="sm"
                data-graph-result-adopt
                variant="primary"
                className="order-first col-span-2 !h-11 !w-full gap-2 px-3"
                busy={adopting}
                disabled={busy || adopting}
                onClick={() => { onCloseDetail(); onAdoptItem?.(item); }}
              >
                <Check size={14} aria-hidden="true" /><span className="text-xs">{t("graph.results.adopt")}</span>
              </IconButton>
            ) : null}
            {canEdit ? (
              <IconButton
                label={t("localEdit.open")}
                size="sm"
                data-graph-result-edit
                className="!h-11 !w-full justify-start gap-2 px-3"
                onClick={() => { onCloseDetail(); onOpenLocalEdit?.(item); }}
              >
                <Pencil size={14} aria-hidden="true" /><span className="text-xs">{t("localEdit.open")}</span>
              </IconButton>
            ) : null}
            {canBind ? (
              <IconButton
                label={t("graph.inspector.bind")}
                size="sm"
                data-graph-result-bind
                className="!h-11 !w-full justify-start gap-2 px-3"
                onClick={() => { onCloseDetail(); onBindEvidence?.(item); }}
              >
                <Images size={14} aria-hidden="true" /><span className="text-xs">{t("graph.inspector.bind")}</span>
              </IconButton>
            ) : null}
            {item.runnable ? (
              <Tooltip content={runBlockedReason ?? t("graph.canvas.runNode")}>
                <span>
                  <IconButton
                    label={runBlockedReason ?? t("graph.canvas.runNode")}
                    size="sm"
                    busy={running}
                    disabled={busy || Boolean(runBlockedReason)}
                    data-graph-result-run
                    className="!h-11 !w-full justify-start gap-2 px-3"
                    {...runPreviewPointerHandlers(
                      { scope: "node", node_id: item.nodeId },
                      onPreviewRun,
                      onHideRunPreview,
                    )}
                    onClick={() => onRunItem(item)}
                  >
                    <Play size={14} aria-hidden="true" /><span className="text-xs">{t("graph.canvas.runNode")}</span>
                  </IconButton>
                </span>
              </Tooltip>
            ) : null}
          </div>
        {qualityStatus ? (
          <div
            data-graph-result-quality
            data-graph-result-quality-status={qualityStatus}
            className={`flex min-w-0 flex-col gap-0.5 text-[10px] leading-4 ${
              qualityStatus === "fail" ? "text-state-error" : qualityStatus === "unchecked" ? "text-state-warning" : "text-state-success"
            }`}
          >
            <span className="font-semibold">{t(adoptionQualityStatusMessageKey(qualityStatus))}</span>
            {qualityDetail ? (
              <span title={qualityDetail} className="break-words">{qualityDetail}</span>
            ) : null}
          </div>
        ) : null}
    </div>
  );

  return (
    <article
      ref={cardRef}
      data-graph-result-item={item.nodeId}
      data-graph-result-kind={item.kind}
      data-graph-result-status={item.status}
      data-graph-result-stale={item.showingStaleCurrent ? "true" : "false"}
      data-graph-result-delivery-adopted={isAdopted ? "true" : "false"}
      data-graph-planned-action={plannedAction ?? undefined}
      className={`group flex min-w-0 flex-col rounded-panel border bg-surface-raised ${plannedActionClassName(plannedAction)} ${
        failed
          ? "border-state-error/60"
          : selected
            ? "border-accent"
            : "border-border-l1"
      }`}
    >
      <div className="relative overflow-hidden rounded-t-panel">
      <Tooltip content={failureReason ?? focusLabel}>
        <button
          type="button"
          data-graph-result-select
          aria-pressed={selected}
          aria-label={focusLabel}
          title={failureReason ?? undefined}
          onClick={() => onSelectItem(item)}
          className="relative aspect-square w-full overflow-hidden bg-surface-subtle text-text-muted outline-none focus-visible:ring-2 focus-visible:ring-focus-ring focus-visible:ring-inset"
        >
          {item.currentAssetId ? (
            <img
              src={api.getProductImageAssetMediaUrl(item.currentAssetId, "thumbnail")}
              alt=""
              className="h-full w-full object-contain"
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
            <span className="absolute bottom-1 left-1 rounded bg-surface-inverse/80 px-1 text-[9px] font-semibold text-surface-inverse-fg">
              {t("graph.results.staleCurrent")}
            </span>
          ) : null}
          {isAdopted ? (
            <span
              data-graph-result-adopted="true"
              className="absolute bottom-1 right-1 rounded bg-accent/90 px-1 text-[9px] font-semibold text-accent-fg"
            >
              {t("graph.results.adopted")}
            </span>
          ) : null}
        </button>
      </Tooltip>
      {canPreview ? (
        <IconButton
          label={t("graph.results.preview")}
          size="sm"
          data-graph-result-preview
          className="absolute left-2 top-2 !h-8 !w-8 bg-surface-raised shadow-sm"
          onClick={() => onPreviewImage?.(item)}
        >
          <Eye size={12} aria-hidden="true" />
        </IconButton>
      ) : null}
      </div>
      <div className="flex min-w-0 flex-col gap-2 p-3">
        <span className="truncate text-sm font-semibold text-text-primary" title={item.title}>
          {item.title}
        </span>
        {purpose ? (
          <span className="truncate text-xs text-text-muted" title={purpose}>{purpose}</span>
        ) : null}
        <div className="flex items-center justify-between gap-1">
          <span className="min-w-0 truncate text-xs font-medium text-text-secondary">{statusLabel}</span>
        </div>
        {compact ? (
          <Dialog open={showDetail && detailOpen} onOpenChange={(open) => { if (!open) onCloseDetail(); }}>
            <DialogContent title={t("graph.inspector.title")} closeLabel={t("common.cancel")} placement="right" bodyClassName="max-h-[calc(100dvh-80px)] overflow-y-auto">{detailBody}</DialogContent>
          </Dialog>
        ) : (
          <aside hidden={!showDetail} data-graph-result-detail={item.nodeId} aria-label={t("graph.inspector.title")} className="absolute inset-y-0 right-0 z-20 w-[300px] overflow-y-auto border-l border-border-l1 bg-surface-raised p-5">
            <h2 className="mb-5 text-sm font-semibold">{t("graph.inspector.title")}</h2>{detailBody}
          </aside>
        )}
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

function adoptionFreshnessIssueText(
  issue: DeliveryAdoptionFreshnessIssue,
  resultTitleByNodeId: ReadonlyMap<string, string>,
  t: (key: TranslationKey, params?: TranslationParams) => string,
): string {
  const key = deliveryAdoptionFreshnessIssueMessageKey(issue.code);
  if (issue.code === "different_graph" || issue.code === "unlinked_node") return t(key);
  return t(key, {
    title: issue.nodeId
      ? resultTitleByNodeId.get(issue.nodeId) ?? t("graph.results.adoptionSnapshotUnknownSlot")
      : t("graph.results.adoptionSnapshotUnknownSlot"),
  });
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
