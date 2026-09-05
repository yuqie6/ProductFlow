import { useInfiniteQuery } from "@tanstack/react-query";
import { ChevronDown, History, RefreshCw } from "lucide-react";
import { useState } from "react";
import { api } from "../../../lib/api";
import { formatDateTime } from "../../../lib/format";
import { toImageUrl, type DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import { IconButton } from "../../../components/ui/icon-button";
import { DownloadLink } from "../chrome/ImageDownloadComponents";

export function NodeImageHistory({ productId, nodeId, currentAssetId, onPreview }: {
  productId: string; nodeId: string; currentAssetId: string | null;
  onPreview: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const query = useInfiniteQuery({
    queryKey: ["node-image-history", productId, nodeId, currentAssetId],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam }) => api.listGalleryAssets(productId, {
      directory_kind: "generated", node_id: nodeId, limit: 12, after: pageParam,
    }),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: open,
  });
  return <section data-node-image-history className="space-y-2">
    <IconButton label={t("nodeDetail.history")} onClick={() => setOpen(!open)} aria-expanded={open}><History size={14} /></IconButton>
    {open ? <>
      {query.isPending ? <p className="text-xs text-text-muted">{t("app.loading")}</p> : null}
      {query.isError ? <div role="alert" className="text-xs text-state-error">{t("workbench.error.structure")}
        <IconButton label={t("workbench.retry")} onClick={() => void query.refetch()}><RefreshCw size={14} /></IconButton>
      </div> : null}
      {!query.isPending && !query.isError && !query.data?.pages.some((page) => page.items.length) ? <p className="text-xs text-text-muted">{t("nodeDetail.noResult")}</p> : null}
      <div className="grid grid-cols-3 gap-2">
        {query.data?.pages.flatMap((page) => page.items).map((asset) => {
          const image: DownloadableImage = { previewUrl: toImageUrl(asset.preview_url), downloadUrl: toImageUrl(asset.download_url), filename: asset.original_filename, alt: asset.display_name };
          return <div key={asset.id} className="relative min-w-0" data-history-asset={asset.id}>
            <button type="button" onClick={() => onPreview(image)} aria-label={t("detail.previewImage", { alt: asset.display_name })}
              className="block aspect-square w-full overflow-hidden rounded border border-border-l1 focus-visible:outline-accent" title={formatDateTime(asset.created_at, t.locale)}>
              <img src={toImageUrl(asset.thumbnail_url)} alt={asset.display_name} className="h-full w-full object-contain" />
            </button>
            <DownloadLink image={image} variant="overlay" />
          </div>;
        })}
      </div>
      {query.hasNextPage ? <IconButton label={t("nodeDetail.moreHistory")} disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}><ChevronDown size={14} /></IconButton> : null}
    </> : null}
  </section>;
}
