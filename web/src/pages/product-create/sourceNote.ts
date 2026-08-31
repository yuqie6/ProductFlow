import type { GeneratedSourceNote } from "../../lib/types";
import { CREATE_BRIEF_MAX_LENGTH } from "./createIntake";

export interface SourceNoteFieldRow {
  id: string;
  label: string;
  value: string;
}

export interface CreateSourceNoteDraft {
  visible: string;
  fields: SourceNoteFieldRow[];
}

let sourceNoteFieldSeq = 0;

export function newSourceNoteField(label = "", value = ""): SourceNoteFieldRow {
  sourceNoteFieldSeq += 1;
  return { id: `snf-${sourceNoteFieldSeq}`, label, value };
}

export function defaultCreateSourceNoteDraft(): CreateSourceNoteDraft {
  return { visible: "", fields: [] };
}

export function sourceNoteDraftFromGenerated(note: GeneratedSourceNote): CreateSourceNoteDraft {
  const fields: SourceNoteFieldRow[] = [];
  for (const item of note.fields ?? []) {
    const label = item.label.trim();
    if (!label) continue;
    fields.push(newSourceNoteField(label, item.value ?? ""));
  }
  return {
    visible: note.visible ?? "",
    fields,
  };
}

export function formatSourceNote(draft: CreateSourceNoteDraft): string {
  const parts: string[] = [];
  const visible = draft.visible.trim();
  if (visible) parts.push(visible);
  for (const field of draft.fields) {
    const label = field.label.trim();
    const value = field.value.trim();
    if (!label || !value) continue;
    parts.push(`${label}：${value}`);
  }
  const joined = parts.join("\n\n");
  const runes = [...joined];
  if (runes.length <= CREATE_BRIEF_MAX_LENGTH) return joined;
  return runes.slice(0, CREATE_BRIEF_MAX_LENGTH).join("");
}

export function isCreateSourceNoteReady(draft: CreateSourceNoteDraft): boolean {
  return formatSourceNote(draft).trim().length > 0;
}
