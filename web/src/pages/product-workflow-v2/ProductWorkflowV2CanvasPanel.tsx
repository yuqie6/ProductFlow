import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleAlert, Hand, Loader2, MousePointer2, Move } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { ConfirmDialog } from "../../components/ConfirmDialog";
import { api, ApiError } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import type {
  ActiveProductWorkflowV2,
  ProductWorkflowV2,
  WorkflowCanvasMutationResult,
  WorkflowNodeV2,
  WorkflowRunListV2Response,
} from "../../lib/types";
import { ProductWorkbenchCanvasChromeToggle } from "../product-detail/ProductWorkbenchCanvasChromeToggle";
import {
  WorkflowCanvasMobileModeTabs,
  type WorkflowCanvasMobileModeItem,
} from "../product-detail/WorkflowCanvasChrome";
import type { CanvasInteractionMode } from "../product-detail/types";
import {
  emptyWorkflowCanvasState,
  parseWorkflowCanvasState,
  reconcileWorkflowCanvasState,
  workflowCanvasStateStorageKey,
  type WorkflowCanvasStateV1,
  type WorkflowCanvasViewport,
} from "./canvasState";
import { visibleRealNodeIds } from "./graph";
import {
  V2WorkflowCanvas,
  type WorkflowRevealVisibility,
} from "./V2WorkflowCanvas";
import type { RecipeSourceSelection } from "./recipeSource";
import { V2WorkflowCommandBar } from "./V2WorkflowCommandBar";
import { WorkflowTextDialog } from "./WorkflowDialogs";

type CanvasStateUpdate = WorkflowCanvasStateV1 | ((current: WorkflowCanvasStateV1) => WorkflowCanvasStateV1);
type StructureOperation = (expectedEditVersion: number) => Promise<WorkflowCanvasMutationResult>;
const COMPACT_WORKFLOW_CANVAS_MEDIA_QUERY = "(max-width: 1023px)";
type TextDialogState =
  | { kind: "create-folder" }
  | { kind: "rename-folder"; folderId: string; initialValue: string }
  | null;

export interface ProductWorkflowV2CanvasContext {
  openFolderId: string | null;
  selectedNodeIds: string[];
}

interface ProductWorkflowV2CanvasPanelProps {
  productId: string;
  workflow: ProductWorkflowV2;
  revealVisibility: WorkflowRevealVisibility | null;
  activityLabel: string | null;
  interactionLocked: boolean;
  externalError: string | null;
  topChromeCollapsed: boolean;
  onToggleTopChrome: () => void;
  onRefetchWorkflow: () => Promise<unknown>;
  onCanvasContextChange: (context: ProductWorkflowV2CanvasContext) => void;
  onOpenSidebarTool: (tool: "details" | "runs" | "library") => Promise<boolean>;
  onBeforeWorkflowAction: () => Promise<number | null>;
  onReferenceNodeChange: (nodeId: string | null) => void;
  onSaveRecipe: (source: RecipeSourceSelection) => void | Promise<void>;
}

export function ProductWorkflowV2CanvasPanel({
  productId,
  workflow,
  revealVisibility,
  activityLabel,
  interactionLocked,
  externalError,
  topChromeCollapsed,
  onToggleTopChrome,
  onRefetchWorkflow,
  onCanvasContextChange,
  onOpenSidebarTool,
  onBeforeWorkflowAction,
  onReferenceNodeChange,
  onSaveRecipe,
}: ProductWorkflowV2CanvasPanelProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [canvasState, setCanvasState] = useState<WorkflowCanvasStateV1>(emptyWorkflowCanvasState);
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>([]);
  const [textDialog, setTextDialog] = useState<TextDialogState>(null);
  const [dissolveFolder, setDissolveFolder] = useState<{ id: string; title: string } | null>(null);
  const [currentRun, setCurrentRun] = useState<{ id: string; nodeId: string } | null>(null);
  const [operationError, setOperationError] = useState<string | null>(null);
  const [canvasResetVersion, setCanvasResetVersion] = useState(0);
  const [canvasRenderable, setCanvasRenderable] = useState(false);
  const [mobileCanvasMode, setMobileCanvasMode] = useState<CanvasInteractionMode>("browse");
  const [mobileCanvasControlsActive, setMobileCanvasControlsActive] = useState(() => (
    typeof window !== "undefined"
      && typeof window.matchMedia === "function"
      && window.matchMedia(COMPACT_WORKFLOW_CANVAS_MEDIA_QUERY).matches
  ));
  const loadedCanvasWorkflowRef = useRef<string | null>(null);
  const workflowRef = useRef(workflow);
  const selectionTransitionSequenceRef = useRef(0);
  const canvasSurfaceRef = useRef<HTMLDivElement | null>(null);
  const previousActiveWorkflowRunIdsRef = useRef<Set<string>>(new Set());

  workflowRef.current = workflow;

  const openFolder = workflow.folders.find((folder) => folder.id === canvasState.open_folder_id) ?? null;
  const mobileCanvasModeItems: WorkflowCanvasMobileModeItem[] = [
    {
      key: "browse",
      label: t("detail.mobileCanvasBrowse"),
      description: t("detail.mobileCanvasBrowseHint"),
      icon: <Hand size={15} />,
    },
    {
      key: "edit",
      label: t("detail.mobileCanvasEdit"),
      description: t("detail.mobileCanvasEditHint"),
      icon: <Move size={15} />,
    },
    {
      key: "select",
      label: t("detail.mobileCanvasSelect"),
      description: t("detail.mobileCanvasSelectHint"),
      icon: <MousePointer2 size={15} />,
    },
  ];

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }
    const media = window.matchMedia(COMPACT_WORKFLOW_CANVAS_MEDIA_QUERY);
    const update = () => setMobileCanvasControlsActive(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  const persistCanvasState = useCallback((update: CanvasStateUpdate) => {
    setCanvasState((current) => {
      const next = typeof update === "function" ? update(current) : update;
      if (next === current) {
        return current;
      }
      if (typeof window !== "undefined") {
        window.localStorage.setItem(workflowCanvasStateStorageKey(workflow.id), JSON.stringify(next));
      }
      return next;
    });
  }, [workflow.id]);

  useEffect(() => {
    if (loadedCanvasWorkflowRef.current !== workflow.id) {
      loadedCanvasWorkflowRef.current = workflow.id;
      const raw = typeof window === "undefined"
        ? null
        : window.localStorage.getItem(workflowCanvasStateStorageKey(workflow.id));
      setCanvasState(
        reconcileWorkflowCanvasState(
          parseWorkflowCanvasState(raw),
          workflow.folders.map((folder) => folder.id),
        ),
      );
      setSelectedNodeIds([]);
      onReferenceNodeChange(null);
      return;
    }
    setCanvasState((current) =>
      reconcileWorkflowCanvasState(current, workflow.folders.map((folder) => folder.id)),
    );
    setSelectedNodeIds((current) => {
      const visibleNodeIds = new Set(visibleRealNodeIds(workflow, canvasState.open_folder_id));
      return current.filter((nodeId) => visibleNodeIds.has(nodeId));
    });
  }, [canvasState.open_folder_id, onReferenceNodeChange, workflow]);

  useEffect(() => {
    onCanvasContextChange({
      openFolderId: openFolder?.id ?? null,
      selectedNodeIds,
    });
  }, [onCanvasContextChange, openFolder?.id, selectedNodeIds]);

  useEffect(() => {
    const surface = canvasSurfaceRef.current;
    if (!surface) {
      return;
    }
    const update = () => {
      const bounds = surface.getBoundingClientRect();
      setCanvasRenderable(bounds.width > 0 && bounds.height > 0);
    };
    update();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", update);
      return () => window.removeEventListener("resize", update);
    }
    const observer = new ResizeObserver(update);
    observer.observe(surface);
    return () => observer.disconnect();
  }, [workflow.id]);

  const runQuery = useQuery({
    queryKey: ["workflow-node-run-v2", currentRun?.id],
    queryFn: () => api.getWorkflowNodeRunV2(currentRun!.id),
    enabled: Boolean(currentRun),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return !status || status === "queued" || status === "running" ? 1_000 : false;
    },
  });
  const workflowRunsQueryKey = ["v2-workflow-runs", productId, workflow.id] as const;
  const workflowRunsQuery = useQuery({
    queryKey: workflowRunsQueryKey,
    queryFn: () => api.listWorkflowRunsV2(productId, workflow.id),
    refetchInterval: (query) => query.state.data?.items.some((run) => run.status === "running")
      ? 1_200
      : false,
  });
  const activeWorkflowRun = workflowRunsQuery.data?.items.find((run) => run.status === "running") ?? null;

  useEffect(() => {
    const result = workflowRunsQuery.data;
    if (!result) {
      return;
    }
    queryClient.setQueryData<ActiveProductWorkflowV2>(
      ["active-product-workflow-v2", productId],
      { latest_revision: result.workflow.revision, workflow: result.workflow },
    );
    const activeIds = new Set(
      result.items.filter((run) => run.status === "running").map((run) => run.id),
    );
    if (previousActiveWorkflowRunIdsRef.current.size > 0 && activeIds.size === 0) {
      void onRefetchWorkflow();
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] }),
        queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] }),
        queryClient.invalidateQueries({ queryKey: ["product", productId] }),
        queryClient.invalidateQueries({ queryKey: ["products"] }),
      ]);
    }
    previousActiveWorkflowRunIdsRef.current = activeIds;
  }, [onRefetchWorkflow, productId, queryClient, workflowRunsQuery.data]);

  useEffect(() => {
    const status = runQuery.data?.status;
    if (!currentRun || !status || status === "queued" || status === "running") {
      return;
    }
    setCurrentRun(null);
    void onRefetchWorkflow();
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] }),
      queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] }),
      queryClient.invalidateQueries({ queryKey: ["product", productId] }),
      queryClient.invalidateQueries({ queryKey: ["products"] }),
    ]);
  }, [currentRun, onRefetchWorkflow, productId, queryClient, runQuery.data?.status]);

  const acceptCanvasMutation = useCallback((result: WorkflowCanvasMutationResult) => {
    queryClient.setQueryData<ActiveProductWorkflowV2>(
      ["active-product-workflow-v2", productId],
      { latest_revision: result.workflow.revision, workflow: result.workflow },
    );
    setOperationError(null);
  }, [productId, queryClient]);

  const structureMutation = useMutation({
    mutationFn: async (operation: StructureOperation) => {
      const flushedEditVersion = await onBeforeWorkflowAction();
      return operation(flushedEditVersion ?? workflowRef.current.edit_version);
    },
    onSuccess: acceptCanvasMutation,
    onError: async (error) => {
      setOperationError(errorDetail(error, t("workflowV2.error.structure")));
      setCanvasResetVersion((version) => version + 1);
      if (error instanceof ApiError && error.status === 409) {
        await onRefetchWorkflow();
      }
    },
  });
  const workflowRunMutation = useMutation({
    mutationFn: async () => {
      await onBeforeWorkflowAction();
      return api.runWorkflowV2(productId, workflow.id);
    },
    onMutate: () => setOperationError(null),
    onSuccess: async (result) => {
      queryClient.setQueryData<WorkflowRunListV2Response>(workflowRunsQueryKey, (current) => ({
        workflow: result.workflow,
        items: [
          result.workflow_run,
          ...(current?.items ?? []).filter((run) => run.id !== result.workflow_run.id),
        ],
      }));
      queryClient.setQueryData<ActiveProductWorkflowV2>(
        ["active-product-workflow-v2", productId],
        { latest_revision: result.workflow.revision, workflow: result.workflow },
      );
      await onOpenSidebarTool("runs");
      await onRefetchWorkflow();
    },
    onError: (error) => {
      setOperationError(errorDetail(error, t("workflowV2.error.runWorkflow")));
    },
  });
  const nodeRunMutation = useMutation({
    mutationFn: async (node: WorkflowNodeV2) => {
      await onBeforeWorkflowAction();
      return api.runWorkflowNodeV2(node.id);
    },
    onMutate: () => {
      setOperationError(null);
      setCurrentRun(null);
    },
    onSuccess: async (result) => {
      setCurrentRun({ id: result.node_run.id, nodeId: result.node_run.node_id });
      await onRefetchWorkflow();
    },
    onError: (error) => {
      setCurrentRun(null);
      setOperationError(errorDetail(error, t("workflowV2.error.run")));
    },
  });

  const moveSelection = (folderId: string | null) => {
    if (selectedNodeIds.length === 0) {
      return;
    }
    if (folderId) {
      const targetIds = workflow.nodes
        .filter((node) => node.folder_id === folderId)
        .map((node) => node.id);
      const nodeIds = Array.from(new Set([...targetIds, ...selectedNodeIds]));
      structureMutation.mutate((expectedEditVersion) => api.setWorkflowFolderMembers(productId, workflow.id, folderId, {
        node_ids: nodeIds,
        expected_edit_version: expectedEditVersion,
      }));
      return;
    }
    if (!openFolder) {
      return;
    }
    const selected = new Set(selectedNodeIds);
    const remainingIds = workflow.nodes
      .filter((node) => node.folder_id === openFolder.id && !selected.has(node.id))
      .map((node) => node.id);
    structureMutation.mutate((expectedEditVersion) => api.setWorkflowFolderMembers(productId, workflow.id, openFolder.id, {
      node_ids: remainingIds,
      expected_edit_version: expectedEditVersion,
    }), { onSuccess: () => setSelectedNodeIds([]) });
  };

  const viewportFolderId = openFolder?.id ?? null;
  const updateViewport = useCallback((viewport: WorkflowCanvasViewport) => {
    persistCanvasState((current) => viewportFolderId ? {
      ...current,
      folder_viewports: { ...current.folder_viewports, [viewportFolderId]: viewport },
    } : {
      ...current,
      global_viewport: viewport,
    });
  }, [persistCanvasState, viewportFolderId]);

  const openCanvasFolder = useCallback((folderId: string | null) => {
    const sequence = ++selectionTransitionSequenceRef.current;
    void (async () => {
      try {
        await onBeforeWorkflowAction();
      } catch {
        return;
      }
      if (sequence !== selectionTransitionSequenceRef.current) {
        return;
      }
      persistCanvasState((current) => ({ ...current, open_folder_id: folderId }));
      setSelectedNodeIds([]);
      onReferenceNodeChange(null);
    })();
  }, [onBeforeWorkflowAction, onReferenceNodeChange, persistCanvasState]);

  const selectNodes = useCallback((nodeIds: string[]) => {
    if (
      selectedNodeIds.length === nodeIds.length
      && selectedNodeIds.every((nodeId, index) => nodeId === nodeIds[index])
    ) {
      return;
    }
    const sequence = ++selectionTransitionSequenceRef.current;
    void (async () => {
      const keepCanvasVisible = mobileCanvasControlsActive && mobileCanvasMode === "select";
      try {
        if (!keepCanvasVisible && nodeIds.length === 1) {
          const accepted = await onOpenSidebarTool("details");
          if (!accepted) {
            return;
          }
        } else {
          await onBeforeWorkflowAction();
        }
      } catch {
        return;
      }
      if (sequence !== selectionTransitionSequenceRef.current) {
        return;
      }
      onReferenceNodeChange(null);
      setSelectedNodeIds(nodeIds);
    })();
  }, [
    mobileCanvasControlsActive,
    mobileCanvasMode,
    onBeforeWorkflowAction,
    onOpenSidebarTool,
    onReferenceNodeChange,
    selectedNodeIds,
  ]);

  const runningNodeId = currentRun?.nodeId ?? (
    nodeRunMutation.isPending ? nodeRunMutation.variables?.id ?? null : null
  );
  const workflowRunBusy = workflowRunMutation.isPending || Boolean(activeWorkflowRun);
  const busy = structureMutation.isPending || interactionLocked || workflowRunBusy;

  return (
    <>
      <section
        data-product-workflow-v2-canvas
        data-agent-workflow-canvas
        className="relative flex h-full min-h-0 flex-col overflow-hidden bg-zinc-50 text-zinc-950 dark:bg-[#080c12] dark:text-slate-100"
      >
        {operationError || externalError ? (
          <div role="alert" className="flex shrink-0 items-start gap-2 border-b border-red-200 bg-red-50 px-3 py-2 text-xs leading-5 text-red-700 dark:border-red-400/25 dark:bg-red-500/10 dark:text-red-200">
            <CircleAlert size={14} className="mt-0.5 shrink-0" />
            <span className="min-w-0 flex-1">{operationError ?? externalError}</span>
            {operationError ? (
              <button type="button" onClick={() => setOperationError(null)} className="font-semibold hover:underline">
                {t("workflowV2.dialog.close")}
              </button>
            ) : null}
          </div>
        ) : null}

        <div
          ref={canvasSurfaceRef}
          className="relative min-h-0 flex-1 overflow-hidden"
          aria-label={t("workflowV2.canvas.ariaLabel")}
        >
          <ProductWorkbenchCanvasChromeToggle
            collapsed={topChromeCollapsed}
            maximizeLabel={t("detail.maximizeCanvas")}
            restoreLabel={t("detail.restoreCanvas")}
            onToggle={onToggleTopChrome}
          />
          {canvasRenderable ? (
            <V2WorkflowCanvas
              workflow={workflow}
              resetVersion={canvasResetVersion}
              revealVisibility={revealVisibility ?? undefined}
              openFolderId={openFolder?.id ?? null}
              viewport={openFolder
                ? canvasState.folder_viewports[openFolder.id] ?? null
                : canvasState.global_viewport}
              structureBusy={busy}
              runningNodeId={runningNodeId}
              selectedNodeIds={selectedNodeIds}
              mobileInteractionMode={mobileCanvasControlsActive ? mobileCanvasMode : "edit"}
              mobileCanvasControlsActive={mobileCanvasControlsActive}
              onOpenFolder={openCanvasFolder}
              onRunNode={(node) => nodeRunMutation.mutate(node)}
              onBindReference={(node) => {
                void (async () => {
                  if (await onOpenSidebarTool("library")) {
                    onReferenceNodeChange(node.id);
                  }
                })();
              }}
              onSelectionChange={selectNodes}
              onLayoutCommit={(positions) => {
                structureMutation.mutate((expectedEditVersion) => api.updateWorkflowNodeLayoutV2(productId, workflow.id, {
                  positions,
                  expected_edit_version: expectedEditVersion,
                }));
              }}
              onFolderTranslate={(folderId, deltaX, deltaY) => {
                structureMutation.mutate((expectedEditVersion) => api.translateWorkflowFolder(productId, workflow.id, folderId, {
                  delta_x: deltaX,
                  delta_y: deltaY,
                  expected_edit_version: expectedEditVersion,
                }));
              }}
              onViewportChange={updateViewport}
            />
          ) : null}

          <V2WorkflowCommandBar
            workflow={workflow}
            openFolderId={openFolder?.id ?? null}
            selectedNodeIds={selectedNodeIds}
            structureBusy={busy}
            workflowRunBusy={workflowRunBusy}
            variant="overlay"
            activityLabel={activityLabel}
            onOpenFolder={openCanvasFolder}
            onCreateFolder={() => setTextDialog({ kind: "create-folder" })}
            onMoveSelection={moveSelection}
            onSaveRecipe={(source) => void onSaveRecipe(source)}
            onRenameFolder={() => {
              if (openFolder) {
                setTextDialog({
                  kind: "rename-folder",
                  folderId: openFolder.id,
                  initialValue: openFolder.title,
                });
              }
            }}
            onDissolveFolder={() => {
              if (openFolder) {
                setDissolveFolder({ id: openFolder.id, title: openFolder.title });
              }
            }}
            onRunWorkflow={() => workflowRunMutation.mutate()}
          />

          <div
            data-canvas-control
            className="pointer-events-none absolute inset-x-3 bottom-3 z-30 lg:hidden"
          >
            <div className="pointer-events-auto mx-auto max-w-[28rem] rounded-2xl border border-slate-200 bg-white p-1.5 shadow-[0_-6px_18px_rgba(15,23,42,0.12)] dark:border-slate-700 dark:bg-slate-950 dark:shadow-[0_-12px_28px_rgba(0,0,0,0.30)]">
              <WorkflowCanvasMobileModeTabs
                value={mobileCanvasMode}
                items={mobileCanvasModeItems}
                onChange={setMobileCanvasMode}
              />
            </div>
          </div>

          {structureMutation.isPending ? (
            <div className="pointer-events-none absolute bottom-20 left-3 z-20 inline-flex items-center gap-1.5 rounded-md border border-zinc-200 bg-white/95 px-2.5 py-1.5 text-[11px] font-semibold text-zinc-600 shadow-sm backdrop-blur dark:border-slate-700 dark:bg-slate-900/95 dark:text-slate-200 lg:bottom-3">
              <Loader2 size={12} className="animate-spin" />
              {t("workflowV2.canvas.saving")}
            </div>
          ) : null}
        </div>
      </section>

      <WorkflowTextDialog
        open={textDialog !== null}
        title={textDialog?.kind === "rename-folder" ? t("workflowV2.folder.rename") : t("workflowV2.folder.create")}
        label={t("workflowV2.folder.name")}
        initialValue={textDialog?.kind === "rename-folder" ? textDialog.initialValue : ""}
        busy={structureMutation.isPending}
        error={structureMutation.isError ? operationError : null}
        onClose={() => setTextDialog(null)}
        onSubmit={(title) => {
          if (!textDialog) {
            return;
          }
          if (textDialog.kind === "create-folder") {
            structureMutation.mutate((expectedEditVersion) => api.createWorkflowFolder(productId, workflow.id, {
              title,
              node_ids: selectedNodeIds,
              expected_edit_version: expectedEditVersion,
            }), { onSuccess: () => { setTextDialog(null); setSelectedNodeIds([]); } });
            return;
          }
          structureMutation.mutate((expectedEditVersion) => api.renameWorkflowFolder(productId, workflow.id, textDialog.folderId, {
            title,
            expected_edit_version: expectedEditVersion,
          }), { onSuccess: () => setTextDialog(null) });
        }}
      />

      <ConfirmDialog
        open={dissolveFolder !== null}
        title={t("workflowV2.folder.dissolve")}
        description={t("workflowV2.folder.dissolveConfirm", { title: dissolveFolder?.title ?? "" })}
        confirmLabel={t("workflowV2.folder.dissolve")}
        cancelLabel={t("common.cancel")}
        busy={structureMutation.isPending}
        destructive
        onClose={() => setDissolveFolder(null)}
        onConfirm={() => {
          if (!dissolveFolder) {
            return;
          }
          structureMutation.mutate(
            (expectedEditVersion) => api.dissolveWorkflowFolder(
              productId,
              workflow.id,
              dissolveFolder.id,
              expectedEditVersion,
            ),
            {
              onSuccess: () => {
                setDissolveFolder(null);
                persistCanvasState((current) => ({ ...current, open_folder_id: null }));
                setSelectedNodeIds([]);
              },
            },
          );
        }}
      />
    </>
  );
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}
