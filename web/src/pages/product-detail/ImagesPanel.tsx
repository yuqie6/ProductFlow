import { Image as ImageIcon } from "lucide-react";
import { useEffect, useState } from "react";
import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type { PosterVariant, ProductDetail, SourceAsset, WorkflowNode } from "../../lib/types";

import { PosterThumb, SourceAssetThumb } from "./ImageDownloadComponents";
import { workflowNodeDisplayTitle } from "./nodeDisplay";
import { ProductImageExplorer } from "./image-explorer/ProductImageExplorer";

interface ImagesPanelProps {
  product: ProductDetail;
  posters: PosterVariant[];
  referenceAssets: SourceAsset[];
  artifactCount: number;
  selectedReferenceNode: WorkflowNode | null;
  posterSourceAssetIds: Map<string, string>;
  onPreviewImage: (image: DownloadableImage) => void;
  onFillFromSourceAsset: (sourceAssetId: string) => void;
  onFillFromPoster: (posterId: string) => void;
  fillReferenceBusy: boolean;
}

export function ImagesPanel({
  product,
  posters,
  referenceAssets,
  artifactCount,
  selectedReferenceNode,
  posterSourceAssetIds,
  onPreviewImage,
  onFillFromSourceAsset,
  onFillFromPoster,
  fillReferenceBusy,
}: ImagesPanelProps) {
  const { t } = useI18n();
  const canFillReference = Boolean(selectedReferenceNode);
  const selectedReferenceLabel = selectedReferenceNode ? workflowNodeDisplayTitle(selectedReferenceNode, t) : "";
  const [activeView, setActiveView] = useState<"explorer" | "legacy">("explorer");
  useEffect(() => {
    if (!canFillReference && activeView === "legacy") {
      setActiveView("explorer");
    }
  }, [activeView, canFillReference]);
  return (
    <section>
      {canFillReference ? (
        <div className="mb-3 grid grid-cols-2 gap-1 rounded-md bg-slate-100 p-1 dark:bg-slate-900/80">
          <button type="button" onClick={() => setActiveView("explorer")} className={`h-8 rounded text-xs font-semibold ${activeView === "explorer" ? "bg-white text-indigo-700 shadow-sm dark:bg-slate-800 dark:text-violet-200" : "text-slate-500 dark:text-slate-400"}`}>
            {t("detail.library.explorerTab")}
          </button>
          <button type="button" onClick={() => setActiveView("legacy")} className={`h-8 rounded text-xs font-semibold ${activeView === "legacy" ? "bg-white text-indigo-700 shadow-sm dark:bg-slate-800 dark:text-violet-200" : "text-slate-500 dark:text-slate-400"}`}>
            {t("detail.library.legacyTab")}
          </button>
        </div>
      ) : null}
      {activeView === "explorer" ? (
        <ProductImageExplorer
          productId={product.id}
          productName={product.name}
          onPreviewImage={onPreviewImage}
        />
      ) : (
        <LegacyReferenceImages
          product={product}
          posters={posters}
          referenceAssets={referenceAssets}
          artifactCount={artifactCount}
          selectedReferenceLabel={selectedReferenceLabel}
          posterSourceAssetIds={posterSourceAssetIds}
          onPreviewImage={onPreviewImage}
          onFillFromSourceAsset={onFillFromSourceAsset}
          onFillFromPoster={onFillFromPoster}
          fillReferenceBusy={fillReferenceBusy}
        />
      )}
    </section>
  );
}

function LegacyReferenceImages({
  product,
  posters,
  referenceAssets,
  artifactCount,
  selectedReferenceLabel,
  posterSourceAssetIds,
  onPreviewImage,
  onFillFromSourceAsset,
  onFillFromPoster,
  fillReferenceBusy,
}: Omit<ImagesPanelProps, "selectedReferenceNode"> & { selectedReferenceLabel: string }) {
  const { t } = useI18n();
  return (
    <>
      <div className="mb-3 space-y-1 text-xs text-zinc-500 dark:text-slate-400">
        <div>{artifactCount ? t("detail.downloadableCount", { count: artifactCount }) : t("detail.waitingAssets")}</div>
        <div className="text-indigo-600 dark:text-violet-400 font-semibold">
          {t("detail.fillInto", { label: selectedReferenceLabel })}
        </div>
      </div>
      {artifactCount ? (
        <div className="grid grid-cols-2 gap-2">
          {posters.map((poster) => {
            const sourceAssetId = posterSourceAssetIds.get(poster.id);
            return (
              <PosterThumb
                key={poster.id}
                poster={poster}
                productName={product.name}
                onPreview={onPreviewImage}
                onUseAsReference={
                  () => {
                    if (sourceAssetId) {
                      onFillFromSourceAsset(sourceAssetId);
                      return;
                    }
                    onFillFromPoster(poster.id);
                  }
                }
                useAsReferenceBusy={fillReferenceBusy}
              />
            );
          })}
          {referenceAssets.map((asset) => (
            <SourceAssetThumb
              key={asset.id}
              asset={asset}
              product={product}
              onPreview={onPreviewImage}
              onUseAsReference={
                () => onFillFromSourceAsset(asset.id)
              }
              useAsReferenceBusy={fillReferenceBusy}
            />
          ))}
        </div>
      ) : (
        <div className="glass-empty-state flex min-h-[160px] flex-col items-center justify-center gap-2 p-6 text-center text-xs leading-relaxed text-zinc-500 dark:text-slate-400">
          <ImageIcon size={18} className="text-indigo-500 opacity-80 dark:text-violet-400" />
          <div>{t("detail.noImages")}</div>
        </div>
      )}
    </>
  );
}
