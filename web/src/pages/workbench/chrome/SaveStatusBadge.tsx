import { CheckCircle2, Loader2, XCircle } from "lucide-react";

import type { TranslationKey } from "../../../lib/i18n";
import { useI18n } from "../../../lib/preferences";

export type SaveStatus = "idle" | "saving" | "saved" | "failed";

const SAVE_STATUS_LABEL_KEYS: Record<SaveStatus, TranslationKey> = {
  idle: "detail.inspector.saveIdle",
  saving: "detail.inspector.saving",
  saved: "detail.inspector.saved",
  failed: "detail.inspector.saveFailed",
};

const SAVE_STATUS_CLASS_NAMES: Record<SaveStatus, string> = {
  idle: "border-border-l1 bg-surface-subtle text-text-muted",
  saving: "border-accent/30 bg-accent-soft text-accent",
  saved: "border-state-success/30 bg-state-success/10 text-state-success",
  failed: "border-state-error/30 bg-state-error/10 text-state-error",
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
