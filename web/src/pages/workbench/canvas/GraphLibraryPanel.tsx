import { useState } from "react";

import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { CanonicalProductDetail, GraphNode, GraphProjection } from "../../../lib/types";
import { ProductImageExplorer } from "../chrome/image-explorer/ProductImageExplorer";
import { WorkflowMediaLibraryPanel } from "./WorkflowMediaLibraryPanel";

type LibraryTab = "workflow" | "product";

export function GraphLibraryPanel({
  product,
  graph,
  bindNode,
  bindLocked = false,
  onPreviewImage,
  onBindAsset,
  onBound,
}: {
  product: CanonicalProductDetail;
  graph: GraphProjection;
  bindNode: GraphNode | null;
  bindLocked?: boolean;
  onPreviewImage: (image: DownloadableImage) => void;
  onBindAsset: (assetId: string) => Promise<unknown>;
  onBound: () => void;
}) {
  const { t } = useI18n();
  const [tab, setTab] = useState<LibraryTab>("product");
  const referenceTarget = bindNode && !bindLocked ? {
    bindAsset: async (asset: { id: string }) => onBindAsset(asset.id),
    onBound,
  } : undefined;
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden" data-graph-library-panel>
      <div className="flex shrink-0 gap-1 border-b border-zinc-200 p-2 dark:border-slate-800">
        <TabButton active={tab === "product"} onClick={() => setTab("product")}>
          {t("graph.library.product")}
        </TabButton>
        <TabButton active={tab === "workflow"} onClick={() => setTab("workflow")}>
          {t("graph.library.workflow")}
        </TabButton>
      </div>
      {tab === "workflow" ? (
        <WorkflowMediaLibraryPanel
          productId={product.id}
          workflowId={graph.id}
          referenceTarget={referenceTarget}
          onPreviewImage={onPreviewImage}
        />
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto p-3">
          <ProductImageExplorer
            productId={product.id}
            productName={product.name}
            onPreviewImage={onPreviewImage}
            referenceTarget={referenceTarget}
          />
        </div>
      )}
    </div>
  );
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`h-8 flex-1 rounded-lg px-2 text-[11px] font-semibold ${
        active
          ? "bg-slate-100 text-slate-800 dark:bg-slate-800 dark:text-slate-100"
          : "text-zinc-500 hover:bg-zinc-50 dark:text-slate-400 dark:hover:bg-slate-900"
      }`}
    >
      {children}
    </button>
  );
}
