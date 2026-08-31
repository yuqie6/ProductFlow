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
              ? "border-red-200 bg-red-50 text-red-600 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200"
              : unknown
                ? "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-400/35 dark:bg-amber-500/10 dark:text-amber-100"
              : active
                ? "animate-status-pulse border-accent/55 bg-accent-soft text-accent motion-reduce:animate-none"
                : "border-indigo-100 bg-indigo-50 text-indigo-700 dark:border-violet-400/35 dark:bg-violet-500/14 dark:text-violet-100"
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
        <div className="mt-4 line-clamp-3 max-w-sm rounded-xl border border-slate-200/80 bg-white/80 px-3 py-2 text-xs font-medium leading-5 text-[#334155] shadow-sm dark:border-slate-700/70 dark:bg-slate-950/75 dark:text-[#e2e8f0]">
          {candidate.prompt}
        </div>
        {isImageSessionGenerationTaskCancelable(candidate.task) ? (
          <button
            type="button"
            onClick={() => onCancel(candidate.task)}
            disabled={cancelling}
            className="mt-5 inline-flex items-center justify-center rounded-xl border border-red-200 bg-white px-4 py-2 text-sm font-semibold text-red-600 shadow-sm transition-colors hover:bg-red-50 disabled:opacity-60 dark:border-red-400/40 dark:bg-[#0b1220] dark:text-red-200 dark:hover:bg-red-500/12"
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
            className="mt-5 inline-flex items-center justify-center rounded-xl bg-red-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-red-500/20 transition-colors hover:bg-red-500 disabled:opacity-60"
          >
            {retrying ? <Loader2 size={15} className="mr-2 animate-spin" /> : <RotateCcw size={15} className="mr-2" />}
            {t("chat.retryGeneration")}
          </button>
        ) : failed ? (
          <div className="mt-5 max-w-sm rounded-xl border border-red-200 bg-white px-3 py-2 text-xs font-medium leading-5 text-red-500 dark:border-red-400/40 dark:bg-[#0b1220] dark:text-red-200">
            <div>{t("chat.notRetryable")}</div>
            {nonRetryableReason ? <div className="mt-1 text-red-500/80 dark:text-red-100/80">{nonRetryableReason}</div> : null}
          </div>
        ) : cancelled ? (
          <>
            <div className="mt-5 rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs font-medium text-slate-500 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300">
              {t("chat.taskCancelled")}
            </div>
            {regeneratable ? (
              <button
                type="button"
                onClick={() => onRegenerate(candidate.task)}
                disabled={regenerating}
                className="mt-3 inline-flex items-center justify-center rounded-xl bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-indigo-500/20 transition-colors hover:bg-indigo-700 disabled:opacity-60 dark:bg-violet-500 dark:hover:bg-violet-400"
              >
                {regenerating ? <Loader2 size={15} className="mr-2 animate-spin" /> : <RotateCcw size={15} className="mr-2" />}
                {t("chat.regenerateCancelled")}
              </button>
            ) : null}
          </>
        ) : unknown ? (
          <div className="mt-5 max-w-sm rounded-xl border border-amber-200 bg-white px-3 py-2 text-xs font-medium leading-5 text-amber-700 dark:border-amber-400/40 dark:bg-[#0b1220] dark:text-amber-100">
            <div>{t("chat.providerResultUnknown")}</div>
            {nonRetryableReason ? <div className="mt-1 text-amber-700/80 dark:text-amber-100/80">{nonRetryableReason}</div> : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
