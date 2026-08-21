import type {
  CanonicalProductDetail,
  GraphProductFact,
  GraphProductFactSet,
  GraphNode,
  JsonValue,
  WorkflowDeliverySpec,
  WorkflowGenerationSpec,
} from "../../../lib/types";
import { parseWorkflowDeliverySpec } from "./deliveryRenditions";
import { parseWorkflowGenerationSpec } from "./generationSpec";

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

export const DEFAULT_GRAPH_GENERATION_SPEC: WorkflowGenerationSpec = {
  aspect_ratio: "1:1",
  resolution_tier: "high",
  quality_intent: "high",
  reference_fidelity: "high",
  background_intent: "auto",
  text_policy: "none",
  text_language: null,
};

export interface GraphTitleDraft {
  title: string;
}

export interface GraphProductSourceDraft {
  title: string;
  source_product_id: string | null;
  fact_set_version_id: string | null;
}

export interface ProductFactRowDraft {
  id: string;
  key: string;
  value: string;
  original: GraphProductFact;
}

export interface ProductFactsDraft {
  name: string;
  category: string;
  price: string;
  source_note: string;
  facts: ProductFactRowDraft[];
}

export type ProductFactsDraftError = "empty_key" | "empty_value" | "duplicate_key";

export interface GraphImageAssetDraft {
  title: string;
  role: string;
  label: string;
}

export interface GraphBriefDraft {
  title: string;
  goal: string;
  design_goals: string[];
  required_copy: string[];
  prohibitions: string[];
}

export const GRAPH_PROMPT_STRIPPED_KEYS = [
  "images",
  "fact_keys",
  "evidence_asset_ids",
  "prompt_plan_key",
  "image_plan_key",
  "prompt_plan_keys",
  "image_plan_keys",
] as const;

export interface GraphVisualDraft {
  title: string;
  visual_system_version_id: string;
  style: string[];
  prohibitions: string[];
  background: string;
}

export interface GraphPromptDraft {
  title: string;
  design_goal: string;
  shared_rules: string[];
  creative_boundary: string[];
  complex_structure: boolean;
  product_present: boolean;
  picture_in_picture: "none" | "allowed" | "required";
  requirements: string[];
  viewpoint: string;
  product_share_percent: number;
  layout: string;
  copy_regions: string[];
  focus: string[];
  selling_points: string[];
  background: string;
  decorations: string[];
  headline: string;
  subtitle: string;
  body: string;
  keywords: string[];
  lighting: string;
}

export interface GraphImageGenerationDraft {
  title: string;
  variation: string;
  generation: WorkflowGenerationSpec;
  delivery: WorkflowDeliverySpec | null;
}

export function graphTitleDraft(node: GraphNode): GraphTitleDraft {
  return { title: node.title };
}

export function graphProductSourceDraft(node: GraphNode): GraphProductSourceDraft {
  return {
    title: node.title,
    source_product_id: configHasKey(node.config, "source_product_id")
      ? nullableTextValue(node.config.source_product_id)
      : node.source_product?.id ?? null,
    fact_set_version_id: configHasKey(node.config, "fact_set_version_id")
      ? nullableTextValue(node.config.fact_set_version_id)
      : node.product_fact_set?.id ?? null,
  };
}

export function graphProductSourceConfig(node: GraphNode, draft: GraphProductSourceDraft): Record<string, unknown> {
  return {
    ...node.config,
    source_product_id: draft.source_product_id,
    fact_set_version_id: draft.fact_set_version_id,
  };
}

export function productFactsDraft(
  product: CanonicalProductDetail,
  factSet: GraphProductFactSet | null,
): ProductFactsDraft {
  return {
    name: product.name,
    category: product.category ?? "",
    price: product.price ?? "",
    source_note: product.source_note ?? "",
    facts: (factSet?.facts ?? []).map((fact, index) => ({
      id: `fact-${index}`,
      key: fact.key,
      value: factValueText(fact.value),
      original: fact,
    })),
  };
}

export function normalizeProductFactsDraft(draft: ProductFactsDraft): ProductFactsDraft {
  return {
    name: draft.name.trim(),
    category: draft.category.trim(),
    price: draft.price.trim(),
    source_note: draft.source_note.trim(),
    facts: draft.facts.map((fact) => ({
      ...fact,
      key: fact.key.trim(),
      value: fact.value.trim(),
    })),
  };
}

export function productFactsPayload(draft: ProductFactsDraft): GraphProductFact[] {
  return normalizeProductFactsDraft(draft).facts.map(({ key, value, original }) => ({
    ...original,
    key,
    value: value === factValueText(original.value) ? original.value : value,
  }));
}

export function validateProductFactsDraft(draft: ProductFactsDraft): ProductFactsDraftError | null {
  const normalized = normalizeProductFactsDraft(draft);
  if (!normalized.name) return "empty_value";
  const keys = new Set<string>();
  for (const fact of normalized.facts) {
    if (!fact.key) return "empty_key";
    if (!fact.value) return "empty_value";
    const key = fact.key.toLocaleLowerCase();
    if (keys.has(key)) return "duplicate_key";
    keys.add(key);
  }
  return null;
}

export function graphImageAssetDraft(node: GraphNode): GraphImageAssetDraft {
  return {
    title: node.title,
    role: textValue(node.config.role),
    label: textValue(node.config.label),
  };
}

export function graphBriefDraft(node: GraphNode): GraphBriefDraft {
  return {
    title: node.title,
    goal: textValue(node.config.goal) || textValue(node.config.title),
    design_goals: stringList(node.config.design_goals),
    required_copy: stringList(node.config.required_copy),
    prohibitions: stringList(node.config.prohibitions),
  };
}

export function graphVisualDraft(node: GraphNode): GraphVisualDraft {
  const overlay = isRecord(node.config.visual_overlay) ? node.config.visual_overlay : {};
  return {
    title: node.title,
    visual_system_version_id: textValue(node.config.visual_system_version_id),
    style: stringList(overlay.style),
    prohibitions: stringList(overlay.prohibitions),
    background: visualBackground(overlay.colors),
  };
}

export function graphPromptDraft(node: GraphNode): GraphPromptDraft {
  const prompt = isRecord(node.config.prompt) ? node.config.prompt : {};
  const fidelity = isRecord(prompt.product_fidelity) ? prompt.product_fidelity : {};
  const composition = isRecord(prompt.composition) ? prompt.composition : {};
  const content = isRecord(prompt.content) ? prompt.content : {};
  const text = isRecord(prompt.text) ? prompt.text : {};
  const atmosphere = isRecord(prompt.atmosphere) ? prompt.atmosphere : {};
  const pictureInPicture = fidelity.picture_in_picture;
  return {
    title: node.title,
    design_goal: textValue(prompt.design_goal),
    shared_rules: stringList(prompt.shared_rules),
    creative_boundary: stringList(prompt.creative_boundary),
    complex_structure: Boolean(fidelity.complex_structure),
    product_present: fidelity.product_present !== false,
    picture_in_picture: pictureInPicture === "allowed" || pictureInPicture === "required" ? pictureInPicture : "none",
    requirements: stringList(fidelity.requirements),
    viewpoint: textValue(composition.viewpoint),
    product_share_percent: numberValue(composition.product_share_percent, 70),
    layout: textValue(composition.layout),
    copy_regions: stringList(composition.copy_regions),
    focus: stringList(content.focus),
    selling_points: stringList(content.selling_points),
    background: textValue(content.background),
    decorations: stringList(content.decorations),
    headline: textValue(text.headline),
    subtitle: textValue(text.subtitle),
    body: textValue(text.body),
    keywords: stringList(atmosphere.keywords),
    lighting: textValue(atmosphere.lighting),
  };
}

export function graphImageGenerationDraft(node: GraphNode): GraphImageGenerationDraft {
  const generation = parseWorkflowGenerationSpec(node.config.generation_spec) ?? DEFAULT_GRAPH_GENERATION_SPEC;
  const deliveryValue = node.config.delivery_spec;
  const delivery = deliveryValue == null ? null : parseWorkflowDeliverySpec(deliveryValue);
  return {
    title: node.title,
    variation: textValue(node.config.variation_instruction),
    generation,
    delivery: deliveryValue != null && !delivery ? null : delivery,
  };
}

export function graphImageAssetConfig(node: GraphNode, draft: GraphImageAssetDraft): Record<string, unknown> {
  return {
    ...node.config,
    role: draft.role.trim() || null,
    label: draft.label.trim() || null,
  };
}

export function graphBriefConfig(node: GraphNode, draft: GraphBriefDraft): Record<string, unknown> {
  return {
    ...node.config,
    goal: draft.goal.trim(),
    design_goals: draft.design_goals,
    required_copy: draft.required_copy,
    prohibitions: draft.prohibitions,
  };
}

export function graphVisualConfig(node: GraphNode, draft: GraphVisualDraft): Record<string, unknown> {
  const overlay: Record<string, unknown> = {};
  if (draft.style.length) overlay.style = draft.style;
  if (draft.prohibitions.length) overlay.prohibitions = draft.prohibitions;
  const background = draft.background.trim();
  if (background) {
    overlay.colors = [{ role: "background", value: background, label: "背景" }];
  }
  return {
    ...node.config,
    visual_system_version_id: textValue(node.config.visual_system_version_id).trim() || null,
    visual_overlay: Object.keys(overlay).length ? overlay : null,
  };
}

export function graphPromptConfig(node: GraphNode, draft: GraphPromptDraft): Record<string, unknown> {
  const previous = omitTopologyKeys(isRecord(node.config.prompt) ? node.config.prompt : {});
  const config = omitTopologyKeys({ ...node.config });
  return {
    ...config,
    prompt: {
      ...previous,
      design_goal: draft.design_goal.trim(),
      shared_rules: draft.shared_rules,
      creative_boundary: draft.creative_boundary,
      product_fidelity: {
        complex_structure: draft.complex_structure,
        product_present: draft.product_present,
        picture_in_picture: draft.picture_in_picture,
        requirements: draft.requirements,
      },
      composition: {
        viewpoint: draft.viewpoint.trim(),
        product_share_percent: draft.product_share_percent,
        layout: draft.layout.trim(),
        copy_regions: draft.copy_regions,
      },
      content: {
        focus: draft.focus,
        selling_points: draft.selling_points,
        background: draft.background.trim(),
        decorations: draft.decorations,
      },
      text: {
        headline: nullableText(draft.headline),
        subtitle: nullableText(draft.subtitle),
        body: nullableText(draft.body),
      },
      atmosphere: {
        keywords: draft.keywords,
        lighting: draft.lighting.trim(),
      },
    },
  };
}

export function graphImageGenerationConfig(node: GraphNode, draft: GraphImageGenerationDraft): Record<string, unknown> {
  return {
    ...node.config,
    variation_instruction: nullableText(draft.variation),
    generation_spec: draft.generation,
    delivery_spec: draft.delivery,
  };
}

export function validateGraphTitle(title: string, message: string): string | null {
  return validRequiredText(title, 255) ? null : message;
}

export function validateGraphImageGenerationDraft(draft: GraphImageGenerationDraft, message: string): string | null {
  if (validateGraphTitle(draft.title, message) || draft.variation.length > 4000) {
    return message;
  }
  if (!parseWorkflowGenerationSpec(draft.generation)) {
    return message;
  }
  if (draft.delivery !== null && !parseWorkflowDeliverySpec(draft.delivery)) {
    return message;
  }
  return null;
}

export function normalizeGraphImageGenerationDraft(draft: GraphImageGenerationDraft): GraphImageGenerationDraft {
  return {
    title: draft.title.trim(),
    variation: draft.variation.trim(),
    generation: parseWorkflowGenerationSpec(draft.generation) ?? draft.generation,
    delivery: draft.delivery === null ? null : parseWorkflowDeliverySpec(draft.delivery) ?? draft.delivery,
  };
}

function textValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function nullableTextValue(value: unknown): string | null {
  const text = textValue(value).trim();
  return text || null;
}

function configHasKey(config: Record<string, unknown>, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(config, key);
}

function factValueText(value: JsonValue): string {
  if (typeof value === "string") return value;
  if (value === null) return "";
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function stringList(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string" && Boolean(item.trim()));
}

function nullableText(value: string): string | null {
  const normalized = value.trim();
  return normalized || null;
}

function validRequiredText(value: string, maxLength: number): boolean {
  const normalized = value.trim();
  return normalized.length > 0 && value.length <= maxLength;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function omitTopologyKeys(value: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(value).filter(([key]) => !GRAPH_PROMPT_STRIPPED_KEYS.includes(key as typeof GRAPH_PROMPT_STRIPPED_KEYS[number])),
  );
}

function visualBackground(value: unknown): string {
  if (!Array.isArray(value)) return "";
  const background = value.find((item) => isRecord(item) && item.role === "background" && typeof item.value === "string");
  return background && isRecord(background) ? String(background.value) : "";
}
