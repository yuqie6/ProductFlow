import { Check, Loader2, RotateCcw } from "lucide-react";

import { Button } from "../../../components/ui/button";
import { useI18n } from "../../../lib/preferences";
import type { GraphCatalogConfigField, GraphDocumentCandidate } from "../../../lib/types";
import {
  candidateSectionLabelKey,
  documentCandidateFieldLabelKey,
  documentCandidateFieldRows,
  formatDocumentCandidateValue,
  type DocumentCandidateDisplayItem,
  type DocumentCandidateFieldRow,
} from "./documentCandidateView";

export function DocumentCandidateReview({
  candidate,
  loading,
  error,
  fields,
  selectedKeys,
  busy,
  onToggle,
  onApplySelected,
  onApplyAll,
  onDiscard,
  onRetry,
}: {
  candidate: GraphDocumentCandidate | null;
  loading: boolean;
  error: string | null;
  fields: GraphCatalogConfigField[];
  selectedKeys: string[];
  busy: boolean;
  onToggle: (key: string) => void;
  onApplySelected: () => void;
  onApplyAll: () => void;
  onDiscard: () => void;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  if (error) {
    return (
      <section className="border-b border-border-l1 pb-4" data-graph-document-candidate>
        <div role="alert" className="text-xs leading-5 text-state-error">{error}</div>
        <Button size="sm" variant="secondary" className="mt-2" onClick={onRetry} disabled={busy}>
          <RotateCcw size={12} aria-hidden="true" />
          {t("workbench.retry")}
        </Button>
      </section>
    );
  }
  if (loading || !candidate) {
    return (
      <section className="border-b border-border-l1 pb-4" data-graph-document-candidate>
        <div className="flex items-center gap-2 text-xs text-text-muted">
          <Loader2 size={14} className="animate-spin motion-reduce:animate-none" aria-hidden="true" />
          {t("graph.candidate.loading")}
        </div>
      </section>
    );
  }
  const outdated = candidate.status === "outdated";
  const changedSections = candidate.sections.filter((section) => section.changed);
  return (
    <section className="border-b border-border-l1 pb-4" data-graph-document-candidate data-candidate-status={candidate.status}>
      <div className="flex items-center justify-between gap-2">
        <h4 className="text-xs font-semibold text-text-primary">{t("graph.candidate.title")}</h4>
        <span
          className={`inline-flex shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-semibold ${outdated
              ? "border-state-warning/35 bg-state-warning-soft text-state-warning"
              : "border-accent/35 bg-accent-soft text-accent"
            }`}
        >
          {outdated ? t("graph.candidate.outdated") : t("graph.candidate.ready")}
        </span>
      </div>
      {outdated ? (
        <p className="mt-2 text-xs leading-5 text-state-warning">{t("graph.candidate.outdatedDetail")}</p>
      ) : (
        <div className="mt-3 space-y-4">
          {changedSections.map((section) => {
            const selected = selectedKeys.includes(section.key);
            const rows = documentCandidateFieldRows(section.current, section.candidate, fields);
            return (
              <div
                key={section.key}
                data-candidate-section={section.key}
                className={`border-l-2 pl-3 ${selected ? "border-accent" : "border-border-l1"}`}
              >
                <button
                  type="button"
                  aria-pressed={selected}
                  disabled={busy}
                  onClick={() => onToggle(section.key)}
                  className={`inline-flex min-h-11 items-center gap-1.5 rounded-full border px-2.5 text-xs font-semibold outline-none transition-colors duration-fast focus-visible:ring-2 focus-visible:ring-focus-ring disabled:cursor-not-allowed disabled:opacity-45 motion-reduce:transition-none lg:min-h-8 ${selected
                      ? "border-accent/40 bg-accent-soft text-accent"
                      : "border-border-l1 bg-surface-raised text-text-secondary hover:border-border-l2 hover:bg-surface-subtle hover:text-text-primary"
                    }`}
                >
                  {selected ? <Check size={13} strokeWidth={2.5} aria-hidden="true" /> : null}
                  {t(candidateSectionLabelKey(section.key))}
                </button>
                <div className={`mt-2 space-y-3 ${selected ? "" : "opacity-45"}`}>
                  {rows.map((row) => (
                    <CandidateFieldCompare key={row.path} row={row} />
                  ))}
                </div>
              </div>
            );
          })}
          {changedSections.length === 0 ? (
            <p className="text-xs leading-5 text-text-muted">{t("graph.candidate.noChanges")}</p>
          ) : null}
        </div>
      )}
      <div className="mt-3 flex flex-wrap gap-2">
        {!outdated ? (
          <>
            <Button size="sm" variant="primary" onClick={onApplySelected} disabled={busy || selectedKeys.length === 0} busy={busy}>
              {t("graph.candidate.applySelected")}
            </Button>
            <Button size="sm" variant="secondary" onClick={onApplyAll} disabled={busy || changedSections.length === 0}>
              {t("graph.candidate.applyAll")}
            </Button>
          </>
        ) : null}
        <Button size="sm" variant="ghost" onClick={onDiscard} disabled={busy}>
          {t("graph.candidate.discard")}
        </Button>
      </div>
    </section>
  );
}

function CandidateFieldCompare({ row }: { row: DocumentCandidateFieldRow }) {
  const { t } = useI18n();
  const labelKey = documentCandidateFieldLabelKey(row);
  const label = labelKey ? t(labelKey) : row.key.replace(/[_-]+/g, " ");
  return (
    <div className="min-w-0" data-candidate-field={row.path}>
      <div className="text-[10px] font-medium text-text-muted">{label}</div>
      <div className="mt-1 space-y-1">
        <CandidateSide
          side="current"
          label={t("graph.candidate.current")}
          items={formatDocumentCandidateValue(row.current, row, t)}
        />
        <CandidateSide
          side="proposed"
          label={t("graph.candidate.proposed")}
          items={formatDocumentCandidateValue(row.proposed, row, t)}
          accent
        />
      </div>
    </div>
  );
}

function CandidateSide({
  side,
  label,
  items,
  accent = false,
}: {
  side: "current" | "proposed";
  label: string;
  items: DocumentCandidateDisplayItem[];
  accent?: boolean;
}) {
  return (
    <div
      data-candidate-side={side}
      className={`min-w-0 px-2 py-1.5${accent ? " rounded-md bg-accent-soft" : ""}`}
    >
      <div className={`text-[10px] font-medium ${accent ? "text-accent" : "text-text-muted"}`}>{label}</div>
      <div className="mt-0.5 space-y-1 text-xs leading-5 text-text-primary">
        {items.map((item, index) => (
          <div key={`${item.text}-${index}`} className="flex min-w-0 items-start gap-1.5">
            {item.swatch ? (
              <span
                className="mt-1 size-3 shrink-0 rounded-sm border border-border-l2"
                style={{ backgroundColor: item.swatch }}
                aria-hidden="true"
              />
            ) : null}
            <span className={`min-w-0 whitespace-pre-wrap break-words ${item.empty ? "text-text-muted" : ""}`}>
              {item.text}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
