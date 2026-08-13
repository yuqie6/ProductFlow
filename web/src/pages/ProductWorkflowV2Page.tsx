import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import { ConfirmDialog } from "../components/ConfirmDialog";
import { GalleryImagePreviewDialog } from "../components/GalleryImagePreviewDialog";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import type { DownloadableImage } from "../lib/image-downloads";
import { useI18n } from "../lib/preferences";
import type {
  ActiveProductWorkflowV2,
  ProductWorkflowV2,
  WorkflowCanvasMutationResult,
  WorkflowNodeV2,
  WorkflowRecipe,
  WorkflowRecipeApplicationResult,
  WorkflowRecipeSummary,
} from "../lib/types";
import {
  emptyWorkflowCanvasState,
  parseWorkflowCanvasState,
  reconcileWorkflowCanvasState,
  workflowCanvasStateStorageKey,
  type WorkflowCanvasStateV1,
  type WorkflowCanvasViewport,
} from "./product-workflow-v2/canvasState";
import { visibleRealNodeIds } from "./product-workflow-v2/graph";
import { V2WorkflowWorkbench, type RecipeSourceSelection } from "./product-workflow-v2/V2WorkflowWorkbench";
import { WorkflowRecipeDialog, WorkflowTextDialog } from "./product-workflow-v2/WorkflowDialogs";

type TextDialogState =
  | { kind: "create-folder" }
  | { kind: "rename-folder"; folderId: string; initialValue: string }
  | null;

type RecipeDialogState =
  | { kind: "create"; source: RecipeSourceSelection }
  | { kind: "append"; source: RecipeSourceSelection; recipe: WorkflowRecipeSummary }
  | null;

type ConfirmState =
  | { kind: "dissolve-folder"; folderId: string; title: string }
  | { kind: "archive-recipe"; recipe: WorkflowRecipeSummary }
  | null;

type RecipeOperation =
  | { kind: "create"; source: RecipeSourceSelection; title: string; description: string | null }
  | { kind: "append"; source: RecipeSourceSelection; title: string; description: string | null; recipe: WorkflowRecipeSummary }
  | { kind: "archive"; recipe: WorkflowRecipeSummary }
  | { kind: "apply"; recipe: WorkflowRecipeSummary; idempotencyKey: string };

type CanvasStateUpdate = WorkflowCanvasStateV1 | ((current: WorkflowCanvasStateV1) => WorkflowCanvasStateV1);

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.detail;
  if (error instanceof Error) return error.message;
  return fallback;
}

function newIdempotencyKey(recipeId: string): string {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `recipe-${recipeId}-${suffix}`.slice(0, 120);
}

function activeWorkflowPayload(workflow: ProductWorkflowV2): ActiveProductWorkflowV2 {
  return { latest_revision: workflow.revision, workflow };
}

export function ProductWorkflowV2Page() {
  const { productId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [canvasState, setCanvasState] = useState<WorkflowCanvasStateV1>(emptyWorkflowCanvasState);
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>([]);
  const [textDialog, setTextDialog] = useState<TextDialogState>(null);
  const [recipeDialog, setRecipeDialog] = useState<RecipeDialogState>(null);
  const [confirmState, setConfirmState] = useState<ConfirmState>(null);
  const [referenceNodeId, setReferenceNodeId] = useState<string | null>(null);
  const [currentRun, setCurrentRun] = useState<{ id: string; nodeId: string } | null>(null);
  const [application, setApplication] = useState<WorkflowRecipeApplicationResult | null>(null);
  const [operationError, setOperationError] = useState<string | null>(null);
  const [previewImage, setPreviewImage] = useState<DownloadableImage | null>(null);
  const loadedCanvasWorkflowRef = useRef<string | null>(null);
  const recipeApplyKeysRef = useRef(new Map<string, string>());

  const productQuery = useQuery({
    queryKey: ["product", productId],
    queryFn: () => api.getProduct(productId),
    enabled: Boolean(productId),
  });
  const workflowQuery = useQuery({
    queryKey: ["active-product-workflow-v2", productId],
    queryFn: () => api.getActiveProductWorkflowV2(productId),
    enabled: Boolean(productId),
    refetchInterval: (query) => query.state.data?.workflow?.nodes.some((node) => node.status === "queued" || node.status === "running") ? 1500 : false,
  });
  const recipesQuery = useQuery({
    queryKey: ["workflow-recipes", false],
    queryFn: () => api.listWorkflowRecipes(false),
  });
  const runQuery = useQuery({
    queryKey: ["workflow-node-run-v2", currentRun?.id],
    queryFn: () => api.getWorkflowNodeRunV2(currentRun!.id),
    enabled: Boolean(currentRun),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return !status || status === "queued" || status === "running" ? 1000 : false;
    },
  });

  const workflow = workflowQuery.data?.workflow ?? null;
  const product = productQuery.data ?? null;
  const openFolder = workflow?.folders.find((folder) => folder.id === canvasState.open_folder_id) ?? null;
  const referenceNode = referenceNodeId
    ? workflow?.nodes.find((node) => node.id === referenceNodeId && node.node_type === "reference_image") ?? null
    : null;

  const persistCanvasState = useCallback((update: CanvasStateUpdate) => {
    setCanvasState((current) => {
      const next = typeof update === "function" ? update(current) : update;
      if (next === current) return current;
      const workflowId = workflowQuery.data?.workflow?.id;
      if (workflowId && typeof window !== "undefined") {
        window.localStorage.setItem(workflowCanvasStateStorageKey(workflowId), JSON.stringify(next));
      }
      return next;
    });
  }, [workflowQuery.data?.workflow?.id]);

  const updateSelection = useCallback((nodeIds: string[]) => {
    setSelectedNodeIds((current) => (
      current.length === nodeIds.length && current.every((nodeId, index) => nodeId === nodeIds[index])
        ? current
        : nodeIds
    ));
  }, []);

  useEffect(() => {
    if (!workflow) {
      loadedCanvasWorkflowRef.current = null;
      setCanvasState(emptyWorkflowCanvasState());
      setSelectedNodeIds([]);
      return;
    }
    if (loadedCanvasWorkflowRef.current !== workflow.id) {
      loadedCanvasWorkflowRef.current = workflow.id;
      const raw = typeof window === "undefined"
        ? null
        : window.localStorage.getItem(workflowCanvasStateStorageKey(workflow.id));
      setCanvasState(reconcileWorkflowCanvasState(parseWorkflowCanvasState(raw), workflow.folders.map((folder) => folder.id)));
      setSelectedNodeIds([]);
      return;
    }
    setCanvasState((current) => reconcileWorkflowCanvasState(current, workflow.folders.map((folder) => folder.id)));
    setSelectedNodeIds((current) => {
      const visibleNodeIds = new Set(visibleRealNodeIds(workflow, canvasState.open_folder_id));
      return current.filter((nodeId) => visibleNodeIds.has(nodeId));
    });
  }, [canvasState.open_folder_id, workflow]);

  useEffect(() => {
    const status = runQuery.data?.status;
    if (!currentRun || !status || status === "queued" || status === "running") return;
    setCurrentRun(null);
    void workflowQuery.refetch();
    void queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] });
    void queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] });
  }, [currentRun, productId, queryClient, runQuery.data?.status, workflowQuery]);

  const acceptCanvasMutation = useCallback((result: WorkflowCanvasMutationResult) => {
    queryClient.setQueryData<ActiveProductWorkflowV2>(
      ["active-product-workflow-v2", productId],
      activeWorkflowPayload(result.workflow),
    );
    setOperationError(null);
  }, [productId, queryClient]);

  const structureMutation = useMutation({
    mutationFn: (operation: () => Promise<WorkflowCanvasMutationResult>) => operation(),
    onSuccess: acceptCanvasMutation,
    onError: async (error) => {
      setOperationError(errorDetail(error, t("workflowV2.error.structure")));
      if (error instanceof ApiError && error.status === 409) await workflowQuery.refetch();
    },
  });

  const nodeRunMutation = useMutation({
    mutationFn: (node: WorkflowNodeV2) => api.runWorkflowNodeV2(node.id),
    onMutate: (node) => {
      setOperationError(null);
      setCurrentRun(null);
      return { nodeId: node.id };
    },
    onSuccess: async (result) => {
      setCurrentRun({ id: result.node_run.id, nodeId: result.node_run.node_id });
      await workflowQuery.refetch();
    },
    onError: (error) => {
      setCurrentRun(null);
      setOperationError(errorDetail(error, t("workflowV2.error.run")));
    },
  });

  const recipeMutation = useMutation({
    mutationFn: async (operation: RecipeOperation): Promise<WorkflowRecipe | WorkflowRecipeApplicationResult> => {
      if (operation.kind === "apply") {
        return api.applyWorkflowRecipe(productId, operation.recipe.id, {
          expected_recipe_version: operation.recipe.current_version.version,
          idempotency_key: operation.idempotencyKey,
        });
      }
      if (operation.kind === "archive") {
        const result = await api.archiveWorkflowRecipe(operation.recipe.id, operation.recipe.current_version.version);
        return result.recipe;
      }
      if (!workflow) throw new Error(t("workflowV2.canvas.noWorkflowShort"));
      const input = {
        source_type: operation.source.source_type,
        folder_id: operation.source.folder_id,
        node_ids: operation.source.node_ids,
        expected_edit_version: workflow.edit_version,
        title: operation.title,
        description: operation.description,
      };
      if (operation.kind === "create") return api.createWorkflowRecipe(productId, workflow.id, input);
      return api.appendWorkflowRecipeVersion(productId, workflow.id, operation.recipe.id, {
        ...input,
        expected_recipe_version: operation.recipe.current_version.version,
      });
    },
    onMutate: () => setOperationError(null),
    onSuccess: async (result, operation) => {
      if (operation.kind === "apply") {
        recipeApplyKeysRef.current.delete(operation.recipe.id);
        setApplication(result as WorkflowRecipeApplicationResult);
      } else {
        await queryClient.invalidateQueries({ queryKey: ["workflow-recipes"] });
      }
      setRecipeDialog(null);
      setConfirmState(null);
    },
    onError: async (error) => {
      setOperationError(errorDetail(error, t("workflowV2.error.recipe")));
      if (error instanceof ApiError && error.status === 409) {
        const operation = recipeMutation.variables;
        if (operation?.kind === "apply") recipeApplyKeysRef.current.delete(operation.recipe.id);
        await Promise.all([workflowQuery.refetch(), recipesQuery.refetch()]);
      }
    },
  });

  const recipeOperationId = useMemo(() => {
    const variables = recipeMutation.variables;
    if (!recipeMutation.isPending || !variables) return null;
    return "recipe" in variables ? variables.recipe.id : null;
  }, [recipeMutation.isPending, recipeMutation.variables]);

  const moveSelection = (folderId: string | null) => {
    if (!workflow || selectedNodeIds.length === 0) return;
    if (folderId) {
      const targetIds = workflow.nodes.filter((node) => node.folder_id === folderId).map((node) => node.id);
      const nodeIds = Array.from(new Set([...targetIds, ...selectedNodeIds]));
      structureMutation.mutate(() => api.setWorkflowFolderMembers(productId, workflow.id, folderId, {
        node_ids: nodeIds,
        expected_edit_version: workflow.edit_version,
      }));
      return;
    }
    if (!openFolder) return;
    const selected = new Set(selectedNodeIds);
    const remainingIds = workflow.nodes
      .filter((node) => node.folder_id === openFolder.id && !selected.has(node.id))
      .map((node) => node.id);
    structureMutation.mutate(() => api.setWorkflowFolderMembers(productId, workflow.id, openFolder.id, {
      node_ids: remainingIds,
      expected_edit_version: workflow.edit_version,
    }), { onSuccess: () => setSelectedNodeIds([]) });
  };

  const viewportFolderId = openFolder?.id ?? null;
  const updateViewport = useCallback((viewport: WorkflowCanvasViewport) => {
    if (!workflow) return;
    persistCanvasState((current) => viewportFolderId ? {
      ...current,
      folder_viewports: { ...current.folder_viewports, [viewportFolderId]: viewport },
    } : {
      ...current,
      global_viewport: viewport,
    });
  }, [persistCanvasState, viewportFolderId, workflow]);

  const loading = productQuery.isLoading;
  if (loading || !product) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-50 text-slate-400 dark:!bg-[#080b10]">
        {productQuery.isError
          ? <span className="text-sm text-red-600 dark:text-red-300">{errorDetail(productQuery.error, t("detail.loadFailed"))}</span>
          : <Loader2 size={22} className="animate-spin" />}
      </div>
    );
  }

  const dialogError = recipeMutation.isError || structureMutation.isError ? operationError : null;
  return (
    <div className="flex min-h-screen flex-col bg-white text-slate-900 dark:bg-[#080b10] dark:text-slate-100 lg:h-screen lg:min-h-0 lg:overflow-hidden">
      <TopNav onHome={() => navigate("/products")} breadcrumbs={`${product.name} / ${t("workflowV2.breadcrumb")}`} />
      {operationError ? (
        <div role="alert" className="flex items-center justify-between gap-3 border-b border-red-200 bg-red-50 px-4 py-2 text-xs text-red-700 dark:border-red-400/30 dark:!bg-red-500/10 dark:text-red-200">
          <span className="min-w-0 truncate">{operationError}</span>
          <button type="button" onClick={() => setOperationError(null)} className="shrink-0 font-semibold hover:underline">{t("workflowV2.dialog.close")}</button>
        </div>
      ) : null}
      <V2WorkflowWorkbench
        product={product}
        workflow={workflow}
        workflowLoading={workflowQuery.isLoading}
        workflowError={workflowQuery.isError ? errorDetail(workflowQuery.error, t("workflowV2.error.workflow")) : null}
        canvasState={canvasState}
        selectedNodeIds={selectedNodeIds}
        structureBusy={structureMutation.isPending}
        runningNodeId={currentRun?.nodeId ?? (nodeRunMutation.isPending ? nodeRunMutation.variables?.id ?? null : null)}
        recipes={recipesQuery.data ?? []}
        recipesLoading={recipesQuery.isLoading}
        recipesError={recipesQuery.isError ? errorDetail(recipesQuery.error, t("workflowV2.error.recipes")) : null}
        operationRecipeId={recipeOperationId}
        application={application}
        referenceNode={referenceNode}
        onRetryWorkflow={() => void workflowQuery.refetch()}
        onRetryRecipes={() => void recipesQuery.refetch()}
        onOpenFolder={(folderId) => {
          persistCanvasState((current) => ({ ...current, open_folder_id: folderId }));
          setSelectedNodeIds([]);
        }}
        onSelectionChange={updateSelection}
        onViewportChange={updateViewport}
        onLayoutCommit={(positions) => {
          if (!workflow) return;
          structureMutation.mutate(() => api.updateWorkflowNodeLayoutV2(productId, workflow.id, {
            positions,
            expected_edit_version: workflow.edit_version,
          }));
        }}
        onFolderTranslate={(folderId, deltaX, deltaY) => {
          if (!workflow) return;
          structureMutation.mutate(() => api.translateWorkflowFolder(productId, workflow.id, folderId, {
            delta_x: deltaX,
            delta_y: deltaY,
            expected_edit_version: workflow.edit_version,
          }));
        }}
        onCreateFolder={() => setTextDialog({ kind: "create-folder" })}
        onRenameFolder={() => {
          if (openFolder) setTextDialog({ kind: "rename-folder", folderId: openFolder.id, initialValue: openFolder.title });
        }}
        onDissolveFolder={() => {
          if (openFolder) setConfirmState({ kind: "dissolve-folder", folderId: openFolder.id, title: openFolder.title });
        }}
        onMoveSelection={moveSelection}
        onSaveRecipe={(source) => setRecipeDialog({ kind: "create", source })}
        onRunNode={(node) => nodeRunMutation.mutate(node)}
        onBindReference={(node) => setReferenceNodeId(node.id)}
        onReferenceBound={() => {
          setReferenceNodeId(null);
          void workflowQuery.refetch();
        }}
        onCancelReference={() => setReferenceNodeId(null)}
        onPreviewImage={setPreviewImage}
        onApplyRecipe={(recipe) => {
          const idempotencyKey = recipeApplyKeysRef.current.get(recipe.id) ?? newIdempotencyKey(recipe.id);
          recipeApplyKeysRef.current.set(recipe.id, idempotencyKey);
          recipeMutation.mutate({ kind: "apply", recipe, idempotencyKey });
        }}
        onAppendRecipe={(recipe, source) => setRecipeDialog({ kind: "append", recipe, source })}
        onArchiveRecipe={(recipe) => setConfirmState({ kind: "archive-recipe", recipe })}
      />

      <WorkflowTextDialog
        open={textDialog !== null}
        title={textDialog?.kind === "rename-folder" ? t("workflowV2.folder.rename") : t("workflowV2.folder.create")}
        label={t("workflowV2.folder.name")}
        initialValue={textDialog?.kind === "rename-folder" ? textDialog.initialValue : ""}
        busy={structureMutation.isPending}
        error={dialogError}
        onClose={() => setTextDialog(null)}
        onSubmit={(title) => {
          if (!workflow || !textDialog) return;
          if (textDialog.kind === "create-folder") {
            structureMutation.mutate(() => api.createWorkflowFolder(productId, workflow.id, {
              title,
              node_ids: selectedNodeIds,
              expected_edit_version: workflow.edit_version,
            }), { onSuccess: () => { setTextDialog(null); setSelectedNodeIds([]); } });
          } else {
            structureMutation.mutate(() => api.renameWorkflowFolder(productId, workflow.id, textDialog.folderId, {
              title,
              expected_edit_version: workflow.edit_version,
            }), { onSuccess: () => setTextDialog(null) });
          }
        }}
      />

      <WorkflowRecipeDialog
        open={recipeDialog !== null}
        heading={recipeDialog?.kind === "append" ? t("workflowV2.recipe.append") : t("workflowV2.recipe.save")}
        sourceLabel={recipeDialog?.source.label ?? ""}
        initialTitle={recipeDialog?.kind === "append" ? recipeDialog.recipe.current_version.title : ""}
        initialDescription={recipeDialog?.kind === "append" ? recipeDialog.recipe.current_version.description : null}
        busy={recipeMutation.isPending}
        error={dialogError}
        onClose={() => setRecipeDialog(null)}
        onSubmit={(title, description) => {
          if (!recipeDialog) return;
          if (recipeDialog.kind === "create") recipeMutation.mutate({ kind: "create", source: recipeDialog.source, title, description });
          else recipeMutation.mutate({ kind: "append", source: recipeDialog.source, recipe: recipeDialog.recipe, title, description });
        }}
      />

      <ConfirmDialog
        open={confirmState !== null}
        title={confirmState?.kind === "archive-recipe" ? t("workflowV2.recipe.archive") : t("workflowV2.folder.dissolve")}
        description={confirmState?.kind === "archive-recipe"
          ? t("workflowV2.recipe.archiveConfirm", { title: confirmState.recipe.current_version.title })
          : t("workflowV2.folder.dissolveConfirm", { title: confirmState?.title ?? "" })}
        confirmLabel={confirmState?.kind === "archive-recipe" ? t("workflowV2.recipe.archive") : t("workflowV2.folder.dissolve")}
        cancelLabel={t("common.cancel")}
        busy={structureMutation.isPending || recipeMutation.isPending}
        destructive
        onClose={() => setConfirmState(null)}
        onConfirm={() => {
          if (!confirmState) return;
          if (confirmState.kind === "archive-recipe") {
            recipeMutation.mutate({ kind: "archive", recipe: confirmState.recipe });
            return;
          }
          if (!workflow) return;
          structureMutation.mutate(() => api.dissolveWorkflowFolder(productId, workflow.id, confirmState.folderId, workflow.edit_version), {
            onSuccess: () => {
              setConfirmState(null);
              persistCanvasState((current) => ({ ...current, open_folder_id: null }));
              setSelectedNodeIds([]);
            },
          });
        }}
      />

      {previewImage ? (
        <GalleryImagePreviewDialog
          ariaLabel={t("gallery.previewLabel")}
          imageUrl={previewImage.previewUrl}
          imageAlt={previewImage.alt}
          title={previewImage.alt}
          subtitle={previewImage.filename}
          body={t("workflowV2.preview.body")}
          providerNotesTitle={t("gallery.providerNotes")}
          downloadUrl={previewImage.downloadUrl}
          downloadLabel={t("gallery.download")}
          closeLabel={t("gallery.closePreview")}
          onClose={() => setPreviewImage(null)}
        />
      ) : null}
    </div>
  );
}
