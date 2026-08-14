import { CheckCircle2, Loader2, XCircle } from "lucide-react";

import type { TranslationKey } from "../../lib/i18n";
import { useI18n } from "../../lib/preferences";
import type { SaveStatus } from "./types";

const SAVE_STATUS_LABEL_KEYS: Record<SaveStatus, TranslationKey> = {
  idle: "detail.inspector.saveIdle",
  saving: "detail.inspector.saving",
  saved: "detail.inspector.saved",
  failed: "detail.inspector.saveFailed",
};

const SAVE_STATUS_CLASS_NAMES: Record<SaveStatus, string> = {
  idle: "border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300",
  saving: "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-400/35 dark:bg-blue-500/12 dark:text-blue-200",
  saved: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-400/35 dark:bg-emerald-500/12 dark:text-emerald-200",
  failed: "border-red-200 bg-red-50 text-red-700 dark:border-red-400/35 dark:bg-red-500/12 dark:text-red-200",
};

export function SaveStatusBadge({ status }: { status: SaveStatus }) {
  const { t } = useI18n();
  return (
    <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium ${SAVE_STATUS_CLASS_NAMES[status]}`}>
      {status === "saving" ? (
        <Loader2 size={11} className="mr-1 animate-spin" />
      ) : status === "saved" ? (
        <CheckCircle2 size={11} className="mr-1" />
      ) : status === "failed" ? (
        <XCircle size={11} className="mr-1" />
      ) : null}
      {t(SAVE_STATUS_LABEL_KEYS[status])}
    </span>
  );
}
