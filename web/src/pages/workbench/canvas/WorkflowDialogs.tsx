import { Loader2, X } from "lucide-react";
import { useEffect, useId, useState } from "react";

import { useI18n } from "../../../lib/preferences";

interface WorkflowTextDialogProps {
  open: boolean;
  title: string;
  label: string;
  initialValue?: string;
  maxLength?: number;
  busy?: boolean;
  error?: string | null;
  onClose: () => void;
  onSubmit: (value: string) => void;
}

export function WorkflowTextDialog({
  open,
  title,
  label,
  initialValue = "",
  maxLength = 120,
  busy = false,
  error,
  onClose,
  onSubmit,
}: WorkflowTextDialogProps) {
  const { t } = useI18n();
  const titleId = useId();
  const inputId = useId();
  const [value, setValue] = useState(initialValue);

  useEffect(() => {
    if (open) {
      setValue(initialValue);
    }
  }, [initialValue, open]);

  useEffect(() => {
    if (!open) {
      return;
    }
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) {
        onClose();
      }
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [busy, onClose, open]);

  if (!open) {
    return null;
  }

  const normalized = value.trim();
  return (
    <div className="fixed inset-0 z-[90] flex items-center justify-center bg-slate-950/55 p-4 backdrop-blur-sm" onMouseDown={(event) => {
      if (event.target === event.currentTarget && !busy) onClose();
    }}>
      <form
        className="w-full max-w-md overflow-hidden rounded-lg border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:!bg-[#10151d]"
        onSubmit={(event) => {
          event.preventDefault();
          if (normalized) onSubmit(normalized);
        }}
        aria-labelledby={titleId}
      >
        <div className="flex items-center justify-between border-b border-slate-100 px-5 py-4 dark:border-slate-800">
          <h2 id={titleId} className="text-base font-semibold text-slate-950 dark:text-slate-100">{title}</h2>
          <button type="button" onClick={onClose} disabled={busy} className="inline-flex h-8 w-8 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800" aria-label={t("workbench.dialog.close")} title={t("workbench.dialog.close")}>
            <X size={16} />
          </button>
        </div>
        <div className="px-5 py-5">
          <label htmlFor={inputId} className="text-xs font-semibold text-slate-600 dark:text-slate-300">{label}</label>
          <input
            id={inputId}
            value={value}
            onChange={(event) => setValue(event.target.value)}
            maxLength={maxLength}
            autoFocus
            className="mt-2 h-10 w-full rounded-md border border-slate-200 bg-white px-3 text-sm outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/15 dark:border-slate-700 dark:!bg-slate-950"
          />
          {error ? <p role="alert" className="mt-2 text-xs text-red-600 dark:text-red-300">{error}</p> : null}
        </div>
        <div className="flex justify-end gap-2 border-t border-slate-100 bg-slate-50 px-5 py-3 dark:border-slate-800 dark:!bg-slate-950/50">
          <button type="button" onClick={onClose} disabled={busy} className="h-9 rounded-md border border-slate-200 bg-white px-3 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-700 dark:!bg-slate-900 dark:text-slate-200">{t("common.cancel")}</button>
          <button type="submit" disabled={!normalized || busy} className="inline-flex h-9 min-w-20 items-center justify-center rounded-md bg-slate-950 px-3 text-sm font-semibold text-white hover:bg-indigo-700 disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400">
            {busy ? <Loader2 size={14} className="mr-2 animate-spin" /> : null}
            {t("workbench.dialog.confirm")}
          </button>
        </div>
      </form>
    </div>
  );
}

interface WorkflowReferenceNodeDialogProps {
  open: boolean;
  busy?: boolean;
  error?: string | null;
  onClose: () => void;
  onSubmit: (input: { title: string; role: string; label: string }) => void;
}

export function WorkflowReferenceNodeDialog({
  open,
  busy = false,
  error,
  onClose,
  onSubmit,
}: WorkflowReferenceNodeDialogProps) {
  const { t } = useI18n();
  const headingId = useId();
  const titleId = useId();
  const roleId = useId();
  const labelId = useId();
  const [title, setTitle] = useState("");
  const [role, setRole] = useState("");
  const [label, setLabel] = useState("");

  useEffect(() => {
    if (open) {
      setTitle(t("workbench.reference.defaultTitle"));
      setRole(t("workbench.reference.defaultRole"));
      setLabel(t("workbench.reference.defaultLabel"));
    }
  }, [open, t]);

  useEffect(() => {
    if (!open) return;
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) onClose();
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [busy, onClose, open]);

  if (!open) return null;
  const normalized = { title: title.trim(), role: role.trim(), label: label.trim() };
  const valid = Boolean(normalized.title && normalized.role && normalized.label);

  return (
    <div className="fixed inset-0 z-[90] flex items-center justify-center bg-slate-950/55 p-4 backdrop-blur-sm" onMouseDown={(event) => {
      if (event.target === event.currentTarget && !busy) onClose();
    }}>
      <form
        role="dialog"
        aria-modal="true"
        aria-labelledby={headingId}
        className="w-full max-w-md overflow-hidden rounded-lg border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:!bg-[#10151d]"
        onSubmit={(event) => {
          event.preventDefault();
          if (valid) onSubmit(normalized);
        }}
      >
        <div className="flex items-center justify-between border-b border-slate-100 px-5 py-4 dark:border-slate-800">
          <h2 id={headingId} className="text-base font-semibold text-slate-950 dark:text-slate-100">{t("workbench.reference.create")}</h2>
          <button type="button" onClick={onClose} disabled={busy} className="inline-flex h-8 w-8 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800" aria-label={t("workbench.dialog.close")} title={t("workbench.dialog.close")}>
            <X size={16} />
          </button>
        </div>
        <div className="space-y-4 px-5 py-5">
          <ReferenceField id={titleId} label={t("workbench.reference.titleField")} value={title} maxLength={255} autoFocus onChange={setTitle} />
          <ReferenceField id={roleId} label={t("workbench.reference.roleField")} value={role} maxLength={120} onChange={setRole} />
          <ReferenceField id={labelId} label={t("workbench.reference.labelField")} value={label} maxLength={255} onChange={setLabel} />
          {error ? <p role="alert" className="text-xs text-red-600 dark:text-red-300">{error}</p> : null}
        </div>
        <div className="flex justify-end gap-2 border-t border-slate-100 bg-slate-50 px-5 py-3 dark:border-slate-800 dark:!bg-slate-950/50">
          <button type="button" onClick={onClose} disabled={busy} className="h-9 rounded-md border border-slate-200 bg-white px-3 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-700 dark:!bg-slate-900 dark:text-slate-200">{t("common.cancel")}</button>
          <button type="submit" disabled={!valid || busy} className="inline-flex h-9 min-w-20 items-center justify-center rounded-md bg-slate-950 px-3 text-sm font-semibold text-white hover:bg-indigo-700 disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400">
            {busy ? <Loader2 size={14} className="mr-2 animate-spin" /> : null}
            {t("workbench.dialog.confirm")}
          </button>
        </div>
      </form>
    </div>
  );
}

function ReferenceField({
  id,
  label,
  value,
  maxLength,
  autoFocus = false,
  onChange,
}: {
  id: string;
  label: string;
  value: string;
  maxLength: number;
  autoFocus?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <div>
      <label htmlFor={id} className="text-xs font-semibold text-slate-600 dark:text-slate-300">{label}</label>
      <input
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        maxLength={maxLength}
        autoFocus={autoFocus}
        className="mt-2 h-10 w-full rounded-md border border-slate-200 bg-white px-3 text-sm outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/15 dark:border-slate-700 dark:!bg-slate-950"
      />
    </div>
  );
}

interface WorkflowRecipeDialogProps {
  open: boolean;
  heading: string;
  sourceLabel: string;
  initialTitle?: string;
  initialDescription?: string | null;
  busy?: boolean;
  error?: string | null;
  onClose: () => void;
  onSubmit: (title: string, description: string | null) => void;
}

export function WorkflowRecipeDialog({
  open,
  heading,
  sourceLabel,
  initialTitle = "",
  initialDescription = null,
  busy = false,
  error,
  onClose,
  onSubmit,
}: WorkflowRecipeDialogProps) {
  const { t } = useI18n();
  const headingId = useId();
  const titleId = useId();
  const descriptionId = useId();
  const [title, setTitle] = useState(initialTitle);
  const [description, setDescription] = useState(initialDescription ?? "");

  useEffect(() => {
    if (open) {
      setTitle(initialTitle);
      setDescription(initialDescription ?? "");
    }
  }, [initialDescription, initialTitle, open]);

  useEffect(() => {
    if (!open) return;
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) onClose();
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [busy, onClose, open]);

  if (!open) return null;

  const normalizedTitle = title.trim();
  return (
    <div className="fixed inset-0 z-[90] flex items-center justify-center bg-slate-950/55 p-4 backdrop-blur-sm" onMouseDown={(event) => {
      if (event.target === event.currentTarget && !busy) onClose();
    }}>
      <form
        role="dialog"
        aria-modal="true"
        aria-labelledby={headingId}
        className="w-full max-w-lg overflow-hidden rounded-lg border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:!bg-[#10151d]"
        onSubmit={(event) => {
          event.preventDefault();
          if (normalizedTitle) onSubmit(normalizedTitle, description.trim() || null);
        }}
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-5 py-4 dark:border-slate-800">
          <div className="min-w-0">
            <h2 id={headingId} className="text-base font-semibold text-slate-950 dark:text-slate-100">{heading}</h2>
            <p className="mt-1 truncate text-xs text-slate-500 dark:text-slate-400">{sourceLabel}</p>
          </div>
          <button type="button" onClick={onClose} disabled={busy} className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800" aria-label={t("workbench.dialog.close")} title={t("workbench.dialog.close")}>
            <X size={16} />
          </button>
        </div>
        <div className="space-y-4 px-5 py-5">
          <div>
            <label htmlFor={titleId} className="text-xs font-semibold text-slate-600 dark:text-slate-300">{t("workbench.recipe.titleField")}</label>
            <input id={titleId} value={title} onChange={(event) => setTitle(event.target.value)} maxLength={255} autoFocus className="mt-2 h-10 w-full rounded-md border border-slate-200 bg-white px-3 text-sm outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/15 dark:border-slate-700 dark:!bg-slate-950" />
          </div>
          <div>
            <label htmlFor={descriptionId} className="text-xs font-semibold text-slate-600 dark:text-slate-300">{t("workbench.recipe.descriptionField")}</label>
            <textarea id={descriptionId} value={description} onChange={(event) => setDescription(event.target.value)} maxLength={4000} rows={4} className="mt-2 w-full resize-none rounded-md border border-slate-200 bg-white px-3 py-2 text-sm leading-5 outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/15 dark:border-slate-700 dark:!bg-slate-950" />
          </div>
          {error ? <p role="alert" className="text-xs text-red-600 dark:text-red-300">{error}</p> : null}
        </div>
        <div className="flex justify-end gap-2 border-t border-slate-100 bg-slate-50 px-5 py-3 dark:border-slate-800 dark:!bg-slate-950/50">
          <button type="button" onClick={onClose} disabled={busy} className="h-9 rounded-md border border-slate-200 bg-white px-3 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-700 dark:!bg-slate-900 dark:text-slate-200">{t("common.cancel")}</button>
          <button type="submit" disabled={!normalizedTitle || busy} className="inline-flex h-9 min-w-24 items-center justify-center rounded-md bg-slate-950 px-3 text-sm font-semibold text-white hover:bg-indigo-700 disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400">
            {busy ? <Loader2 size={14} className="mr-2 animate-spin" /> : null}
            {t("workbench.recipe.save")}
          </button>
        </div>
      </form>
    </div>
  );
}
