import { type CSSProperties, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Image as ImageIcon, Loader2 } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { GalleryImagePreviewDialog } from "../components/GalleryImagePreviewDialog";
import { TopNav } from "../components/TopNav";
import { api } from "../lib/api";
import { formatDateTime } from "../lib/format";
import { useI18n } from "../lib/preferences";
import type { GalleryEntry } from "../lib/types";
import { galleryEntrySizeLabel, galleryTileLayout } from "./gallery/helpers";

function metadataRows(entry: GalleryEntry, locale: ReturnType<typeof useI18n>["locale"], t: ReturnType<typeof useI18n>["t"]) {
  return [
    ["gallery.meta.size", galleryEntrySizeLabel(entry, locale)],
    ["gallery.meta.model", [entry.provider_name, entry.model_name].filter(Boolean).join(" / ") || t("common.unknown")],
    ["gallery.meta.session", entry.image_session_title],
    ["gallery.meta.product", entry.product_name ?? t("gallery.global")],
    [
      "gallery.meta.candidate",
      entry.candidate_index != null && entry.candidate_count != null
        ? `${entry.candidate_index}/${entry.candidate_count}`
        : t("common.unknown"),
    ],
    ["gallery.meta.savedAt", formatDateTime(entry.created_at)],
  ] as const;
}

export function GalleryPage() {
  const { locale, t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [previewEntry, setPreviewEntry] = useState<GalleryEntry | null>(null);
  const [gridContentWidth, setGridContentWidth] = useState<number | null>(null);
  const [isDesktopGrid, setIsDesktopGrid] = useState(false);
  const gridRef = useRef<HTMLDivElement | null>(null);

  const galleryQuery = useQuery({
    queryKey: ["gallery"],
    queryFn: api.listGalleryEntries,
  });
  const entries = galleryQuery.data?.items ?? [];

  useEffect(() => {
    const updateGridMetrics = () => {
      setIsDesktopGrid(window.matchMedia("(min-width: 1024px)").matches);
      if (gridRef.current) {
        setGridContentWidth(gridRef.current.clientWidth);
      }
    };

    updateGridMetrics();
    window.addEventListener("resize", updateGridMetrics);

    const gridElement = gridRef.current;
    const resizeObserver =
      typeof ResizeObserver === "undefined" || !gridElement ? null : new ResizeObserver(updateGridMetrics);
    if (gridElement) {
      resizeObserver?.observe(gridElement);
    }

    return () => {
      window.removeEventListener("resize", updateGridMetrics);
      resizeObserver?.disconnect();
    };
  }, []);

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  return (
    <div className="min-h-screen bg-[#07111d] text-slate-950">
      <TopNav breadcrumbs={t("gallery.title")} onHome={() => navigate("/products")} onLogout={() => logoutMutation.mutate()} />

      <main className="w-full">
        {galleryQuery.isLoading ? (
          <div className="flex min-h-[calc(100svh-80px)] items-center justify-center bg-[#f3eadc] text-slate-500">
            <Loader2 size={28} className="animate-spin" />
          </div>
        ) : galleryQuery.isError ? (
          <div className="flex min-h-[calc(100svh-80px)] items-center justify-center bg-[#f3eadc] px-6 text-sm font-medium text-red-700">
            {t("gallery.loadFailed")}
          </div>
        ) : entries.length ? (
          <>
            <section className="relative isolate min-h-[420px] overflow-hidden bg-[#f4eddf] sm:min-h-[480px] lg:min-h-[460px]">
              <img
                src="/hero.png"
                alt=""
                decoding="async"
                className="absolute inset-y-0 right-0 h-full w-full object-cover object-center opacity-35 sm:opacity-50 lg:w-[62%] lg:opacity-100"
              />
              <div className="absolute inset-0 bg-[linear-gradient(90deg,#f4eddf_0%,rgba(244,237,223,0.99)_36%,rgba(244,237,223,0.72)_52%,rgba(244,237,223,0.08)_76%,rgba(244,237,223,0)_100%)]" />
              <div className="absolute inset-x-0 bottom-0 h-px bg-[#020617]/10" />
              <div className="absolute left-5 top-16 hidden h-64 flex-col items-center gap-4 text-[#1d4cff] sm:flex">
                <span className="h-2 w-2 rounded-full bg-[#1d4cff]" />
                <span className="h-28 w-px bg-[#1d4cff]/30" />
                <span className="[writing-mode:vertical-rl] text-xs font-black uppercase tracking-[0.18em]">Gallery</span>
                <span className="h-2 w-2 rounded-full border-2 border-[#1d4cff]" />
              </div>

              <div className="relative z-10 mx-auto grid min-h-[420px] max-w-7xl grid-cols-1 px-6 py-14 sm:min-h-[480px] sm:px-10 lg:min-h-[460px] lg:grid-cols-[minmax(0,0.43fr)_minmax(360px,0.57fr)] lg:items-center lg:px-14">
                <div className="max-w-xl">
                  <div className="mb-7 h-px w-44 bg-[#020617]/22" />
                  <h1 className="text-6xl font-black leading-none text-[#020617] sm:text-7xl lg:text-8xl">
                    {t("gallery.title")}
                  </h1>
                  <p className="mt-6 max-w-md text-base leading-7 text-[#1f2937]">
                    {t("gallery.description")}
                  </p>
                  <div className="mt-7 flex max-w-xs items-center gap-3">
                    <span className="relative h-4 w-4 rounded-full border-2 border-[#020617]">
                      <span className="absolute left-1/2 top-1/2 h-1.5 w-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-[#020617]" />
                    </span>
                    <span className="h-px flex-1 bg-[#020617]/18" />
                  </div>
                </div>

                <div className="hidden lg:block" />
              </div>
            </section>

            <section className="bg-[#07111d] px-4 py-8 sm:px-6 lg:px-10">
              <div className="mx-auto mb-6 flex max-w-7xl items-end justify-between gap-4 border-b border-white/10 pb-5">
                <div>
                  <div className="text-xs font-bold uppercase text-indigo-300">{t("gallery.feed")}</div>
                  <h2 className="mt-2 text-2xl font-black text-white">{t("gallery.works")}</h2>
                </div>
                <div className="text-sm font-medium text-white/55">{t("gallery.count", { count: entries.length })}</div>
              </div>

              <div
                ref={gridRef}
                className="mx-auto grid max-w-7xl grid-flow-dense grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-12 lg:auto-rows-[8px]"
              >
                {entries.map((entry, index) => {
                  const tileLayout = galleryTileLayout(entry, index, gridContentWidth ?? undefined);
                  const tileStyle: CSSProperties = {
                    aspectRatio: tileLayout.aspectRatio,
                    ...(isDesktopGrid ? { gridRowEnd: `span ${tileLayout.rowSpan}` } : {}),
                  };
                  return (
                    <button
                      key={entry.id}
                      type="button"
                      onClick={() => setPreviewEntry(entry)}
                      className={`group relative min-w-0 overflow-hidden rounded-md bg-slate-900 text-left shadow-sm transition duration-300 hover:-translate-y-1 hover:shadow-2xl hover:shadow-[#0b4eea]/20 ${tileLayout.className}`}
                      style={tileStyle}
                    >
                      <div className="relative h-full overflow-hidden bg-slate-900">
                        <img
                          src={api.toApiUrl(entry.image.thumbnail_url)}
                          alt={entry.prompt ?? entry.image.original_filename}
                          loading="lazy"
                          decoding="async"
                          className="h-full w-full object-contain transition duration-300"
                        />
                        <div className="absolute inset-0 bg-gradient-to-t from-slate-950/82 via-slate-950/10 to-transparent opacity-80 transition-opacity group-hover:opacity-95" />
                        <div className="absolute inset-x-0 bottom-0 p-4 text-white">
                          <div className="line-clamp-2 text-sm font-semibold leading-5">
                            {entry.prompt ?? entry.image.original_filename}
                          </div>
                          <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px] font-semibold text-white/70">
                            <span>{galleryEntrySizeLabel(entry, locale)}</span>
                            <span>{formatDateTime(entry.created_at)}</span>
                          </div>
                        </div>
                      </div>
                    </button>
                  );
                })}
              </div>

            </section>
          </>
        ) : (
          <div className="flex min-h-[calc(100svh-80px)] flex-col items-center justify-center bg-[#f3eadc] px-6 text-sm text-slate-600">
            <ImageIcon size={30} className="mb-4 text-indigo-500" />
            <div className="text-5xl font-black text-slate-950">{t("gallery.title")}</div>
            <div className="mt-4 text-center">{t("gallery.empty")}</div>
          </div>
        )}
      </main>

      {previewEntry ? (
        <GalleryImagePreviewDialog
          ariaLabel={t("gallery.previewLabel")}
          imageUrl={api.toApiUrl(previewEntry.image.preview_url)}
          imageAlt={previewEntry.prompt ?? previewEntry.image.original_filename}
          title={t("gallery.prompt")}
          subtitle={previewEntry.image.original_filename}
          body={previewEntry.prompt ?? t("gallery.noPrompt")}
          metadataRows={metadataRows(previewEntry, locale, t).map(([label, value]) => ({ label: t(label), value }))}
          providerNotes={previewEntry.provider_notes}
          providerNotesTitle={t("gallery.providerNotes")}
          downloadUrl={previewEntry.image.download_url}
          downloadLabel={t("gallery.download")}
          closeLabel={t("gallery.closePreview")}
          onClose={() => setPreviewEntry(null)}
        />
      ) : null}
    </div>
  );
}
