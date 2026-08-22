import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Boxes, CircleAlert, CircleDot, Eye, Images, Plus, X } from "lucide-react";
import { useCallback, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { GalleryImagePreviewDialog } from "../../../components/GalleryImagePreviewDialog";
import { openGlobalAgent } from "../../../components/GlobalAgentDock";
import { TopNav } from "../../../components/TopNav";
import { useRegisterAgentPageContext } from "../../../lib/agentPageContext";
import { ApiError, api } from "../../../lib/api";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type {
  AgentPageContextSnapshotInput,
  AgentWorkbenchBootstrap,
  GraphProjection,
  WorkflowDraft,
  WorkflowDraftRevision,
  WorkflowRecipe,
  WorkflowRecipeApplicationResult,
  WorkflowRecipeSummary,
} from "../../../lib/types";
import { inspectableGraphNodeId } from "../canvas/graphCatalog";
import { GraphAddNodePanel } from "../canvas/GraphAddNodePanel";
import { GraphCanvasPanel, type GraphCanvasActions } from "../canvas/GraphCanvasPanel";
import { GraphLibraryPanel } from "../canvas/GraphLibraryPanel";
import { GraphNodeInspector } from "../canvas/GraphNodeInspector";
import { GraphRunsPanel } from "../canvas/GraphRunsPanel";
import { RecipeLibraryPanel } from "../canvas/RecipeLibraryPanel";
import { AgentConversationPanel } from "./AgentConversationPanel";
import {
  AgentWorkbenchShell,
  type AgentWorkbenchSidebarTool,
} from "./AgentWorkbenchShell";
import { isHttpErrorStatus, readWorkflowGraphOrNull } from "./productWorkbenchRoute";
import { useWorkflowMaterialization } from "./useWorkflowMaterialization";
import { WorkflowDraftConfirmation } from "./WorkflowDraftConfirmation";
import { WorkflowOnboardingHero } from "./WorkflowOnboardingHero";

export type AgentWorkbenchPageBootstrap = AgentWorkbenchBootstrap;
type AgentSidebarToolId = "agent" | "add" | "details" | "runs" | "library" | "recipes";

const EMPTY_ACTIONS: GraphCanvasActions = {
  createNode: () => undefined,
  createShot: () => undefined,
  duplicateSelected: () => undefined,
  groupSelected: () => undefined,
  dissolveSelected: () => undefined,
  saveRecipe: () => undefined,
  appendRecipe: () => undefined,
  commitNode: async () => undefined,
};

interface AgentProductWorkbenchPageProps {
  bootstrap: AgentWorkbenchPageBootstrap;
  agentTaskId?: string | null;
  onRefetchBootstrap: () => Promise<unknown>;
}

export function AgentProductWorkbenchPage({
  bootstrap,
  agentTaskId = null,
  onRefetchBootstrap,
}: AgentProductWorkbenchPageProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [materialization, setMaterialization] = useState<GraphProjection | null>(null);
  const [dismissedRevisionId, setDismissedRevisionId] = useState<string | null>(null);
  const [conflictDetected, setConflictDetected] = useState(false);
  const [sidebarTool, setSidebarTool] = useState<AgentSidebarToolId>("agent");
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>([]);
  const [actions, setActions] = useState<GraphCanvasActions>(EMPTY_ACTIONS);
  const [bindNodeId, setBindNodeId] = useState<string | null>(null);
  const [archiveRecipe, setArchiveRecipe] = useState<WorkflowRecipeSummary | null>(null);
  const [recipeApplication, setRecipeApplication] = useState<WorkflowRecipeApplicationResult | null>(null);
  const [recipeError, setRecipeError] = useState<string | null>(null);
  const [previewImage, setPreviewImage] = useState<DownloadableImage | null>(null);
  const [canvasBusy, setCanvasBusy] = useState(false);
  const [chromeCollapsed, setChromeCollapsed] = useState(false);
  const recipeApplyKeysRef = useRef(new Map<string, string>());
  const sidebarToolRef = useRef<AgentSidebarToolId>(sidebarTool);
  const flushInspectorRef = useRef<() => Promise<void>>(async () => undefined);
  sidebarToolRef.current = sidebarTool;
  const registerInspectorFlush = useCallback((flush: () => Promise<void>) => {
    flushInspectorRef.current = flush;
  }, []);
  const beforeRun = useCallback(() => flushInspectorRef.current(), []);

  const catalogQuery = useQuery({
    queryKey: ["graph-node-catalog"],
    queryFn: () => api.getGraphNodeCatalog(),
    staleTime: Infinity,
  });
  const graphQuery = useQuery({
    queryKey: ["workflow-graph", bootstrap.product.id],
    queryFn: () => readWorkflowGraphOrNull(() => api.getCurrentWorkflowGraph(bootstrap.product.id)),
    initialData: bootstrap.graph ?? undefined,
    retry: (failureCount, error) => !isHttpErrorStatus(error, 404) && failureCount < 2,
  });
  const liveGraph = graphQuery.data ?? materialization ?? bootstrap.graph;
  const catalog = catalogQuery.data ?? null;
  const catalogError = catalogQuery.error
    ? errorDetail(catalogQuery.error, t("graph.inspector.catalogLoadFailed"))
    : null;
  const workflowAvailable = Boolean(liveGraph);
  const selected = liveGraph?.nodes.find((node) => node.id === selectedNodeIds[0]) ?? null;
  const bindNode = bindNodeId
    ? liveGraph?.nodes.find((node) => node.id === bindNodeId && node.node_type === "image_asset") ?? null
    : null;
  const activeSidebarTool = resolveAgentWorkbenchSidebarTool(sidebarTool, workflowAvailable);
  const pageContext = useMemo<AgentPageContextSnapshotInput>(() => ({
    route: `${location.pathname}${location.search}`,
    page_type: "product_workbench",
    product_id: bootstrap.product.id,
    workflow_id: liveGraph?.id ?? null,
    selected_asset_ids: [],
    visible_asset_ids: [],
    filters: {
      sidebar: activeSidebarTool,
      ...(selectedNodeIds.length ? { selected_node_ids: selectedNodeIds.slice(0, 20).join(",") } : {}),
    },
    workflow_revision: liveGraph?.revision ?? null,
    library_revision: null,
    captured_at: new Date().toISOString(),
  }), [activeSidebarTool, bootstrap.product.id, liveGraph?.id, liveGraph?.revision, location.pathname, location.search, selectedNodeIds]);
  useRegisterAgentPageContext(pageContext);

  const recipesQuery = useQuery({
    queryKey: ["workflow-recipes", false],
    queryFn: () => api.listWorkflowRecipes(false),
    enabled: activeSidebarTool === "recipes",
  });
  const reviewableRevision = selectReviewableWorkflowRevision(bootstrap.workflow_draft, liveGraph);
  const confirmationOpen = Boolean(
    reviewableRevision && dismissedRevisionId !== reviewableRevision.id,
  );

  const updateBootstrapDraft = useCallback((draft: WorkflowDraft) => {
    queryClient.setQueriesData<AgentWorkbenchBootstrap>(
      { queryKey: ["agent-workbench", bootstrap.product.id] },
      (current) => current?.mode === "agent"
        ? { ...current, workflow_draft: draft }
        : current,
    );
  }, [bootstrap.product.id, queryClient]);

  const materializationMutation = useWorkflowMaterialization({
    productId: bootstrap.product.id,
    draftId: bootstrap.workflow_draft.id,
    conversationId: bootstrap.conversation.id,
    onDraftConfirmed: updateBootstrapDraft,
    onConflict: async () => {
      await onRefetchBootstrap();
      setConflictDetected(true);
      setDismissedRevisionId(null);
    },
    onMaterialized: (result) => {
      setMaterialization(result);
      setConflictDetected(false);
      setDismissedRevisionId(result.source_draft_revision_id);
    },
  });

  const recipeMutation = useMutation({
    mutationFn: async (operation: {
      kind: "apply" | "archive";
      recipe: WorkflowRecipeSummary;
      idempotencyKey?: string;
    }): Promise<WorkflowRecipe | WorkflowRecipeApplicationResult> => {
      if (operation.kind === "apply") {
        return api.applyWorkflowRecipe(bootstrap.product.id, operation.recipe.id, {
          expected_recipe_version: operation.recipe.current_version.version,
          idempotency_key: operation.idempotencyKey ?? newRecipeIdempotencyKey(operation.recipe.id),
        });
      }
      const result = await api.archiveWorkflowRecipe(
        operation.recipe.id,
        operation.recipe.current_version.version,
      );
      return result.recipe;
    },
    onMutate: () => setRecipeError(null),
    onSuccess: async (result, operation) => {
      if (operation.kind === "apply") {
        const application = result as WorkflowRecipeApplicationResult;
        recipeApplyKeysRef.current.delete(operation.recipe.id);
        setRecipeApplication(application);
        queryClient.setQueriesData(
          { queryKey: ["workflow-graph", bootstrap.product.id] },
          application.graph,
        );
        await queryClient.invalidateQueries({ queryKey: ["workflow-graph", bootstrap.product.id] });
        await onRefetchBootstrap();
      } else {
        await queryClient.invalidateQueries({ queryKey: ["workflow-recipes"] });
      }
      setArchiveRecipe(null);
    },
    onError: (error) => {
      setRecipeError(errorDetail(error, t("workbench.error.recipe")));
    },
  });

  const requestSidebarTool = useCallback(async (tool: AgentSidebarToolId): Promise<boolean> => {
    try {
      await flushInspectorRef.current();
    } catch {
      return false;
    }
    sidebarToolRef.current = tool;
    setSidebarTool(tool);
    return true;
  }, []);
  const inspectNode = useCallback((nodeId: string) => {
    if (!liveGraph || !inspectableGraphNodeId(liveGraph, nodeId)) return;
    void (async () => {
      try {
        await flushInspectorRef.current();
      } catch {
        return;
      }
      setSelectedNodeIds([nodeId]);
      void requestSidebarTool("details");
    })();
  }, [liveGraph, requestSidebarTool]);
  const selectCanvasNodes = useCallback(async (nodeIds: string[]) => {
    try {
      await flushInspectorRef.current();
    } catch {
      return;
    }
    setSelectedNodeIds(nodeIds);
    if (nodeIds.length === 1 && liveGraph && inspectableGraphNodeId(liveGraph, nodeIds[0])) {
      void requestSidebarTool("details");
    }
  }, [liveGraph, requestSidebarTool]);

  const confirmRevision = (revision: WorkflowDraftRevision) => {
    if (materializationMutation.isPending) return;
    setConflictDetected(false);
    materializationMutation.mutate({
      revision,
      expectedWorkflowRevision: liveGraph?.revision ?? 0,
    });
  };
  const materializationError = materializationMutation.error
    ? errorDetail(materializationMutation.error, t("agentWorkbench.materializationFailed"))
    : null;

  const graphTools: AgentWorkbenchSidebarTool[] = liveGraph ? [
    {
      id: "add",
      label: t("graph.palette.title"),
      icon: <Plus size={17} />,
      content: (
        <GraphAddNodePanel
          catalog={catalog}
          busy={canvasBusy}
          onCreate={actions.createNode}
          onCreateShot={actions.createShot}
          canDuplicate={selectedNodeIds.length > 0}
          canGroup={selectedNodeIds.length > 1}
          canDissolve={liveGraph.nodes.some((node) => selectedNodeIds.includes(node.id) && Boolean(node.group_id))}
          onDuplicate={actions.duplicateSelected}
          onGroup={actions.groupSelected}
          onDissolve={actions.dissolveSelected}
          canSaveFull={liveGraph.nodes.length > 0}
          canSaveGroup={liveGraph.groups.length > 0}
          canSaveSelection={selectedNodeIds.length > 0}
          onSaveFull={() => actions.saveRecipe("workflow")}
          onSaveGroup={() => actions.saveRecipe("group")}
          onSaveSelection={() => actions.saveRecipe("selection")}
          onOpenRecipesTab={() => {
            void requestSidebarTool("recipes");
          }}
        />
      ),
    },
    {
      id: "details",
      label: t("graph.inspector.title"),
      icon: <Eye size={17} />,
      content: (
        <GraphNodeInspector
          graph={liveGraph}
          node={selected}
          product={bootstrap.product}
          catalog={catalog}
          catalogError={catalogError}
          onRetryCatalog={() => void catalogQuery.refetch()}
          busy={canvasBusy}
          onRegisterFlush={registerInspectorFlush}
          onCommit={async (input) => {
            if (!selected) return;
            return actions.commitNode({ nodeId: selected.id, ...input });
          }}
          onBind={selected?.node_type === "image_asset" ? () => {
            setBindNodeId(selected.id);
            void requestSidebarTool("library");
          } : undefined}
          onJump={inspectNode}
          onPreviewImage={setPreviewImage}
          onOpenAdd={() => void requestSidebarTool("add")}
          onOpenLibrary={() => void requestSidebarTool("library")}
        />
      ),
    },
    {
      id: "runs",
      label: t("graph.runs.title"),
      icon: <CircleDot size={17} />,
      content: (
        <GraphRunsPanel
          productId={bootstrap.product.id}
          graph={liveGraph}
          selectedNodeId={selected?.id ?? null}
          structureBusy={canvasBusy}
          onBeforeRun={beforeRun}
          onJump={inspectNode}
          onPreviewImage={setPreviewImage}
        />
      ),
    },
    {
      id: "library",
      label: t("workbench.sidebar.library"),
      icon: <Images size={17} />,
      contentClassName: "flex min-h-0 flex-1 flex-col overflow-hidden",
      content: (
        <GraphLibraryPanel
          product={bootstrap.product}
          graph={liveGraph}
          bindNode={bindNode}
          bindLocked={canvasBusy}
          onPreviewImage={setPreviewImage}
          onBindAsset={async (assetId) => {
            if (!bindNode) return;
            return actions.commitNode({
              nodeId: bindNode.id,
              config: bindNode.config,
              boundAssetId: assetId,
            });
          }}
          onBound={() => setBindNodeId(null)}
        />
      ),
    },
  ] : [];

  const sidebarTools: AgentWorkbenchSidebarTool[] = [
    ...graphTools,
    {
      id: "recipes",
      label: t("workbench.sidebar.recipes"),
      icon: <Boxes size={17} />,
      contentClassName: "flex min-h-0 flex-1 flex-col overflow-hidden",
      content: (
        <>
          {recipeError ? (
            <SidebarError
              message={recipeError}
              closeLabel={t("workbench.dialog.close")}
              onClose={() => setRecipeError(null)}
            />
          ) : null}
          <div className="min-h-0 flex-1 overflow-y-auto">
            <RecipeLibraryPanel
              recipes={recipesQuery.data ?? []}
              loading={recipesQuery.isLoading}
              error={recipesQuery.isError
                ? errorDetail(recipesQuery.error, t("workbench.error.recipes"))
                : null}
              operationRecipeId={recipeMutation.isPending && recipeMutation.variables?.kind === "apply"
                ? recipeMutation.variables.recipe.id
                : null}
              application={recipeApplication}
              structureBusy={canvasBusy}
              canAppend={() => Boolean(liveGraph)}
              onRetry={() => void recipesQuery.refetch()}
              onPreview={(recipe) => api.previewWorkflowRecipe(bootstrap.product.id, recipe.id, {
                expected_recipe_version: recipe.current_version.version,
              })}
              onApply={(recipe) => {
                const idempotencyKey = recipeApplyKeysRef.current.get(recipe.id)
                  ?? newRecipeIdempotencyKey(recipe.id);
                recipeApplyKeysRef.current.set(recipe.id, idempotencyKey);
                recipeMutation.mutate({ kind: "apply", recipe, idempotencyKey });
              }}
              onAppend={(recipe) => {
                actions.appendRecipe({
                  id: recipe.id,
                  version: recipe.current_version.version,
                  title: recipe.current_version.title,
                  description: recipe.current_version.description,
                });
              }}
              onArchive={setArchiveRecipe}
            />
          </div>
        </>
      ),
    },
  ];

  return (
    <div className="flex h-dvh min-h-[560px] flex-col overflow-hidden bg-white text-zinc-950 dark:bg-[#060a12] dark:text-slate-100">
      {chromeCollapsed ? null : (
        <TopNav
          breadcrumbs={`${bootstrap.product.name} / ${t("agentWorkbench.breadcrumb")}`}
          onHome={() => navigate("/products")}
        />
      )}
      <AgentWorkbenchShell
        workflowAvailable={workflowAvailable}
        activeSidebarTool={activeSidebarTool}
        onSidebarToolChange={(toolId) => requestSidebarTool(toolId as AgentSidebarToolId)}
        sidebarTools={sidebarTools}
        canvasContent={liveGraph ? (
          <GraphCanvasPanel
            productId={bootstrap.product.id}
            graph={liveGraph}
            catalog={catalog}
            selectedNodeIds={selectedNodeIds}
            onSelect={selectCanvasNodes}
            onGraphChange={(next) => queryClient.setQueryData(["workflow-graph", bootstrap.product.id], next)}
            onRegisterActions={setActions}
            onBusyChange={setCanvasBusy}
            onBeforeRun={beforeRun}
            chromeCollapsed={chromeCollapsed}
            onToggleChrome={() => setChromeCollapsed((current) => !current)}
            onBindNode={(nodeId) => {
              setBindNodeId(nodeId);
              void requestSidebarTool("library");
            }}
          />
        ) : (
          <WorkflowOnboardingHero
            productName={bootstrap.product.name}
            onOpenAgent={() => {
              const composer = document.querySelector<HTMLTextAreaElement>("[data-agent-composer] textarea");
              composer?.focus();
            }}
            onOpenRecipes={() => {
              void requestSidebarTool("recipes");
            }}
            onOpenAddPanel={() => {
              void requestSidebarTool("add");
            }}
          />
        )}
        agentContent={(
          <AgentConversationPanel
            productId={bootstrap.product.id}
            productName={bootstrap.product.name}
            conversation={bootstrap.conversation}
            workflowDraft={bootstrap.workflow_draft}
            graph={liveGraph}
            taskId={agentTaskId}
            pageContext={pageContext}
            reviewDraftAvailable={Boolean(reviewableRevision)}
            onReviewDraft={() => {
              setConflictDetected(false);
              setDismissedRevisionId(null);
            }}
            onOpenRuns={() => {
              void requestSidebarTool("runs");
            }}
            onExpandGlobalAgent={() => {
              openGlobalAgent({ tab: "chat", sessionId: bootstrap.conversation.session_id ?? undefined });
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

      <ConfirmDialog
        open={archiveRecipe !== null}
        title={t("workbench.recipe.archive")}
        description={t("workbench.recipe.archiveConfirm", {
          title: archiveRecipe?.current_version.title ?? "",
        })}
        confirmLabel={t("workbench.recipe.archive")}
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
          body={t("workbench.preview.body")}
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
  graph: { source_draft_revision_id: string | null } | null,
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
    graph?.source_draft_revision_id !== revision.id
  ) {
    return revision;
  }
  return null;
}

export function resolveAgentWorkbenchSidebarTool(
  requested: AgentSidebarToolId,
  workflowAvailable: boolean,
): AgentSidebarToolId {
  if (requested === "recipes" || requested === "agent") {
    return requested;
  }
  return workflowAvailable ? requested : "agent";
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
