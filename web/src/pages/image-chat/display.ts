import { formatDateTime } from "../../lib/format";
import { formatImageSizeValue } from "../../lib/imageSizes";
import type { ImageSessionGenerationTask, ImageSessionRound } from "../../lib/types";
import type { useI18n } from "../../lib/preferences";
import type { ImageHistoryPlaceholderCandidate } from "./branching";
import {
  imageGenerationRetryMetadata,
  isImageSessionGenerationTaskAutoRetrying,
} from "./branching";

export type ImageChatTranslate = ReturnType<typeof useI18n>["t"];

export function generationTaskQueueText(task: ImageSessionGenerationTask, t: ImageChatTranslate) {
  const retryMetadata = imageGenerationRetryMetadata(task);
  if (isImageSessionGenerationTaskAutoRetrying(task) && retryMetadata?.auto_retry_attempt && retryMetadata.max_attempts) {
    return t("chat.autoRetryText", {
      attempt: Math.min(retryMetadata.auto_retry_attempt + 1, retryMetadata.max_attempts),
      max: retryMetadata.max_attempts,
      reason: retryMetadata.last_failure_reason ?? t("chat.autoRetryGenericReason"),
    });
  }
  if (task.status === "queued") {
    const ahead = task.queued_ahead_count ?? 0;
    const position = task.queue_position
      ? t("chat.queuePosition", { position: task.queue_position })
      : t("chat.queueWaiting");
    return t("chat.queueText", {
      ahead,
      position,
      active: task.queue_active_count,
      max: task.queue_max_concurrent_tasks,
    });
  }
  if (task.status === "running") {
    const providerStatus = task.provider_response_status
      ? t("chat.providerStatus", { status: task.provider_response_status })
      : "";
    return t("chat.runningText", {
      providerStatus,
      progress: task.progress_updated_at ? formatDateTime(task.progress_updated_at) : t("chat.progressJustStarted"),
      running: task.queue_running_count,
      queued: task.queue_queued_count,
    });
  }
  return "";
}

export function imageRoundSizeLabel(round: ImageSessionRound, t: ImageChatTranslate) {
  if (round.actual_size && round.actual_size !== round.size) {
    return t("gallery.sizeActualRequested", { actual: round.actual_size, requested: round.size });
  }
  return round.actual_size ?? round.size;
}

export function placeholderStatusLabel(candidate: ImageHistoryPlaceholderCandidate, t: ImageChatTranslate) {
  const retryMetadata = imageGenerationRetryMetadata(candidate.task);
  if (
    isImageSessionGenerationTaskAutoRetrying(candidate.task) &&
    retryMetadata?.auto_retry_attempt &&
    retryMetadata.max_attempts
  ) {
    return t("chat.statusAutoRetry", {
      attempt: Math.min(retryMetadata.auto_retry_attempt + 1, retryMetadata.max_attempts),
      max: retryMetadata.max_attempts,
    });
  }
  if (candidate.status === "queued") {
    return candidate.task.queue_position
      ? t("chat.statusQueuedPosition", { position: candidate.task.queue_position })
      : t("chat.statusQueued");
  }
  if (candidate.status === "running") {
    return t("chat.statusRunning", { index: candidate.candidate_index, count: candidate.candidate_count });
  }
  if (candidate.status === "completed") {
    return t("chat.statusCompletedRefreshing");
  }
  if (candidate.status === "failed") {
    return t("chat.statusFailed");
  }
  if (candidate.status === "cancelled") {
    return t("chat.statusCancelled");
  }
  return t("chat.statusCompleted");
}

export function placeholderStatusClass(candidate: ImageHistoryPlaceholderCandidate) {
  if (candidate.status === "failed") {
    return "border-red-200 bg-red-50 text-red-700 dark:border-red-400/40 dark:bg-red-500/15 dark:text-red-100";
  }
  if (candidate.status === "queued") {
    return "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-300/40 dark:bg-amber-500/15 dark:text-amber-100";
  }
  if (candidate.status === "completed") {
    return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-300/40 dark:bg-emerald-500/15 dark:text-emerald-100";
  }
  if (candidate.status === "cancelled") {
    return "border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-600 dark:bg-slate-800/80 dark:text-slate-200";
  }
  return "border-indigo-200 bg-indigo-50 text-indigo-700 dark:border-violet-400/50 dark:bg-violet-500/15 dark:text-violet-100";
}

export function placeholderSizeLabel(candidate: ImageHistoryPlaceholderCandidate) {
  return formatImageSizeValue(candidate.size);
}
