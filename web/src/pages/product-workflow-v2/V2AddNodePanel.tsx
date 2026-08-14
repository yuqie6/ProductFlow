import { ImagePlus } from "lucide-react";

import { useI18n } from "../../lib/preferences";

export function V2AddNodePanel({
  busy,
  onCreateReference,
}: {
  busy: boolean;
  onCreateReference: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="p-3">
      <button
        type="button"
        onClick={onCreateReference}
        disabled={busy}
        className="flex min-h-12 w-full items-center gap-3 rounded-md border border-slate-200 bg-white px-3 text-left text-sm font-semibold text-slate-800 transition-colors hover:border-indigo-300 hover:bg-indigo-50 hover:text-indigo-700 disabled:cursor-not-allowed disabled:opacity-45 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100 dark:hover:border-violet-400 dark:hover:bg-violet-500/10 dark:hover:text-violet-100"
      >
        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-sky-50 text-sky-700 dark:bg-sky-500/15 dark:text-sky-200">
          <ImagePlus size={17} aria-hidden="true" />
        </span>
        <span className="min-w-0 truncate">{t("workflowV2.reference.create")}</span>
      </button>
    </div>
  );
}
