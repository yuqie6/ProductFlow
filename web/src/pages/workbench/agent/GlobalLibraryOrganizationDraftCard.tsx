import {
  Archive,
  Check,
  CircleAlert,
  CircleCheck,
  FolderInput,
  Link2,
  Loader2,
  Pencil,
  RotateCcw,
  Tags,
} from "lucide-react";
import type { ReactNode } from "react";

import type { TranslationKey } from "../../../lib/i18n";
import { useI18n } from "../../../lib/preferences";
import type {
  LibraryOrganizationDraft,
  LibraryOrganizationOperation,
} from "../../../lib/types";

const OPERATION_LABEL_KEYS: Record<LibraryOrganizationOperation["operation"], TranslationKey> = {
  rename: "globalAgent.draft.operation.rename",
  move: "globalAgent.draft.operation.move",
  set_tags: "globalAgent.draft.operation.setTags",
  archive: "globalAgent.draft.operation.archive",
  restore: "globalAgent.draft.operation.restore",
  link_workflow: "globalAgent.draft.operation.linkWorkflow",
};

const OPERATION_ICONS: Record<LibraryOrganizationOperation["operation"], ReactNode> = {
  rename: <Pencil size={13} aria-hidden="true" />,
  move: <FolderInput size={13} aria-hidden="true" />,
  set_tags: <Tags size={13} aria-hidden="true" />,
  archive: <Archive size={13} aria-hidden="true" />,
  restore: <RotateCcw size={13} aria-hidden="true" />,
  link_workflow: <Link2 size={13} aria-hidden="true" />,
};

interface GlobalLibraryOrganizationDraftCardProps {
  draft: LibraryOrganizationDraft | null;
  loading: boolean;
  error: string | null;
  busy: boolean;
  onConfirm: () => void;
}

export function GlobalLibraryOrganizationDraftCard({
  draft,
  loading,
  error,
  busy,
  onConfirm,
}: GlobalLibraryOrganizationDraftCardProps) {
  const { t } = useI18n();
  if (loading && !draft) {
    return (
      <div className="mt-3 border-l-2 border-accent bg-surface-subtle px-3 py-3 text-xs text-text-secondary">
        <div className="flex items-center gap-2">
          <Loader2 size={14} className="animate-spin" aria-hidden="true" />
          <span>{t("globalAgent.draft.loading")}</span>
        </div>
      </div>
    );
  }
  if (!draft?.current_revision) {
    return error ? <DraftError message={error} /> : null;
  }

  const revision = draft.current_revision;
  const operations = revision.payload.operations;
  const awaitingConfirmation = draft.status === "awaiting_confirmation";
  const confirmed = draft.status === "confirmed";

  return (
    <section
      data-global-library-organization-draft
      aria-label={t("globalAgent.draft.title")}
      className={`mt-3 border-l-2 px-3 py-3 ${confirmed ? "border-state-success bg-state-success/5" : "border-accent bg-surface-subtle"}`}
    >
      <div className="flex items-start gap-2">
        <span className={`mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-md ${confirmed ? "bg-state-success/15 text-state-success" : "bg-accent-soft text-accent-strong"}`}>
          {confirmed ? <CircleCheck size={14} aria-hidden="true" /> : <Check size={14} aria-hidden="true" />}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <h4 className="min-w-0 flex-1 truncate text-xs font-semibold text-text-primary">
              {confirmed ? t("globalAgent.draft.confirmed") : t("globalAgent.draft.title")}
            </h4>
            <span className="shrink-0 text-[10px] font-medium text-text-muted">
              {t("globalAgent.draft.version", { version: revision.version })}
            </span>
          </div>
          <p className="mt-1 text-xs leading-5 text-text-secondary">
            {revision.payload.confirmation_summary}
          </p>
        </div>
      </div>

      <div className="mt-3 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-text-muted">
        <span>{t("globalAgent.draft.assetsCount", { count: operations.length })}</span>
        <span>{t("globalAgent.draft.operationsCount", { count: operations.length })}</span>
      </div>

      <div className="mt-2 space-y-1">
        {operations.slice(0, 4).map((operation) => (
          <OperationRow key={operation.asset_id} operation={operation} />
        ))}
        {operations.length > 4 ? (
          <p className="pt-1 text-[11px] text-text-muted">
            {t("globalAgent.draft.moreOperations", { count: operations.length - 4 })}
          </p>
        ) : null}
      </div>

      {error ? <DraftError message={error} /> : null}
      {awaitingConfirmation ? (
        <button
          type="button"
          onClick={onConfirm}
          disabled={busy}
          className="mt-3 inline-flex h-9 w-full items-center justify-center gap-2 rounded-md bg-accent px-3 text-xs font-semibold text-accent-fg transition-colors hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/60 disabled:cursor-wait disabled:opacity-60"
        >
          {busy ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Check size={14} aria-hidden="true" />}
          {busy ? t("globalAgent.draft.confirming") : t("globalAgent.draft.confirm")}
        </button>
      ) : null}
    </section>
  );
}

function OperationRow({ operation }: { operation: LibraryOrganizationOperation }) {
  const { t } = useI18n();
  const workflowTitle = "workflow_title" in operation.target ? operation.target.workflow_title : null;
  return (
    <div className="flex min-w-0 items-center gap-2 text-xs">
      <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-surface-raised text-text-secondary">
        {OPERATION_ICONS[operation.operation]}
      </span>
      <span className="min-w-0 flex-1 truncate text-text-secondary" title={operation.before.display_name}>
        {operation.before.display_name}
      </span>
      {workflowTitle ? (
        <span className="min-w-0 max-w-[42%] truncate text-[11px] text-text-muted" title={workflowTitle}>
          {workflowTitle}
        </span>
      ) : null}
      <span className="shrink-0 text-[11px] font-medium text-text-muted">
        {t(OPERATION_LABEL_KEYS[operation.operation])}
      </span>
    </div>
  );
}

function DraftError({ message }: { message: string }) {
  return (
    <p role="alert" className="mt-2 flex items-start gap-1.5 text-[11px] leading-5 text-state-error">
      <CircleAlert size={13} className="mt-0.5 shrink-0" aria-hidden="true" />
      <span className="min-w-0 break-words">{message}</span>
    </p>
  );
}
