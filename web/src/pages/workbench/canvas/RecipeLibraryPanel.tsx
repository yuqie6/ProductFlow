import {
  Archive,
  Boxes,
  FileStack,
  Images,
  Loader2,
  Play,
  RefreshCw,
} from "lucide-react";

import { useI18n } from "../../../lib/preferences";
import type {
  WorkflowRecipeApplicationResult,
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
  onApply: (recipe: WorkflowRecipeSummary) => void;
  onAppend: (recipe: WorkflowRecipeSummary) => void;
  onArchive: (recipe: WorkflowRecipeSummary) => void;
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
  onApply,
  onAppend,
  onArchive,
}: RecipeLibraryPanelProps) {
  const { t } = useI18n();

  if (loading) {
    return <PanelState icon={<Loader2 size={18} className="animate-spin" />} text={t("workbench.recipe.loading")} />;
  }
  if (error) {
    return <PanelState text={error} action={t("workbench.retry")} onAction={onRetry} />;
  }

  return (
    <div className="space-y-3 p-3">
      {application ? (
        <section className="rounded-lg border border-emerald-200 bg-emerald-50 p-3 dark:border-emerald-400/30 dark:!bg-emerald-500/10" aria-live="polite">
          <div className="flex items-start gap-2">
            <span className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-emerald-600 text-white"><FileStack size={14} /></span>
            <div className="min-w-0">
              <div className="text-xs font-semibold text-emerald-900 dark:text-emerald-100">{t("workbench.recipe.applied")}</div>
              <div className="mt-1 text-[11px] leading-4 text-emerald-700 dark:text-emerald-200">
                {t("workbench.recipe.appliedDetail", { version: application.draft.current_version })}
              </div>
            </div>
          </div>
        </section>
      ) : null}

      {recipes.length === 0 ? (
        <PanelState icon={<Boxes size={19} />} text={t("workbench.recipe.empty")} />
      ) : recipes.map((recipe) => {
        const version = recipe.current_version;
        const payload = version.payload;
        const busy = structureBusy || operationRecipeId === recipe.id;
        const appendable = canAppend(recipe);
        return (
          <article key={recipe.id} className="rounded-lg border border-slate-200 bg-white p-3 shadow-sm dark:border-slate-700 dark:!bg-[#11151d]">
            <div className="flex items-start gap-2.5">
              <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md ${recipe.kind === "workflow_recipe" ? "bg-blue-50 text-blue-700 dark:bg-blue-500/15 dark:text-blue-200" : "bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-200"}`}>
                {recipe.kind === "workflow_recipe" ? <Boxes size={16} /> : <FileStack size={16} />}
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 items-center gap-2">
                  <h3 className="min-w-0 flex-1 truncate text-sm font-semibold text-slate-950 dark:text-slate-100" title={version.title}>{version.title}</h3>
                  <span className="shrink-0 rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-semibold text-slate-500 dark:bg-slate-800 dark:text-slate-300">v{version.version}</span>
                </div>
                <p className="mt-1 line-clamp-2 text-[11px] leading-4 text-slate-500 dark:text-slate-400">
                  {version.description || t("workbench.recipe.noDescription")}
                </p>
              </div>
            </div>

            <div className="mt-3 flex flex-wrap gap-x-3 gap-y-1 border-y border-slate-100 py-2 text-[10px] font-medium text-slate-500 dark:border-slate-800 dark:text-slate-400">
              <span className="inline-flex items-center gap-1"><Boxes size={11} /> {t("workbench.recipe.nodeCount", { count: payload.nodes.length })}</span>
              <span className="inline-flex items-center gap-1"><Images size={11} /> {t("workbench.recipe.imageTypeCount", { count: payload.image_types.length })}</span>
              <span>{recipe.kind === "workflow_recipe" ? t("workbench.recipe.fullKind") : t("workbench.recipe.fragmentKind")}</span>
            </div>

            <div className="mt-3 grid grid-cols-[minmax(0,1fr)_36px_36px] gap-1.5">
              <button
                type="button"
                onClick={() => onApply(recipe)}
                disabled={busy}
                className="inline-flex h-9 items-center justify-center rounded-md bg-slate-950 px-3 text-xs font-semibold text-white hover:bg-indigo-700 disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400"
              >
                {busy ? <Loader2 size={13} className="mr-1.5 animate-spin" /> : <Play size={13} className="mr-1.5" fill="currentColor" />}
                {t("workbench.recipe.apply")}
              </button>
              <button type="button" onClick={() => onAppend(recipe)} disabled={busy || !appendable} className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-slate-200 text-slate-600 hover:border-indigo-300 hover:text-indigo-700 disabled:opacity-35 dark:border-slate-700 dark:text-slate-300 dark:hover:border-violet-400 dark:hover:text-violet-200" aria-label={t("workbench.recipe.append")} title={t("workbench.recipe.append")}>
                <RefreshCw size={14} />
              </button>
              <button type="button" onClick={() => onArchive(recipe)} disabled={busy} className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-slate-200 text-slate-600 hover:border-red-300 hover:text-red-600 disabled:opacity-35 dark:border-slate-700 dark:text-slate-300 dark:hover:border-red-400 dark:hover:text-red-300" aria-label={t("workbench.recipe.archive")} title={t("workbench.recipe.archive")}>
                <Archive size={14} />
              </button>
            </div>
          </article>
        );
      })}
    </div>
  );
}

function PanelState({ icon, text, action, onAction }: { icon?: React.ReactNode; text: string; action?: string; onAction?: () => void }) {
  return (
    <div className="flex min-h-[220px] flex-col items-center justify-center gap-2 p-6 text-center text-xs text-slate-500 dark:text-slate-400">
      {icon ? <span className="text-slate-400 dark:text-slate-500">{icon}</span> : null}
      <span className="max-w-[250px] leading-5">{text}</span>
      {action && onAction ? <button type="button" onClick={onAction} className="mt-1 font-semibold text-indigo-700 hover:underline dark:text-violet-300">{action}</button> : null}
    </div>
  );
}
