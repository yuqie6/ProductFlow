import {
  FolderOpen,
  FileOutput,
  Images,
  Library,
  Loader2,
  PackageOpen,
} from "lucide-react";
import { useCallback, useMemo, useState } from "react";

import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type {
  ProductDetail,
  ProductWorkflowV2,
  WorkflowNodeV2,
  WorkflowRecipeApplicationResult,
  WorkflowRecipeSummary,
} from "../../lib/types";
import { ProductImageExplorer } from "../product-detail/image-explorer/ProductImageExplorer";
import type { WorkflowCanvasStateV1, WorkflowCanvasViewport } from "./canvasState";
import { DeliveryRenditionPanel } from "./DeliveryRenditionPanel";
import { RecipeLibraryPanel } from "./RecipeLibraryPanel";
import {
  labelRecipeVersionSource,
  resolveRecipeVersionSource,
  type RecipeSourceSelection,
} from "./recipeSource";
import { shouldOpenArtifactsForSelection } from "./sidePanel";
import { V2WorkflowCanvas } from "./V2WorkflowCanvas";
import { V2WorkflowCommandBar } from "./V2WorkflowCommandBar";

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

type SidePanel = "recipes" | "library" | "artifacts";

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
  const selectedImageNode = selectedNodes.length === 1 && selectedNodes[0].node_type === "image_generation"
    ? selectedNodes[0]
    : null;
  const appendSource = (recipe: WorkflowRecipeSummary) => {
    const source = resolveRecipeVersionSource(
      recipe.kind,
      workflow,
      openFolder?.id ?? null,
      selectedNodeIds,
    );
    return source && workflow ? labelRecipeVersionSource(source, workflow, t) : null;
  };

  const bindReference = useCallback((node: WorkflowNodeV2) => {
    setSidePanel("library");
    onBindReference(node);
  }, [onBindReference]);
  const selectNodes = useCallback((nodeIds: string[]) => {
    if (shouldOpenArtifactsForSelection(selectedNodeIds, nodeIds, workflow?.nodes ?? [])) {
      setSidePanel("artifacts");
    }
    onSelectionChange(nodeIds);
  }, [onSelectionChange, selectedNodeIds, workflow?.nodes]);

  return (
    <main className="flex min-h-0 flex-1 flex-col bg-slate-100 pb-[calc(4.5rem+env(safe-area-inset-bottom))] dark:!bg-[#080b10] lg:pb-0">
      <V2WorkflowCommandBar
        workflow={workflow}
        openFolderId={openFolder?.id ?? null}
        selectedNodeIds={selectedNodeIds}
        structureBusy={structureBusy}
        variant="header"
        onOpenFolder={onOpenFolder}
        onCreateFolder={onCreateFolder}
        onMoveSelection={onMoveSelection}
        onSaveRecipe={onSaveRecipe}
        onRenameFolder={onRenameFolder}
        onDissolveFolder={onDissolveFolder}
      />

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
              onSelectionChange={selectNodes}
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
          <div className="grid grid-cols-3 border-b border-slate-200 bg-white p-1.5 dark:border-slate-800 dark:!bg-[#0d1118]">
            <SideTab active={sidePanel === "recipes"} icon={<Library size={14} />} label={t("workflowV2.sidebar.recipes")} onClick={() => setSidePanel("recipes")} />
            <SideTab active={sidePanel === "library"} icon={<Images size={14} />} label={t("workflowV2.sidebar.library")} onClick={() => setSidePanel("library")} />
            <SideTab active={sidePanel === "artifacts"} icon={<FileOutput size={14} />} label={t("workflowV2.sidebar.artifacts")} onClick={() => setSidePanel("artifacts")} />
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
            ) : sidePanel === "library" ? (
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
            ) : (
              <DeliveryRenditionPanel
                productId={product.id}
                node={selectedImageNode}
                onPreviewImage={onPreviewImage}
              />
            )}
          </div>
        </aside>
      </div>
    </main>
  );
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
