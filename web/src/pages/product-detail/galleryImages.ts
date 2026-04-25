import type { PosterVariant, ProductDetail, SourceAsset, ProductWorkflow } from "../../lib/types";
import { outputStringArray } from "./utils";

const MIME_TYPE_EXTENSIONS: Record<string, readonly string[]> = {
  "image/gif": [".gif"],
  "image/jpeg": [".jpg", ".jpeg"],
  "image/png": [".png"],
  "image/webp": [".webp"],
};

function baseFilename(filename: string): string {
  return filename.split(/[/\\]/).pop() ?? filename;
}

function materializedPosterFilenameMatches(asset: SourceAsset, poster: PosterVariant): boolean {
  if (asset.mime_type !== poster.mime_type) {
    return false;
  }
  const expectedStem = `poster-${poster.id}`;
  const filename = baseFilename(asset.original_filename);
  const extensions = MIME_TYPE_EXTENSIONS[poster.mime_type] ?? [];
  return extensions.some((extension) => filename === `${expectedStem}${extension}`);
}

export function getMaterializedPosterIdFromSourceAsset(
  asset: SourceAsset,
  postersById: ReadonlyMap<string, PosterVariant>,
): string | null {
  if (asset.kind !== "reference_image") {
    return null;
  }
  if (asset.source_poster_variant_id && postersById.has(asset.source_poster_variant_id)) {
    return asset.source_poster_variant_id;
  }
  if (Object.prototype.hasOwnProperty.call(asset, "source_poster_variant_id")) {
    return null;
  }
  for (const poster of postersById.values()) {
    if (materializedPosterFilenameMatches(asset, poster)) {
      return poster.id;
    }
  }
  return null;
}

export function buildPosterSourceAssetMap({
  product,
  workflow,
  posters,
}: {
  product: ProductDetail;
  workflow: ProductWorkflow | null;
  posters: PosterVariant[];
}): Map<string, string> {
  const posterSourceAssetIds = new Map<string, string>();
  const postersById = new Map(posters.map((poster) => [poster.id, poster]));

  for (const node of workflow?.nodes ?? []) {
    if (node.node_type === "image_generation") {
      const generatedPosterIds = [
        ...outputStringArray(node, "generated_poster_variant_ids"),
        ...outputStringArray(node, "poster_variant_ids"),
      ];
      const filledSourceAssetIds = outputStringArray(node, "filled_source_asset_ids");
      generatedPosterIds.forEach((posterId, index) => {
        const sourceAssetId = filledSourceAssetIds[index];
        if (sourceAssetId) {
          posterSourceAssetIds.set(posterId, sourceAssetId);
        }
      });
    }
    if (node.node_type === "reference_image") {
      const posterVariantId =
        typeof node.output_json?.source_poster_variant_id === "string"
          ? node.output_json.source_poster_variant_id
          : null;
      const sourceAssetId = outputStringArray(node, "source_asset_ids")[0];
      if (posterVariantId && sourceAssetId) {
        posterSourceAssetIds.set(posterVariantId, sourceAssetId);
      }
    }
  }

  for (const asset of product.source_assets) {
    const posterId = getMaterializedPosterIdFromSourceAsset(asset, postersById);
    if (posterId && !posterSourceAssetIds.has(posterId)) {
      posterSourceAssetIds.set(posterId, asset.id);
    }
  }

  return posterSourceAssetIds;
}

export function getVisibleReferenceAssets({
  product,
  posterSourceAssetIds,
  posters,
}: {
  product: ProductDetail;
  posterSourceAssetIds: ReadonlyMap<string, string>;
  posters: PosterVariant[];
}): SourceAsset[] {
  const sourceAssetIdsBackedByPosters = new Set(posterSourceAssetIds.values());
  const postersById = new Map(posters.map((poster) => [poster.id, poster]));
  return [...product.source_assets]
    .filter((asset) => asset.kind === "reference_image")
    .filter((asset) => !sourceAssetIdsBackedByPosters.has(asset.id))
    .filter((asset) => !getMaterializedPosterIdFromSourceAsset(asset, postersById))
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
}
