import {
  ArrowLeft,
  ChevronDown,
  FolderInput,
  FolderOpen,
  FolderPlus,
  Images,
  Library,
  Loader2,
  MoreHorizontal,
  PackageOpen,
  Pencil,
  Save,
  Trash2,
} from "lucide-react";
import { useCallback, useMemo, useState } from "react";

import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type {
  ProductDetail,
  ProductWorkflowV2,
  WorkflowNodeV2,
  WorkflowRecipeApplicationResult,
  WorkflowRecipeSourceType,
  WorkflowRecipeSummary,
} from "../../lib/types";
import { ProductImageExplorer } from "../product-detail/image-explorer/ProductImageExplorer";
import type { WorkflowCanvasStateV1, WorkflowCanvasViewport } from "./canvasState";
import { RecipeLibraryPanel } from "./RecipeLibraryPanel";
import { resolveRecipeVersionSource } from "./recipeSource";
import { V2WorkflowCanvas } from "./V2WorkflowCanvas";

export interface RecipeSourceSelection {
  source_type: WorkflowRecipeSourceType;
  folder_id?: string | null;
  node_ids?: string[];
  label: string;
}

interface V2WorkflowWorkbenchProps {
  product: ProductDetail;
  workflow: ProductWorkflowV2 | null;
  workflowLoading: boolean;
  workflowError: string | null;
  canvasState: WorkflowCanvasStateV1;
  selectedNodeIds: string[];
  structureBusy: boolean;
  runningNodeId: string | null;
  recipes: WorkflowRecipeSummary[];
  recipesLoading: boolean;
  recipesError: string | null;
  operationRecipeId: string | null;
  application: WorkflowRecipeApplicationResult | null;
  referenceNode: WorkflowNodeV2 | null;
  onRetryWorkflow: () => void;
  onRetryRecipes: () => void;
  onOpenFolder: (folderId: string | null) => void;
  onSelectionChange: (nodeIds: string[]) => void;
  onViewportChange: (viewport: WorkflowCanvasViewport) => void;
  onLayoutCommit: (positions: Array<{ node_id: string; position_x: number; position_y: number }>) => void;
  onFolderTranslate: (folderId: string, deltaX: number, deltaY: number) => void;
  onCreateFolder: () => void;
  onRenameFolder: () => void;
  onDissolveFolder: () => void;
  onMoveSelection: (folderId: string | null) => void;
  onSaveRecipe: (source: RecipeSourceSelection) => void;
  onRunNode: (node: WorkflowNodeV2) => void;
  onBindReference: (node: WorkflowNodeV2) => void;
  onReferenceBound: () => void;
  onCancelReference: () => void;
  onPreviewImage: (image: DownloadableImage) => void;
  onApplyRecipe: (recipe: WorkflowRecipeSummary) => void;
  onAppendRecipe: (recipe: WorkflowRecipeSummary, source: RecipeSourceSelection) => void;
  onArchiveRecipe: (recipe: WorkflowRecipeSummary) => void;
}

type SidePanel = "recipes" | "library";

export function V2WorkflowWorkbench({
  product,
  workflow,
  workflowLoading,
  workflowError,
  canvasState,
  selectedNodeIds,
  structureBusy,
  runningNodeId,
  recipes,
  recipesLoading,
  recipesError,
  operationRecipeId,
  application,
  referenceNode,
  onRetryWorkflow,
  onRetryRecipes,
  onOpenFolder,
  onSelectionChange,
  onViewportChange,
  onLayoutCommit,
  onFolderTranslate,
  onCreateFolder,
  onRenameFolder,
  onDissolveFolder,
  onMoveSelection,
  onSaveRecipe,
  onRunNode,
  onBindReference,
  onReferenceBound,
  onCancelReference,
  onPreviewImage,
  onApplyRecipe,
  onAppendRecipe,
  onArchiveRecipe,
}: V2WorkflowWorkbenchProps) {
  const { t } = useI18n();
  const [sidePanel, setSidePanel] = useState<SidePanel>("recipes");
  const openFolder = workflow?.folders.find((folder) => folder.id === canvasState.open_folder_id) ?? null;
  const selectedNodes = useMemo(() => {
    const selected = new Set(selectedNodeIds);
    return workflow?.nodes.filter((node) => selected.has(node.id)) ?? [];
  }, [selectedNodeIds, workflow?.nodes]);
  const appendSource = (recipe: WorkflowRecipeSummary) => {
    const source = resolveRecipeVersionSource(
      recipe.kind,
      workflow,
      openFolder?.id ?? null,
      selectedNodeIds,
    );
    return source && workflow ? withRecipeSourceLabel(source, workflow, t) : null;
  };

  const bindReference = useCallback((node: WorkflowNodeV2) => {
    setSidePanel("library");
    onBindReference(node);
  }, [onBindReference]);

  return (
    <main className="flex min-h-0 flex-1 flex-col bg-slate-100 pb-[calc(4.5rem+env(safe-area-inset-bottom))] dark:!bg-[#080b10] lg:pb-0">
      <header className="flex min-h-[58px] flex-wrap items-center gap-2 border-b border-slate-200 bg-white px-3 py-2 dark:border-slate-800 dark:!bg-[#0d1118] sm:px-4">
        <div className="flex min-w-0 items-center gap-2">
          {openFolder ? (
            <button type="button" onClick={() => onOpenFolder(null)} disabled={structureBusy} className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-slate-200 text-slate-600 hover:border-indigo-300 hover:text-indigo-700 disabled:opacity-40 dark:border-slate-700 dark:text-slate-300 dark:hover:border-violet-400 dark:hover:text-violet-200" aria-label={t("workflowV2.canvas.back")} title={t("workflowV2.canvas.back")}>
              <ArrowLeft size={16} />
            </button>
          ) : null}
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-1.5 text-sm font-semibold text-slate-950 dark:text-slate-100">
              <span className="truncate">{workflow?.title ?? t("workflowV2.canvas.title")}</span>
              {openFolder ? <><span className="text-slate-300 dark:text-slate-700">/</span><span className="truncate text-indigo-700 dark:text-violet-300">{openFolder.title}</span></> : null}
            </div>
            <div className="mt-0.5 text-[10px] font-medium text-slate-400 dark:text-slate-500">
              {workflow ? t("workflowV2.canvas.version", { revision: workflow.revision, editVersion: workflow.edit_version }) : t("workflowV2.canvas.noWorkflowShort")}
            </div>
          </div>
        </div>

        <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-1.5">
          {selectedNodeIds.length ? (
            <span className="hidden rounded-md bg-indigo-50 px-2 py-1.5 text-[11px] font-semibold text-indigo-700 dark:bg-violet-500/15 dark:text-violet-200 sm:inline-flex">
              {t("workflowV2.canvas.selected", { count: selectedNodeIds.length })}
            </span>
          ) : null}

          {workflow && selectedNodeIds.length ? (
            <button type="button" onClick={onCreateFolder} disabled={structureBusy} className="inline-flex h-9 items-center gap-1.5 rounded-md border border-slate-200 bg-white px-2.5 text-xs font-semibold text-slate-700 hover:border-indigo-300 hover:text-indigo-700 disabled:opacity-40 dark:border-slate-700 dark:!bg-slate-900 dark:text-slate-200 dark:hover:border-violet-400 dark:hover:text-violet-200" title={t("workflowV2.folder.create")}>
              <FolderPlus size={14} /> <span className="hidden sm:inline">{t("workflowV2.folder.create")}</span>
            </button>
          ) : null}

          {workflow && selectedNodeIds.length ? (
            <label className="relative">
              <span className="sr-only">{t("workflowV2.folder.move")}</span>
              <FolderInput size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-slate-500" />
              <select
                value=""
                disabled={structureBusy}
                onChange={(event) => {
                  const value = event.target.value;
                  if (value === "__ungrouped__") onMoveSelection(null);
                  else if (value) onMoveSelection(value);
                }}
                className="h-9 max-w-[170px] appearance-none rounded-md border border-slate-200 bg-white py-0 pl-8 pr-7 text-xs font-semibold text-slate-700 outline-none hover:border-indigo-300 disabled:opacity-40 dark:border-slate-700 dark:!bg-slate-900 dark:text-slate-200"
                aria-label={t("workflowV2.folder.move")}
              >
                <option value="">{t("workflowV2.folder.move")}</option>
                {openFolder && selectedNodes.some((node) => node.folder_id === openFolder.id) ? <option value="__ungrouped__">{t("workflowV2.folder.ungroup")}</option> : null}
                {workflow.folders.filter((folder) => folder.id !== openFolder?.id).map((folder) => <option key={folder.id} value={folder.id}>{folder.title}</option>)}
              </select>
              <ChevronDown size={13} className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-slate-400" />
            </label>
          ) : null}

          {workflow ? (
            <details className="group relative">
              <summary className="flex h-9 cursor-pointer list-none items-center gap-1.5 rounded-md bg-slate-950 px-2.5 text-xs font-semibold text-white marker:hidden hover:bg-indigo-700 dark:bg-violet-500 dark:hover:bg-violet-400 [&::-webkit-details-marker]:hidden">
                <Save size={14} /> <span className="hidden sm:inline">{t("workflowV2.recipe.save")}</span><ChevronDown size={12} className="transition-transform group-open:rotate-180" />
              </summary>
              <div className="absolute right-0 top-full z-40 mt-1.5 w-52 overflow-hidden rounded-md border border-slate-200 bg-white p-1.5 shadow-xl dark:border-slate-700 dark:!bg-[#10151d]">
                <RecipeSourceButton label={t("workflowV2.recipe.saveFull")} onClick={() => onSaveRecipe({ source_type: "workflow", label: t("workflowV2.recipe.sourceFull") })} />
                {openFolder ? <RecipeSourceButton label={t("workflowV2.recipe.saveFolder")} onClick={() => onSaveRecipe({ source_type: "folder", folder_id: openFolder.id, label: t("workflowV2.recipe.sourceFolder", { title: openFolder.title }) })} /> : null}
                {selectedNodeIds.length ? <RecipeSourceButton label={t("workflowV2.recipe.saveSelection")} onClick={() => onSaveRecipe({ source_type: "selection", node_ids: selectedNodeIds, label: t("workflowV2.recipe.sourceSelection", { count: selectedNodeIds.length }) })} /> : null}
              </div>
            </details>
          ) : null}

          {openFolder ? (
            <details className="group relative">
              <summary className="flex h-9 w-9 cursor-pointer list-none items-center justify-center rounded-md border border-slate-200 text-slate-600 marker:hidden hover:border-indigo-300 hover:text-indigo-700 dark:border-slate-700 dark:text-slate-300 [&::-webkit-details-marker]:hidden" aria-label={t("workflowV2.folder.actions")} title={t("workflowV2.folder.actions")}><MoreHorizontal size={16} /></summary>
              <div className="absolute right-0 top-full z-40 mt-1.5 w-44 overflow-hidden rounded-md border border-slate-200 bg-white p-1.5 shadow-xl dark:border-slate-700 dark:!bg-[#10151d]">
                <button type="button" onClick={onRenameFolder} className="flex h-9 w-full items-center gap-2 rounded px-2.5 text-left text-xs font-medium text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800"><Pencil size={13} />{t("workflowV2.folder.rename")}</button>
                <button type="button" onClick={onDissolveFolder} className="flex h-9 w-full items-center gap-2 rounded px-2.5 text-left text-xs font-medium text-red-600 hover:bg-red-50 dark:text-red-300 dark:hover:bg-red-500/10"><Trash2 size={13} />{t("workflowV2.folder.dissolve")}</button>
              </div>
            </details>
          ) : null}
        </div>
      </header>

      <div className="grid min-h-0 flex-1 grid-rows-[minmax(420px,58svh)_minmax(360px,auto)] lg:grid-cols-[minmax(0,1fr)_380px] lg:grid-rows-1">
        <section className="relative min-h-0 overflow-hidden border-b border-slate-200 bg-slate-50 dark:border-slate-800 dark:!bg-[#080b10] lg:border-b-0 lg:border-r" aria-label={t("workflowV2.canvas.ariaLabel")}>
          {workflowLoading ? (
            <CanvasState icon={<Loader2 size={22} className="animate-spin" />} text={t("workflowV2.canvas.loading")} />
          ) : workflowError ? (
            <CanvasState text={workflowError} action={t("workflowV2.retry")} onAction={onRetryWorkflow} />
          ) : workflow ? (
            <V2WorkflowCanvas
              workflow={workflow}
              openFolderId={openFolder?.id ?? null}
              viewport={openFolder ? canvasState.folder_viewports[openFolder.id] ?? null : canvasState.global_viewport}
              structureBusy={structureBusy}
              runningNodeId={runningNodeId}
              onOpenFolder={onOpenFolder}
              onRunNode={onRunNode}
              onBindReference={bindReference}
              onSelectionChange={onSelectionChange}
              onLayoutCommit={onLayoutCommit}
              onFolderTranslate={onFolderTranslate}
              onViewportChange={onViewportChange}
            />
          ) : (
            <CanvasState icon={<PackageOpen size={24} />} text={t("workflowV2.canvas.noWorkflow")} />
          )}
          {structureBusy ? (
            <div className="pointer-events-none absolute left-3 top-3 z-20 inline-flex items-center gap-1.5 rounded-md border border-slate-200 bg-white/95 px-2.5 py-1.5 text-[11px] font-semibold text-slate-600 shadow-sm backdrop-blur dark:border-slate-700 dark:!bg-slate-900/95 dark:text-slate-200">
              <Loader2 size={12} className="animate-spin" /> {t("workflowV2.canvas.saving")}
            </div>
          ) : null}
        </section>

        <aside className="flex min-h-0 flex-col bg-slate-50 dark:!bg-[#0b0f15]" aria-label={t("workflowV2.sidebar.ariaLabel")}>
          {referenceNode ? (
            <div className="flex items-center gap-2 border-b border-indigo-200 bg-indigo-50 px-3 py-2 text-xs text-indigo-800 dark:border-violet-400/30 dark:!bg-violet-500/10 dark:text-violet-100">
              <FolderOpen size={14} className="shrink-0" />
              <span className="min-w-0 flex-1 truncate">{t("workflowV2.reference.target", { title: referenceNode.title })}</span>
              <button type="button" onClick={onCancelReference} className="shrink-0 font-semibold hover:underline">{t("workflowV2.reference.cancel")}</button>
            </div>
          ) : null}
          <div className="grid grid-cols-2 border-b border-slate-200 bg-white p-1.5 dark:border-slate-800 dark:!bg-[#0d1118]">
            <SideTab active={sidePanel === "recipes"} icon={<Library size={14} />} label={t("workflowV2.sidebar.recipes")} onClick={() => setSidePanel("recipes")} />
            <SideTab active={sidePanel === "library"} icon={<Images size={14} />} label={t("workflowV2.sidebar.library")} onClick={() => setSidePanel("library")} />
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            {sidePanel === "recipes" ? (
              <RecipeLibraryPanel
                recipes={recipes}
                loading={recipesLoading}
                error={recipesError}
                operationRecipeId={operationRecipeId}
                application={application}
                canAppend={(recipe) => appendSource(recipe) !== null}
                onRetry={onRetryRecipes}
                onApply={onApplyRecipe}
                onAppend={(recipe) => {
                  const source = appendSource(recipe);
                  if (source) onAppendRecipe(recipe, source);
                }}
                onArchive={onArchiveRecipe}
              />
            ) : (
              <div className="p-3">
                <ProductImageExplorer
                  productId={product.id}
                  productName={product.name}
                  onPreviewImage={onPreviewImage}
                  referenceTarget={referenceNode && workflow ? {
                    workflowId: workflow.id,
                    nodeId: referenceNode.id,
                    expectedWorkflowRevision: workflow.revision,
                    expectedBoundAssetId: referenceNode.bound_image_asset_id,
                    onBound: onReferenceBound,
                  } : undefined}
                />
              </div>
            )}
          </div>
        </aside>
      </div>
    </main>
  );
}

function withRecipeSourceLabel(
  source: Omit<RecipeSourceSelection, "label">,
  workflow: ProductWorkflowV2,
  t: ReturnType<typeof useI18n>["t"],
): RecipeSourceSelection {
  if (source.source_type === "selection") {
    return {
      ...source,
      label: t("workflowV2.recipe.sourceSelection", { count: source.node_ids?.length ?? 0 }),
    };
  }
  if (source.source_type === "folder") {
    const title = workflow.folders.find((folder) => folder.id === source.folder_id)?.title ?? source.folder_id ?? "";
    return { ...source, label: t("workflowV2.recipe.sourceFolder", { title }) };
  }
  return { ...source, label: t("workflowV2.recipe.sourceFull") };
}

function RecipeSourceButton({ label, onClick }: { label: string; onClick: () => void }) {
  return <button type="button" onClick={onClick} className="flex h-9 w-full items-center gap-2 rounded px-2.5 text-left text-xs font-medium text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800"><Save size={13} />{label}</button>;
}

function SideTab({ active, icon, label, onClick }: { active: boolean; icon: React.ReactNode; label: string; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className={`inline-flex h-9 items-center justify-center gap-1.5 rounded-md text-xs font-semibold transition-colors ${active ? "bg-slate-950 text-white shadow-sm dark:bg-violet-500" : "text-slate-500 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100"}`}>
      {icon}{label}
    </button>
  );
}

function CanvasState({ icon, text, action, onAction }: { icon?: React.ReactNode; text: string; action?: string; onAction?: () => void }) {
  return (
    <div className="flex h-full min-h-[420px] flex-col items-center justify-center gap-2 p-8 text-center text-sm text-slate-500 dark:text-slate-400">
      {icon ? <span className="text-slate-400 dark:text-slate-500">{icon}</span> : null}
      <span>{text}</span>
      {action && onAction ? <button type="button" onClick={onAction} className="mt-1 font-semibold text-indigo-700 hover:underline dark:text-violet-300">{action}</button> : null}
    </div>
  );
}
