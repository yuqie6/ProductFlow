import {
  Braces,
  Check,
  CircleAlert,
  GitBranch,
  Image as ImageIcon,
  Languages,
  Loader2,
  Palette,
  RefreshCw,
  ScanSearch,
  X,
} from "lucide-react";
import type { ReactNode } from "react";

import { api } from "../../lib/api";
import type { TranslationKey } from "../../lib/i18n";
import { useI18n } from "../../lib/preferences";
import type {
  ProductFactSourceType,
  ProductFactStatus,
  WorkflowDeliverySpec,
  WorkflowDraft,
  WorkflowDraftRevision,
  WorkflowGenerationSpec,
} from "../../lib/types";
import { AGENT_IMAGE_TYPE_TRANSLATIONS } from "../product-create/imageTypeSelection";
import {
  deriveWorkflowDraftReview,
  formatWorkflowDraftValue,
  isAgentProductImageTypeKey,
  type WorkflowImageTypeChange,
} from "./workflowDraftConfirmation";

interface WorkflowDraftConfirmationProps {
  draft: WorkflowDraft;
  busy: boolean;
  error: string | null;
  conflictDetected?: boolean;
  onConfirm: (revision: WorkflowDraftRevision) => void;
  onClose?: () => void;
}

const factStatusKeys = {
  observed: "workflowConfirmation.factStatus.observed",
  user_declared: "workflowConfirmation.factStatus.userDeclared",
  confirmed: "workflowConfirmation.factStatus.confirmed",
  conflicted: "workflowConfirmation.factStatus.conflicted",
} satisfies Record<ProductFactStatus, TranslationKey>;

const factSourceKeys = {
  user: "workflowConfirmation.factSource.user",
  image_observation: "workflowConfirmation.factSource.imageObservation",
  agent_inference: "workflowConfirmation.factSource.agentInference",
  legacy_product: "workflowConfirmation.factSource.legacyProduct",
} satisfies Record<ProductFactSourceType, TranslationKey>;

const changeKeys = {
  added: "workflowConfirmation.change.added",
  removed: "workflowConfirmation.change.removed",
  quantity: "workflowConfirmation.change.quantity",
  order: "workflowConfirmation.change.order",
} satisfies Record<WorkflowImageTypeChange, TranslationKey>;

export function WorkflowDraftConfirmation({
  draft,
  busy,
  error,
  conflictDetected = false,
  onConfirm,
  onClose,
}: WorkflowDraftConfirmationProps) {
  const { t } = useI18n();
  const revision = draft.current_revision;
  if (!revision) return null;
  const payload = revision.payload;
  const review = deriveWorkflowDraftReview(draft.intake, payload);
  const unresolved = payload.missing_fact_keys.length > 0 ||
    payload.facts.some((fact) => fact.status === "conflicted");
  const actionAllowed = draft.status === "awaiting_confirmation" || draft.status === "confirmed";
  const retrying = Boolean(revision.confirmed_at);

  return (
    <section
      data-workflow-draft-confirmation
      className="min-h-0 overflow-y-auto bg-white text-zinc-900 dark:bg-[#090d13] dark:text-slate-100"
      aria-labelledby="workflow-confirmation-title"
    >
      <header className="sticky top-0 z-10 border-b border-zinc-200 bg-white/95 px-4 py-4 backdrop-blur dark:border-slate-800 dark:bg-[#090d13]/95 sm:px-6">
        <div className="flex flex-wrap items-start gap-4">
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 id="workflow-confirmation-title" className="text-base font-semibold text-zinc-950 dark:text-white">
                {t("workflowConfirmation.title")}
              </h2>
              <span className="rounded bg-zinc-100 px-2 py-1 font-mono text-[11px] font-semibold text-zinc-600 dark:bg-slate-800 dark:text-slate-300">
                {t("workflowConfirmation.revision", { version: revision.version })}
              </span>
            </div>
            <p className="mt-2 max-w-4xl text-sm leading-6 text-zinc-600 dark:text-slate-300">
              {payload.confirmation_summary}
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {onClose ? (
              <button
                type="button"
                onClick={onClose}
                disabled={busy}
                aria-label={t("workflowConfirmation.returnToConversation")}
                title={t("workflowConfirmation.returnToConversation")}
                className="inline-flex h-11 w-11 items-center justify-center rounded-md border border-zinc-200 text-zinc-500 hover:border-zinc-400 hover:text-zinc-950 disabled:opacity-40 dark:border-slate-700 dark:text-slate-400 dark:hover:border-slate-500 dark:hover:text-white"
              >
                <X size={18} />
              </button>
            ) : null}
            <button
              type="button"
              onClick={() => onConfirm(revision)}
              disabled={busy || unresolved || !actionAllowed}
              className="inline-flex h-11 shrink-0 items-center gap-2 rounded-md bg-blue-600 px-4 text-sm font-semibold text-white hover:bg-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-45 dark:bg-cyan-400 dark:text-[#061018] dark:hover:bg-cyan-300"
            >
              {busy ? <Loader2 size={16} className="animate-spin" /> : retrying ? <RefreshCw size={16} /> : <Check size={16} />}
              {busy
                ? t("workflowConfirmation.building")
                : retrying
                  ? t("workflowConfirmation.retryBuild")
                  : t("workflowConfirmation.confirmAndBuild")}
            </button>
          </div>
        </div>
        <div className="mt-4 grid grid-cols-2 gap-px overflow-hidden rounded-md border border-zinc-200 bg-zinc-200 dark:border-slate-700 dark:bg-slate-700 sm:grid-cols-4">
          <SummaryMetric label={t("workflowConfirmation.metric.imageTypes")} value={payload.image_types.length} />
          <SummaryMetric label={t("workflowConfirmation.metric.images")} value={review.currentImageCount} />
          <SummaryMetric label={t("workflowConfirmation.metric.references")} value={payload.reference_bindings.length} />
          <SummaryMetric label={t("workflowConfirmation.metric.nodes")} value={payload.nodes.length} />
        </div>
        {conflictDetected ? (
          <InlineAlert>{t("workflowConfirmation.versionChanged")}</InlineAlert>
        ) : error ? (
          <InlineAlert>{error}</InlineAlert>
        ) : unresolved ? (
          <InlineAlert>{t("workflowConfirmation.unresolved")}</InlineAlert>
        ) : null}
      </header>

      <ConfirmationSection icon={<ImageIcon size={17} />} title={t("workflowConfirmation.section.imagePlan")}>
        <div className="mb-3 flex flex-wrap gap-2 text-xs text-zinc-500 dark:text-slate-400">
          <span>{t("workflowConfirmation.initialImages", { count: review.initialImageCount })}</span>
          <span aria-hidden="true">/</span>
          <span>{t("workflowConfirmation.currentImages", { count: review.currentImageCount })}</span>
          <span aria-hidden="true">/</span>
          <span>{t("workflowConfirmation.plannedImages", { count: review.plannedImageCount })}</span>
        </div>
        <div className="overflow-x-auto border border-zinc-200 dark:border-slate-800">
          <table className="w-full min-w-[620px] border-collapse text-left text-sm">
            <thead className="bg-zinc-50 text-xs text-zinc-500 dark:bg-[#0d131c] dark:text-slate-400">
              <tr>
                <th className="px-3 py-2.5 font-medium">{t("workflowConfirmation.imageType")}</th>
                <th className="px-3 py-2.5 font-medium">{t("workflowConfirmation.previous")}</th>
                <th className="px-3 py-2.5 font-medium">{t("workflowConfirmation.current")}</th>
                <th className="px-3 py-2.5 font-medium">{t("workflowConfirmation.changes")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-100 dark:divide-slate-800">
              {review.imageTypes.map((row) => (
                <tr key={row.key} className={row.changes.length ? "bg-amber-50/45 dark:bg-amber-400/5" : ""}>
                  <td className="px-3 py-3">
                    <div className="font-medium text-zinc-950 dark:text-white">{imageTypeTitle(row.key, row.title, t)}</div>
                    <div className="mt-0.5 font-mono text-[10px] text-zinc-400 dark:text-slate-500">{row.key}</div>
                  </td>
                  <td className="px-3 py-3 tabular-nums text-zinc-500 dark:text-slate-400">
                    {row.initialQuantity === null
                      ? t("workflowConfirmation.none")
                      : t("workflowConfirmation.quantityAndOrder", {
                          quantity: row.initialQuantity,
                          order: (row.initialOrder ?? 0) + 1,
                        })}
                  </td>
                  <td className="px-3 py-3 tabular-nums font-medium text-zinc-800 dark:text-slate-200">
                    {row.currentQuantity === null
                      ? t("workflowConfirmation.none")
                      : t("workflowConfirmation.quantityAndOrder", {
                          quantity: row.currentQuantity,
                          order: (row.currentOrder ?? 0) + 1,
                        })}
                  </td>
                  <td className="px-3 py-3">
                    <div className="flex flex-wrap gap-1.5">
                      {row.changes.length
                        ? row.changes.map((change) => (
                            <span key={change} className="rounded bg-amber-100 px-2 py-1 text-[11px] font-semibold text-amber-800 dark:bg-amber-400/15 dark:text-amber-200">
                              {t(changeKeys[change])}
                            </span>
                          ))
                        : <span className="text-xs text-zinc-400 dark:text-slate-500">{t("workflowConfirmation.unchanged")}</span>}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </ConfirmationSection>

      <ConfirmationSection icon={<ScanSearch size={17} />} title={t("workflowConfirmation.section.facts")}>
        {(payload.required_fact_keys.length || payload.missing_fact_keys.length) ? (
          <div className="mb-4 grid gap-3 text-xs sm:grid-cols-2">
            <KeyList label={t("workflowConfirmation.requiredFacts")} values={payload.required_fact_keys} />
            <KeyList label={t("workflowConfirmation.missingFacts")} values={payload.missing_fact_keys} warning={payload.missing_fact_keys.length > 0} />
          </div>
        ) : null}
        <div className="divide-y divide-zinc-100 border-y border-zinc-200 dark:divide-slate-800 dark:border-slate-800">
          {payload.facts.map((fact) => (
            <div key={fact.key} className="grid gap-2 py-3 sm:grid-cols-[minmax(120px,0.35fr)_minmax(0,1fr)] sm:gap-5">
              <div>
                <div className="font-mono text-xs font-semibold text-zinc-800 dark:text-slate-200">{fact.key}</div>
                <div className="mt-1 flex flex-wrap gap-1.5">
                  <span className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${fact.status === "conflicted" ? "bg-red-100 text-red-700 dark:bg-red-400/15 dark:text-red-200" : "bg-zinc-100 text-zinc-600 dark:bg-slate-800 dark:text-slate-300"}`}>
                    {t(factStatusKeys[fact.status])}
                  </span>
                  <span className="rounded bg-blue-50 px-1.5 py-0.5 text-[10px] font-semibold text-blue-700 dark:bg-cyan-400/10 dark:text-cyan-200">
                    {t(factSourceKeys[fact.source_type])}
                  </span>
                </div>
              </div>
              <div className="min-w-0">
                <div className="break-words text-sm leading-6 text-zinc-900 dark:text-slate-100">{formatWorkflowDraftValue(fact.value)}</div>
                {fact.requires_confirmation ? <div className="mt-1 text-xs text-amber-700 dark:text-amber-300">{t("workflowConfirmation.requiresConfirmation")}</div> : null}
                {fact.evidence_asset_ids.length ? <div className="mt-1 break-all text-[11px] text-zinc-400 dark:text-slate-500">{t("workflowConfirmation.evidence")}: {fact.evidence_asset_ids.join(", ")}</div> : null}
                {fact.conflicts.map((conflict, index) => (
                  <div key={`${fact.key}:${index}`} className="mt-2 border-l-2 border-red-300 pl-2 text-xs text-red-700 dark:border-red-400/50 dark:text-red-200">
                    {formatWorkflowDraftValue(conflict.value)} · {t(factSourceKeys[conflict.source_type])}
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </ConfirmationSection>

      <ConfirmationSection icon={<Palette size={17} />} title={t("workflowConfirmation.section.visual")}>
        {payload.visual_system.mode === "confirmed_version" ? (
          <Definition label={t("workflowConfirmation.visualVersion")} value={payload.visual_system.version_id} mono />
        ) : (
          <div className="space-y-5">
            <div>
              <div className="text-sm font-semibold text-zinc-950 dark:text-white">{payload.visual_system.payload.name}</div>
              <TagList values={payload.visual_system.payload.style} />
            </div>
            <div>
              <FieldLabel>{t("workflowConfirmation.colors")}</FieldLabel>
              <div className="mt-2 flex flex-wrap gap-2">
                {payload.visual_system.payload.colors.map((color) => (
                  <span key={color.role} className="inline-flex min-h-9 items-center gap-2 border border-zinc-200 px-2.5 text-xs dark:border-slate-700">
                    <span className="h-4 w-4 shrink-0 border border-black/10" style={{ backgroundColor: color.value }} />
                    <span>{color.label}</span>
                    <span className="font-mono text-zinc-400 dark:text-slate-500">{color.value}</span>
                  </span>
                ))}
              </div>
            </div>
            <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2 xl:grid-cols-3">
              <Definition label={t("workflowConfirmation.typography")} value={`${payload.visual_system.payload.typography.title_font} / ${payload.visual_system.payload.typography.body_font} · ${payload.visual_system.payload.typography.scale.headline}:${payload.visual_system.payload.typography.scale.subtitle}:${payload.visual_system.payload.typography.scale.body}`} />
              <Definition label={t("workflowConfirmation.whitespace")} value={`${payload.visual_system.payload.spacing.min_edge_whitespace_percent}% · ${payload.visual_system.payload.spacing.principles.join(" / ")}`} />
              <Definition label={t("workflowConfirmation.decorations")} value={`${payload.visual_system.payload.decorations.elements.join(" / ")} · ${payload.visual_system.payload.decorations.icon_style}`} />
              <Definition label={t("workflowConfirmation.photography")} value={`${payload.visual_system.payload.photography.lighting} · ${payload.visual_system.payload.photography.depth_of_field} · ${payload.visual_system.payload.photography.camera_parameters.join(" / ")}`} />
              <Definition label={t("workflowConfirmation.quality")} value={`${payload.visual_system.payload.quality.resolution} · ${payload.visual_system.payload.quality.commercial_grade} · ${payload.visual_system.payload.quality.realism} · ${payload.visual_system.payload.quality.minimum_quality}`} />
              <Definition label={t("workflowConfirmation.fidelity")} value={`${t("workflowConfirmation.fidelityShape")} ${booleanMark(payload.visual_system.payload.product_fidelity.preserve_shape)} · ${t("workflowConfirmation.fidelityProportions")} ${booleanMark(payload.visual_system.payload.product_fidelity.preserve_proportions)} · ${t("workflowConfirmation.fidelityMaterials")} ${booleanMark(payload.visual_system.payload.product_fidelity.preserve_materials)} · ${payload.visual_system.payload.product_fidelity.requirements.join(" / ")}`} />
            </div>
            <Definition label={t("workflowConfirmation.lockedFields")} value={payload.visual_system.payload.locked_fields.join(", ")} mono />
            <Definition label={t("workflowConfirmation.prohibitions")} value={payload.visual_system.payload.prohibitions.join(" / ") || t("workflowConfirmation.none")} />
            <Definition label={t("workflowConfirmation.visualReferences")} value={payload.visual_system.payload.reference_assets.map((reference) => `${reference.label} (${reference.role}: ${reference.asset_id})`).join(" / ") || t("workflowConfirmation.none")} mono />
            {payload.visual_system.payload.variants.length ? (
              <div>
                <FieldLabel>{t("workflowConfirmation.variants")}</FieldLabel>
                <div className="mt-2 divide-y divide-zinc-100 border-y border-zinc-200 text-sm dark:divide-slate-800 dark:border-slate-800">
                  {payload.visual_system.payload.variants.map((variant) => (
                    <div key={variant.key} className="py-2.5"><span className="font-medium">{variant.title}</span><span className="ml-2 text-zinc-500 dark:text-slate-400">{variant.guidance.join(" / ")}</span></div>
                  ))}
                </div>
              </div>
            ) : null}
          </div>
        )}
        {payload.visual_exceptions.length ? (
          <div className="mt-5">
            <FieldLabel>{t("workflowConfirmation.visualExceptions")}</FieldLabel>
            <div className="mt-2 divide-y divide-zinc-100 border-y border-zinc-200 dark:divide-slate-800 dark:border-slate-800">
              {payload.visual_exceptions.map((exception) => (
                <div key={exception.key} className="py-2.5 text-sm">
                  <span className="font-medium text-zinc-900 dark:text-white">{exception.reason}</span>
                  <span className="ml-2 font-mono text-xs text-zinc-400 dark:text-slate-500">{exception.scope.type}{exception.scope.key ? `:${exception.scope.key}` : ""} · {exception.overrides.map((override) => override.field).join(", ")}</span>
                </div>
              ))}
            </div>
          </div>
        ) : null}
      </ConfirmationSection>

      <ConfirmationSection icon={<ImageIcon size={17} />} title={t("workflowConfirmation.section.references")}>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {payload.reference_bindings.map((reference) => (
            <div key={reference.key} className="grid grid-cols-[64px_minmax(0,1fr)] gap-3 border-b border-zinc-200 pb-3 dark:border-slate-800">
              <img
                src={api.getProductImageAssetMediaUrl(reference.asset_id, "thumbnail")}
                alt={reference.label}
                className="h-16 w-16 border border-zinc-200 object-cover dark:border-slate-700"
                loading="lazy"
              />
              <div className="min-w-0 py-0.5">
                <div className="truncate text-sm font-medium text-zinc-950 dark:text-white">{reference.label}</div>
                <div className="mt-1 text-xs text-zinc-500 dark:text-slate-400">{reference.role}</div>
                <div className="mt-1 truncate font-mono text-[10px] text-zinc-400 dark:text-slate-500">{reference.asset_id}</div>
              </div>
            </div>
          ))}
        </div>
        {(review.addedReferenceAssetIds.length || review.removedReferenceAssetIds.length) ? (
          <div className="mt-4 grid gap-3 text-xs sm:grid-cols-2">
            <KeyList label={t("workflowConfirmation.addedReferences")} values={review.addedReferenceAssetIds} />
            <KeyList label={t("workflowConfirmation.removedReferences")} values={review.removedReferenceAssetIds} warning={review.removedReferenceAssetIds.length > 0} />
          </div>
        ) : null}
      </ConfirmationSection>

      <ConfirmationSection icon={<Braces size={17} />} title={t("workflowConfirmation.section.prompts")}>
        <div className="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-slate-800 dark:border-slate-800">
          {payload.prompt_plans.map((plan) => (
            <details key={plan.key} className="group" open={payload.prompt_plans.length === 1}>
              <summary className="flex min-h-12 cursor-pointer list-none items-center justify-between gap-3 py-3 marker:hidden [&::-webkit-details-marker]:hidden">
                <span><span className="text-sm font-semibold text-zinc-950 dark:text-white">{plan.title}</span><span className="ml-2 font-mono text-[10px] text-zinc-400 dark:text-slate-500">{plan.image_type_key}</span></span>
                <span className="text-xs text-zinc-400 group-open:text-blue-600 dark:text-slate-500 dark:group-open:text-cyan-300">{t("workflowConfirmation.promptDetails")}</span>
              </summary>
              <div className="grid gap-x-6 gap-y-4 pb-5 lg:grid-cols-2">
                <Definition label={t("workflowConfirmation.designGoal")} value={plan.payload.design_goal} />
                <Definition label={t("workflowConfirmation.sharedRules")} value={plan.payload.shared_rules.join(" / ")} />
                <Definition label={t("workflowConfirmation.creativeBoundary")} value={plan.payload.creative_boundary.join(" / ")} />
                <Definition label={t("workflowConfirmation.productFidelity")} value={`${plan.payload.product_fidelity.complex_structure ? "complex" : "simple"} · ${plan.payload.product_fidelity.product_present ? "product" : "no-product"} · ${plan.payload.product_fidelity.picture_in_picture} · ${plan.payload.product_fidelity.requirements.join(" / ")}`} />
                <Definition label={t("workflowConfirmation.composition")} value={`${plan.payload.composition.viewpoint} · ${plan.payload.composition.product_share_percent}% · ${plan.payload.composition.layout} · ${plan.payload.composition.copy_regions.join(" / ")}`} />
                <Definition label={t("workflowConfirmation.content")} value={`${plan.payload.content.focus.join(" / ")} · ${plan.payload.content.selling_points.join(" / ")} · ${plan.payload.content.background} · ${plan.payload.content.decorations.join(" / ")}`} />
                <Definition label={t("workflowConfirmation.textContent")} value={[plan.payload.text.headline, plan.payload.text.subtitle, plan.payload.text.body].filter(Boolean).join(" / ") || t("workflowConfirmation.none")} />
                <Definition label={t("workflowConfirmation.atmosphere")} value={`${plan.payload.atmosphere.keywords.join(" / ")} · ${plan.payload.atmosphere.lighting}`} />
                <Definition label={t("workflowConfirmation.visualVariant")} value={plan.payload.visual_variant_key || t("workflowConfirmation.none")} mono />
                <Definition label={t("workflowConfirmation.promptFacts")} value={plan.payload.fact_keys.join(", ") || t("workflowConfirmation.none")} mono />
                <Definition label={t("workflowConfirmation.evidence")} value={plan.payload.evidence_asset_ids.join(", ") || t("workflowConfirmation.none")} mono />
                <div className="lg:col-span-2">
                  <FieldLabel>{t("workflowConfirmation.perImageInstructions")}</FieldLabel>
                  <div className="mt-2 divide-y divide-zinc-100 border-y border-zinc-200 dark:divide-slate-800 dark:border-slate-800">
                    {plan.payload.images.map((image) => (
                      <div key={image.image_plan_key} className="py-2.5 text-sm">
                        <span className="font-mono text-xs font-semibold text-zinc-800 dark:text-slate-200">{image.image_plan_key}</span>
                        <span className="ml-3 text-zinc-600 dark:text-slate-300">{image.instruction}</span>
                        {[image.viewpoint, image.lighting, ...image.composition_adjustments].filter(Boolean).length ? <div className="mt-1 text-xs text-zinc-400 dark:text-slate-500">{[image.viewpoint, image.lighting, ...image.composition_adjustments].filter(Boolean).join(" / ")}</div> : null}
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </details>
          ))}
        </div>
      </ConfirmationSection>

      <ConfirmationSection icon={<Languages size={17} />} title={t("workflowConfirmation.section.specs")}>
        <div className="mb-3 text-xs text-zinc-500 dark:text-slate-400">
          {t("workflowConfirmation.textLanguages")}: {review.textLanguages.join(", ") || t("workflowConfirmation.none")}
        </div>
        <div className="overflow-x-auto border border-zinc-200 dark:border-slate-800">
          <table className="w-full min-w-[820px] border-collapse text-left text-xs">
            <thead className="bg-zinc-50 text-zinc-500 dark:bg-[#0d131c] dark:text-slate-400">
              <tr>
                <th className="px-3 py-2.5 font-medium">{t("workflowConfirmation.imagePlan")}</th>
                <th className="px-3 py-2.5 font-medium">{t("workflowConfirmation.generationSpec")}</th>
                <th className="px-3 py-2.5 font-medium">{t("workflowConfirmation.deliverySpec")}</th>
                <th className="px-3 py-2.5 font-medium">{t("workflowConfirmation.variation")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-100 dark:divide-slate-800">
              {payload.image_types.flatMap((imageType) => imageType.images.map((image) => (
                <tr key={image.key}>
                  <td className="px-3 py-3"><div className="font-medium text-zinc-900 dark:text-slate-100">{imageType.title}</div><div className="mt-0.5 font-mono text-[10px] text-zinc-400">{image.key}</div></td>
                  <td className="px-3 py-3 leading-5 text-zinc-600 dark:text-slate-300">{generationSpecLabel(image.generation_spec)}</td>
                  <td className="px-3 py-3 leading-5 text-zinc-600 dark:text-slate-300">{image.delivery_spec ? deliverySpecLabel(image.delivery_spec) : t("workflowConfirmation.none")}</td>
                  <td className="px-3 py-3 leading-5 text-zinc-500 dark:text-slate-400">{image.variation_instruction || t("workflowConfirmation.none")}</td>
                </tr>
              ))) }
            </tbody>
          </table>
        </div>
      </ConfirmationSection>

      <ConfirmationSection icon={<GitBranch size={17} />} title={t("workflowConfirmation.section.topology")} last>
        <div className="grid gap-3 sm:grid-cols-3">
          <SummaryMetric label={t("workflowConfirmation.folders")} value={payload.folders.length} bordered />
          <SummaryMetric label={t("workflowConfirmation.nodes")} value={payload.nodes.length} bordered />
          <SummaryMetric label={t("workflowConfirmation.edges")} value={payload.edges.length} bordered />
        </div>
        <div className="mt-4 flex flex-wrap gap-2 text-xs">
          {nodeTypeMetrics(review.nodeTypeCounts, t).map(({ key, label, count }) => (
            <span key={key} className="rounded bg-zinc-100 px-2 py-1.5 text-zinc-600 dark:bg-slate-800 dark:text-slate-300">{label}: <span className="font-mono">{count}</span></span>
          ))}
        </div>
        <details className="mt-4 border-y border-zinc-200 dark:border-slate-800">
          <summary className="cursor-pointer py-3 text-sm font-medium text-zinc-800 dark:text-slate-200">{t("workflowConfirmation.topologyDetails")}</summary>
          <div className="grid gap-5 pb-4 lg:grid-cols-2 xl:grid-cols-3">
            <KeyList label={t("workflowConfirmation.folders")} values={payload.folders.slice().sort((left, right) => left.order - right.order).map((folder) => `${folder.key}: ${folder.title}`)} />
            <KeyList label={t("workflowConfirmation.nodes")} values={payload.nodes.map((node) => `${node.key}: ${node.node_type} / ${node.title}`)} />
            <KeyList label={t("workflowConfirmation.edges")} values={payload.edges.map((edge) => `${edge.source_node_key} -> ${edge.target_node_key}`)} />
          </div>
        </details>
      </ConfirmationSection>
    </section>
  );
}

function ConfirmationSection({ icon, title, children, last = false }: { icon: ReactNode; title: string; children: ReactNode; last?: boolean }) {
  return (
    <section className={`px-4 py-6 sm:px-6 ${last ? "" : "border-b border-zinc-200 dark:border-slate-800"}`}>
      <h3 className="mb-4 flex items-center gap-2 text-sm font-semibold text-zinc-950 dark:text-white">{icon}{title}</h3>
      {children}
    </section>
  );
}

function SummaryMetric({ label, value, bordered = false }: { label: string; value: number; bordered?: boolean }) {
  return (
    <div className={`min-w-0 bg-white px-3 py-2.5 dark:bg-[#0d131c] ${bordered ? "border border-zinc-200 dark:border-slate-800" : ""}`}>
      <div className="text-[10px] font-medium uppercase text-zinc-400 dark:text-slate-500">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-zinc-950 dark:text-white">{value}</div>
    </div>
  );
}

function InlineAlert({ children }: { children: ReactNode }) {
  return <div role="alert" className="mt-3 flex items-start gap-2 border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-400/25 dark:bg-amber-400/10 dark:text-amber-100"><CircleAlert size={14} className="mt-0.5 shrink-0" />{children}</div>;
}

function FieldLabel({ children }: { children: ReactNode }) {
  return <div className="text-[11px] font-semibold uppercase text-zinc-400 dark:text-slate-500">{children}</div>;
}

function Definition({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="min-w-0"><FieldLabel>{label}</FieldLabel><div className={`mt-1 break-words text-sm leading-6 text-zinc-700 dark:text-slate-300 ${mono ? "font-mono text-xs" : ""}`}>{value}</div></div>;
}

function KeyList({ label, values, warning = false }: { label: string; values: string[]; warning?: boolean }) {
  const { t } = useI18n();
  return <div className={`border-l-2 pl-3 ${warning ? "border-red-300 dark:border-red-400/50" : "border-zinc-200 dark:border-slate-700"}`}><FieldLabel>{label}</FieldLabel><div className={`mt-1 break-all font-mono text-xs leading-5 ${warning ? "text-red-700 dark:text-red-200" : "text-zinc-600 dark:text-slate-300"}`}>{values.join(", ") || t("workflowConfirmation.none")}</div></div>;
}

function TagList({ values }: { values: string[] }) {
  return <div className="mt-2 flex flex-wrap gap-1.5">{values.map((value) => <span key={value} className="rounded bg-blue-50 px-2 py-1 text-xs font-medium text-blue-700 dark:bg-cyan-400/10 dark:text-cyan-200">{value}</span>)}</div>;
}

function imageTypeTitle(key: string, fallback: string, t: ReturnType<typeof useI18n>["t"]): string {
  if (!isAgentProductImageTypeKey(key)) return fallback;
  return t(AGENT_IMAGE_TYPE_TRANSLATIONS[key].title);
}

function booleanMark(value: boolean): string {
  return value ? "✓" : "×";
}

function generationSpecLabel(spec: WorkflowGenerationSpec): string {
  return [
    spec.aspect_ratio,
    spec.resolution_tier,
    spec.quality_intent,
    `reference:${spec.reference_fidelity}`,
    `background:${spec.background_intent}`,
    `text:${spec.text_policy}${spec.text_language ? `/${spec.text_language}` : ""}`,
  ].join(" · ");
}

function formatBytes(bytes: number): string {
  return bytes >= 1024 * 1024
    ? `${Math.round(bytes / (1024 * 1024) * 10) / 10} MiB`
    : `${Math.round(bytes / 1024)} KiB`;
}

function deliverySpecLabel(spec: WorkflowDeliverySpec): string {
  return [
    `${spec.width}x${spec.height}`,
    spec.format.toUpperCase(),
    spec.fit,
    spec.max_byte_size ? formatBytes(spec.max_byte_size) : null,
    spec.background_color ? `background:${spec.background_color}` : null,
    spec.crop_anchor ? `crop:${spec.crop_anchor}` : null,
  ].filter(Boolean).join(" · ");
}

function nodeTypeMetrics(
  counts: ReturnType<typeof deriveWorkflowDraftReview>["nodeTypeCounts"],
  t: ReturnType<typeof useI18n>["t"],
) {
  return [
    { key: "product_context", label: t("workflowV2.node.productContext"), count: counts.product_context },
    { key: "reference_image", label: t("workflowV2.node.referenceImage"), count: counts.reference_image },
    { key: "prompt_generation", label: t("workflowV2.node.promptGeneration"), count: counts.prompt_generation },
    { key: "image_generation", label: t("workflowV2.node.imageGeneration"), count: counts.image_generation },
  ];
}
