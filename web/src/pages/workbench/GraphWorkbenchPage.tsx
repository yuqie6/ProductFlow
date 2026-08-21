import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, CircleDot, Eye, Images, Plus, RotateCw } from "lucide-react";
import { useCallback, useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";

import { GalleryImagePreviewDialog } from "../../components/GalleryImagePreviewDialog";
import { TopNav } from "../../components/TopNav";
import { api, ApiError } from "../../lib/api";
import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type { CanonicalProductDetail, GraphProjection } from "../../lib/types";
import { AgentWorkbenchShell } from "./agent/AgentWorkbenchShell";
import { GraphAddNodePanel } from "./canvas/GraphAddNodePanel";
import { GraphCanvasPanel, type GraphCanvasActions } from "./canvas/GraphCanvasPanel";
import { inspectableGraphNodeId } from "./canvas/graphCatalog";
import { GraphLibraryPanel } from "./canvas/GraphLibraryPanel";
import { GraphNodeInspector } from "./canvas/GraphNodeInspector";
import { GraphRunsPanel } from "./canvas/GraphRunsPanel";

const EMPTY_ACTIONS: GraphCanvasActions = {
  createNode: () => undefined,
  duplicateSelected: () => undefined,
  groupSelected: () => undefined,
  dissolveSelected: () => undefined,
  commitNode: async () => undefined,
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
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>([]);
  const [actions, setActions] = useState<GraphCanvasActions>(EMPTY_ACTIONS);
  const [tool, setTool] = useState("agent");
  const [bindNodeId, setBindNodeId] = useState<string | null>(null);
  const [previewImage, setPreviewImage] = useState<DownloadableImage | null>(null);

  const graphQuery = useQuery({
    queryKey: ["workflow-graph", product.id],
    queryFn: () => api.getCurrentWorkflowGraph(product.id),
    initialData: initialGraph,
  });
  const catalogQuery = useQuery({
    queryKey: ["graph-node-catalog"],
    queryFn: () => api.getGraphNodeCatalog(),
    staleTime: Infinity,
  });
  const liveGraph = graphQuery.data ?? initialGraph;
  const catalog = catalogQuery.data ?? null;
  const selected = liveGraph.nodes.find((node) => node.id === selectedNodeIds[0]) ?? null;
  const bindNode = bindNodeId
    ? liveGraph.nodes.find((node) => node.id === bindNodeId && node.node_type === "image_asset") ?? null
    : null;

  const registerActions = useCallback((next: GraphCanvasActions) => {
    setActions(next);
  }, []);
  const inspectNode = useCallback((nodeId: string) => {
    if (!inspectableGraphNodeId(liveGraph, nodeId)) return;
    setSelectedNodeIds([nodeId]);
    setTool("details");
  }, [liveGraph]);
  const selectCanvasNodes = useCallback((nodeIds: string[]) => {
    setSelectedNodeIds(nodeIds);
    if (nodeIds.length === 1) inspectNode(nodeIds[0]);
  }, [inspectNode]);

  return (
    <div className="flex h-dvh min-h-[560px] flex-col overflow-hidden bg-white text-zinc-950 dark:bg-[#060a12] dark:text-slate-100">
      <TopNav
        breadcrumbs={`${product.name} / ${t("agentWorkbench.breadcrumb")} · r${liveGraph.revision}`}
        onHome={() => navigate("/products")}
      />
      <AgentWorkbenchShell
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
            onBindNode={(nodeId) => {
              setBindNodeId(nodeId);
              setTool("library");
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
                  busy={false}
                  onCreate={actions.createNode}
                  canDuplicate={selectedNodeIds.length > 0}
                  canGroup={selectedNodeIds.length > 1}
                  canDissolve={liveGraph.nodes.some((node) => selectedNodeIds.includes(node.id) && Boolean(node.group_id))}
                  onDuplicate={actions.duplicateSelected}
                  onGroup={actions.groupSelected}
                  onDissolve={actions.dissolveSelected}
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
                  busy={false}
                  onCommit={async (input) => {
                    if (!selected) return;
                    return actions.commitNode({ nodeId: selected.id, ...input });
                  }}
                  onBind={selected?.node_type === "image_asset" ? () => {
                    setBindNodeId(selected.id);
                    setTool("library");
                  } : undefined}
                  onJump={inspectNode}
                  onPreviewImage={setPreviewImage}
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
                  onJump={inspectNode}
                  onPreviewImage={setPreviewImage}
                />
              ),
            },
            {
              id: "library",
              label: t("workflowV2.sidebar.library"),
              icon: <Images size={17} />,
              contentClassName: "flex min-h-0 flex-1 flex-col overflow-hidden",
              content: (
                <GraphLibraryPanel
                  product={product}
                  graph={liveGraph}
                  bindNode={bindNode}
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
        ]}
        activeSidebarTool={tool}
        onSidebarToolChange={(next) => {
          setTool(next);
          return true;
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

export function GraphAgentPanel({
  error = null,
  onRetry,
}: {
  error?: unknown;
  onRetry?: () => void;
}) {
  const { t } = useI18n();
  const message = error instanceof ApiError
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
          <p className="truncate text-xs text-text-secondary">{t("graph.workbench.agentOptional")}</p>
        </div>
      </header>
      <div className="flex min-h-0 flex-1 flex-col items-start justify-center gap-3 p-4">
        <p role="status" className="text-sm leading-6 text-text-secondary">{message}</p>
        {onRetry ? (
          <button
            type="button"
            onClick={onRetry}
            className="inline-flex h-10 items-center gap-2 rounded-md border border-zinc-300 px-3 text-sm font-semibold text-zinc-700 hover:border-zinc-500 dark:border-slate-700 dark:text-slate-200 dark:hover:border-slate-500"
          >
            <RotateCw size={15} />
            {t("graph.workbench.agentRetry")}
          </button>
        ) : null}
      </div>
    </section>
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
