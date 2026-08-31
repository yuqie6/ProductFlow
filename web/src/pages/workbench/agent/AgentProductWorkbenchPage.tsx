/**
 * Agent 优先的商品工作台：对话，加上可选的 live 图。
 *
 * live graph 就是编辑器。Agent 用 ChangeSet 协作，不能再提交一份 Draft 覆盖这张图。
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Boxes, CircleAlert, CircleDot, Eye, Images, Plus, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
  WorkflowRecipe,
  WorkflowRecipeApplicationResult,
  WorkflowRecipePreview,
  WorkflowRecipeSummary,
} from "../../../lib/types";
import { inspectableGraphNodeId } from "../canvas/graphCatalog";
import { GraphAddNodePanel } from "../canvas/GraphAddNodePanel";
import { GraphCanvasPanel, type GraphCanvasActions } from "../canvas/GraphCanvasPanel";
import { GraphLibraryPanel } from "../canvas/GraphLibraryPanel";
import { GraphNodeInspector } from "../canvas/GraphNodeInspector";
import { GraphRunsPanel } from "../canvas/GraphRunsPanel";
import { RecipeLibraryPanel } from "../canvas/RecipeLibraryPanel";
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

export type CanvasSelectionSource = "pointer" | "agent";

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

interface AgentProductWorkbenchPageProps {
  bootstrap: AgentWorkbenchPageBootstrap;
  agentTaskId?: string | null;
  preferConversation?: boolean;
  onRefetchBootstrap: () => Promise<unknown>;
}

export function AgentProductWorkbenchPage({
  bootstrap,
  agentTaskId = null,
  preferConversation = false,
  onRefetchBootstrap,
}: AgentProductWorkbenchPageProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [sidebarTool, setSidebarTool] = useState<AgentSidebarToolId>(
    () => initialAgentWorkbenchSidebarTool({
      hasGraph: Boolean(bootstrap.graph),
      hasTask: Boolean(agentTaskId),
      preferConversation,
      storedTool: readWorkbenchUiState(bootstrap.product.id).sidebarTool,
    }),
  );
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>(
    () => existingWorkbenchNodeIds(
      bootstrap.graph,
      readWorkbenchUiState(bootstrap.product.id).selectedNodeIds,
    ),
  );
  const [actions, setActions] = useState<GraphCanvasActions>(EMPTY_ACTIONS);
  const [bindNodeId, setBindNodeId] = useState<string | null>(null);
  const [archiveRecipe, setArchiveRecipe] = useState<WorkflowRecipeSummary | null>(null);
  const [recipeApplication, setRecipeApplication] = useState<WorkflowRecipeApplicationResult | null>(null);
  const [recipeError, setRecipeError] = useState<string | null>(null);
  const [previewImage, setPreviewImage] = useState<DownloadableImage | null>(null);
  const [canvasBusy, setCanvasBusy] = useState(false);
  const [agentEditing, setAgentEditing] = useState(false);
  const [chromeCollapsed, setChromeCollapsed] = useState(
    () => readWorkbenchUiState(bootstrap.product.id).chromeCollapsed === true,
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
    queryKey: ["workflow-graph", bootstrap.product.id],
    queryFn: () => readWorkflowGraphOrNull(() => api.getCurrentWorkflowGraph(bootstrap.product.id)),
    initialData: bootstrap.graph ?? undefined,
    retry: (failureCount, error) => !isHttpErrorStatus(error, 404) && failureCount < 2,
  });
  const liveGraph = graphQuery.data ?? bootstrap.graph;
  const localEdit = useLocalImageEditController({
    productId: bootstrap.product.id,
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
    patchWorkbenchUiState(bootstrap.product.id, {
      sidebarTool: activeSidebarTool,
      selectedNodeIds,
      chromeCollapsed,
    });
  }, [activeSidebarTool, bootstrap.product.id, chromeCollapsed, selectedNodeIds]);
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
  const createEmptyGraphMutation = useMutation({
    mutationFn: () => createOrLoadEmptyWorkflowGraph({
      create: () => api.createEmptyWorkflowGraph(bootstrap.product.id),
      loadCurrent: () => api.getCurrentWorkflowGraph(bootstrap.product.id),
      isConflict: (error) => isHttpErrorStatus(error, 409),
    }),
    onMutate: () => setEmptyGraphError(null),
    onSuccess: (graph) => {
      queryClient.setQueryData(["workflow-graph", bootstrap.product.id], graph);
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
          bootstrap.product.id,
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
    requestAgentWorkbenchOpen(
      (tool) => {
        sidebarToolRef.current = tool;
        setSidebarTool(tool);
      },
      () => setAgentOpenRequest((request) => request + 1),
    );
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
          onPreviewRun={actions.previewRun}
          onHideRunPreview={actions.hideRunPreview}
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
          onOpenLocalEdit={localEdit.openLocalImageEdit}
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
          </div>
        </>
      ),
    },
  ];

  return (
    <div className="flex h-dvh min-h-[560px] flex-col overflow-hidden bg-surface-base text-text-primary">
      {chromeCollapsed ? null : (
        <TopNav
          breadcrumbs={`${bootstrap.product.name} / ${t("agentWorkbench.breadcrumb")}`}
          onHome={() => navigate("/home")}
        />
      )}
      <AgentWorkbenchShell
        productId={bootstrap.product.id}
        workflowAvailable={workflowAvailable}
        activeSidebarTool={activeSidebarTool}
        onSidebarToolChange={(toolId) => requestSidebarTool(toolId as AgentSidebarToolId)}
        agentOpenRequest={agentOpenRequest}
        onAgentOpened={focusVisibleAgentComposer}
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
            agentEditing={agentEditing}
            onOpenLocalEdit={localEdit.openLocalImageEdit}
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
              productName={bootstrap.product.name}
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
        agentContent={(
          <AgentConversationPanel
            key={bootstrap.conversation.id}
            productId={bootstrap.product.id}
            productName={bootstrap.product.name}
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
