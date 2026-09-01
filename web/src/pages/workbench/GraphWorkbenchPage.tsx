/**
 * 商品 Agent 旁边的 live schema-v3 图编辑器。
 *
 * 画布变更走 Graph Command。运行前先 flush inspector，编译器看到的是已持久化配置。
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Boxes, CircleDot, Eye, Images, Plus, RotateCw, Sparkles } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";

import { ConfirmDialog } from "../../components/ConfirmDialog";
import { GalleryImagePreviewDialog } from "../../components/GalleryImagePreviewDialog";
import { Button } from "../../components/ui/button";
import { TopNav } from "../../components/TopNav";
import { api, ApiError } from "../../lib/api";
import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type {
  CanonicalProductDetail,
  GraphProjection,
  WorkflowRecipe,
  WorkflowRecipeApplicationResult,
  WorkflowRecipePreview,
  WorkflowRecipeSummary,
} from "../../lib/types";
import { AgentWorkbenchShell } from "./agent/AgentWorkbenchShell";
import { GraphAddNodePanel } from "./canvas/GraphAddNodePanel";
import { GraphCanvasPanel, type GraphCanvasActions } from "./canvas/GraphCanvasPanel";
import { inspectableGraphNodeId } from "./canvas/graphCatalog";
import { GraphLibraryPanel } from "./canvas/GraphLibraryPanel";
import { GraphNodeInspector } from "./canvas/GraphNodeInspector";
import { GraphRunsPanel } from "./canvas/GraphRunsPanel";
import { RecipeLibraryPanel } from "./canvas/RecipeLibraryPanel";
import {
  existingWorkbenchNodeIds,
  parseWorkbenchSidebarTool,
  patchWorkbenchUiState,
  readWorkbenchUiState,
  sameWorkbenchIds,
} from "./chrome/workbenchUiState";
import { useLocalImageEditController } from "./local-edit/LocalImageEditController";

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

export function GraphWorkbenchPage({
  product,
  initialGraph,
  agentContent,
}: {
  product: CanonicalProductDetail;
  initialGraph: GraphProjection;
  agentContent?: ReactNode;
}) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>(
    () => existingWorkbenchNodeIds(
      initialGraph,
      readWorkbenchUiState(product.id).selectedNodeIds,
    ),
  );
  const [actions, setActions] = useState<GraphCanvasActions>(EMPTY_ACTIONS);
  const [tool, setTool] = useState(
    () => parseWorkbenchSidebarTool(readWorkbenchUiState(product.id).sidebarTool) ?? "details",
  );
  const [bindNodeId, setBindNodeId] = useState<string | null>(null);
  const [previewImage, setPreviewImage] = useState<DownloadableImage | null>(null);
  const [canvasBusy, setCanvasBusy] = useState(false);
  const [chromeCollapsed, setChromeCollapsed] = useState(
    () => readWorkbenchUiState(product.id).chromeCollapsed === true,
  );
  const [archiveRecipe, setArchiveRecipe] = useState<WorkflowRecipeSummary | null>(null);
  const [recipeApplication, setRecipeApplication] = useState<WorkflowRecipeApplicationResult | null>(null);
  const [recipeError, setRecipeError] = useState<string | null>(null);
  const recipeApplyKeysRef = useRef(new Map<string, string>());
  const flushInspectorRef = useRef<() => Promise<void>>(async () => undefined);
  const registerInspectorFlush = useCallback((flush: () => Promise<void>) => {
    flushInspectorRef.current = flush;
  }, []);
  // Graph Command 是写入者；运行使用最近一次 flush 持久化的配置。
  const beforeRun = useCallback(() => flushInspectorRef.current(), []);
  const requestSidebarTool = useCallback(async (nextTool: string): Promise<boolean> => {
    const parsed = parseWorkbenchSidebarTool(nextTool);
    if (!parsed) return false;
    try {
      await flushInspectorRef.current();
    } catch {
      return false;
    }
    setTool(parsed);
    return true;
  }, []);

  const graphQuery = useQuery({
    queryKey: ["workflow-graph", product.id],
    queryFn: () => api.getCurrentWorkflowGraph(product.id),
    initialData: initialGraph,
    staleTime: 30_000,
  });
  const catalogQuery = useQuery({
    queryKey: ["graph-node-catalog"],
    queryFn: () => api.getGraphNodeCatalog(),
    staleTime: Infinity,
  });
  const recipesQuery = useQuery({
    queryKey: ["workflow-recipes", false],
    queryFn: () => api.listWorkflowRecipes(false),
    enabled: tool === "recipes",
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
          buildWorkflowRecipeApplyInput(
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
        queryClient.setQueryData(["workflow-graph", product.id], application.graph);
        await queryClient.invalidateQueries({ queryKey: ["workflow-graph", product.id] });
      } else {
        await queryClient.invalidateQueries({ queryKey: ["workflow-recipes"] });
      }
      setArchiveRecipe(null);
    },
    onError: (error, operation) => {
      clearWorkflowRecipeIdempotencyKey(recipeApplyKeysRef.current, operation.kind, operation.recipe.id);
      setRecipeError(error instanceof ApiError ? error.detail : t("workbench.error.recipe"));
    },
  });
  const liveGraph = graphQuery.data ?? initialGraph;
  const localEdit = useLocalImageEditController({ productId: product.id, graphId: liveGraph.id });
  const catalog = catalogQuery.data ?? null;
  const catalogError = catalogQuery.error
    ? catalogQuery.error instanceof ApiError
      ? catalogQuery.error.detail
      : catalogQuery.error instanceof Error
        ? catalogQuery.error.message
        : t("graph.inspector.catalogLoadFailed")
    : null;
  const selected = liveGraph.nodes.find((node) => node.id === selectedNodeIds[0]) ?? null;
  const bindNode = bindNodeId
    ? liveGraph.nodes.find((node) => node.id === bindNodeId && node.node_type === "image_asset") ?? null
    : null;

  const registerActions = useCallback((next: GraphCanvasActions) => {
    setActions(next);
  }, []);
  const inspectNode = useCallback((nodeId: string) => {
    if (!inspectableGraphNodeId(liveGraph, nodeId)) return;
    void (async () => {
      try {
        await flushInspectorRef.current();
      } catch {
        return;
      }
      setSelectedNodeIds([nodeId]);
      setTool("details");
      actions.focusNodes([nodeId]);
    })();
  }, [actions, liveGraph]);
  const selectCanvasNodes = useCallback(async (nodeIds: string[]) => {
    try {
      await flushInspectorRef.current();
    } catch {
      return;
    }
    setSelectedNodeIds(nodeIds);
    if (nodeIds.length === 1 && inspectableGraphNodeId(liveGraph, nodeIds[0])) {
      setTool("details");
    }
  }, [liveGraph]);

  useEffect(() => {
    setSelectedNodeIds((current) => {
      const next = existingWorkbenchNodeIds(liveGraph, current);
      return sameWorkbenchIds(current, next) ? current : next;
    });
  }, [liveGraph]);
  useEffect(() => {
    const storedTool = parseWorkbenchSidebarTool(tool);
    patchWorkbenchUiState(product.id, {
      ...(storedTool ? { sidebarTool: storedTool } : {}),
      selectedNodeIds,
      chromeCollapsed,
    });
  }, [chromeCollapsed, product.id, selectedNodeIds, tool]);

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
        workflowAvailable
        canvasContent={(
          <GraphCanvasPanel
            productId={product.id}
            graph={liveGraph}
            catalog={catalog}
            selectedNodeIds={selectedNodeIds}
            onSelect={selectCanvasNodes}
            onGraphChange={(next) => queryClient.setQueryData(["workflow-graph", product.id], next)}
            onRegisterActions={registerActions}
            onBusyChange={setCanvasBusy}
            onBeforeRun={beforeRun}
            onOpenLocalEdit={localEdit.openLocalImageEdit}
            chromeCollapsed={chromeCollapsed}
            onToggleChrome={() => setChromeCollapsed((current) => !current)}
            onBindNode={(nodeId) => {
              setBindNodeId(nodeId);
              void requestSidebarTool("library");
            }}
          />
        )}
        agentContent={agentContent ?? <GraphAgentPanel />}
        sidebarTools={[
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
                onOpenRecipesTab={() => void requestSidebarTool("recipes")}
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
                product={product}
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
            ),
          },
          {
            id: "library",
            label: t("workbench.sidebar.library"),
            icon: <Images size={17} />,
            contentClassName: "flex min-h-0 flex-1 flex-col overflow-hidden",
            content: (
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
                  });
                }}
                onBound={() => setBindNodeId(null)}
              />
            ),
          },
          {
            id: "recipes",
            label: t("workbench.sidebar.recipes"),
            icon: <Boxes size={17} />,
            contentClassName: "flex min-h-0 flex-1 flex-col overflow-hidden",
            content: (
              <>
                {recipeError ? (
                  <p role="alert" className="px-3 pt-3 text-xs text-red-700">{recipeError}</p>
                ) : null}
                <div className="min-h-0 flex-1 overflow-y-auto">
                  <RecipeLibraryPanel
                    recipes={recipesQuery.data ?? []}
                    loading={recipesQuery.isLoading}
                    error={recipesQuery.isError
                      ? (recipesQuery.error instanceof ApiError
                        ? recipesQuery.error.detail
                        : t("workbench.error.recipes"))
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
                </div>
              </>
            ),
          },
        ]}
        activeSidebarTool={tool}
        onSidebarToolChange={requestSidebarTool}
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

function newRecipeIdempotencyKey(recipeId: string): string {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `recipe-${recipeId}-${suffix}`.slice(0, 120);
}

export function buildWorkflowRecipeApplyInput(
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

export function clearWorkflowRecipeIdempotencyKey(
  keys: Map<string, string>,
  kind: "apply" | "archive",
  recipeId: string,
): void {
  if (kind === "apply") {
    keys.delete(recipeId);
  }
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
    {
      src: "/agent-onboarding/ceramic-detail.jpg",
      className: "agent-start-image agent-start-image-left",
    },
    {
      src: "/agent-onboarding/ceramic-feature.jpg",
      className: "agent-start-image agent-start-image-center",
    },
    {
      src: "/agent-onboarding/ceramic-craft.jpg",
      className: "agent-start-image agent-start-image-right",
    },
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

export function GraphWorkbenchLoadError({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  const { t } = useI18n();
  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <div className="text-center">
        <p role="alert" className="text-sm text-red-700">
          {error instanceof ApiError ? error.detail : t("productWorkbench.loadFailed")}
        </p>
        <button type="button" onClick={onRetry} className="mt-3 rounded-md border px-3 py-2 text-sm">
          {t("productWorkbench.retry")}
        </button>
      </div>
    </div>
  );
}
