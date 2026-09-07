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
    <div className="relative flex min-h-[18rem] flex-1 items-center justify-center overflow-hidden rounded-surface border border-border-l1 bg-surface-panel sm:min-h-[22rem] lg:min-h-[360px]">
      <div className="absolute inset-0 bg-[radial-gradient(var(--color-canvas-dot)_1px,transparent_1px)] [background-size:20px_20px]" />
      <div className="pointer-events-none absolute inset-x-0 top-0 z-10 flex items-center justify-between gap-3 px-5 py-4">
        {selectedRound ? (
          <div className="hidden min-w-0 max-w-[calc(100%-5.5rem)] truncate rounded-full bg-surface-raised/90 px-3 py-1.5 text-xs font-medium text-text-secondary shadow-sm ring-1 ring-border-l1 backdrop-blur dark:bg-surface-base/82 dark:text-text-primary dark:ring-border-l1 lg:block">
            {formatDateTime(selectedRound.created_at, t.locale)} · {selectedRound.model_name}
          </div>
        ) : selectedPlaceholder ? (
          <div className="hidden min-w-0 max-w-[calc(100%-5.5rem)] truncate rounded-full bg-surface-raised/90 px-3 py-1.5 text-xs font-medium text-text-secondary shadow-sm ring-1 ring-border-l1 backdrop-blur dark:bg-surface-base/82 dark:text-text-primary dark:ring-border-l1 lg:block">
            {placeholderStatusLabel(selectedPlaceholder, t)} · {placeholderSizeLabel(selectedPlaceholder)}
          </div>
        ) : null}
        <div className="ml-auto flex shrink-0 items-center gap-2">
          {branchBaseRound ? (
            <div className="hidden h-8 items-center gap-1.5 rounded-full bg-accent px-3 text-xs font-semibold text-accent-fg shadow-sm dark:bg-accent/20 dark:text-accent dark:ring-1 dark:ring-accent/40 sm:inline-flex">
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
            className="flex h-full w-full items-center justify-center rounded-2xl focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:focus-visible:ring-accent"
            aria-label={t("chat.previewCurrent")}
            title={t("chat.previewCurrent")}
          >
            <img
              src={api.toApiUrl(selectedRound.generated_asset.preview_url)}
              alt={t("chat.currentResultAlt")}
              decoding="async"
              className="max-h-full max-w-full object-contain"
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
        <div className="relative z-0 flex flex-col items-center gap-4 text-center text-text-muted dark:text-text-primary">
          <div className="flex h-16 w-16 items-center justify-center rounded-3xl bg-surface-raised shadow-sm ring-1 ring-border-l1 dark:bg-surface-base/86 dark:text-accent dark:ring-accent/35">
            <Sparkles size={28} />
          </div>
          <div>
            <div className="text-sm font-semibold text-text-secondary dark:text-white">{t("chat.noResult")}</div>
          </div>
        </div>
      )}
    </div>
  );
}
