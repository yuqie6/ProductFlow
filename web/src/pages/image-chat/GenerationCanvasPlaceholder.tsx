import { Loader2, OctagonX, RotateCcw, Sparkles } from "lucide-react";

import { formatImageSizeValue } from "../../lib/imageSizes";
import type { ImageSessionGenerationTask } from "../../lib/types";
import {
  imageGenerationRetryMetadata,
  isImageSessionGenerationTaskCancelable,
  isImageSessionGenerationTaskRegeneratable,
  isImageSessionGenerationTaskRetryable,
} from "./branching";
import type { ImageHistoryPlaceholderCandidate } from "./branching";
import type { ImageChatTranslate } from "./display";
import { generationTaskQueueText, placeholderStatusLabel } from "./display";

interface GenerationCanvasPlaceholderProps {
  candidate: ImageHistoryPlaceholderCandidate;
  retrying: boolean;
  cancelling: boolean;
  regenerating: boolean;
  onRetry: (task: ImageSessionGenerationTask) => void;
  onCancel: (task: ImageSessionGenerationTask) => void;
  onRegenerate: (task: ImageSessionGenerationTask) => void;
  t: ImageChatTranslate;
}

export function GenerationCanvasPlaceholder({
  candidate,
  retrying,
  cancelling,
  regenerating,
  onRetry,
  onCancel,
  onRegenerate,
  t,
}: GenerationCanvasPlaceholderProps) {
  const active = candidate.status === "queued" || candidate.status === "running";
  const failed = candidate.status === "failed";
  const unknown = candidate.status === "unknown";
  const cancelled = candidate.status === "cancelled";
  const retryable = isImageSessionGenerationTaskRetryable(candidate.task);
  const regeneratable = isImageSessionGenerationTaskRegeneratable(candidate.task);
  const queueText = generationTaskQueueText(candidate.task, t);
  const retryMetadata = imageGenerationRetryMetadata(candidate.task);
  const nonRetryableReason = candidate.failure_reason ?? retryMetadata?.last_failure_reason;

  return (
    <div className="relative z-0 flex h-full min-h-0 w-full items-center justify-center px-6 pb-6 pt-16">
      <div className="flex max-w-md flex-col items-center text-center">
        <div
          className={`relative flex h-72 w-72 items-center justify-center overflow-hidden rounded-panel border shadow-elev-1 transition-[border-color,box-shadow] duration-slow ${
            failed
              ? "border-state-error bg-state-error-soft text-state-error dark:border-state-error/35 dark:bg-state-error/10 dark:text-state-error"
              : unknown
                ? "border-state-warning bg-state-warning-soft text-state-warning dark:border-state-warning/35 dark:bg-state-warning/10 dark:text-state-warning"
              : active
                ? "animate-status-pulse border-accent/55 bg-accent-soft text-accent motion-reduce:animate-none"
                : "border-accent bg-accent-soft text-accent dark:border-accent/35 dark:bg-accent/14 dark:text-accent"
          }`}
        >
          {active ? (
            <>
              <Loader2 size={48} className="relative z-10 animate-spin text-accent motion-reduce:animate-none" />
            </>
          ) : (
            <Sparkles size={48} className="relative" />
          )}
        </div>
        <div className="mt-4 text-sm font-semibold text-text-primary">{placeholderStatusLabel(candidate, t)}</div>
        <div className="mt-1 text-xs text-text-secondary">
          {t("chat.candidate", { index: candidate.candidate_index, count: candidate.candidate_count })} · {formatImageSizeValue(candidate.size)}
        </div>
        {queueText ? <div className="mt-3 max-w-sm text-xs leading-5 text-text-secondary">{queueText}</div> : null}
        <div className="mt-4 line-clamp-3 max-w-sm rounded-xl border border-border-l1/80 bg-surface-raised/80 px-3 py-2 text-xs font-medium leading-5 text-text-primary shadow-sm dark:border-border-l1/70 dark:bg-surface-base/75 dark:text-text-primary">
          {candidate.prompt}
        </div>
        {isImageSessionGenerationTaskCancelable(candidate.task) ? (
          <button
            type="button"
            onClick={() => onCancel(candidate.task)}
            disabled={cancelling}
            className="mt-5 inline-flex items-center justify-center rounded-xl border border-state-error bg-surface-raised px-4 py-2 text-sm font-semibold text-state-error shadow-sm transition-colors hover:bg-state-error-soft disabled:opacity-60 dark:border-state-error/40 dark:bg-surface-panel dark:text-state-error dark:hover:bg-state-error/12"
          >
            {cancelling ? <Loader2 size={15} className="mr-2 animate-spin" /> : <OctagonX size={15} className="mr-2" />}
            {t("chat.cancelGeneration")}
          </button>
        ) : null}
        {failed && retryable ? (
          <button
            type="button"
            onClick={() => onRetry(candidate.task)}
            disabled={retrying}
            className="mt-5 inline-flex items-center justify-center rounded-xl bg-state-error px-4 py-2 text-sm font-semibold text-state-error-fg shadow-sm transition-colors hover:bg-state-error disabled:opacity-60"
          >
            {retrying ? <Loader2 size={15} className="mr-2 animate-spin" /> : <RotateCcw size={15} className="mr-2" />}
            {t("chat.retryGeneration")}
          </button>
        ) : failed ? (
          <div className="mt-5 max-w-sm rounded-xl border border-state-error bg-surface-raised px-3 py-2 text-xs font-medium leading-5 text-state-error dark:border-state-error/40 dark:bg-surface-panel dark:text-state-error">
            <div>{t("chat.notRetryable")}</div>
            {nonRetryableReason ? <div className="mt-1 text-state-error/80 dark:text-state-error/80">{nonRetryableReason}</div> : null}
          </div>
        ) : cancelled ? (
          <>
            <div className="mt-5 rounded-xl border border-border-l1 bg-surface-raised px-3 py-2 text-xs font-medium text-text-muted dark:border-border-l1 dark:bg-surface-panel dark:text-text-secondary">
              {t("chat.taskCancelled")}
            </div>
            {regeneratable ? (
              <button
                type="button"
                onClick={() => onRegenerate(candidate.task)}
                disabled={regenerating}
                className="mt-3 inline-flex items-center justify-center rounded-xl bg-accent px-4 py-2 text-sm font-semibold text-accent-fg shadow-sm transition-colors hover:bg-accent disabled:opacity-60 dark:bg-accent dark:hover:bg-accent"
              >
                {regenerating ? <Loader2 size={15} className="mr-2 animate-spin" /> : <RotateCcw size={15} className="mr-2" />}
                {t("chat.regenerateCancelled")}
              </button>
            ) : null}
          </>
        ) : unknown ? (
          <div className="mt-5 max-w-sm rounded-xl border border-state-warning bg-surface-raised px-3 py-2 text-xs font-medium leading-5 text-state-warning dark:border-state-warning/40 dark:bg-surface-panel dark:text-state-warning">
            <div>{t("chat.providerResultUnknown")}</div>
            {nonRetryableReason ? <div className="mt-1 text-state-warning/80 dark:text-state-warning/80">{nonRetryableReason}</div> : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
