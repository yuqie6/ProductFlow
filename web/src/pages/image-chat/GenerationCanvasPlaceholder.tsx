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
          className={`relative flex h-72 w-72 items-center justify-center overflow-hidden rounded-[40px] border shadow-sm transition-[border-color,box-shadow,transform] transition-spring ${
            failed
              ? "border-red-200 bg-red-50 text-red-600 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200"
              : active
                ? "border-transparent bg-indigo-950/15 text-indigo-700 animate-running-glow shadow-[0_0_35px_rgba(99,102,241,0.22)]"
                : "border-indigo-100 bg-indigo-50 text-indigo-700 dark:border-violet-400/35 dark:bg-violet-500/14 dark:text-violet-100"
          }`}
        >
          {active ? (
            <>
              <div className="absolute inset-0 overflow-hidden pointer-events-none rounded-[40px]">
                <div className="absolute inset-0 bg-gradient-to-b from-indigo-950/5 via-indigo-900/10 to-purple-950/15 dark:from-slate-950/10 dark:to-slate-900/20" />
                <div className="absolute left-[30%] bottom-0 w-5 h-5 rounded-full bg-indigo-400/60 blur-[3px] animate-large-p1" />
                <div className="absolute left-[52%] bottom-0 w-4 h-4 rounded-full bg-purple-400/50 blur-[2px] animate-large-p2" />
                <div className="absolute left-[40%] bottom-0 w-6 h-6 rounded-full bg-violet-400/40 blur-[4px] animate-large-p3" />
                <div className="absolute left-[62%] bottom-0 w-3 h-3 rounded-full bg-pink-400/60 blur-[1px] animate-large-p4" />
                <div className="absolute left-[35%] bottom-0 w-5.5 h-5.5 rounded-full bg-indigo-300/50 blur-[3px] animate-large-p5" />
                <div className="absolute left-[45%] bottom-0 w-4.5 h-4.5 rounded-full bg-purple-300/60 blur-[2px] animate-large-p6" />
              </div>
              <div className="absolute inset-6 rounded-[32px] bg-indigo-200/30 blur-2xl animate-pulse" />
              <Loader2 size={48} className="relative z-10 animate-spin text-indigo-600 dark:text-violet-300" />
            </>
          ) : (
            <Sparkles size={48} className="relative" />
          )}
        </div>
        <div className="mt-4 text-sm font-semibold text-slate-900">{placeholderStatusLabel(candidate, t)}</div>
        <div className="mt-1 text-xs text-slate-500">
          {t("chat.candidate", { index: candidate.candidate_index, count: candidate.candidate_count })} · {formatImageSizeValue(candidate.size)}
        </div>
        {queueText ? <div className="mt-3 max-w-sm text-xs leading-5 text-slate-500">{queueText}</div> : null}
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
        ) : null}
      </div>
    </div>
  );
}
