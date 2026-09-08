import type { KeyboardEvent } from "react";
import { Plus, X } from "lucide-react";

import { useI18n } from "../../lib/preferences";
import { CREATE_BRIEF_MAX_LENGTH } from "./createIntake";
import {
  newSourceNoteField,
  type CreateSourceNoteDraft,
  type SourceNoteFieldRow,
} from "./sourceNote";

interface CreateSourceNoteEditorProps {
  value: CreateSourceNoteDraft;
  disabled?: boolean;
  onChange: (value: CreateSourceNoteDraft) => void;
}

export function CreateSourceNoteEditor({
  value,
  disabled = false,
  onChange,
}: CreateSourceNoteEditorProps) {
  const { t } = useI18n();

  const patchVisible = (visible: string) => {
    onChange({ ...value, visible });
  };

  const patchFields = (fields: SourceNoteFieldRow[]) => {
    onChange({ ...value, fields });
  };

  const patchField = (id: string, next: Partial<SourceNoteFieldRow>) => {
    patchFields(
      value.fields.map((field) => (field.id === id ? { ...field, ...next } : field)),
    );
  };

  const addField = () => {
    if (disabled || value.fields.length >= 12) return;
    patchFields([...value.fields, newSourceNoteField()]);
  };

  const removeField = (id: string) => {
    patchFields(value.fields.filter((field) => field.id !== id));
  };

  const focusValue = (id: string) => {
    const node = document.getElementById(`agent-source-note-value-${id}`);
    if (node instanceof HTMLInputElement) node.focus();
  };

  const onValueKeyDown = (id: string, event: KeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing) return;
    const isInsertTab = event.key === "Tab" && !event.shiftKey;
    if (event.key !== "Enter" && !isInsertTab) return;
    const index = value.fields.findIndex((field) => field.id === id);
    const next = value.fields[index + 1];
    if (!next) return;
    event.preventDefault();
    focusValue(next.id);
  };

  return (
    <div data-create-source-note-editor className="space-y-0 overflow-hidden rounded-control border border-border-l1 bg-surface-raised p-0 focus-within:border-accent focus-within:ring-2 focus-within:ring-focus-ring">
      <label htmlFor="agent-product-brief" className="block px-3 py-3">
        <span className="sr-only">{t("agentCreate.brief")}</span>
        <textarea
          id="agent-product-brief"
          value={value.visible}
          maxLength={CREATE_BRIEF_MAX_LENGTH}
          disabled={disabled}
          onChange={(event) => patchVisible(event.target.value)}
          placeholder={t("agentCreate.briefPlaceholder")}
          className="min-h-24 w-full resize-y border-0 bg-transparent px-1 py-1 text-[15px] leading-6 text-text-primary outline-none focus:bg-surface-raised focus:ring-2 focus:ring-inset focus:ring-focus-ring placeholder:text-text-muted disabled:cursor-not-allowed disabled:opacity-60"
        />
      </label>

      {value.fields.length > 0 ? (
        <table className="w-full border-t border-border-l1 text-sm" data-source-note-fields>
          <thead>
            <tr className="text-left text-[11px] font-medium text-text-muted">
              <th className="w-[7.5rem] px-3 py-1.5 font-medium">{t("agentCreate.briefFieldLabel")}</th>
              <th className="px-2 py-1.5 font-medium">{t("agentCreate.briefFieldValue")}</th>
              <th className="w-9 p-0">
                <span className="sr-only">{t("agentCreate.briefRemoveField")}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {value.fields.map((field) => {
              const empty = field.value.trim().length === 0;
              return (
                <tr
                  key={field.id}
                  data-source-note-field={field.label || field.id}
                  className={empty ? "bg-surface-subtle/80" : "bg-transparent"}
                >
                  <td className="border-t border-border-l1 px-2 py-1 align-middle">
                    <input
                      id={`agent-source-note-label-${field.id}`}
                      value={field.label}
                      disabled={disabled}
                      maxLength={16}
                      placeholder={t("agentCreate.briefFieldLabel")}
                      onChange={(event) => patchField(field.id, { label: event.target.value })}
                      className="w-full border-0 bg-transparent px-1 py-1 text-[13px] font-medium text-text-secondary outline-none focus:bg-surface-raised focus:ring-2 focus:ring-inset focus:ring-focus-ring placeholder:text-text-muted/70 disabled:cursor-not-allowed"
                    />
                  </td>
                  <td className="border-t border-border-l1 px-2 py-1 align-middle">
                    <input
                      id={`agent-source-note-value-${field.id}`}
                      value={field.value}
                      disabled={disabled}
                      maxLength={160}
                      placeholder={t("agentCreate.briefFieldPlaceholder")}
                      onChange={(event) => patchField(field.id, { value: event.target.value })}
                      onKeyDown={(event) => onValueKeyDown(field.id, event)}
                      className="w-full border-0 bg-transparent px-1 py-1 text-[13px] leading-5 text-text-primary outline-none focus:bg-surface-raised focus:ring-2 focus:ring-inset focus:ring-focus-ring placeholder:text-text-muted/60 disabled:cursor-not-allowed"
                    />
                  </td>
                  <td className="border-t border-border-l1 p-0 align-middle">
                    <button
                      type="button"
                      disabled={disabled}
                      title={t("agentCreate.briefRemoveField")}
                      aria-label={t("agentCreate.briefRemoveField")}
                      onClick={() => removeField(field.id)}
                      className="flex h-8 w-9 items-center justify-center text-text-muted transition-colors hover:text-state-error disabled:opacity-40"
                    >
                      <X size={13} />
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      ) : null}

      <div className="border-t border-border-l1 px-2 py-1.5">
        <button
          type="button"
          data-source-note-add-field
          disabled={disabled || value.fields.length >= 12}
          onClick={addField}
          className="inline-flex h-7 items-center gap-1 rounded-md px-1.5 text-[12px] font-medium text-text-muted transition-colors hover:bg-surface-subtle hover:text-text-secondary disabled:cursor-not-allowed disabled:opacity-40"
        >
          <Plus size={12} aria-hidden="true" />
          {t("agentCreate.briefAddField")}
        </button>
      </div>
    </div>
  );
}
