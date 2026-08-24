import { useState } from "react";
import {
  Archive,
  FileStack,
  Image as ImageIcon,
  Link2,
  Loader2,
  PencilLine,
  Play,
} from "lucide-react";

import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { useI18n } from "../../../lib/preferences";
import type {
  WorkflowRecipeApplicationResult,
  WorkflowRecipePreview,
  WorkflowRecipeSummary,
} from "../../../lib/types";

interface RecipeLibraryPanelProps {
  recipes: WorkflowRecipeSummary[];
  loading: boolean;
  error: string | null;
  operationRecipeId: string | null;
  application: WorkflowRecipeApplicationResult | null;
  structureBusy?: boolean;
  canAppend: (recipe: WorkflowRecipeSummary) => boolean;
  onRetry: () => void;
  onPreview: (recipe: WorkflowRecipeSummary) => Promise<WorkflowRecipePreview>;
  onApply: (recipe: WorkflowRecipeSummary, preview: WorkflowRecipePreview) => void;
  onAppend: (recipe: WorkflowRecipeSummary) => void;
  onArchive: (recipe: WorkflowRecipeSummary) => void;
}

export function userSavedRecipes(recipes: WorkflowRecipeSummary[]): WorkflowRecipeSummary[] {
  return recipes.filter((recipe) => recipe.origin !== "official");
}

export function RecipeLibraryPanel({
  recipes,
  loading,
  error,
  operationRecipeId,
  application,
  structureBusy = false,
  canAppend,
  onRetry,
  onPreview,
  onApply,
  onAppend,
  onArchive,
}: RecipeLibraryPanelProps) {
  const { t } = useI18n();
  const [previewRecipe, setPreviewRecipe] = useState<WorkflowRecipeSummary | null>(null);
  const [previewResult, setPreviewResult] = useState<WorkflowRecipePreview | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);

  const visibleRecipes = userSavedRecipes(recipes);
  const previewBusy = Boolean(
    previewRecipe && (previewLoading || structureBusy || operationRecipeId === previewRecipe.id),
  );

  const openPreview = async (recipe: WorkflowRecipeSummary) => {
    setPreviewRecipe(recipe);
    setPreviewResult(null);
    setPreviewError(null);
    setPreviewLoading(true);
    try {
      setPreviewResult(await onPreview(recipe));
    } catch (error) {
      setPreviewError(error instanceof Error ? error.message : String(error));
    } finally {
      setPreviewLoading(false);
    }
  };

  return (
    <div className="space-y-3 p-3" data-graph-recipe-panel>
      {application ? (
        <section className="rounded-lg border border-emerald-200 bg-emerald-50 p-3 dark:border-emerald-400/30 dark:!bg-emerald-500/10" aria-live="polite">
          <div className="flex items-start gap-2">
            <span className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-emerald-600 text-white"><FileStack size={14} /></span>
            <div className="min-w-0">
              <div className="text-xs font-semibold text-emerald-900 dark:text-emerald-100">{t("workbench.recipe.applied")}</div>
              <div className="mt-1 text-[11px] leading-4 text-emerald-700 dark:text-emerald-200">
                {t("workbench.recipe.appliedDetail")}
              </div>
            </div>
          </div>
        </section>
      ) : null}

      {loading ? (
        <PanelState icon={<Loader2 size={18} className="animate-spin" />} text={t("workbench.recipe.loading")} />
      ) : error ? (
        <PanelState text={error} action={t("workbench.retry")} onAction={onRetry} />
      ) : visibleRecipes.length === 0 ? (
        <PanelState icon={<FileStack size={19} />} text={t("workbench.recipe.empty")} />
      ) : (
        visibleRecipes.map((recipe) => {
          const version = recipe.current_version;
          const payload = version.payload;
          const busy = structureBusy || operationRecipeId === recipe.id;
          const appendable = canAppend(recipe);
          return (
            <article key={recipe.id} className="rounded-lg border border-border-l1 bg-surface-raised p-3 shadow-sm" data-recipe-origin={recipe.origin}>
              <div className="flex items-start gap-2.5">
                <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-200">
                  <FileStack size={16} />
                </span>
                <div className="min-w-0 flex-1">
                  <h3 className="min-w-0 truncate text-sm font-semibold text-text-primary" title={version.title}>{version.title}</h3>
                  <p className="mt-1 line-clamp-2 text-[11px] leading-4 text-text-secondary">
                    {version.description || t("workbench.recipe.noDescription")}
                  </p>
                </div>
              </div>

              <div className="mt-3 flex flex-wrap gap-x-3 gap-y-1 border-y border-border-l1 py-2 text-[10px] font-medium text-text-secondary">
                <span>{t("workbench.recipe.nodeCount", { count: payload.nodes.length })}</span>
                <span>{t("workbench.recipe.edgeCount", { count: payload.edges.length })}</span>
                <span>{t("workbench.recipe.groupCount", { count: payload.groups.length })}</span>
                <span>{t("workbench.recipe.version", { version: version.version })}</span>
              </div>

              <div className="mt-3 grid grid-cols-[minmax(0,1fr)_36px_36px] gap-1.5">
                <button
                  type="button"
                  onClick={() => void openPreview(recipe)}
                  disabled={busy}
                  title={t("workbench.recipe.apply")}
                  className="inline-flex h-9 items-center justify-center rounded-md bg-slate-950 px-3 text-xs font-semibold text-white hover:bg-indigo-700 disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400"
                >
                  {busy ? <Loader2 size={13} className="mr-1.5 animate-spin" /> : <Play size={13} className="mr-1.5" fill="currentColor" />}
                  {t("workbench.recipe.apply")}
                </button>
                <button
                  type="button"
                  onClick={() => onAppend(recipe)}
                  disabled={busy || !appendable}
                  className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-border-l1 text-text-secondary hover:border-accent hover:text-accent disabled:opacity-35"
                  aria-label={t("workbench.recipe.append")}
                  title={t("workbench.recipe.append")}
                >
                  <PencilLine size={14} />
                </button>
                <button
                  type="button"
                  onClick={() => onArchive(recipe)}
                  disabled={busy}
                  className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-border-l1 text-text-secondary hover:border-red-300 hover:text-red-600 disabled:opacity-35"
                  aria-label={t("workbench.recipe.archive")}
                  title={t("workbench.recipe.archive")}
                >
                  <Archive size={14} />
                </button>
              </div>
            </article>
          );
        })
      )}

      <ConfirmDialog
        open={Boolean(previewRecipe)}
        title={t("workbench.recipe.previewTitle")}
        description={previewError
          ? previewError
          : previewLoading
            ? t("workbench.recipe.previewLoading")
            : previewResult
              ? t("workbench.recipe.previewDetail", {
                nodes: previewResult.nodes.length,
                edges: previewResult.edges.length,
              })
              : t("workbench.recipe.previewLoading")}
        body={previewError || !previewResult ? null : <RecipeApplyPreviewBody preview={previewResult} />}
        confirmLabel={t("workbench.recipe.previewConfirm")}
        cancelLabel={t("common.cancel")}
        busy={previewBusy}
        confirmDisabled={Boolean(previewError) || !previewResult}
        destructive={false}
        onConfirm={() => {
          if (!previewRecipe || previewBusy || previewError || !previewResult) return;
          confirmRecipeApply(previewRecipe, previewResult, onApply);
          setPreviewRecipe(null);
          setPreviewResult(null);
        }}
        onClose={() => {
          if (previewBusy) return;
          setPreviewRecipe(null);
          setPreviewResult(null);
          setPreviewError(null);
        }}
      />
    </div>
  );
}

export function confirmRecipeApply(
  recipe: WorkflowRecipeSummary,
  preview: WorkflowRecipePreview,
  onApply: (recipe: WorkflowRecipeSummary, preview: WorkflowRecipePreview) => void,
): void {
  onApply(recipe, preview);
}

export function RecipeApplyPreviewBody({ preview }: { preview: WorkflowRecipePreview }) {
  const { t } = useI18n();
  const nodeTitle = new Map(preview.nodes.map((node) => [node.key, node.title]));
  const hasAdditions = Boolean(preview.nodes.length || preview.edges.length || preview.groups.length);
  return (
    <div data-recipe-preview data-recipe-preview-mode={preview.mode} className="space-y-3 text-left">
      <div className="text-xs font-semibold text-text-primary">
        {preview.mode === "merge" ? t("workbench.recipe.previewModeMerge") : t("workbench.recipe.previewModeCreate")}
      </div>

      <PreviewSection title={t("workbench.recipe.addedSection")} empty={!hasAdditions}>
        {preview.nodes.length ? (
          <ul data-recipe-preview-nodes className="max-h-32 space-y-1 overflow-y-auto text-xs text-text-primary">
            {preview.nodes.map((node) => <li key={node.key}>{node.title}</li>)}
          </ul>
        ) : null}
        {preview.edges.length ? (
          <ul data-recipe-preview-edges className="mt-1 max-h-24 space-y-1 overflow-y-auto text-[11px] text-text-secondary">
            {preview.edges.map((edge) => (
              <li key={edge.key}>
                {nodeTitle.get(edge.source_node_key) ?? edge.source_node_key}
                {" → "}
                {nodeTitle.get(edge.target_node_key) ?? edge.target_node_key}
              </li>
            ))}
          </ul>
        ) : null}
        {preview.groups.length ? (
          <ul data-recipe-preview-groups className="mt-1 space-y-1 text-[11px] text-text-secondary">
            {preview.groups.map((group) => <li key={group.key}>{group.title}</li>)}
          </ul>
        ) : null}
      </PreviewSection>

      <PreviewSection title={t("workbench.recipe.updatedSection")} empty={!preview.updated_nodes.length}>
        {preview.updated_nodes.length ? (
          <ul data-recipe-preview-updated className="space-y-1 text-xs text-text-primary">
            {preview.updated_nodes.map((node) => (
              <li key={node.id}>
                <span>{node.title}</span>
                <span className="ml-1 text-[10px] text-text-secondary">({node.changed_config_keys.join(", ") || t("workbench.recipe.noChangedKeys")})</span>
              </li>
            ))}
          </ul>
        ) : null}
      </PreviewSection>

      <PreviewSection title={t("workbench.recipe.requiredBindings")} empty={!preview.required_bindings.length}>
        {preview.required_bindings.length ? (
          <ul data-recipe-preview-bindings className="space-y-1 text-xs text-text-primary">
            {preview.required_bindings.map((binding) => <li key={binding} className="inline-flex items-center gap-1"><Link2 size={12} />{binding}</li>)}
          </ul>
        ) : null}
      </PreviewSection>

      <div className="text-[10px] text-text-secondary">{t("workbench.recipe.previewRevision", { revision: preview.base_graph_revision })}</div>
    </div>
  );
}

function PreviewSection({
  title,
  empty,
  children,
}: {
  title: string;
  empty: boolean;
  children: React.ReactNode;
}) {
  const { t } = useI18n();
  return (
    <section className="border-t border-border-l1 pt-2">
      <div className="mb-1 flex items-center gap-1 text-[11px] font-semibold text-text-primary">
        <ImageIcon size={12} />
        {title}
      </div>
      {empty ? <p className="text-[11px] text-text-secondary">{t("workbench.recipe.noChanges")}</p> : children}
    </section>
  );
}

function PanelState({
  icon,
  text,
  action,
  onAction,
}: {
  icon?: React.ReactNode;
  text: string;
  action?: string;
  onAction?: () => void;
}) {
  return (
    <div className="flex min-h-[220px] flex-col items-center justify-center gap-2 p-6 text-center text-xs text-text-secondary">
      {icon ? <span className="text-text-secondary">{icon}</span> : null}
      <span className="max-w-[250px] leading-5">{text}</span>
      {action && onAction ? <button type="button" onClick={onAction} className="mt-1 font-semibold text-accent hover:underline">{action}</button> : null}
    </div>
  );
}
