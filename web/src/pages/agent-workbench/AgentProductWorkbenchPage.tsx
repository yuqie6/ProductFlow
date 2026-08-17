import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Boxes, CircleAlert, CircleDot, Eye, FolderOpen, Images, Plus, Workflow, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { ConfirmDialog } from "../../components/ConfirmDialog";
import { GalleryImagePreviewDialog } from "../../components/GalleryImagePreviewDialog";
import { TopNav } from "../../components/TopNav";
import { ApiError, api } from "../../lib/api";
import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type {
  ActiveProductWorkflowV2,
  AgentPageContextSnapshotInput,
  AgentWorkbenchBootstrap,
  ProductWorkflowV2,
  WorkflowDraft,
  WorkflowDraftRevision,
  WorkflowMaterializationResult,
  WorkflowRecipe,
  WorkflowRecipeApplicationResult,
  WorkflowRecipeSummary,
} from "../../lib/types";
import { ProductImageExplorer } from "../product-detail/image-explorer/ProductImageExplorer";
import {
  ProductWorkflowV2CanvasPanel,
  type ProductWorkflowV2CanvasContext,
  type V2CreateReferenceControl,
} from "../product-workflow-v2/ProductWorkflowV2CanvasPanel";
import { V2AddNodePanel } from "../product-workflow-v2/V2AddNodePanel";
import { RecipeLibraryPanel } from "../product-workflow-v2/RecipeLibraryPanel";
import {
  labelRecipeVersionSource,
  resolveRecipeVersionSource,
  type RecipeSourceSelection,
} from "../product-workflow-v2/recipeSource";
import { WorkflowRecipeDialog } from "../product-workflow-v2/WorkflowDialogs";
import {
  V2NodeInspector,
  type V2NodeInspectorFlush,
} from "../product-workflow-v2/V2NodeInspector";
import { V2NodeRunsPanel } from "../product-workflow-v2/V2NodeRunsPanel";
import { AgentConversationPanel } from "./AgentConversationPanel";
import {
  AgentWorkbenchShell,
  type AgentWorkbenchSidebarTool,
} from "./AgentWorkbenchShell";
import { useWorkflowMaterialization } from "./useWorkflowMaterialization";
import { useWorkflowReveal } from "./useWorkflowReveal";
import { WorkflowDraftConfirmation } from "./WorkflowDraftConfirmation";

export type AgentV2WorkbenchBootstrap = Extract<AgentWorkbenchBootstrap, { mode: "agent_v2" }>;
type AgentSidebarToolId = "agent" | "add" | "details" | "runs" | "library" | "recipes";
type RecipeDialogState =
  | { kind: "create"; source: RecipeSourceSelection }
  | { kind: "append"; source: RecipeSourceSelection; recipe: WorkflowRecipeSummary }
  | null;
type RecipeOperation =
  | { kind: "create"; source: RecipeSourceSelection; title: string; description: string | null }
  | {
      kind: "append";
      source: RecipeSourceSelection;
      title: string;
      description: string | null;
      recipe: WorkflowRecipeSummary;
    }
  | { kind: "archive"; recipe: WorkflowRecipeSummary }
  | { kind: "apply"; recipe: WorkflowRecipeSummary; idempotencyKey: string };

interface AgentProductWorkbenchPageProps {
  bootstrap: AgentV2WorkbenchBootstrap;
  agentTaskId?: string | null;
  onRefetchBootstrap: () => Promise<unknown>;
}

const EMPTY_CANVAS_CONTEXT: ProductWorkflowV2CanvasContext = {
  openFolderId: null,
  selectedNodeIds: [],
};
const EMPTY_INSPECTOR_FLUSH: V2NodeInspectorFlush = async () => null;
const EMPTY_CREATE_REFERENCE_CONTROL: V2CreateReferenceControl = {
  request: () => undefined,
  busy: true,
};

export function AgentProductWorkbenchPage({
  bootstrap,
  agentTaskId = null,
  onRefetchBootstrap,
}: AgentProductWorkbenchPageProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [materialization, setMaterialization] = useState<WorkflowMaterializationResult | null>(null);
  const [dismissedRevisionId, setDismissedRevisionId] = useState<string | null>(null);
  const [conflictDetected, setConflictDetected] = useState(false);
  const [sidebarTool, setSidebarTool] = useState<AgentSidebarToolId>("agent");
  const [topChromeCollapsed, setTopChromeCollapsed] = useState(false);
  const [canvasContext, setCanvasContext] = useState<ProductWorkflowV2CanvasContext>(EMPTY_CANVAS_CONTEXT);
  const [referenceNodeId, setReferenceNodeId] = useState<string | null>(null);
  const [recipeDialog, setRecipeDialog] = useState<RecipeDialogState>(null);
  const [archiveRecipe, setArchiveRecipe] = useState<WorkflowRecipeSummary | null>(null);
  const [recipeApplication, setRecipeApplication] = useState<WorkflowRecipeApplicationResult | null>(null);
  const [recipeError, setRecipeError] = useState<string | null>(null);
  const [previewImage, setPreviewImage] = useState<DownloadableImage | null>(null);
  const recipeApplyKeysRef = useRef(new Map<string, string>());
  const inspectorFlushRef = useRef<V2NodeInspectorFlush>(EMPTY_INSPECTOR_FLUSH);
  const [createReferenceControl, setCreateReferenceControl] = useState<V2CreateReferenceControl>(
    EMPTY_CREATE_REFERENCE_CONTROL,
  );
  const sidebarTransitionSequenceRef = useRef(0);
  const sidebarToolRef = useRef<AgentSidebarToolId>(sidebarTool);

  sidebarToolRef.current = sidebarTool;

  const bootstrapSnapshot = useMemo<ActiveProductWorkflowV2>(() => ({
    latest_revision: bootstrap.latest_workflow_revision,
    workflow: bootstrap.active_workflow,
  }), [bootstrap.active_workflow, bootstrap.latest_workflow_revision]);
  const activeWorkflowQuery = useQuery({
    queryKey: ["active-product-workflow-v2", bootstrap.product.id],
    queryFn: () => api.getActiveProductWorkflowV2(bootstrap.product.id),
    initialData: bootstrapSnapshot,
    staleTime: 15_000,
    refetchInterval: (query) => query.state.data?.workflow?.nodes.some(
      (node) => node.status === "queued" || node.status === "running",
    ) ? 1_500 : false,
  });

  useEffect(() => {
    queryClient.setQueryData<ActiveProductWorkflowV2>(
      ["active-product-workflow-v2", bootstrap.product.id],
      (current) => preferActiveWorkflowSnapshot(current, bootstrapSnapshot),
    );
  }, [bootstrap.product.id, bootstrapSnapshot, queryClient]);

  const workflow = selectAgentWorkbenchWorkflow(
    activeWorkflowQuery.data?.workflow ?? bootstrap.active_workflow,
    materialization?.workflow ?? null,
  );
  const pageContext = useMemo<AgentPageContextSnapshotInput>(() => ({
    route: `${location.pathname}${location.search}`,
    page_type: "product_workbench",
    product_id: bootstrap.product.id,
    workflow_id: workflow?.id ?? null,
    selected_asset_ids: [],
    visible_asset_ids: [],
    filters: { sidebar: sidebarTool },
    workflow_revision: workflow?.revision ?? null,
    library_revision: null,
    captured_at: new Date().toISOString(),
  }), [bootstrap.product.id, location.pathname, location.search, sidebarTool, workflow?.id, workflow?.revision]);
  const recipesQuery = useQuery({
    queryKey: ["workflow-recipes", false],
    queryFn: () => api.listWorkflowRecipes(false),
    enabled: Boolean(workflow && sidebarTool === "recipes"),
  });
  const reviewableRevision = selectReviewableWorkflowRevision(bootstrap.workflow_draft, workflow);
  const confirmationOpen = Boolean(
    reviewableRevision && dismissedRevisionId !== reviewableRevision.id,
  );
  const selectedNode = canvasContext.selectedNodeIds.length === 1
    ? workflow?.nodes.find(
        (node) => node.id === canvasContext.selectedNodeIds[0],
      ) ?? null
    : null;
  const referenceNode = referenceNodeId
    ? workflow?.nodes.find(
        (node) => node.id === referenceNodeId && node.node_type === "reference_image",
      ) ?? null
    : null;

  const refetchWorkflow = useCallback(async () => {
    await activeWorkflowQuery.refetch();
  }, [activeWorkflowQuery.refetch]);
  const handleCanvasContextChange = useCallback((next: ProductWorkflowV2CanvasContext) => {
    setCanvasContext((current) => (
      current.openFolderId === next.openFolderId &&
      current.selectedNodeIds.length === next.selectedNodeIds.length &&
      current.selectedNodeIds.every((nodeId, index) => nodeId === next.selectedNodeIds[index])
        ? current
        : next
    ));
  }, []);
  const registerInspectorFlush = useCallback((flush: V2NodeInspectorFlush | null) => {
    inspectorFlushRef.current = flush ?? EMPTY_INSPECTOR_FLUSH;
  }, []);
  const registerCreateReference = useCallback((control: V2CreateReferenceControl | null) => {
    setCreateReferenceControl(control ?? EMPTY_CREATE_REFERENCE_CONTROL);
  }, []);
  const flushInspector = useCallback(() => inspectorFlushRef.current(), []);
  const requestSidebarTool = useCallback(async (
    tool: AgentSidebarToolId,
    flushWhenActive = false,
  ): Promise<boolean> => {
    if (!flushWhenActive && tool === sidebarToolRef.current) {
      return true;
    }
    const sequence = ++sidebarTransitionSequenceRef.current;
    try {
      await flushInspector();
    } catch {
      return false;
    }
    if (sequence !== sidebarTransitionSequenceRef.current) {
      return false;
    }
    sidebarToolRef.current = tool;
    setSidebarTool(tool);
    return true;
  }, [flushInspector]);
  const openSidebarTool = useCallback(
    (tool: "details" | "runs" | "library") => requestSidebarTool(tool, true),
    [requestSidebarTool],
  );
  const updateReferenceNode = useCallback((nodeId: string | null) => {
    setReferenceNodeId(nodeId);
  }, []);

  useEffect(() => {
    if (referenceNodeId && !referenceNode) {
      setReferenceNodeId(null);
    }
  }, [referenceNode, referenceNodeId]);

  const updateBootstrapDraft = useCallback((draft: WorkflowDraft) => {
    queryClient.setQueriesData<AgentWorkbenchBootstrap>(
      { queryKey: ["agent-workbench", bootstrap.product.id] },
      (current) => current?.mode === "agent_v2"
        ? { ...current, workflow_draft: draft }
        : current,
    );
  }, [bootstrap.product.id, queryClient]);

  const materializationMutation = useWorkflowMaterialization({
    productId: bootstrap.product.id,
    draftId: bootstrap.workflow_draft.id,
    onDraftConfirmed: updateBootstrapDraft,
    onConflict: async () => {
      await Promise.all([onRefetchBootstrap(), refetchWorkflow()]);
      setConflictDetected(true);
      setDismissedRevisionId(null);
    },
    onMaterialized: (result) => {
      setMaterialization(result);
      setConflictDetected(false);
      setDismissedRevisionId(result.workflow.source_draft_revision_id);
    },
  });
  const reveal = useWorkflowReveal({
    materialization,
    enabled: Boolean(materialization),
    onFallback: refetchWorkflow,
  });
  const revealActive = reveal.status === "loading" || reveal.status === "revealing";
  const revealActivityLabel = revealActive
    ? t(reveal.status === "loading" ? "agentWorkbench.reveal.loading" : "agentWorkbench.reveal.running")
    : null;

  const recipeMutation = useMutation({
    mutationFn: async (operation: RecipeOperation): Promise<WorkflowRecipe | WorkflowRecipeApplicationResult> => {
      if (operation.kind === "apply") {
        return api.applyWorkflowRecipe(bootstrap.product.id, operation.recipe.id, {
          expected_recipe_version: operation.recipe.current_version.version,
          idempotency_key: operation.idempotencyKey,
        });
      }
      if (operation.kind === "archive") {
        const result = await api.archiveWorkflowRecipe(
          operation.recipe.id,
          operation.recipe.current_version.version,
        );
        return result.recipe;
      }
      if (!workflow) {
        throw new Error(t("workflowV2.canvas.noWorkflowShort"));
      }
      const flushedEditVersion = await flushInspector();
      const input = {
        source_type: operation.source.source_type,
        folder_id: operation.source.folder_id,
        node_ids: operation.source.node_ids,
        expected_edit_version: flushedEditVersion ?? workflow.edit_version,
        title: operation.title,
        description: operation.description,
      };
      if (operation.kind === "create") {
        return api.createWorkflowRecipe(bootstrap.product.id, workflow.id, input);
      }
      return api.appendWorkflowRecipeVersion(
        bootstrap.product.id,
        workflow.id,
        operation.recipe.id,
        {
          ...input,
          expected_recipe_version: operation.recipe.current_version.version,
        },
      );
    },
    onMutate: () => setRecipeError(null),
    onSuccess: async (result, operation) => {
      if (operation.kind === "apply") {
        const application = result as WorkflowRecipeApplicationResult;
        recipeApplyKeysRef.current.delete(operation.recipe.id);
        setRecipeApplication(application);
        queryClient.setQueriesData<AgentWorkbenchBootstrap>(
          { queryKey: ["agent-workbench", bootstrap.product.id] },
          (current) => current?.mode === "agent_v2"
            ? {
                ...current,
                conversation: application.conversation,
                workflow_draft: application.draft,
              }
            : current,
        );
        await onRefetchBootstrap();
        sidebarToolRef.current = "agent";
        setSidebarTool("agent");
      } else {
        await queryClient.invalidateQueries({ queryKey: ["workflow-recipes"] });
      }
      setRecipeDialog(null);
      setArchiveRecipe(null);
    },
    onError: async (error) => {
      setRecipeError(errorDetail(error, t("workflowV2.error.recipe")));
      const operation = recipeMutation.variables;
      if (operation?.kind === "apply") {
        recipeApplyKeysRef.current.delete(operation.recipe.id);
      }
      if (error instanceof ApiError && error.status === 409) {
        await Promise.all([refetchWorkflow(), recipesQuery.refetch()]);
      }
    },
  });
  const recipeOperationId = useMemo(() => {
    const operation = recipeMutation.variables;
    if (!recipeMutation.isPending || !operation || !("recipe" in operation)) {
      return null;
    }
    return operation.recipe.id;
  }, [recipeMutation.isPending, recipeMutation.variables]);

  const recipeSourceForAppend = (recipe: WorkflowRecipeSummary): RecipeSourceSelection | null => {
    const source = resolveRecipeVersionSource(
      recipe.kind,
      workflow,
      canvasContext.openFolderId,
      canvasContext.selectedNodeIds,
    );
    if (!source || !workflow) {
      return null;
    }
    return labelRecipeVersionSource(source, workflow, t);
  };

  const confirmRevision = (revision: WorkflowDraftRevision) => {
    if (materializationMutation.isPending) {
      return;
    }
    setConflictDetected(false);
    materializationMutation.mutate({
      revision,
      expectedWorkflowRevision:
        activeWorkflowQuery.data?.latest_revision ?? bootstrap.latest_workflow_revision,
    });
  };
  const materializationError = materializationMutation.error
    ? errorDetail(materializationMutation.error, t("agentWorkbench.materializationFailed"))
    : null;
  const sidebarTools: AgentWorkbenchSidebarTool[] = workflow ? [
    {
      id: "add",
      label: t("workflowV2.sidebar.add"),
      icon: <Plus size={17} />,
      content: (
        <V2AddNodePanel
          busy={createReferenceControl.busy}
          onCreateReference={createReferenceControl.request}
        />
      ),
    },
    {
      id: "details",
      label: t("detail.tabDetails"),
      icon: <Eye size={17} />,
      content: (
        <V2NodeInspector
          product={bootstrap.product}
          workflow={workflow}
          node={selectedNode}
          facts={bootstrap.workflow_draft.current_revision?.payload.facts ?? []}
          onBindReference={async (node) => {
            if (await requestSidebarTool("library")) {
              setReferenceNodeId(node.id);
            }
          }}
          onPreviewImage={setPreviewImage}
          onWorkflowChanged={refetchWorkflow}
          onFlushRegistration={registerInspectorFlush}
        />
      ),
    },
    {
      id: "runs",
      label: t("detail.tabRuns"),
      icon: <CircleDot size={17} />,
      content: (
        <V2NodeRunsPanel
          product={bootstrap.product}
          workflow={workflow}
          node={selectedNode}
          onPreviewImage={setPreviewImage}
          onWorkflowChanged={refetchWorkflow}
        />
      ),
    },
    {
      id: "library",
      label: t("workflowV2.sidebar.library"),
      icon: <Images size={17} />,
      contentClassName: "flex min-h-0 flex-1 flex-col overflow-hidden",
      content: (
        <>
          {referenceNode ? (
            <div className="flex shrink-0 items-center gap-2 border-b border-indigo-200 bg-indigo-50 px-3 py-2 text-xs text-indigo-800 dark:border-violet-400/25 dark:bg-violet-400/10 dark:text-violet-100">
              <FolderOpen size={14} className="shrink-0" />
              <span className="min-w-0 flex-1 truncate">
                {t("workflowV2.reference.target", { title: referenceNode.title })}
              </span>
              <button
                type="button"
                onClick={() => setReferenceNodeId(null)}
                className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md hover:bg-indigo-100 dark:hover:bg-violet-400/10"
                aria-label={t("workflowV2.reference.cancel")}
                title={t("workflowV2.reference.cancel")}
              >
                <X size={14} />
              </button>
            </div>
          ) : null}
          <div className="min-h-0 flex-1 overflow-y-auto p-3">
            <ProductImageExplorer
              productId={bootstrap.product.id}
              productName={bootstrap.product.name}
              onPreviewImage={setPreviewImage}
              referenceTarget={referenceNode ? {
                workflowId: workflow.id,
                nodeId: referenceNode.id,
                expectedWorkflowRevision: workflow.revision,
                expectedBoundAssetId: referenceNode.bound_image_asset_id,
                onBound: () => {
                  setReferenceNodeId(null);
                  void refetchWorkflow();
                },
              } : undefined}
            />
          </div>
        </>
      ),
    },
    {
      id: "recipes",
      label: t("workflowV2.sidebar.recipes"),
      icon: <Boxes size={17} />,
      contentClassName: "flex min-h-0 flex-1 flex-col overflow-hidden",
      content: (
        <>
          {recipeError ? (
            <SidebarError
              message={recipeError}
              closeLabel={t("workflowV2.dialog.close")}
              onClose={() => setRecipeError(null)}
            />
          ) : null}
          <div className="min-h-0 flex-1 overflow-y-auto">
            <RecipeLibraryPanel
              recipes={recipesQuery.data ?? []}
              loading={recipesQuery.isLoading}
              error={recipesQuery.isError
                ? errorDetail(recipesQuery.error, t("workflowV2.error.recipes"))
                : null}
              operationRecipeId={recipeOperationId}
              application={recipeApplication}
              canAppend={(recipe) => recipeSourceForAppend(recipe) !== null}
              onRetry={() => void recipesQuery.refetch()}
              onApply={(recipe) => {
                const idempotencyKey = recipeApplyKeysRef.current.get(recipe.id)
                  ?? newRecipeIdempotencyKey(recipe.id);
                recipeApplyKeysRef.current.set(recipe.id, idempotencyKey);
                recipeMutation.mutate({ kind: "apply", recipe, idempotencyKey });
              }}
              onAppend={(recipe) => {
                const source = recipeSourceForAppend(recipe);
                if (source) {
                  setRecipeDialog({ kind: "append", source, recipe });
                }
              }}
              onArchive={setArchiveRecipe}
            />
          </div>
        </>
      ),
    },
  ] : [];

  return (
    <div className="flex h-dvh min-h-[560px] flex-col overflow-hidden bg-white text-zinc-950 dark:bg-[#060a12] dark:text-slate-100">
      {!topChromeCollapsed ? (
        <TopNav
          breadcrumbs={`${bootstrap.product.name} / ${t("agentWorkbench.breadcrumb")}`}
          onHome={() => navigate("/products")}
        />
      ) : null}
      <AgentWorkbenchShell
        workflowAvailable={Boolean(workflow)}
        activeSidebarTool={sidebarTool}
        onSidebarToolChange={(toolId) => requestSidebarTool(toolId as AgentSidebarToolId)}
        sidebarTools={sidebarTools}
        canvasContent={workflow ? (
          <ProductWorkflowV2CanvasPanel
            productId={bootstrap.product.id}
            workflow={workflow}
            revealVisibility={reveal.visibility}
            activityLabel={revealActivityLabel}
            interactionLocked={revealActive}
            externalError={reveal.error}
            topChromeCollapsed={topChromeCollapsed}
            onToggleTopChrome={() => setTopChromeCollapsed((collapsed) => !collapsed)}
            onRefetchWorkflow={refetchWorkflow}
            onCanvasContextChange={handleCanvasContextChange}
            onOpenSidebarTool={openSidebarTool}
            onBeforeWorkflowAction={flushInspector}
            onReferenceNodeChange={updateReferenceNode}
            onCreateReferenceRegistration={registerCreateReference}
            onSaveRecipe={async (source) => {
              try {
                await flushInspector();
              } catch {
                return;
              }
              setRecipeError(null);
              setRecipeDialog({ kind: "create", source });
            }}
          />
        ) : (
          <PendingWorkflowCanvas />
        )}
        agentContent={(
          <AgentConversationPanel
            productId={bootstrap.product.id}
            productName={bootstrap.product.name}
            conversation={bootstrap.conversation}
            workflowDraft={bootstrap.workflow_draft}
            taskId={agentTaskId}
            pageContext={pageContext}
            reviewDraftAvailable={Boolean(reviewableRevision)}
            onReviewDraft={() => {
              setConflictDetected(false);
              setDismissedRevisionId(null);
            }}
            className="h-full"
          />
        )}
        confirmationContent={confirmationOpen ? (
          <WorkflowDraftConfirmation
            draft={bootstrap.workflow_draft}
            busy={materializationMutation.isPending}
            error={materializationError}
            conflictDetected={conflictDetected}
            onConfirm={confirmRevision}
            onClose={() => {
              if (reviewableRevision) {
                setDismissedRevisionId(reviewableRevision.id);
              }
            }}
          />
        ) : null}
      />

      <WorkflowRecipeDialog
        open={recipeDialog !== null}
        heading={recipeDialog?.kind === "append"
          ? t("workflowV2.recipe.append")
          : t("workflowV2.recipe.save")}
        sourceLabel={recipeDialog?.source.label ?? ""}
        initialTitle={recipeDialog?.kind === "append"
          ? recipeDialog.recipe.current_version.title
          : ""}
        initialDescription={recipeDialog?.kind === "append"
          ? recipeDialog.recipe.current_version.description
          : null}
        busy={recipeMutation.isPending}
        error={recipeMutation.isError ? recipeError : null}
        onClose={() => setRecipeDialog(null)}
        onSubmit={(title, description) => {
          if (!recipeDialog) {
            return;
          }
          if (recipeDialog.kind === "create") {
            recipeMutation.mutate({ kind: "create", source: recipeDialog.source, title, description });
            return;
          }
          recipeMutation.mutate({
            kind: "append",
            source: recipeDialog.source,
            recipe: recipeDialog.recipe,
            title,
            description,
          });
        }}
      />

      <ConfirmDialog
        open={archiveRecipe !== null}
        title={t("workflowV2.recipe.archive")}
        description={t("workflowV2.recipe.archiveConfirm", {
          title: archiveRecipe?.current_version.title ?? "",
        })}
        confirmLabel={t("workflowV2.recipe.archive")}
        cancelLabel={t("common.cancel")}
        busy={recipeMutation.isPending}
        destructive
        onClose={() => setArchiveRecipe(null)}
        onConfirm={() => {
          if (archiveRecipe) {
            recipeMutation.mutate({ kind: "archive", recipe: archiveRecipe });
          }
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

export function selectReviewableWorkflowRevision(
  draft: WorkflowDraft,
  workflow: ProductWorkflowV2 | null,
): WorkflowDraftRevision | null {
  const revision = draft.current_revision;
  if (!revision) {
    return null;
  }
  if (draft.status === "awaiting_confirmation") {
    return revision;
  }
  if (
    draft.status === "confirmed" &&
    workflow?.source_draft_revision_id !== revision.id
  ) {
    return revision;
  }
  return null;
}

export function selectAgentWorkbenchWorkflow(
  persisted: ProductWorkflowV2 | null,
  materialized: ProductWorkflowV2 | null,
): ProductWorkflowV2 | null {
  if (!persisted) {
    return materialized;
  }
  if (!materialized) {
    return persisted;
  }
  if (materialized.revision !== persisted.revision) {
    return materialized.revision > persisted.revision ? materialized : persisted;
  }
  return materialized.edit_version > persisted.edit_version ? materialized : persisted;
}

export function preferActiveWorkflowSnapshot(
  current: ActiveProductWorkflowV2 | undefined,
  incoming: ActiveProductWorkflowV2,
): ActiveProductWorkflowV2 {
  if (!current || incoming.latest_revision > current.latest_revision) {
    return incoming;
  }
  if (incoming.latest_revision < current.latest_revision) {
    return current;
  }
  const selected = selectAgentWorkbenchWorkflow(current.workflow, incoming.workflow);
  return selected === incoming.workflow ? incoming : current;
}

function PendingWorkflowCanvas() {
  const { t } = useI18n();
  return (
    <div className="flex h-full min-h-0 items-center justify-center bg-zinc-50 p-8 text-center text-sm text-zinc-500 dark:bg-[#080c12] dark:text-slate-400">
      <div className="flex max-w-sm flex-col items-center gap-3">
        <Workflow size={24} className="text-zinc-400 dark:text-slate-500" />
        <span>{t("agentWorkbench.canvasPending")}</span>
      </div>
    </div>
  );
}

function SidebarError({
  message,
  closeLabel,
  onClose,
}: {
  message: string;
  closeLabel: string;
  onClose: () => void;
}) {
  return (
    <div role="alert" className="flex shrink-0 items-start gap-2 border-b border-red-200 bg-red-50 px-3 py-2 text-xs leading-5 text-red-700 dark:border-red-400/20 dark:bg-red-500/10 dark:text-red-200">
      <CircleAlert size={14} className="mt-0.5 shrink-0" />
      <span className="min-w-0 flex-1">{message}</span>
      <button
        type="button"
        onClick={onClose}
        className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md hover:bg-red-100 dark:hover:bg-red-500/10"
        aria-label={closeLabel}
        title={closeLabel}
      >
        <X size={13} />
      </button>
    </div>
  );
}

function newRecipeIdempotencyKey(recipeId: string): string {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `recipe-${recipeId}-${suffix}`.slice(0, 120);
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}
