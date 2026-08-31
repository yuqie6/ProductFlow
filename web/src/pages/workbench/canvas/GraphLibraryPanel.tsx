import { useState } from "react";

import { TabList, TabTrigger, Tabs } from "../../../components/ui/tabs";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { CanonicalProductDetail, GraphNode, GraphProjection } from "../../../lib/types";
import type { LocalImageEditOpenRequest } from "../local-edit/LocalImageEditController";
import { ProductImageExplorer } from "../chrome/image-explorer/ProductImageExplorer";
import { WorkflowMediaLibraryPanel } from "./WorkflowMediaLibraryPanel";

type LibraryTab = "workflow" | "product";

export function GraphLibraryPanel({
  product,
  graph,
  bindNode,
  bindLocked = false,
  onPreviewImage,
  onOpenLocalEdit,
  onBindAsset,
  onBound,
}: {
  product: CanonicalProductDetail;
  graph: GraphProjection;
  bindNode: GraphNode | null;
  bindLocked?: boolean;
  onPreviewImage: (image: DownloadableImage) => void;
  onOpenLocalEdit?: (request: LocalImageEditOpenRequest) => void;
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
      <Tabs value={tab} onValueChange={(value) => setTab(value as LibraryTab)} className="flex min-h-0 flex-1 flex-col">
        <div className="flex shrink-0 border-b border-border-l1 p-2">
          <TabList aria-label={t("graph.library.product")} className="w-full">
            <TabTrigger value="product">{t("graph.library.product")}</TabTrigger>
            <TabTrigger value="workflow">{t("graph.library.workflow")}</TabTrigger>
          </TabList>
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
              onOpenLocalEdit={onOpenLocalEdit}
              referenceTarget={referenceTarget}
            />
          </div>
        )}
      </Tabs>
    </div>
  );
}
