import type { TranslationKey } from "../../../lib/i18n";
import type {
  GraphCatalogConfigField,
  GraphNode,
  GraphNodeCatalog,
  GraphNodeType,
} from "../../../lib/types";
import { catalogLabelKey } from "./catalogConfig";
import { graphNodeConfigFields } from "./graphCatalog";

const HIDDEN_DOCUMENT_KEYS = new Set(["schema_version", "visual_variant_key"]);

const DOCUMENT_FIELD_LABEL_KEYS: Record<string, TranslationKey> = {
  atmosphere: "workflowConfirmation.atmosphere",
  background: "agentWorkbench.nodeEditor.background",
  body: "agentWorkbench.nodeEditor.body",
  colors: "graph.inspector.visualBackground",
  complex_structure: "agentWorkbench.nodeEditor.complexStructure",
  composition: "workflowConfirmation.composition",
  content: "workflowConfirmation.content",
  copy_regions: "agentWorkbench.nodeEditor.copyRegions",
  creative_boundary: "workflowConfirmation.creativeBoundary",
  decorations: "agentWorkbench.nodeEditor.decorations",
  design_goal: "workflowConfirmation.designGoal",
  key_messages: "graph.inspector.designGoals",
  fact_gaps: "graph.inspector.factGaps",
  focus: "agentWorkbench.nodeEditor.focus",
  goal: "workflowConfirmation.designGoal",
  headline: "agentWorkbench.nodeEditor.headline",
  keywords: "agentWorkbench.nodeEditor.keywords",
  layout: "agentWorkbench.nodeEditor.layout",
  lighting: "agentWorkbench.nodeEditor.lighting",
  picture_in_picture: "agentWorkbench.nodeEditor.pictureInPicture",
  product_fidelity: "workflowConfirmation.productFidelity",
  product_present: "agentWorkbench.nodeEditor.productPresent",
  product_share_percent: "agentWorkbench.nodeEditor.productShare",
  prohibitions: "workflowConfirmation.creativeBoundary",
  required_elements: "graph.inspector.requiredCopy",
  requirements: "agentWorkbench.nodeEditor.requirements",
  selling_points: "agentWorkbench.nodeEditor.sellingPoints",
  shared_rules: "workflowConfirmation.sharedRules",
  style: "graph.inspector.visualStyle",
  subtitle: "agentWorkbench.nodeEditor.subtitle",
  text: "workflowConfirmation.textContent",
  viewpoint: "agentWorkbench.nodeEditor.viewpoint",
};

const CANDIDATE_SECTION_LABEL_KEYS: Record<string, TranslationKey> = {
  objective: "graph.candidate.section.objective",
  copy: "graph.candidate.section.copy",
  requirements: "graph.inspector.requiredCopy",
  gaps: "graph.candidate.section.gaps",
  guardrails: "graph.candidate.section.guardrails",
  style: "graph.candidate.section.style",
  palette: "graph.candidate.section.palette",
  subject: "graph.candidate.section.subject",
  composition: "graph.candidate.section.composition",
  visual_style: "graph.candidate.section.visualStyle",
  constraints: "graph.candidate.section.constraints",
};

const HEX_COLOR = /^#[0-9A-Fa-f]{6}$/;

export interface DocumentCandidateFieldRow {
  path: string;
  key: string;
  labelKey: TranslationKey | null;
  current: unknown;
  proposed: unknown;
  control?: GraphCatalogConfigField["control"];
  valueKind?: GraphCatalogConfigField["value_kind"];
  choices?: string[];
}

export interface DocumentCandidateDisplayItem {
  text: string;
  empty?: boolean;
  swatch?: string;
}

export function documentCandidateCatalogFields(
  catalog: GraphNodeCatalog | null | undefined,
  nodeType: GraphNode["node_type"] | GraphNodeType,
): GraphCatalogConfigField[] {
  const fields = graphNodeConfigFields(catalog, nodeType);
  if (nodeType === "image_prompt") {
    return fields.find((field) => field.key === "prompt")?.fields ?? fields;
  }
  if (nodeType === "visual_system") {
    return fields.find((field) => field.key === "visual_overlay")?.fields ?? fields;
  }
  return fields;
}

export function candidateSectionLabelKey(key: string): TranslationKey {
  return CANDIDATE_SECTION_LABEL_KEYS[key] ?? "graph.candidate.section.other";
}

export function documentCandidateFieldLabelKey(row: DocumentCandidateFieldRow): TranslationKey | null {
  return row.labelKey ?? DOCUMENT_FIELD_LABEL_KEYS[row.key] ?? null;
}

export function documentCandidateFieldRows(
  current: Record<string, unknown>,
  proposed: Record<string, unknown>,
  fields: GraphCatalogConfigField[] = [],
): DocumentCandidateFieldRow[] {
  const rows = collectFieldRows(current, proposed, fields, "");
  if (rows.length > 0) return rows;
  if (documentValueEqual(current, proposed)) return [];
  return [{
    path: "document",
    key: "document",
    labelKey: null,
    current,
    proposed,
  }];
}

export function formatDocumentCandidateValue(
  value: unknown,
  row: Pick<DocumentCandidateFieldRow, "control" | "valueKind" | "choices" | "key">,
  t: (key: TranslationKey) => string,
): DocumentCandidateDisplayItem[] {
  if (isEmptyDocumentValue(value)) {
    return [{ text: t("graph.candidate.empty"), empty: true }];
  }
  if (typeof value === "boolean") {
    return [{ text: t(value ? "graph.candidate.yes" : "graph.candidate.no") }];
  }
  if (typeof value === "number") {
    return [{ text: String(value) }];
  }
  if (typeof value === "string") {
    return [{ text: translateChoice(value, row.choices, t) }];
  }
  if (row.control === "visual_background" || isColorList(value)) {
    return formatColorList(value, t);
  }
  if (Array.isArray(value)) {
    const items = value.flatMap((item) => formatListItem(item, t));
    return items.length ? items : [{ text: t("graph.candidate.empty"), empty: true }];
  }
  if (isRecord(value)) {
    const nested = Object.entries(value).flatMap(([key, item]) => {
      if (HIDDEN_DOCUMENT_KEYS.has(key) || isEmptyDocumentValue(item)) return [];
      const label = fieldLabelText(key, t);
      return formatDocumentCandidateValue(item, { key }, t).map((entry) => ({
        ...entry,
        text: entry.empty ? `${label}：${entry.text}` : `${label}：${entry.text}`,
      }));
    });
    return nested.length ? nested : [{ text: t("graph.candidate.empty"), empty: true }];
  }
  return [{ text: String(value) }];
}

function collectFieldRows(
  current: Record<string, unknown>,
  proposed: Record<string, unknown>,
  fields: GraphCatalogConfigField[],
  prefix: string,
): DocumentCandidateFieldRow[] {
  const rows: DocumentCandidateFieldRow[] = [];
  const fieldByKey = new Map(fields.map((field) => [field.key, field]));
  for (const key of orderedDocumentKeys(current, proposed, fields)) {
    const field = fieldByKey.get(key);
    const path = prefix ? `${prefix}.${key}` : key;
    const currentValue = current[key];
    const proposedValue = proposed[key];
    if (shouldDescend(field, currentValue, proposedValue)) {
      rows.push(...collectFieldRows(
        asRecord(currentValue),
        asRecord(proposedValue),
        field?.fields ?? [],
        path,
      ));
      continue;
    }
    if (documentValueEqual(currentValue, proposedValue)) continue;
    rows.push({
      path,
      key,
      labelKey: catalogLabelKey(field?.label_key) ?? DOCUMENT_FIELD_LABEL_KEYS[key] ?? null,
      current: currentValue,
      proposed: proposedValue,
      control: field?.control,
      valueKind: field?.value_kind,
      choices: field?.choices,
    });
  }
  return rows;
}

function orderedDocumentKeys(
  current: Record<string, unknown>,
  proposed: Record<string, unknown>,
  fields: GraphCatalogConfigField[],
): string[] {
  const seen = new Set<string>();
  const keys: string[] = [];
  const push = (key: string) => {
    if (HIDDEN_DOCUMENT_KEYS.has(key) || seen.has(key)) return;
    if (!(key in current) && !(key in proposed)) return;
    seen.add(key);
    keys.push(key);
  };
  for (const field of fields) {
    if (field.control === "hidden") continue;
    push(field.key);
  }
  for (const key of [...Object.keys(current), ...Object.keys(proposed)]) push(key);
  return keys;
}

function shouldDescend(
  field: GraphCatalogConfigField | undefined,
  current: unknown,
  proposed: unknown,
): boolean {
  if (field?.control === "visual_background" || field?.value_kind === "object_list") return false;
  if (isColorList(current) || isColorList(proposed)) return false;
  const nested = isRecord(current) || isRecord(proposed);
  if (!nested) return false;
  if (field?.control === "group" || field?.value_kind === "object" || field?.value_kind === "object_or_null") {
    return true;
  }
  return !field;
}

function formatListItem(value: unknown, t: (key: TranslationKey) => string): DocumentCandidateDisplayItem[] {
  if (typeof value === "string") {
    const text = value.trim();
    return text ? [{ text }] : [];
  }
  if (isColorItem(value)) return formatColorList([value], t);
  if (isRecord(value) || Array.isArray(value)) {
    return formatDocumentCandidateValue(value, { key: "" }, t);
  }
  if (value == null) return [];
  return [{ text: String(value) }];
}

function formatColorList(value: unknown, t: (key: TranslationKey) => string): DocumentCandidateDisplayItem[] {
  if (!Array.isArray(value)) return [{ text: t("graph.candidate.empty"), empty: true }];
  const items = value.filter(isColorItem).map((item) => {
    const swatch = HEX_COLOR.test(item.value) ? item.value : undefined;
    const role = item.role === "background" ? t("graph.inspector.visualBackground") : item.role;
    const name = item.label.trim() || role;
    return { text: swatch && name !== swatch ? `${name} ${swatch}` : name || swatch || item.value, swatch };
  });
  return items.length ? items : [{ text: t("graph.candidate.empty"), empty: true }];
}

function translateChoice(
  value: string,
  choices: string[] | undefined,
  t: (key: TranslationKey) => string,
): string {
  if (value === "png" || value === "jpeg" || value === "webp") return value.toUpperCase();
  const optionKey = catalogLabelKey(`agentWorkbench.nodeEditor.option.${value}`);
  if (optionKey && (!choices?.length || choices.includes(value))) return t(optionKey);
  return value;
}

function fieldLabelText(key: string, t: (key: TranslationKey) => string): string {
  const labelKey = DOCUMENT_FIELD_LABEL_KEYS[key] ?? catalogLabelKey(key);
  if (labelKey) return t(labelKey);
  return key.replace(/[_-]+/g, " ");
}

function documentValueEqual(left: unknown, right: unknown): boolean {
  return JSON.stringify(canonicalize(left)) === JSON.stringify(canonicalize(right));
}

function isEmptyDocumentValue(value: unknown): boolean {
  if (value == null) return true;
  if (typeof value === "string") return !value.trim();
  if (Array.isArray(value)) return value.length === 0;
  if (isRecord(value)) {
    return Object.entries(value).every(([key, item]) => (
      HIDDEN_DOCUMENT_KEYS.has(key) || isEmptyDocumentValue(item)
    ));
  }
  return false;
}

function canonicalize(value: unknown): unknown {
  if (isEmptyDocumentValue(value)) return null;
  if (Array.isArray(value)) return value.map(canonicalize);
  if (!isRecord(value)) return value;
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(value).sort()) {
    if (HIDDEN_DOCUMENT_KEYS.has(key)) continue;
    const item = canonicalize(value[key]);
    if (item === null) continue;
    out[key] = item;
  }
  return Object.keys(out).length === 0 ? null : out;
}

function asRecord(value: unknown): Record<string, unknown> {
  return isRecord(value) ? value : {};
}

function isColorList(value: unknown): boolean {
  return Array.isArray(value) && value.length > 0 && value.every(isColorItem);
}

function isColorItem(value: unknown): value is { role: string; value: string; label: string } {
  return isRecord(value)
    && typeof value.role === "string"
    && typeof value.value === "string"
    && (value.label === undefined || typeof value.label === "string");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
