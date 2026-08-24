import type {
  CanonicalProductDetail,
  GraphProductFact,
  GraphProductFactSet,
  GraphNode,
  JsonValue,
} from "../../../lib/types";

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

/** 已退休的 V1 plan key 和编译器拥有的输入，不能经 inspector 回写。 */
export const GRAPH_PROMPT_STRIPPED_KEYS = [
  "images",
  "fact_keys",
  "evidence_asset_ids",
  "prompt_plan_key",
  "image_plan_key",
  "prompt_plan_keys",
  "image_plan_keys",
] as const;

export function graphTitleDraft(node: GraphNode): GraphTitleDraft {
  return { title: node.title };
}

/** 缺少 `source_product_id` 只给旧数据读；新节点必须显式写绑定。 */
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

export function graphProductSourceConfig(_node: GraphNode, draft: GraphProductSourceDraft): Record<string, unknown> {
  return {
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

export function validateGraphTitle(title: string, message: string): string | null {
  const normalized = title.trim();
  return normalized.length > 0 && title.length <= 255 ? null : message;
}

function nullableTextValue(value: unknown): string | null {
  const text = typeof value === "string" ? value.trim() : "";
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
