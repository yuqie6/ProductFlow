import { Layers3, Sparkles } from "lucide-react";

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
  retryingTaskId: string | null;
  cancellingTaskId: string | null;
  regenerating: boolean;
  onPreviewRound: (round: ImageSessionRound) => void;
  onRetryGenerationTask: (task: ImageSessionGenerationTask) => void;
  onCancelGenerationTask: (task: ImageSessionGenerationTask) => void;
  onRegenerateGenerationTask: (task: ImageSessionGenerationTask) => void;
  t: ImageChatTranslate;
}

export function ImageChatMainStage({
  selectedRound,
  selectedPlaceholder,
  branchBaseRound,
  retryingTaskId,
  cancellingTaskId,
  regenerating,
  onPreviewRound,
  onRetryGenerationTask,
  onCancelGenerationTask,
  onRegenerateGenerationTask,
  t,
}: ImageChatMainStageProps) {
  return (
    <div className="relative flex min-h-[18rem] flex-1 items-center justify-center overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm dark:border-slate-600/80 dark:bg-[#121b2d] dark:shadow-[0_0_0_1px_rgba(139,92,246,0.10),0_24px_80px_rgba(0,0,0,0.35)] sm:min-h-[22rem] lg:min-h-[360px]">
      <div className="absolute inset-0 bg-[radial-gradient(#cbd5e1_1px,transparent_1px)] [background-size:20px_20px] dark:bg-[radial-gradient(rgba(148,163,184,0.26)_1px,transparent_1px)]" />
      <div className="pointer-events-none absolute inset-x-0 top-0 z-10 flex items-center justify-between gap-3 px-5 py-4">
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
        <div className="ml-auto flex shrink-0 items-center gap-2">
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
          regenerating={regenerating}
          onRetry={onRetryGenerationTask}
          onCancel={onCancelGenerationTask}
          onRegenerate={onRegenerateGenerationTask}
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
