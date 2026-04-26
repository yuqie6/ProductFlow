import type { DownloadableImage } from "../../lib/image-downloads";
import type { PosterVariant, ProductDetail, SourceAsset, WorkflowNode } from "../../lib/types";

import { PosterThumb, SourceAssetThumb } from "./ImageDownloadComponents";

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
  const canFillReference = Boolean(selectedReferenceNode);
  return (
    <section>
      <div className="mb-3 space-y-1 text-xs text-zinc-500">
        <div>{artifactCount ? `可下载 ${artifactCount} 张` : "等待生成素材"}</div>
        {canFillReference ? (
          <div className="text-blue-600">
            当前参考图：{selectedReferenceNode?.title || "参考图"}，可点击填充。
          </div>
        ) : (
          <div>选择一个参考图节点后，可把图片填充到该节点。</div>
        )}
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
                  canFillReference
                    ? () => {
                        if (sourceAssetId) {
                          onFillFromSourceAsset(sourceAssetId);
                          return;
                        }
                        onFillFromPoster(poster.id);
                      }
                    : undefined
                }
                useAsReferenceDisabled={!canFillReference}
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
                canFillReference
                  ? () => onFillFromSourceAsset(asset.id)
                  : undefined
              }
              useAsReferenceDisabled={!canFillReference}
              useAsReferenceBusy={fillReferenceBusy}
            />
          ))}
        </div>
      ) : (
        <div className="flex min-h-[160px] items-center justify-center rounded-xl border border-dashed border-zinc-200 bg-zinc-50/60 px-3 py-6 text-center text-xs leading-relaxed text-zinc-500">
          暂无图片
        </div>
      )}
    </section>
  );
}
