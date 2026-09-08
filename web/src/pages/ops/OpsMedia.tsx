import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Download, Eye } from "lucide-react";
import { Button } from "../../components/ui/button";
import { Dialog, DialogContent } from "../../components/ui/dialog";
import { api } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import type { GalleryAsset } from "../../lib/types";
import { OpsEmpty, OpsError, OpsLoading, useLiveView } from "./OpsShared";

export function OpsMedia({ merchantId, productId }: { merchantId: string; productId: string }) {
  const { t } = useI18n();
  const live = useLiveView();
  const download = useMutation({ mutationFn: (asset: GalleryAsset) => api.downloadOpsAsset(asset.download_url), onSuccess: (blob, asset) => {
    if (!live.current) return;
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url; anchor.download = asset.original_filename;
    document.body.appendChild(anchor); anchor.click(); anchor.remove(); URL.revokeObjectURL(url);
  } });
  const [cursors, setCursors] = useState<string[]>([]);
  const [preview, setPreview] = useState<GalleryAsset | null>(null);
  const after = cursors.at(-1);
  const media = useQuery({ queryKey: ["ops-media", merchantId, productId, after], queryFn: () => api.listOpsProductAssets(merchantId, productId, { after, limit: 20 }), retry: false });
  return <>
    {download.isError ? <OpsError error={download.error} /> : null}
    {media.isPending ? <OpsLoading /> : media.isError ? <OpsError error={media.error} retry={() => void media.refetch()} /> : <>
      {media.data.items.length === 0 ? <OpsEmpty /> : <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4">{media.data.items.map((asset) => <li key={asset.id} className="min-w-0 overflow-hidden rounded-panel border border-border-l1 bg-surface-raised">
        <button type="button" disabled={asset.verification_status === "missing"} onClick={() => setPreview(asset)} className="flex aspect-square w-full items-center justify-center bg-surface-panel focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-focus-ring" aria-label={`${t("graph.results.preview")}: ${asset.display_name}`}>{asset.verification_status === "missing" ? <span className="px-3 text-xs text-text-muted">{t("localEdit.validation.sourceUnavailable")}</span> : <img src={api.toApiUrl(asset.thumbnail_url)} alt={asset.display_name} className="h-full w-full object-contain" loading="lazy" />}</button>
        <div className="p-3"><p className="truncate text-xs font-medium" title={asset.display_name}>{asset.display_name}</p><div className="mt-2 flex gap-2"><Button aria-label={t("graph.results.preview")} title={t("graph.results.preview")} disabled={asset.verification_status === "missing"} onClick={() => setPreview(asset)}><Eye size={15} aria-hidden="true" /></Button>{asset.verification_status !== "missing" ? <Button busy={download.isPending && download.variables?.id === asset.id} disabled={download.isPending} onClick={() => download.mutate(asset)} aria-label={t("gallery.download")} title={t("gallery.download")}><Download size={15} aria-hidden="true" /></Button> : null}</div></div>
      </li>)}</ul>}
      <div className="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-border-l1 pt-4"><span className="text-xs text-text-muted">{t("account.page", { page: cursors.length + 1 })}</span><div className="flex gap-2"><Button disabled={!cursors.length} onClick={() => setCursors((values) => values.slice(0, -1))}>{t("account.previous")}</Button><Button disabled={!media.data.next_cursor} onClick={() => { const cursor = media.data.next_cursor; if (cursor) setCursors((values) => [...values, cursor]); }}>{t("account.next")}</Button></div></div>
    </>}
    <Dialog open={Boolean(preview)} onOpenChange={(open) => { if (!open) setPreview(null); }}><DialogContent title={preview?.display_name ?? t("graph.results.preview")} closeLabel={t("account.cancel")} onClose={() => setPreview(null)} size="xl">{preview ? <img src={api.toApiUrl(preview.preview_url)} alt={preview.display_name} className="max-h-[70dvh] w-full object-contain" /> : null}</DialogContent></Dialog>
  </>;
}
