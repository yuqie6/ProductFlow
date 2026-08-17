import { Check, ExternalLink, GitBranch, Image as ImageIcon, Loader2 } from "lucide-react";
import type { ReactNode } from "react";

import { useI18n } from "../../lib/preferences";
import type { GlobalWorkflowDraftReview } from "../../lib/types";

interface GlobalWorkflowDraftCardProps {
  review: GlobalWorkflowDraftReview | null;
  loading: boolean;
  error: string | null;
  busy: boolean;
  onConfirm: () => void;
  onOpenProduct: () => void;
}

export function GlobalWorkflowDraftCard({
  review,
  loading,
  error,
  busy,
  onConfirm,
  onOpenProduct,
}: GlobalWorkflowDraftCardProps) {
  const { t } = useI18n();
  const draft = review?.draft;
  const revision = draft?.current_revision;
  const payload = revision?.payload;
  const imageCount = payload?.image_types.reduce((total, imageType) => total + imageType.quantity, 0) ?? 0;
  const unresolved = Boolean(
    payload && (
      payload.missing_fact_keys.length > 0 ||
      payload.facts.some((fact) => fact.status === "conflicted")
    ),
  );

  return (
    <section className="mt-3 overflow-hidden border border-border-l2 bg-surface-raised shadow-sm">
      <header className="border-b border-border-l2 bg-surface-subtle/60 px-3 py-3">
        <div className="flex items-start gap-2">
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-accent/10 text-accent">
            <GitBranch size={16} aria-hidden="true" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h4 className="text-sm font-semibold text-text-primary">{t("globalAgent.workflowDraft.title")}</h4>
              {revision ? (
                <span className="rounded bg-surface-subtle px-2 py-0.5 text-[10px] font-semibold text-text-muted">
                  {t("globalAgent.workflowDraft.version", { version: revision.version })}
                </span>
              ) : null}
            </div>
            {loading ? (
              <div className="mt-2 flex items-center gap-2 text-xs text-text-secondary">
                <Loader2 size={14} className="animate-spin" />
                {t("globalAgent.workflowDraft.loading")}
              </div>
            ) : review ? (
              <p className="mt-1 text-xs leading-5 text-text-secondary">
                {t("globalAgent.workflowDraft.target", { product: review.product_name })}
              </p>
            ) : null}
          </div>
        </div>
      </header>

      {error ? <p role="alert" className="border-b border-state-error/20 bg-state-error/10 px-3 py-2 text-xs leading-5 text-state-error">{error}</p> : null}

      {payload && revision ? (
        <div className="space-y-3 px-3 py-3">
          <p className="text-sm leading-6 text-text-primary">{payload.confirmation_summary}</p>
          <div className="grid grid-cols-3 gap-2">
            <Metric icon={<ImageIcon size={13} />} label={t("globalAgent.workflowDraft.imageTypes")} value={payload.image_types.length} />
            <Metric icon={<ImageIcon size={13} />} label={t("globalAgent.workflowDraft.images")} value={imageCount} />
            <Metric icon={<GitBranch size={13} />} label={t("globalAgent.workflowDraft.nodes")} value={payload.nodes.length} />
          </div>
          <details className="border-t border-border-l2 pt-2">
            <summary className="cursor-pointer text-xs font-semibold text-text-secondary hover:text-text-primary">
              {t("globalAgent.workflowDraft.details")}
            </summary>
            <div className="mt-3 space-y-2 text-xs text-text-secondary">
              {payload.image_types.map((imageType) => (
                <div key={imageType.key} className="flex items-center justify-between gap-3 border-b border-border-l1 pb-2">
                  <span className="min-w-0 truncate font-medium text-text-primary">{imageType.title}</span>
                  <span className="shrink-0 tabular-nums">{imageType.quantity} {t("globalAgent.workflowDraft.imagesUnit")}</span>
                </div>
              ))}
              <div className="pt-1">{t("globalAgent.workflowDraft.references", { count: payload.reference_bindings.length })}</div>
            </div>
          </details>
          {unresolved ? <p className="border-l-2 border-state-warning bg-state-warning/10 px-2 py-1.5 text-xs leading-5 text-state-warning">{t("globalAgent.workflowDraft.unresolved")}</p> : null}
          {draft.status === "confirmed" ? (
            <p className="flex items-center gap-2 text-xs font-semibold text-state-success"><Check size={14} />{t("globalAgent.workflowDraft.confirmed")}</p>
          ) : null}
          <div className="flex flex-wrap gap-2 pt-1">
            <button
              type="button"
              onClick={onConfirm}
              disabled={busy || unresolved || draft.status !== "awaiting_confirmation"}
              className="inline-flex min-h-9 items-center justify-center gap-2 rounded-md bg-accent px-3 text-xs font-semibold text-accent-fg hover:bg-accent-strong disabled:cursor-not-allowed disabled:opacity-45"
            >
              {busy ? <Loader2 size={14} className="animate-spin" /> : <Check size={14} />}
              {busy ? t("globalAgent.workflowDraft.confirming") : t("globalAgent.workflowDraft.confirm")}
            </button>
            <button
              type="button"
              onClick={onOpenProduct}
              className="inline-flex min-h-9 items-center justify-center gap-2 rounded-md border border-border-l2 px-3 text-xs font-semibold text-text-primary hover:border-accent hover:text-accent"
            >
              <ExternalLink size={14} />
              {t("globalAgent.workflowDraft.openProduct")}
            </button>
          </div>
        </div>
      ) : null}
    </section>
  );
}

function Metric({ icon, label, value }: { icon: ReactNode; label: string; value: number }) {
  return (
    <div className="min-w-0 border border-border-l1 bg-surface-subtle px-2 py-2">
      <div className="flex items-center gap-1 text-[10px] text-text-muted">{icon}<span className="truncate">{label}</span></div>
      <div className="mt-1 text-base font-semibold tabular-nums text-text-primary">{value}</div>
    </div>
  );
}
