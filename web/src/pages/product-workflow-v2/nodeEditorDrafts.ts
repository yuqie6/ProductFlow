import type {
  JsonValue,
  WorkflowDeliverySpec,
  WorkflowGenerationSpec,
  WorkflowNodeV2,
} from "../../lib/types";
import { parseWorkflowDeliverySpec } from "./deliveryRenditions";
import { parseWorkflowGenerationSpec } from "./generationSpec";

export interface ReferenceEditorDraft {
  title: string;
  role: string;
  label: string;
}

export interface ImageEditorDraft {
  title: string;
  variation: string;
  generation: WorkflowGenerationSpec;
  delivery: WorkflowDeliverySpec | null;
}

export function referenceEditorDraft(node: WorkflowNodeV2): ReferenceEditorDraft {
  return {
    title: node.title,
    role: textConfig(node.config_json.role),
    label: textConfig(node.config_json.label),
  };
}

export function imageEditorDraft(node: WorkflowNodeV2): ImageEditorDraft | null {
  const generation = parseWorkflowGenerationSpec(node.config_json.generation_spec);
  const deliveryValue = node.config_json.delivery_spec;
  const delivery = deliveryValue == null ? null : parseWorkflowDeliverySpec(deliveryValue);
  if (!generation || (deliveryValue != null && !delivery)) {
    return null;
  }
  return {
    title: node.title,
    variation: textConfig(node.config_json.variation_instruction),
    generation,
    delivery,
  };
}

export function validateReferenceDraft(draft: ReferenceEditorDraft, message: string): string | null {
  if (
    !validRequiredText(draft.title, 255)
    || !validRequiredText(draft.role, 120)
    || !validRequiredText(draft.label, 255)
  ) {
    return message;
  }
  return null;
}

export function normalizeReferenceDraft(draft: ReferenceEditorDraft): ReferenceEditorDraft {
  return {
    title: draft.title.trim(),
    role: draft.role.trim(),
    label: draft.label.trim(),
  };
}

export function validateImageDraft(draft: ImageEditorDraft, message: string): string | null {
  if (
    !validRequiredText(draft.title, 255)
    || draft.variation.length > 4000
    || !parseWorkflowGenerationSpec(draft.generation)
    || (draft.delivery !== null && !parseWorkflowDeliverySpec(draft.delivery))
  ) {
    return message;
  }
  return null;
}

export function normalizeImageDraft(draft: ImageEditorDraft): ImageEditorDraft {
  return {
    title: draft.title.trim(),
    variation: draft.variation.trim(),
    generation: parseWorkflowGenerationSpec(draft.generation) ?? draft.generation,
    delivery: draft.delivery === null
      ? null
      : parseWorkflowDeliverySpec(draft.delivery) ?? draft.delivery,
  };
}

export function defaultDeliverySpec(): WorkflowDeliverySpec {
  return {
    width: 1200,
    height: 1200,
    format: "png",
    max_byte_size: null,
    fit: "contain",
    background_color: null,
    crop_anchor: null,
  };
}

export function humanizeFactKey(key: string): string {
  return key
    .replace(/[._-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/(^|\s)\S/g, (character) => character.toUpperCase());
}

export function formatFactValue(
  value: JsonValue,
  noValue: string,
  booleanLabels: { true: string; false: string } = { true: "Yes", false: "No" },
): string {
  if (value === null || value === "") return noValue;
  if (typeof value === "string" || typeof value === "number") return String(value);
  if (typeof value === "boolean") return value ? booleanLabels.true : booleanLabels.false;
  if (Array.isArray(value)) {
    return value.length
      ? value.map((item) => formatFactValue(item, noValue, booleanLabels)).join(" · ")
      : noValue;
  }
  const entries = Object.entries(value);
  if (!entries.length) return noValue;
  return entries
    .map(([key, item]) => `${humanizeFactKey(key)}: ${formatFactValue(item, noValue, booleanLabels)}`)
    .join("; ");
}

function textConfig(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function validRequiredText(value: string, maxLength: number): boolean {
  const normalized = value.trim();
  return normalized.length > 0 && value.length <= maxLength;
}
