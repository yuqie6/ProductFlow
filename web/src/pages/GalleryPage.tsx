import { type CSSProperties, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Image as ImageIcon, Loader2, X } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api } from "../lib/api";
import { formatDateTime } from "../lib/format";
import type { GalleryEntry } from "../lib/types";
import { galleryEntrySizeLabel, galleryTileLayout } from "./gallery/helpers";

function metadataRows(entry: GalleryEntry) {
  return [
    ["尺寸", galleryEntrySizeLabel(entry)],
    ["模型", [entry.provider_name, entry.model_name].filter(Boolean).join(" / ") || "未知"],
    ["会话", entry.image_session_title],
    ["商品", entry.product_name ?? "全局"],
    [
      "候选",
      entry.candidate_index != null && entry.candidate_count != null
        ? `${entry.candidate_index}/${entry.candidate_count}`
        : "未知",
    ],
    ["保存时间", formatDateTime(entry.created_at)],
  ] as const;
}

export function GalleryPage() {
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
      <TopNav breadcrumbs="画廊" onHome={() => navigate("/products")} onLogout={() => logoutMutation.mutate()} />

      <main className="w-full">
        {galleryQuery.isLoading ? (
          <div className="flex min-h-[calc(100svh-80px)] items-center justify-center bg-[#f3eadc] text-slate-500">
            <Loader2 size={28} className="animate-spin" />
          </div>
        ) : galleryQuery.isError ? (
          <div className="flex min-h-[calc(100svh-80px)] items-center justify-center bg-[#f3eadc] px-6 text-sm font-medium text-red-700">
            画廊加载失败
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
              <div className="absolute inset-x-0 bottom-0 h-px bg-slate-950/10" />
              <div className="absolute left-5 top-16 hidden h-64 flex-col items-center gap-4 text-[#1d4cff] sm:flex">
                <span className="h-2 w-2 rounded-full bg-[#1d4cff]" />
                <span className="h-28 w-px bg-[#1d4cff]/30" />
                <span className="[writing-mode:vertical-rl] text-xs font-black uppercase tracking-[0.18em]">Gallery</span>
                <span className="h-2 w-2 rounded-full border-2 border-[#1d4cff]" />
              </div>

              <div className="relative z-10 mx-auto grid min-h-[420px] max-w-7xl grid-cols-1 px-6 py-14 sm:min-h-[480px] sm:px-10 lg:min-h-[460px] lg:grid-cols-[minmax(0,0.43fr)_minmax(360px,0.57fr)] lg:items-center lg:px-14">
                <div className="max-w-xl">
                  <div className="mb-7 h-px w-44 bg-slate-950/22" />
                  <h1 className="text-6xl font-black leading-none text-slate-950 sm:text-7xl lg:text-8xl">画廊</h1>
                  <p className="mt-6 max-w-md text-base leading-7 text-slate-800">
                    由 AI 生成的创意作品，灵感无限，想象即现实。
                  </p>
                  <div className="mt-7 flex max-w-xs items-center gap-3">
                    <span className="relative h-4 w-4 rounded-full border-2 border-slate-950">
                      <span className="absolute left-1/2 top-1/2 h-1.5 w-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-slate-950" />
                    </span>
                    <span className="h-px flex-1 bg-slate-950/18" />
                  </div>
                </div>

                <div className="hidden lg:block" />
              </div>
            </section>

            <section className="bg-[#07111d] px-4 py-8 sm:px-6 lg:px-10">
              <div className="mx-auto mb-6 flex max-w-7xl items-end justify-between gap-4 border-b border-white/10 pb-5">
                <div>
                  <div className="text-xs font-bold uppercase text-indigo-300">Gallery Feed</div>
                  <h2 className="mt-2 text-2xl font-black text-white">作品</h2>
                </div>
                <div className="text-sm font-medium text-white/55">{entries.length} 张</div>
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
                            <span>{galleryEntrySizeLabel(entry)}</span>
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
            <div className="text-5xl font-black text-slate-950">画廊</div>
            <div className="mt-4 text-center">还没有保存到画廊的图片</div>
          </div>
        )}
      </main>

      {previewEntry ? (
        <div
          className="fixed inset-0 z-[80] flex items-center justify-center bg-slate-950/86 p-4 backdrop-blur-sm"
          role="dialog"
          aria-modal="true"
          aria-label="画廊大图预览"
        >
          <div className="grid max-h-[92svh] w-full max-w-6xl overflow-y-auto rounded-lg bg-white shadow-2xl lg:grid-cols-[minmax(0,1fr)_340px] lg:overflow-hidden">
            <div className="flex min-h-[280px] max-h-[45svh] items-center justify-center bg-slate-950 lg:min-h-[320px] lg:max-h-none">
              <img
                src={api.toApiUrl(previewEntry.image.preview_url)}
                alt={previewEntry.prompt ?? previewEntry.image.original_filename}
                decoding="async"
                className="max-h-[92svh] w-full object-contain"
              />
            </div>
            <aside className="flex min-h-0 flex-col border-l border-slate-200">
              <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3">
                <div>
                  <div className="text-sm font-bold text-slate-950">原提示词</div>
                  <div className="mt-0.5 text-xs text-slate-500">{previewEntry.image.original_filename}</div>
                </div>
                <button
                  type="button"
                  onClick={() => setPreviewEntry(null)}
                  className="inline-flex h-9 w-9 items-center justify-center rounded-lg text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-950"
                  aria-label="关闭预览"
                >
                  <X size={18} />
                </button>
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                <p className="whitespace-pre-wrap break-words text-sm leading-6 text-slate-800">
                  {previewEntry.prompt ?? "无 Prompt"}
                </p>
                <div className="mt-6 grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
                  {metadataRows(previewEntry).map(([label, value]) => (
                    <div key={label} className="min-w-0">
                      <div className="font-semibold text-slate-400">{label}</div>
                      <div className="mt-1 truncate font-medium text-slate-800">{value}</div>
                    </div>
                  ))}
                </div>
                {previewEntry.provider_notes.length ? (
                  <div className="mt-6 border-t border-slate-200 pt-4">
                    <div className="text-xs font-bold uppercase text-slate-400">Provider Notes</div>
                    <ul className="mt-2 space-y-1 text-xs leading-5 text-slate-600">
                      {previewEntry.provider_notes.map((note) => (
                        <li key={note}>{note}</li>
                      ))}
                    </ul>
                  </div>
                ) : null}
              </div>
              <div className="border-t border-slate-200 p-4">
                <a
                  href={api.toApiUrl(previewEntry.image.download_url)}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex w-full items-center justify-center rounded-lg bg-slate-950 px-3 py-2.5 text-sm font-semibold text-white transition-colors hover:bg-indigo-700"
                >
                  <Download size={16} className="mr-2" />
                  下载原图
                </a>
              </div>
            </aside>
          </div>
        </div>
      ) : null}
    </div>
  );
}
