/**
 * 商品工作台的唯一编排入口：live 图始终由用户直接操作，Agent 对话按商品工作区可选挂载。
 * Agent 用 ChangeSet 协作，不能提交另一份 Draft 覆盖 live 图。
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Boxes, CircleAlert, CircleDot, Eye, Images, Plus, RotateCw, Sparkles, X } from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { GalleryImagePreviewDialog } from "../../../components/GalleryImagePreviewDialog";
import { openGlobalAgent } from "../../../lib/globalAgentEvents";
import { TopNav } from "../../../components/TopNav";
import { Button } from "../../../components/ui/button";
import { useRegisterAgentPageContext } from "../../../lib/agentPageContext";
import { ApiError, api } from "../../../lib/api";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type {
  AgentPageContextSnapshotInput,
  AgentWorkbenchBootstrap,
  CanonicalProductDetail,
  GraphProjection,
  WorkflowRecipe,
  WorkflowRecipeApplicationResult,
  WorkflowRecipePreview,
  WorkflowRecipeSummary,
} from "../../../lib/types";
import { inspectableGraphNodeId } from "../canvas/graphCatalog";
import { GraphCanvasPanel, type GraphCanvasActions, type GraphCanvasCommitNodeInput, type WorkbenchMainView } from "../canvas/GraphCanvasPanel";
import { resolveWorkbenchMainView } from "../canvas/resultProjection";

const GraphAddNodePanel = lazy(() =>
  import("../canvas/GraphAddNodePanel").then((module) => ({ default: module.GraphAddNodePanel })),
);
const GraphLibraryPanel = lazy(() =>
  import("../canvas/GraphLibraryPanel").then((module) => ({ default: module.GraphLibraryPanel })),
);
const GraphNodeInspector = lazy(() =>
  import("../canvas/GraphNodeInspector").then((module) => ({ default: module.GraphNodeInspector })),
);
const GraphRunsPanel = lazy(() =>
  import("../canvas/GraphRunsPanel").then((module) => ({ default: module.GraphRunsPanel })),
);
const RecipeLibraryPanel = lazy(() =>
  import("../canvas/RecipeLibraryPanel").then((module) => ({ default: module.RecipeLibraryPanel })),
);
import {
  existingWorkbenchNodeIds,
  patchWorkbenchUiState,
  readWorkbenchUiState,
  sameWorkbenchIds,
  type WorkbenchSidebarToolId,
} from "../chrome/workbenchUiState";
import { useLocalImageEditController } from "../local-edit/LocalImageEditController";
import { AgentConversationPanel } from "./AgentConversationPanel";
import {
  AgentWorkbenchShell,
  type AgentWorkbenchSidebarTool,
} from "./AgentWorkbenchShell";
import { isHttpErrorStatus, readWorkflowGraphOrNull } from "./productWorkbenchRoute";
import { WorkflowOnboardingHero } from "./WorkflowOnboardingHero";


function SidebarPanelSuspense({ children }: { children: import("react").ReactNode }) {
  return <Suspense fallback={null}>{children}</Suspense>;
}

export type CanvasSelectionSource = "pointer" | "agent";

export function inspectorSaveToCommitNode(
  nodeId: string,
  input: {
    title: string;
    config: Record<string, unknown>;
    boundAssetId: string | null;
    expectedEditVersion: number;
  },
): GraphCanvasCommitNodeInput {
  return {
    nodeId,
    title: input.title,
    config: input.config,
    boundAssetId: input.boundAssetId,
    baseGraphRevision: input.expectedEditVersion,
  };
}

export function shouldOpenInspectorForCanvasSelection(
  source: CanvasSelectionSource,
  nodeCount: number,
  inspectable: boolean,
): boolean {
  return source === "pointer" && nodeCount === 1 && inspectable;
}

export type AgentWorkbenchPageBootstrap = AgentWorkbenchBootstrap;
type AgentSidebarToolId = WorkbenchSidebarToolId;

const EMPTY_ACTIONS: GraphCanvasActions = {
  createNode: () => undefined,
  createShot: () => undefined,
  duplicateSelected: () => undefined,
  groupSelected: () => undefined,
  dissolveSelected: () => undefined,
  saveRecipe: () => undefined,
  appendRecipe: () => undefined,
  commitNode: async () => undefined,
  pinCurrentOutput: () => undefined,
  previewRun: () => undefined,
  hideRunPreview: () => undefined,
  focusNodes: () => undefined,
};

interface ProductWorkbenchSurfaceProps {
  product: CanonicalProductDetail;
  initialGraph: GraphProjection | null;
  bootstrap?: AgentWorkbenchPageBootstrap | null;
  agentTaskId?: string | null;
  preferConversation?: boolean;
  agentError?: unknown;
  onRetryAgent?: () => void;
  onOpenConversation?: () => void;
  onRefetchBootstrap?: () => Promise<unknown>;
}

export function ProductWorkbenchSurface({
  product,
  initialGraph,
  bootstrap,
  agentTaskId = null,
  preferConversation = false,
  agentError = null,
  onRetryAgent,
  onOpenConversation,
  onRefetchBootstrap,
}: ProductWorkbenchSurfaceProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [sidebarTool, setSidebarTool] = useState<AgentSidebarToolId>(
    () => initialAgentWorkbenchSidebarTool({
      hasGraph: Boolean(initialGraph),
      hasTask: Boolean(agentTaskId),
      preferConversation,
      storedTool: readWorkbenchUiState(product.id).sidebarTool,
    }),
  );
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>(
    () => existingWorkbenchNodeIds(
      initialGraph,
      readWorkbenchUiState(product.id).selectedNodeIds,
    ),
  );
  const [actions, setActions] = useState<GraphCanvasActions>(EMPTY_ACTIONS);
  const [bindNodeId, setBindNodeId] = useState<string | null>(null);
  const [archiveRecipe, setArchiveRecipe] = useState<WorkflowRecipeSummary | null>(null);
  const [recipeApplication, setRecipeApplication] = useState<WorkflowRecipeApplicationResult | null>(null);
  const [recipeError, setRecipeError] = useState<string | null>(null);
  const [previewImage, setPreviewImage] = useState<DownloadableImage | null>(null);
  const [mainView, setMainViewState] = useState<WorkbenchMainView>(() => resolveWorkbenchMainView(
    initialGraph,
    readWorkbenchUiState(product.id).mainView,
  ));
  const setMainView = useCallback((view: WorkbenchMainView) => {
    setMainViewState(view);
    patchWorkbenchUiState(product.id, { mainView: view });
  }, [product.id]);
  const [canvasBusy, setCanvasBusy] = useState(false);
  const [agentEditing, setAgentEditing] = useState(false);
  const [chromeCollapsed, setChromeCollapsed] = useState(
    () => readWorkbenchUiState(product.id).chromeCollapsed === true,
  );
  const [emptyGraphError, setEmptyGraphError] = useState<string | null>(null);
  const [agentOpenRequest, setAgentOpenRequest] = useState(preferConversation ? 1 : 0);
  const recipeApplyKeysRef = useRef(new Map<string, string>());
  const emptyGraphInFlightRef = useRef(false);
  const sidebarToolRef = useRef<AgentSidebarToolId>(sidebarTool);
  const flushInspectorRef = useRef<() => Promise<void>>(async () => undefined);
  sidebarToolRef.current = sidebarTool;
  const registerInspectorFlush = useCallback((flush: () => Promise<void>) => {
    flushInspectorRef.current = flush;
  }, []);
  const beforeRun = useCallback(() => flushInspectorRef.current(), []);
  const onAgentPresenceChange = useCallback((editing: boolean) => setAgentEditing(editing), []);

  const catalogQuery = useQuery({
    queryKey: ["graph-node-catalog"],
    queryFn: () => api.getGraphNodeCatalog(),
    staleTime: Infinity,
  });
  const graphQuery = useQuery({
    queryKey: ["workflow-graph", product.id],
    queryFn: () => readWorkflowGraphOrNull(() => api.getCurrentWorkflowGraph(product.id)),
    initialData: initialGraph ?? undefined,
    staleTime: 30_000,
    retry: (failureCount, error) => !isHttpErrorStatus(error, 404) && failureCount < 2,
  });
  const liveGraph = graphQuery.data ?? initialGraph;
  const localEdit = useLocalImageEditController({
    productId: product.id,
    graphId: liveGraph?.id ?? null,
  });
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
  useEffect(() => {
    setSelectedNodeIds((current) => {
      const next = existingWorkbenchNodeIds(liveGraph, current);
      return sameWorkbenchIds(current, next) ? current : next;
    });
  }, [liveGraph]);
  useEffect(() => {
    patchWorkbenchUiState(product.id, {
      sidebarTool: activeSidebarTool,
      selectedNodeIds,
      chromeCollapsed,
    });
  }, [activeSidebarTool, chromeCollapsed, product.id, selectedNodeIds]);
  const pageContext = useMemo<AgentPageContextSnapshotInput>(() => ({
    route: `${location.pathname}${location.search}`,
    page_type: "product_workbench",
    product_id: product.id,
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
  }), [activeSidebarTool, liveGraph?.id, liveGraph?.revision, location.pathname, location.search, product.id, selectedNodeIds]);
  useRegisterAgentPageContext(pageContext);

  const recipesQuery = useQuery({
    queryKey: ["workflow-recipes", false],
    queryFn: () => api.listWorkflowRecipes(false),
    enabled: activeSidebarTool === "recipes",
  });
  const createEmptyGraphMutation = useMutation({
    mutationFn: () => createOrLoadEmptyWorkflowGraph({
      create: () => api.createEmptyWorkflowGraph(product.id),
      loadCurrent: () => api.getCurrentWorkflowGraph(product.id),
      isConflict: (error) => isHttpErrorStatus(error, 409),
    }),
    onMutate: () => setEmptyGraphError(null),
    onSuccess: (graph) => {
      queryClient.setQueryData(["workflow-graph", product.id], graph);
    },
  });

  const recipeMutation = useMutation({
    mutationFn: async (operation: {
      kind: "apply" | "archive";
      recipe: WorkflowRecipeSummary;
      preview?: WorkflowRecipePreview;
      idempotencyKey?: string;
    }): Promise<WorkflowRecipe | WorkflowRecipeApplicationResult> => {
      if (operation.kind === "apply") {
        if (!operation.preview) {
          throw new Error("缺少配方变更预览");
        }
        return api.applyWorkflowRecipe(
          product.id,
          operation.recipe.id,
          buildAgentWorkflowRecipeApplyInput(
            operation.recipe,
            operation.preview,
            operation.idempotencyKey ?? newRecipeIdempotencyKey(operation.recipe.id),
          ),
        );
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
          { queryKey: ["workflow-graph", product.id] },
          application.graph,
        );
        await queryClient.invalidateQueries({ queryKey: ["workflow-graph", product.id] });
        await onRefetchBootstrap?.();
      } else {
        await queryClient.invalidateQueries({ queryKey: ["workflow-recipes"] });
      }
      setArchiveRecipe(null);
    },
    onError: (error, operation) => {
      clearAgentWorkflowRecipeIdempotencyKey(recipeApplyKeysRef.current, operation.kind, operation.recipe.id);
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
  const requestAgentOpen = useCallback(() => {
    if (!bootstrap) {
      onOpenConversation?.();
      return;
    }
    requestAgentWorkbenchOpen(
      (tool) => {
        sidebarToolRef.current = tool;
        setSidebarTool(tool);
      },
      () => setAgentOpenRequest((request) => request + 1),
    );
  }, [bootstrap, onOpenConversation]);
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
      actions.focusNodes([nodeId]);
    })();
  }, [actions, liveGraph, requestSidebarTool]);
  const selectCanvasNodes = useCallback(async (nodeIds: string[], source: CanvasSelectionSource = "pointer") => {
    try {
      await flushInspectorRef.current();
    } catch {
      return;
    }
    setSelectedNodeIds(nodeIds);
    if (source === "agent" && nodeIds.length) {
      actions.focusNodes(nodeIds);
    }
    const inspectable = Boolean(
      liveGraph && nodeIds[0] && inspectableGraphNodeId(liveGraph, nodeIds[0]),
    );
    if (shouldOpenInspectorForCanvasSelection(source, nodeIds.length, inspectable)) {
      void requestSidebarTool("details");
    }
  }, [actions, liveGraph, requestSidebarTool]);



  const graphTools: AgentWorkbenchSidebarTool[] = liveGraph ? [
    {
      id: "add",
      label: t("graph.palette.title"),
      icon: <Plus size={17} />,
      content: (
        <SidebarPanelSuspense>
        <GraphAddNodePanel
          catalog={catalog}
          catalogError={catalogError}
          onRetryCatalog={() => void catalogQuery.refetch()}
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
        </SidebarPanelSuspense>
      ),
    },
    {
      id: "details",
      label: t("graph.inspector.title"),
      icon: <Eye size={17} />,
      content: (
        <SidebarPanelSuspense>
        <GraphNodeInspector
          graph={liveGraph}
          node={selected}
          product={product}
          catalog={catalog}
          catalogError={catalogError}
          onRetryCatalog={() => void catalogQuery.refetch()}
          busy={canvasBusy}
          onRegisterFlush={registerInspectorFlush}
          onCommit={async (input) => {
            if (!selected) return;
            return actions.commitNode(inspectorSaveToCommitNode(selected.id, input));
          }}
          onBind={selected?.node_type === "image_asset" ? () => {
            setBindNodeId(selected.id);
            void requestSidebarTool("library");
          } : undefined}
          onPinAsset={selected?.node_type === "image_generation" && selected.preview_asset_id
            ? () => actions.pinCurrentOutput(selected.id)
            : undefined}
          onJump={inspectNode}
          onPreviewImage={setPreviewImage}
          onOpenLocalEdit={localEdit.openLocalImageEdit}
          onOpenAdd={() => void requestSidebarTool("add")}
          onOpenLibrary={() => void requestSidebarTool("library")}
          onPreviewRun={actions.previewRun}
          onHideRunPreview={actions.hideRunPreview}
        />
        </SidebarPanelSuspense>
      ),
    },
    {
      id: "runs",
      label: t("graph.runs.title"),
      icon: <CircleDot size={17} />,
      content: (
        <SidebarPanelSuspense>
        <GraphRunsPanel
          productId={product.id}
          graph={liveGraph}
          selectedNodeId={selected?.id ?? null}
          structureBusy={canvasBusy}
          onBeforeRun={beforeRun}
          onJump={inspectNode}
          onPreviewImage={setPreviewImage}
          onPreviewRun={actions.previewRun}
          onHideRunPreview={actions.hideRunPreview}
        />
        </SidebarPanelSuspense>
      ),
    },
    {
      id: "library",
      label: t("workbench.sidebar.library"),
      icon: <Images size={17} />,
      contentClassName: "flex min-h-0 flex-1 flex-col overflow-hidden",
      content: (
        <SidebarPanelSuspense>
        <GraphLibraryPanel
          product={product}
          graph={liveGraph}
          bindNode={bindNode}
          bindLocked={canvasBusy}
          onPreviewImage={setPreviewImage}
          onOpenLocalEdit={localEdit.openLocalImageEdit}
          onBindAsset={async (assetId) => {
            if (!bindNode) return;
            return actions.commitNode({
              nodeId: bindNode.id,
              config: bindNode.config,
              boundAssetId: assetId,
              baseGraphRevision: liveGraph.revision,
            });
          }}
          onBound={() => setBindNodeId(null)}
        />
        </SidebarPanelSuspense>
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
            <SidebarPanelSuspense>
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
              onPreview={(recipe) => api.previewWorkflowRecipe(product.id, recipe.id, {
                expected_recipe_version: recipe.current_version.version,
              })}
              onApply={(recipe, preview) => {
                const idempotencyKey = recipeApplyKeysRef.current.get(recipe.id)
                  ?? newRecipeIdempotencyKey(recipe.id);
                recipeApplyKeysRef.current.set(recipe.id, idempotencyKey);
                recipeMutation.mutate({ kind: "apply", recipe, preview, idempotencyKey });
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
            </SidebarPanelSuspense>
          </div>
        </>
      ),
    },
  ];

  return (
    <div className="flex h-dvh min-h-[560px] flex-col overflow-hidden bg-surface-base text-text-primary">
      {chromeCollapsed ? null : (
        <TopNav
          breadcrumbs={`${product.name} / ${t("agentWorkbench.breadcrumb")}`}
          onHome={() => navigate("/home")}
        />
      )}
      <AgentWorkbenchShell
        productId={product.id}
        workflowAvailable={workflowAvailable}
        activeSidebarTool={activeSidebarTool}
        onSidebarToolChange={(toolId) => requestSidebarTool(toolId as AgentSidebarToolId)}
        agentOpenRequest={agentOpenRequest}
        onAgentOpened={focusVisibleAgentComposer}
        sidebarTools={sidebarTools}
        canvasContent={liveGraph ? (
          <GraphCanvasPanel
            productId={product.id}
            graph={liveGraph}
            catalog={catalog}
            selectedNodeIds={selectedNodeIds}
            onSelect={selectCanvasNodes}
            onGraphChange={(next) => queryClient.setQueryData(["workflow-graph", product.id], next)}
            onRegisterActions={setActions}
            onBusyChange={setCanvasBusy}
            onBeforeRun={beforeRun}
            agentEditing={agentEditing}
            onOpenLocalEdit={localEdit.openLocalImageEdit}
            onPreviewImage={(assetId, alt) => setPreviewImage({
              previewUrl: api.getProductImageAssetMediaUrl(assetId, "preview"),
              downloadUrl: api.getProductImageAssetMediaUrl(assetId),
              filename: alt,
              alt,
            })}
            mainView={mainView}
            onMainViewChange={setMainView}
            chromeCollapsed={chromeCollapsed}
            onToggleChrome={() => setChromeCollapsed((current) => !current)}
            onBindNode={(nodeId) => {
              setBindNodeId(nodeId);
              void requestSidebarTool("library");
            }}
          />
        ) : (
          <div className="relative h-full min-h-0">
            {emptyGraphError ? (
              <p role="alert" className="absolute inset-x-4 top-4 z-10 rounded-lg border border-state-error/30 bg-state-error-soft px-3 py-2 text-xs leading-5 text-state-error">
                {emptyGraphError}
              </p>
            ) : null}
            <WorkflowOnboardingHero
              productName={product.name}
              onOpenAgent={requestAgentOpen}
              onOpenRecipes={() => {
                void requestSidebarTool("recipes");
              }}
              onOpenAddPanel={() => {
                if (emptyGraphInFlightRef.current || createEmptyGraphMutation.isPending) return;
                emptyGraphInFlightRef.current = true;
                void startEmptyCanvasAdd({
                  hasGraph: Boolean(liveGraph),
                  createGraph: () => createEmptyGraphMutation.mutateAsync(),
                  openAdd: async () => {
                    sidebarToolRef.current = "add";
                    setSidebarTool("add");
                    return true;
                  },
                }).catch((error) => {
                  setEmptyGraphError(errorDetail(error, t("workbench.error.structure")));
                }).finally(() => {
                  emptyGraphInFlightRef.current = false;
                });
              }}
            />
          </div>
        )}
        agentContent={bootstrap ? (
          <AgentConversationPanel
            key={bootstrap.conversation.id}
            productId={product.id}
            productName={product.name}
            conversation={bootstrap.conversation}
            graph={liveGraph}
            taskId={agentTaskId}
            pageContext={pageContext}
            onOpenRuns={() => {
              void requestSidebarTool("runs");
            }}
            onCanvasFocus={(nodeIds) => {
              void selectCanvasNodes(nodeIds, "agent");
            }}
            onAgentPresenceChange={onAgentPresenceChange}
            onExpandGlobalAgent={() => {
              openGlobalAgent({ tab: "chat", sessionId: bootstrap.conversation.session_id ?? undefined });
            }}
            className="h-full"
          />
        ) : (
          <GraphAgentPanel
            error={agentError}
            onRetry={onRetryAgent}
            onOpenConversation={onOpenConversation}
          />
        )}
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
      {localEdit.dialog}
    </div>
  );
}

export function GraphAgentPanel({
  error = null,
  onRetry,
  onOpenConversation,
}: {
  error?: unknown;
  onRetry?: () => void;
  onOpenConversation?: () => void;
}) {
  const { t } = useI18n();
  const missingWorkspace = error instanceof ApiError && error.status === 409;
  const message = missingWorkspace
    ? t("graph.workbench.agentOptional")
    : error instanceof ApiError
      ? error.detail
      : error instanceof Error
        ? error.message
        : t("graph.workbench.agentUnavailable");
  return (
    <section
      data-graph-agent-panel
      className="flex h-full min-h-0 flex-col overflow-hidden bg-surface-base text-text-primary"
    >
      <header className="flex min-h-14 shrink-0 items-center gap-3 border-b border-border-l1 bg-surface-raised/90 px-4 py-2.5">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent">
          <Bot size={18} />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-sm font-semibold text-text-primary">{t("agentWorkbench.agent")}</h2>
          <p className="truncate text-xs text-text-secondary">
            {t(missingWorkspace ? "graph.workbench.agentStartHeader" : "graph.workbench.agentOptional")}
          </p>
        </div>
      </header>
      {missingWorkspace && onOpenConversation ? (
        <div className="agent-start-state min-h-0 flex-1 overflow-y-auto px-5 py-6 sm:px-6 sm:py-8">
          <div className="mx-auto flex min-h-full w-full max-w-[30rem] flex-col justify-center">
            <AgentStartPreview />
            <div className="agent-start-copy mt-7">
              <p className="flex items-center gap-1.5 text-[11px] font-semibold text-accent">
                <Sparkles size={13} aria-hidden="true" />
                {t("graph.workbench.agentStartEyebrow")}
              </p>
              <h3 className="mt-2 max-w-[22rem] text-xl font-semibold leading-7 text-text-primary">
                {t("graph.workbench.agentStartTitle")}
              </h3>
              <p role="status" className="mt-2 max-w-[26rem] text-sm leading-6 text-text-secondary">
                {t("graph.workbench.agentStartDescription")}
              </p>
              <Button
                variant="primary"
                size="lg"
                data-open-canvas-conversation
                onClick={onOpenConversation}
                className="mt-5 shadow-elev-2"
              >
                <Bot size={15} aria-hidden="true" />
                {t("graph.workbench.openConversation")}
              </Button>
            </div>
          </div>
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col items-start justify-center gap-3 p-4">
          <p role="status" className="text-sm leading-6 text-text-secondary">{message}</p>
          {onRetry ? (
            <Button variant="secondary" size="lg" onClick={onRetry}>
              <RotateCw size={15} />
              {t("graph.workbench.agentRetry")}
            </Button>
          ) : null}
        </div>
      )}
    </section>
  );
}

function AgentStartPreview() {
  const previewImages = [
    { src: "/agent-onboarding/ceramic-detail.jpg", className: "agent-start-image agent-start-image-left" },
    { src: "/agent-onboarding/ceramic-feature.jpg", className: "agent-start-image agent-start-image-center" },
    { src: "/agent-onboarding/ceramic-craft.jpg", className: "agent-start-image agent-start-image-right" },
  ];

  return (
    <figure
      data-agent-start-preview
      aria-hidden="true"
      className="agent-start-preview relative mx-auto h-52 w-full max-w-[27rem] overflow-hidden rounded-lg border border-border-l1 bg-surface-subtle"
    >
      <div className="agent-start-grid absolute inset-0" />
      <div className="agent-start-beam absolute inset-y-0 left-0 w-1/3" />
      <div className="absolute inset-x-[12%] top-1/2 h-px bg-border-l3/80" />
      {previewImages.map((image) => (
        <div key={image.src} className={image.className}>
          <img src={image.src} alt="" loading="lazy" decoding="async" className="h-full w-full object-cover" />
        </div>
      ))}
      <div className="agent-start-node absolute bottom-4 left-1/2 flex h-10 w-10 -translate-x-1/2 items-center justify-center rounded-full border border-accent/30 bg-surface-raised text-accent shadow-elev-2">
        <Bot size={17} />
      </div>
    </figure>
  );
}

export function initialAgentWorkbenchSidebarTool(input: {
  hasGraph: boolean;
  hasTask: boolean;
  preferConversation?: boolean;
  storedTool?: AgentSidebarToolId | null;
}): AgentSidebarToolId {
  if (input.hasTask) return "agent";
  if (input.storedTool) {
    return resolveAgentWorkbenchSidebarTool(input.storedTool, input.hasGraph);
  }
  if (input.preferConversation) return "agent";
  return input.hasGraph ? "details" : "agent";
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

export function requestAgentWorkbenchOpen(
  setSidebarTool: (tool: AgentSidebarToolId) => void,
  requestOpen: () => void,
): void {
  setSidebarTool("agent");
  requestOpen();
}

export function focusVisibleAgentComposer(root?: ParentNode): boolean {
  const owner = root ?? (typeof document === "undefined" ? null : document);
  const composer = owner?.querySelector<HTMLTextAreaElement>("[data-agent-composer] textarea");
  if (!composer || composer.closest("[inert]")) {
    return false;
  }
  composer.focus();
  return true;
}

export async function startEmptyCanvasAdd(input: {
  hasGraph: boolean;
  createGraph: () => Promise<GraphProjection>;
  openAdd: () => void | Promise<boolean | void>;
}): Promise<"created" | "opened"> {
  if (!input.hasGraph) {
    await input.createGraph();
  }
  await input.openAdd();
  return input.hasGraph ? "opened" : "created";
}

/** 按商品幂等创建：409 表示 live 图已经存在。 */
export async function createOrLoadEmptyWorkflowGraph(input: {
  create: () => Promise<GraphProjection>;
  loadCurrent: () => Promise<GraphProjection>;
  isConflict: (error: unknown) => boolean;
}): Promise<GraphProjection> {
  try {
    return await input.create();
  } catch (error) {
    if (input.isConflict(error)) {
      return input.loadCurrent();
    }
    throw error;
  }
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
    <div role="alert" className="flex shrink-0 items-start gap-2 border-b border-state-error/30 bg-state-error-soft px-3 py-2 text-xs leading-5 text-state-error">
      <CircleAlert size={14} className="mt-0.5 shrink-0" />
      <span className="min-w-0 flex-1">{message}</span>
      <button
        type="button"
        onClick={onClose}
        className="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-md hover:bg-state-error-soft lg:h-8 lg:w-8"
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

export function buildAgentWorkflowRecipeApplyInput(
  recipe: WorkflowRecipeSummary,
  preview: WorkflowRecipePreview,
  idempotencyKey: string,
): {
  expected_recipe_version: number;
  expected_graph_revision: number;
  preview_digest: string;
  idempotency_key: string;
} {
  return {
    expected_recipe_version: recipe.current_version.version,
    expected_graph_revision: preview.base_graph_revision,
    preview_digest: preview.preview_digest,
    idempotency_key: idempotencyKey,
  };
}

export function clearAgentWorkflowRecipeIdempotencyKey(
  keys: Map<string, string>,
  kind: "apply" | "archive",
  recipeId: string,
): void {
  if (kind === "apply") {
    keys.delete(recipeId);
  }
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}
