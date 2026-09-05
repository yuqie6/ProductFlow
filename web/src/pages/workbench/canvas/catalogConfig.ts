/**
 * 按 Catalog 字段读取 inspector 草稿，不在打开表单时补默认值。
 *
 * 未登记和已退休的 plan key（`prompt_plan_key` 等）会被去掉。
 * hidden 字段留在 config 里，但不能在 inspector 编辑。
 */

import type { TranslationKey } from "../../../lib/i18n";
import { zhCN } from "../../../lib/i18n";
import type { GraphCatalogConfigField, GraphCatalogVisibleWhen, GraphNode } from "../../../lib/types";
import { parseWorkflowDeliverySpec } from "./deliveryRenditions";
import { parseWorkflowGenerationSpec } from "./generationSpec";
import { GRAPH_PROMPT_STRIPPED_KEYS } from "./graphNodeEditorDrafts";

export interface CatalogNodeDraft {
  title: string;
  config: Record<string, unknown>;
}

export function catalogLabelKey(key: string | null | undefined): TranslationKey | null {
  if (!key || !(key in zhCN)) return null;
  return key as TranslationKey;
}

export function visibleCatalogFields(fields: GraphCatalogConfigField[]): GraphCatalogConfigField[] {
  return fields.filter((field) => field.control !== "hidden");
}

export function catalogNodeDraft(node: GraphNode, fields: GraphCatalogConfigField[]): CatalogNodeDraft {
  return {
    title: node.title,
    config: cloneJson(Object.fromEntries(Object.entries(isRecord(node.config) ? node.config : {})
      .filter(([key]) => fields.some((field) => field.key === key)))),
  };
}

export function catalogConfigForSave(
  fields: GraphCatalogConfigField[],
  config: Record<string, unknown>,
): Record<string, unknown> {
  const next: Record<string, unknown> = {};
  for (const field of fields) {
    if (!Object.prototype.hasOwnProperty.call(config, field.key) || isPromptStrippedKey(field.key)) continue;
    const normalized = normalizeFieldValue(field, config[field.key]);
    if (normalized !== undefined) next[field.key] = normalized;
  }
  return next;
}

export function normalizeCatalogDraft(draft: CatalogNodeDraft, fields: GraphCatalogConfigField[]): CatalogNodeDraft {
  return {
    title: draft.title.trim(),
    config: catalogConfigForSave(fields, draft.config),
  };
}

export function validateCatalogDraft(
  draft: CatalogNodeDraft,
  fields: GraphCatalogConfigField[],
  message: string,
): string | null {
  if (!draft.title.trim() || draft.title.length > 255) return message;
  const generation = draft.config.generation_spec;
  if (generation != null && !parseWorkflowGenerationSpec(generation)) return message;
  const delivery = draft.config.delivery_spec;
  if (delivery != null && !parseWorkflowDeliverySpec(delivery)) return message;
  for (const settings of [draft.config.text_settings, draft.config.text_override]) {
    if (settings == null) continue;
    if (!isRecord(settings)) return message;
    const language = typeof settings.language === "string" ? settings.language.trim() : "";
    if (settings.policy === "required" ? !language : settings.policy !== "none" || Boolean(language)) return message;
  }
  if (!validateFieldTree(fields, draft.config)) return message;
  return null;
}

export function catalogFieldsAtPath(
  fields: GraphCatalogConfigField[],
  path: readonly string[],
): GraphCatalogConfigField[] {
  let current = fields;
  for (const key of path) {
    const match = current.find((field) => field.key === key);
    current = match?.fields ?? [];
  }
  return current;
}

export function patchCatalogValue(
  fields: GraphCatalogConfigField[],
  config: Record<string, unknown>,
  path: readonly string[],
  value: unknown,
): Record<string, unknown> {
  let next = setConfigPath(config, path, value);
  const parentPath = path.slice(0, -1);
  const parentFields = parentPath.length ? catalogFieldsAtPath(fields, parentPath) : fields;
  const parent = parentPath.length ? getConfigPath(next, parentPath) : next;
  if (!isRecord(parent)) return next;
  for (const field of parentFields) {
    if (catalogFieldVisible(field, parent)) continue;
    if (field.value_kind === "string_or_null" || field.control === "optional_object") {
      next = setConfigPath(next, [...parentPath, field.key], null);
    }
  }
  return next;
}

export function getConfigPath(value: unknown, path: readonly string[]): unknown {
  let current = value;
  for (const key of path) {
    if (!isRecord(current)) return undefined;
    current = current[key];
  }
  return current;
}

export function setConfigPath(
  config: Record<string, unknown>,
  path: readonly string[],
  value: unknown,
): Record<string, unknown> {
  if (path.length === 0) return config;
  const [head, ...rest] = path;
  if (rest.length === 0) {
    const next = { ...config, [head]: value };
    if (value === undefined) delete next[head];
    return next;
  }
  const child = isRecord(config[head]) ? config[head] : {};
  return { ...config, [head]: setConfigPath(child, rest, value) };
}

export function catalogFieldVisible(field: GraphCatalogConfigField, parent: unknown): boolean {
  if (field.control === "hidden") return false;
  return visibleWhenMatches(field.visible_when, parent);
}

export function readVisualBackground(value: unknown): string {
  if (!Array.isArray(value)) return "";
  const background = value.find((item) => isRecord(item) && item.role === "background" && typeof item.value === "string");
  return background && isRecord(background) ? String(background.value) : "";
}

export function visualBackgroundValueMaxLength(field: GraphCatalogConfigField): number | undefined {
  const valueField = field.fields?.find((child) => child.key === "value");
  return valueField?.max_length ?? undefined;
}

export function writeVisualBackground(
  text: string,
  existing: unknown = [],
  backgroundLabel = "背景",
): unknown {
  const trimmed = text.trim();
  const existingColors = Array.isArray(existing)
    ? existing.filter(isVisualColorItem)
    : [];
  const background = existingColors.find((item) => item.role === "background");
  const otherColors = existingColors.filter((item) => item.role !== "background");
  if (!trimmed) return otherColors;
  return [
    ...otherColors,
    {
      ...(background ?? {}),
      role: "background",
      value: trimmed,
      label: background?.label || backgroundLabel,
    },
  ];
}

export function readStringList(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string" && Boolean(item.trim()));
}

export function readStringListText(value: unknown): string {
  if (typeof value === "string") return value;
  return readStringList(value).join("\n");
}

export function readText(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function normalizeFieldValue(field: GraphCatalogConfigField, value: unknown): unknown {
  if (field.control === "visual_background") {
    if (!Array.isArray(value)) return value;
    return normalizeUnknownValue(value);
  }
  if (field.control === "optional_object") {
    if (value == null) return null;
    if (!isRecord(value)) return value;
    const nested = field.fields?.length ? catalogConfigForSave(field.fields, value) : value;
    const compact = compactRecord(nested);
    return Object.keys(compact).length ? compact : null;
  }
  if (field.value_kind === "object_or_null" && field.control === "group") {
    if (value == null) return null;
    if (!isRecord(value)) return value;
    const nested = field.fields?.length ? nestedCatalogObject(field.fields, value) : value;
    if (["prompt_overrides", "text_override", "visual_overlay"].includes(field.key)) return nested;
    const compact = compactRecord(nested);
    return Object.keys(compact).length ? compact : null;
  }
  if (field.value_kind === "object" && isRecord(value) && field.fields?.length) {
    const nested = nestedCatalogObject(field.fields, value);
    if (field.key === "prompt") return compactPromptRecord(nested);
    return nested;
  }
  if ((field.value_kind === "object" || field.value_kind === "object_or_null") && isRecord(value)) {
    return normalizeUnknownValue(value);
  }
  if (field.value_kind === "object_list" && Array.isArray(value)) {
    if (field.fields?.length) {
      return value.map((item) => isRecord(item) ? nestedCatalogObject(field.fields ?? [], item) : item);
    }
    return normalizeUnknownValue(value);
  }
  if (field.value_kind === "string_list") {
    if (Array.isArray(value)) return normalizeStringList(value);
    if (typeof value === "string") return normalizeStringList(value.split("\n"));
    return value;
  }
  if (field.value_kind === "string_or_null") {
    if (value === null) return null;
    if (typeof value === "string") {
      const trimmed = value.trim();
      return trimmed || null;
    }
    return value;
  }
  if (field.value_kind === "string") return typeof value === "string" ? value.trim() : value;
  if (field.value_kind === "boolean" || field.value_kind === "number" || field.value_kind === "number_or_null") return value;
  return value;
}

function nestedCatalogObject(
  fields: GraphCatalogConfigField[],
  value: Record<string, unknown>,
): Record<string, unknown> {
  return catalogConfigForSave(fields, value);
}

function compactRecord(value: Record<string, unknown>): Record<string, unknown> {
  const next: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value)) {
    const compacted = compactValue(item);
    if (compacted !== undefined) next[key] = compacted;
  }
  return next;
}

function compactPromptRecord(value: Record<string, unknown>): Record<string, unknown> | undefined {
  const compacted = compactPromptValue(value);
  return isRecord(compacted) ? compacted : undefined;
}

function compactPromptValue(value: unknown): unknown {
  if (value == null) return undefined;
  if (typeof value === "string") {
    const trimmed = value.trim();
    return trimmed || undefined;
  }
  if (Array.isArray(value)) {
    const items = value.map(compactPromptValue).filter((item): item is unknown => item !== undefined);
    return items.length ? items : undefined;
  }
  if (isRecord(value)) {
    const next: Record<string, unknown> = {};
    for (const [key, item] of Object.entries(value)) {
      if (isPromptStrippedKey(key)) continue;
      const compacted = compactPromptValue(item);
      if (compacted !== undefined) next[key] = compacted;
    }
    return Object.keys(next).length ? next : undefined;
  }
  return value;
}

function compactValue(value: unknown): unknown {
  if (value == null) return undefined;
  if (typeof value === "string") {
    const trimmed = value.trim();
    return trimmed || undefined;
  }
  if (Array.isArray(value)) {
    const items = value.map(compactValue).filter((item): item is unknown => item !== undefined);
    return items.length ? items : undefined;
  }
  if (isRecord(value)) {
    const compacted = compactRecord(value);
    return Object.keys(compacted).length ? compacted : undefined;
  }
  return value;
}

function normalizeStringList(value: unknown[]): string[] {
  return value
    .filter((item): item is string => typeof item === "string")
    .map((item) => item.trim())
    .filter(Boolean);
}

function normalizeUnknownValue(value: unknown): unknown {
  if (typeof value === "string") return value.trim();
  if (Array.isArray(value)) return value.map(normalizeUnknownValue);
  if (isRecord(value)) {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, normalizeUnknownValue(item)]));
  }
  return value;
}

function validateFieldTree(fields: GraphCatalogConfigField[], config: unknown): boolean {
  if (!isRecord(config)) return false;
  for (const field of fields) {
    if (!catalogFieldVisible(field, config)) continue;
    const hasValue = Object.prototype.hasOwnProperty.call(config, field.key) && config[field.key] !== undefined;
    if (!hasValue) {
      if (field.required) return false;
      continue;
    }
    const value = config[field.key];
    if (!validateFieldValue(field, value)) return false;
    if (field.required && isEmptyRequiredValue(value)) return false;
    if (field.fields?.length && isRecord(value) && !validateFieldTree(field.fields, value)) return false;
    if (field.fields?.length && field.value_kind === "object_list" && Array.isArray(value)) {
      if (value.some((item) => !isRecord(item) || !validateFieldTree(field.fields ?? [], item))) return false;
    }
  }
  return true;
}

function validateFieldValue(field: GraphCatalogConfigField, value: unknown): boolean {
  if (field.control === "visual_background") {
    if (!Array.isArray(value)) return false;
    if (field.max_length != null && value.length > field.max_length) return false;
    const valueMax = visualBackgroundValueMaxLength(field);
    return value.every((item) => (
      isVisualColorItem(item)
      && (valueMax == null || item.value.length <= valueMax)
    ));
  }
  switch (field.value_kind) {
    case "string":
      if (typeof value !== "string") return false;
      break;
    case "string_or_null":
      if (value !== null && typeof value !== "string") return false;
      break;
    case "string_list":
      if (!Array.isArray(value) || value.some((item) => typeof item !== "string")) return false;
      break;
    case "boolean":
      if (typeof value !== "boolean") return false;
      break;
    case "number":
      if (typeof value !== "number" || !Number.isFinite(value)) return false;
      if (field.min_value != null && value < field.min_value) return false;
      if (field.max_value != null && value > field.max_value) return false;
      break;
    case "number_or_null":
      if (value !== null && (typeof value !== "number" || !Number.isFinite(value))) return false;
      if (typeof value === "number" && field.min_value != null && value < field.min_value) return false;
      if (typeof value === "number" && field.max_value != null && value > field.max_value) return false;
      break;
    case "object":
      if (!isRecord(value)) return false;
      break;
    case "object_or_null":
      if (value !== null && !isRecord(value)) return false;
      break;
    case "object_list":
      if (!Array.isArray(value) || value.some((item) => !isRecord(item))) return false;
      break;
    default:
      return false;
  }
  if (field.choices?.length && typeof value === "string" && !field.choices.includes(value)) return false;
  if (field.max_length != null && typeof value === "string" && value.length > field.max_length) return false;
  if (field.max_length != null && Array.isArray(value) && value.length > field.max_length) return false;
  return true;
}

function isEmptyRequiredValue(value: unknown): boolean {
  if (value == null) return true;
  if (typeof value === "string") return !value.trim();
  if (Array.isArray(value)) return value.length === 0;
  if (isRecord(value)) return Object.keys(value).length === 0;
  return false;
}

function isVisualColorItem(value: unknown): value is Record<string, unknown> & { role: string; value: string } {
  return isRecord(value)
    && typeof value.role === "string"
    && typeof value.value === "string"
    && (value.label === undefined || typeof value.label === "string");
}

function visibleWhenMatches(rule: GraphCatalogVisibleWhen | null | undefined, parent: unknown): boolean {
  if (!rule) return true;
  if (!isRecord(parent)) return false;
  const actual = parent[rule.field];
  const candidate = actual == null ? "" : String(actual);
  return rule.values.includes(candidate);
}

function isPromptStrippedKey(key: string): boolean {
  return GRAPH_PROMPT_STRIPPED_KEYS.includes(key as typeof GRAPH_PROMPT_STRIPPED_KEYS[number]);
}

function cloneJson<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
