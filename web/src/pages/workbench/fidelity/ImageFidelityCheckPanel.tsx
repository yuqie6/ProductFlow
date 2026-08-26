import {
  AlertTriangle,
  Check,
  Clock3,
  History,
  Loader2,
  RefreshCw,
  Save,
  UserRound,
} from "lucide-react";
import type { ChangeEvent, FormEvent } from "react";

import { formatDateTime } from "../../../lib/format";
import type { Locale } from "../../../lib/i18n";

export const FIDELITY_CHECK_MAX_NOTES = 4000;

export const FIDELITY_FIELDS = [
  "shape_fidelity",
  "color_material_fidelity",
  "logo_text_legibility",
  "text_policy_compliance",
] as const;

export type FidelityField = (typeof FIDELITY_FIELDS)[number];
export type FidelityOutcome = "pass" | "fail" | "not_applicable";

export interface ImageFidelityCheckDraft {
  shape_fidelity: FidelityOutcome | null;
  color_material_fidelity: FidelityOutcome | null;
  logo_text_legibility: FidelityOutcome | null;
  text_policy_compliance: FidelityOutcome | null;
  notes: string;
}

export interface ImageFidelityCheckRecord {
  version: number;
  shape_fidelity: FidelityOutcome;
  color_material_fidelity: FidelityOutcome;
  logo_text_legibility: FidelityOutcome;
  text_policy_compliance: FidelityOutcome;
  notes: string | null;
  checked_by: string;
  created_at: string;
}

export interface ImageFidelityCheckSubmitPayload {
  expectedLatestVersion: number;
  shape_fidelity: FidelityOutcome;
  color_material_fidelity: FidelityOutcome;
  logo_text_legibility: FidelityOutcome;
  text_policy_compliance: FidelityOutcome;
  notes: string | null;
}

export interface ImageFidelityCheckLabels {
  title: string;
  latestVersion: string;
  latestCheck: string;
  emptyHistory: string;
  checkedBy: string;
  checkedAt: string;
  noNotes: string;
  loading: string;
  saving: string;
  reload: string;
  reloading: string;
  save: string;
  notes: string;
  notesCount: string;
  required: string;
  notesTooLong: string;
  errorTitle: string;
  conflictTitle: string;
  outcomeLabels: Record<FidelityOutcome, string>;
  fieldLabels: Record<FidelityField, string>;
}

export interface ImageFidelityCheckPanelProps {
  value: ImageFidelityCheckDraft;
  onChange: (value: ImageFidelityCheckDraft) => void;
  latestVersion: number;
  latestVersionKnown?: boolean;
  latestCheck?: ImageFidelityCheckRecord | null;
  locale?: Locale;
  loading?: boolean;
  saving?: boolean;
  reloading?: boolean;
  error?: string | null;
  conflict?: string | null;
  labels?: Partial<ImageFidelityCheckLabels> & {
    outcomeLabels?: Partial<Record<FidelityOutcome, string>>;
    fieldLabels?: Partial<Record<FidelityField, string>>;
  };
  onSubmit: (payload: ImageFidelityCheckSubmitPayload) => void | Promise<void>;
  onReload?: () => void | Promise<void>;
}

const DEFAULT_LABELS: ImageFidelityCheckLabels = {
  title: "人工保真检查",
  latestVersion: "最新版本",
  latestCheck: "最新检查摘要",
  emptyHistory: "尚无人工检查记录",
  checkedBy: "检查人",
  checkedAt: "检查时间",
  noNotes: "无备注",
  loading: "正在加载检查记录",
  saving: "正在保存检查",
  reload: "重新加载",
  reloading: "正在重新加载",
  save: "保存检查",
  notes: "备注",
  notesCount: "字",
  required: "请为四项分别选择结论后再保存。",
  notesTooLong: "备注不能超过 4000 个字符。",
  errorTitle: "检查记录加载或保存失败",
  conflictTitle: "版本冲突",
  outcomeLabels: {
    pass: "通过",
    fail: "不通过",
    not_applicable: "不适用",
  },
  fieldLabels: {
    shape_fidelity: "形体保真",
    color_material_fidelity: "颜色与材质保真",
    logo_text_legibility: "Logo / 文字可读性",
    text_policy_compliance: "文字政策合规",
  },
};

function mergeLabels(
  labels: ImageFidelityCheckPanelProps["labels"],
): ImageFidelityCheckLabels {
  return {
    ...DEFAULT_LABELS,
    ...labels,
    outcomeLabels: { ...DEFAULT_LABELS.outcomeLabels, ...labels?.outcomeLabels },
    fieldLabels: { ...DEFAULT_LABELS.fieldLabels, ...labels?.fieldLabels },
  };
}

export function isFidelityOutcome(value: FidelityOutcome | null | undefined): value is FidelityOutcome {
  return value === "pass" || value === "fail" || value === "not_applicable";
}

type CompleteImageFidelityCheckDraft = Omit<ImageFidelityCheckDraft, FidelityField> & {
  [Field in FidelityField]: FidelityOutcome;
};

function isCompleteImageFidelityCheckDraft(
  value: ImageFidelityCheckDraft,
): value is CompleteImageFidelityCheckDraft {
  return FIDELITY_FIELDS.every((field) => isFidelityOutcome(value[field]));
}

export function buildImageFidelityCheckSubmitPayload(
  value: ImageFidelityCheckDraft,
  expectedLatestVersion: number,
): ImageFidelityCheckSubmitPayload | null {
  if (
    !Number.isInteger(expectedLatestVersion)
    || expectedLatestVersion < 0
    || value.notes.length > FIDELITY_CHECK_MAX_NOTES
    || !isCompleteImageFidelityCheckDraft(value)
  ) {
    return null;
  }

  return {
    expectedLatestVersion,
    shape_fidelity: value.shape_fidelity,
    color_material_fidelity: value.color_material_fidelity,
    logo_text_legibility: value.logo_text_legibility,
    text_policy_compliance: value.text_policy_compliance,
    notes: value.notes.trim() || null,
  };
}

function formatCheckedAt(value: string, locale?: Locale): string {
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : formatDateTime(value, locale);
}

function invokeAction(action: () => void | Promise<void>): void {
  try {
    void Promise.resolve(action()).catch(() => undefined);
  } catch {
    // 错误由父组件受控；被拒绝的 UI 命令不要打到控制台
  }
}

export function ImageFidelityCheckPanel({
  value,
  onChange,
  latestVersion,
  latestVersionKnown = true,
  latestCheck = null,
  locale,
  loading = false,
  saving = false,
  reloading = false,
  error = null,
  conflict = null,
  labels: labelOverrides,
  onSubmit,
  onReload,
}: ImageFidelityCheckPanelProps) {
  const labels = mergeLabels(labelOverrides);
  const editingDisabled = loading || saving || reloading;
  const submitPayload = buildImageFidelityCheckSubmitPayload(value, latestVersion);
  const notesTooLong = value.notes.length > FIDELITY_CHECK_MAX_NOTES;
  const canSubmit = (
    latestVersionKnown
    && !editingDisabled
    && !conflict
    && !notesTooLong
    && submitPayload !== null
  );

  const updateOutcome = (field: FidelityField, outcome: FidelityOutcome) => {
    const nextValue: ImageFidelityCheckDraft = { ...value };
    nextValue[field] = outcome;
    onChange(nextValue);
  };

  const handleNotesChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    onChange({
      ...value,
      notes: event.target.value.slice(0, FIDELITY_CHECK_MAX_NOTES),
    });
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit || !submitPayload) return;
    invokeAction(() => onSubmit(submitPayload));
  };

  return (
    <section
      data-image-fidelity-panel
      className="flex min-w-0 flex-col gap-3 border border-border-l1 bg-surface-raised p-3 text-text-primary sm:p-4"
    >
      <header className="flex min-w-0 items-start justify-between gap-3 border-b border-border-l1 pb-3">
        <div className="min-w-0">
          <h2 className="truncate text-sm font-semibold">{labels.title}</h2>
          <p data-fidelity-latest-version className="mt-1 text-xs text-text-muted">
            {labels.latestVersion} v{latestVersion}
          </p>
        </div>
        {onReload ? (
          <button
            type="button"
            data-fidelity-reload
            disabled={editingDisabled}
            onClick={() => invokeAction(onReload)}
            className="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-md border border-border-l1 px-2.5 text-xs font-semibold text-text-secondary hover:bg-surface-subtle hover:text-text-primary disabled:cursor-not-allowed disabled:opacity-45"
          >
            <RefreshCw size={14} className={reloading ? "animate-spin" : undefined} aria-hidden="true" />
            <span>{reloading ? labels.reloading : labels.reload}</span>
          </button>
        ) : null}
      </header>

      {loading ? (
        <div role="status" data-fidelity-loading className="flex min-h-9 items-center gap-2 text-xs text-text-muted">
          <Loader2 size={15} className="animate-spin" aria-hidden="true" />
          <span>{labels.loading}</span>
        </div>
      ) : null}
      {saving ? (
        <div role="status" data-fidelity-saving className="flex min-h-9 items-center gap-2 text-xs text-text-muted">
          <Loader2 size={15} className="animate-spin" aria-hidden="true" />
          <span>{labels.saving}</span>
        </div>
      ) : null}
      {reloading ? (
        <div role="status" data-fidelity-reloading className="flex min-h-9 items-center gap-2 text-xs text-text-muted">
          <Loader2 size={15} className="animate-spin" aria-hidden="true" />
          <span>{labels.reloading}</span>
        </div>
      ) : null}
      {error ? (
        <div role="alert" data-fidelity-error className="flex min-h-9 min-w-0 items-start gap-2 border border-state-error/30 bg-state-error/10 px-2.5 py-2 text-xs leading-5 text-state-error">
          <AlertTriangle size={15} className="mt-0.5 shrink-0" aria-hidden="true" />
          <span>{labels.errorTitle}: {error}</span>
        </div>
      ) : null}
      {conflict ? (
        <div role="alert" data-fidelity-conflict className="flex min-h-9 min-w-0 items-start gap-2 border border-state-warning/35 bg-state-warning/10 px-2.5 py-2 text-xs leading-5 text-state-warning">
          <AlertTriangle size={15} className="mt-0.5 shrink-0" aria-hidden="true" />
          <span>{labels.conflictTitle}: {conflict}</span>
        </div>
      ) : null}

      <section data-fidelity-latest className="min-w-0 border-b border-border-l1 pb-3">
        <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-text-secondary">
          <History size={15} aria-hidden="true" />
          <span>{labels.latestCheck}</span>
        </div>
        {latestCheck ? (
          <div className="min-w-0 space-y-2">
            <div className="flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-[11px] text-text-muted">
              <span className="inline-flex min-w-0 items-center gap-1">
                <Check size={13} aria-hidden="true" />
                v{latestCheck.version}
              </span>
              <span className="inline-flex min-w-0 items-center gap-1">
                <UserRound size={13} aria-hidden="true" />
                {labels.checkedBy}: {latestCheck.checked_by}
              </span>
              <span className="inline-flex min-w-0 items-center gap-1">
                <Clock3 size={13} aria-hidden="true" />
                {labels.checkedAt}: {formatCheckedAt(latestCheck.created_at, locale)}
              </span>
            </div>
            <div className="grid min-w-0 gap-1.5 sm:grid-cols-2" data-fidelity-latest-outcomes>
              {FIDELITY_FIELDS.map((field) => (
                <div key={field} className="flex min-w-0 items-center justify-between gap-2 text-xs">
                  <span className="min-w-0 truncate text-text-muted">{labels.fieldLabels[field]}</span>
                  <span className="shrink-0 font-semibold text-text-secondary">
                    {labels.outcomeLabels[latestCheck[field]]}
                  </span>
                </div>
              ))}
            </div>
            <p data-fidelity-latest-notes className="truncate text-[11px] text-text-muted">
              {latestCheck.notes?.trim() || labels.noNotes}
            </p>
          </div>
        ) : (
          <p data-fidelity-empty className="text-xs leading-5 text-text-muted">{labels.emptyHistory}</p>
        )}
      </section>

      <form data-fidelity-form onSubmit={handleSubmit} className="min-w-0 space-y-3">
        <fieldset disabled={editingDisabled} className="min-w-0 space-y-3 border-0 p-0">
          <legend className="sr-only">{labels.title}</legend>
          <div className="divide-y divide-border-l1 border-y border-border-l1" data-fidelity-fields>
            {FIDELITY_FIELDS.map((field) => (
              <div key={field} className="flex min-w-0 flex-col gap-2 py-2.5 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
                <span className="min-w-0 text-xs font-medium text-text-secondary">{labels.fieldLabels[field]}</span>
                <div
                  role="group"
                  aria-label={labels.fieldLabels[field]}
                  data-fidelity-field={field}
                  className="flex min-w-0 flex-wrap rounded-md border border-border-l1 bg-surface-subtle p-0.5"
                >
                  {(["pass", "fail", "not_applicable"] as const).map((outcome) => (
                    <button
                      key={outcome}
                      type="button"
                      aria-pressed={value[field] === outcome}
                      data-fidelity-outcome={outcome}
                      onClick={() => updateOutcome(field, outcome)}
                      className="min-h-8 min-w-16 flex-1 rounded px-2 text-[11px] font-semibold text-text-secondary aria-pressed:bg-surface-raised aria-pressed:text-text-primary disabled:cursor-not-allowed disabled:opacity-45"
                    >
                      {labels.outcomeLabels[outcome]}
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>

          <label className="block min-w-0 space-y-1.5 text-xs font-medium text-text-secondary">
            <span className="flex items-center justify-between gap-2">
              <span>{labels.notes}</span>
              <span data-fidelity-notes-count className="shrink-0 text-[11px] font-normal text-text-muted">
                {value.notes.length}/{FIDELITY_CHECK_MAX_NOTES} {labels.notesCount}
              </span>
            </span>
            <textarea
              value={value.notes}
              maxLength={FIDELITY_CHECK_MAX_NOTES}
              aria-invalid={notesTooLong}
              data-fidelity-notes
              onChange={handleNotesChange}
              rows={3}
              className="min-h-20 w-full min-w-0 resize-y rounded-md border border-border-l1 bg-surface-base px-2.5 py-2 text-sm font-normal leading-5 outline-none focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
          </label>
        </fieldset>

        {notesTooLong ? (
          <p role="status" data-fidelity-notes-error className="text-xs leading-5 text-state-error">{labels.notesTooLong}</p>
        ) : null}
        {!notesTooLong && !submitPayload && !loading ? (
          <p role="status" data-fidelity-required className="text-xs leading-5 text-text-muted">{labels.required}</p>
        ) : null}

        <div className="flex min-w-0 items-center justify-end gap-2 border-t border-border-l1 pt-3">
          <button
            type="submit"
            data-fidelity-submit
            disabled={!canSubmit}
            className="inline-flex h-9 min-w-24 items-center justify-center gap-1.5 rounded-md bg-accent px-3 text-xs font-semibold text-accent-fg hover:bg-accent-strong disabled:cursor-not-allowed disabled:opacity-45"
          >
            {saving ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Save size={14} aria-hidden="true" />}
            <span>{saving ? labels.saving : labels.save}</span>
          </button>
        </div>
      </form>
    </section>
  );
}
