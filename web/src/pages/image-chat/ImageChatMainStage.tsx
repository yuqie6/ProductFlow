import { Download, GalleryHorizontalEnd, Layers3, Loader2, Sparkles } from "lucide-react";

import { api } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import type { ImageSessionGenerationTask, ImageSessionRound } from "../../lib/types";
import type { ImageHistoryPlaceholderCandidate } from "./branching";
import { GenerationCanvasPlaceholder } from "./GenerationCanvasPlaceholder";
import type { ImageChatTranslate } from "./display";
import { placeholderSizeLabel, placeholderStatusLabel } from "./display";

interface ImageChatMainStageProps {
  selectedRound: ImageSessionRound | null;
  selectedPlaceholder: ImageHistoryPlaceholderCandidate | null;
  branchBaseRound: ImageSessionRound | null;
  saveGalleryPending: boolean;
  retryingTaskId: string | null;
  cancellingTaskId: string | null;
  onPreviewRound: (round: ImageSessionRound) => void;
  onSaveSelectedToGallery: () => void;
  onRetryGenerationTask: (task: ImageSessionGenerationTask) => void;
  onCancelGenerationTask: (task: ImageSessionGenerationTask) => void;
  t: ImageChatTranslate;
}

export function ImageChatMainStage({
  selectedRound,
  selectedPlaceholder,
  branchBaseRound,
  saveGalleryPending,
  retryingTaskId,
  cancellingTaskId,
  onPreviewRound,
  onSaveSelectedToGallery,
  onRetryGenerationTask,
  onCancelGenerationTask,
  t,
}: ImageChatMainStageProps) {
  return (
    <div className="relative flex min-h-[50dvh] flex-1 items-center justify-center overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm max-h-[58dvh] dark:border-slate-600/80 dark:bg-[#121b2d] dark:shadow-[0_0_0_1px_rgba(139,92,246,0.10),0_24px_80px_rgba(0,0,0,0.35)] lg:min-h-[360px] lg:max-h-none">
      <div className="absolute inset-0 bg-[radial-gradient(#cbd5e1_1px,transparent_1px)] [background-size:20px_20px] dark:bg-[radial-gradient(rgba(148,163,184,0.26)_1px,transparent_1px)]" />
      <div className="absolute inset-x-0 top-0 z-10 flex items-center justify-between gap-3 px-5 py-4">
        {selectedRound ? (
          <div className="hidden min-w-0 max-w-[calc(100%-5.5rem)] truncate rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-600 shadow-sm ring-1 ring-slate-200 backdrop-blur dark:bg-slate-950/82 dark:text-slate-200 dark:ring-slate-700 lg:block">
            {formatDateTime(selectedRound.created_at)} · {selectedRound.model_name}
          </div>
        ) : selectedPlaceholder ? (
          <div className="hidden min-w-0 max-w-[calc(100%-5.5rem)] truncate rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-600 shadow-sm ring-1 ring-slate-200 backdrop-blur dark:bg-slate-950/82 dark:text-slate-200 dark:ring-slate-700 lg:block">
            {placeholderStatusLabel(selectedPlaceholder, t)} · {placeholderSizeLabel(selectedPlaceholder)}
          </div>
        ) : (
          <div className="hidden rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-500 shadow-sm ring-1 ring-slate-200 backdrop-blur dark:border dark:border-violet-400/35 dark:bg-slate-950/82 dark:text-violet-100 dark:ring-violet-400/20 lg:block">
            {t("chat.waitingFirstResult")}
          </div>
        )}
        <div className="hidden shrink-0 items-center gap-2 lg:flex">
          {selectedRound ? (
            <>
              <a
                href={api.toApiUrl(selectedRound.generated_asset.download_url)}
                target="_blank"
                rel="noreferrer"
                title={t("chat.downloadCurrent")}
                aria-label={t("chat.downloadCurrent")}
                className="inline-flex h-11 w-11 items-center justify-center rounded-2xl border border-slate-200 bg-white/92 text-slate-700 shadow-sm backdrop-blur transition-colors active:scale-[0.98] hover:border-indigo-200 hover:text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:border-slate-700 dark:bg-slate-950/82 dark:text-slate-200 dark:hover:border-violet-400/60 dark:hover:text-violet-100 lg:hidden"
              >
                <Download size={16} />
              </a>
              <button
                type="button"
                onClick={onSaveSelectedToGallery}
                disabled={saveGalleryPending}
                title={t("chat.saveSelectedGallery")}
                aria-label={t("chat.saveSelectedGallery")}
                className="inline-flex h-11 w-11 items-center justify-center rounded-2xl bg-indigo-600 text-white shadow-sm shadow-indigo-500/20 ring-1 ring-indigo-500 transition-colors active:scale-[0.98] hover:bg-indigo-700 disabled:opacity-60 dark:bg-gradient-to-r dark:from-indigo-500 dark:to-violet-500 dark:shadow-violet-900/35 dark:ring-violet-300/35 lg:hidden"
              >
                {saveGalleryPending ? <Loader2 size={16} className="animate-spin" /> : <GalleryHorizontalEnd size={16} />}
              </button>
            </>
          ) : null}
          {branchBaseRound ? (
            <div className="hidden h-8 items-center gap-1.5 rounded-full bg-indigo-600 px-3 text-xs font-semibold text-white shadow-sm shadow-indigo-500/20 dark:bg-violet-500/20 dark:text-violet-100 dark:ring-1 dark:ring-violet-400/40 sm:inline-flex">
              <Layers3 size={13} />
              {t("chat.baseSelected")}
            </div>
          ) : null}
        </div>
      </div>

      {selectedRound ? (
        <div className="absolute inset-0 z-0 flex min-h-0 w-full items-center justify-center px-2 py-2 sm:px-3 sm:py-3 lg:pt-14">
          <button
            type="button"
            onClick={() => onPreviewRound(selectedRound)}
            className="flex h-full w-full items-center justify-center rounded-2xl focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-violet-400"
            aria-label={t("chat.previewCurrent")}
            title={t("chat.previewCurrent")}
          >
            <img
              src={api.toApiUrl(selectedRound.generated_asset.preview_url)}
              alt={t("chat.currentResultAlt")}
              decoding="async"
              className="max-h-full max-w-full object-contain drop-shadow-2xl"
            />
          </button>
        </div>
      ) : selectedPlaceholder ? (
        <GenerationCanvasPlaceholder
          candidate={selectedPlaceholder}
          retrying={retryingTaskId === selectedPlaceholder.task_id}
          cancelling={cancellingTaskId === selectedPlaceholder.task_id}
          onRetry={onRetryGenerationTask}
          onCancel={onCancelGenerationTask}
          t={t}
        />
      ) : (
        <div className="relative z-0 flex flex-col items-center gap-4 text-center text-slate-400 dark:text-slate-100">
          <div className="flex h-16 w-16 items-center justify-center rounded-3xl bg-white shadow-sm ring-1 ring-slate-200 dark:bg-slate-950/86 dark:text-violet-200 dark:ring-violet-400/35">
            <Sparkles size={28} />
          </div>
          <div>
            <div className="text-sm font-semibold text-slate-600 dark:text-white">{t("chat.noResult")}</div>
          </div>
        </div>
      )}
    </div>
  );
}
