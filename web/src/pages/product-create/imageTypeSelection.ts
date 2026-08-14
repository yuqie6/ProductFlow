import type { TranslationKey } from "../../lib/i18n";
import type {
  AgentProductImageTypeKey,
  AgentProductSelectionV1,
  AgentProductWorkspaceLimits,
} from "../../lib/types";

export interface AgentImageTypeSelectionDraft {
  key: AgentProductImageTypeKey;
  quantity: number;
}

export type AgentProductCreateValidationIssue =
  | { code: "options_unavailable" }
  | { code: "name_required" }
  | { code: "image_type_required"; minimum: number }
  | { code: "duplicate_image_type" }
  | { code: "quantity_out_of_range"; minimum: number; maximum: number }
  | { code: "total_images_exceeded"; total: number; maximum: number }
  | { code: "reference_count_out_of_range"; minimum: number; maximum: number };

export const AGENT_IMAGE_TYPE_TRANSLATIONS: Record<
  AgentProductImageTypeKey,
  { title: TranslationKey; description: TranslationKey }
> = {
  hero: { title: "agentCreate.type.hero.title", description: "agentCreate.type.hero.description" },
  selling_point: {
    title: "agentCreate.type.sellingPoint.title",
    description: "agentCreate.type.sellingPoint.description",
  },
  scene: { title: "agentCreate.type.scene.title", description: "agentCreate.type.scene.description" },
  detail: { title: "agentCreate.type.detail.title", description: "agentCreate.type.detail.description" },
  sku: { title: "agentCreate.type.sku.title", description: "agentCreate.type.sku.description" },
  dimensions: {
    title: "agentCreate.type.dimensions.title",
    description: "agentCreate.type.dimensions.description",
  },
  specifications: {
    title: "agentCreate.type.specifications.title",
    description: "agentCreate.type.specifications.description",
  },
  after_sales: {
    title: "agentCreate.type.afterSales.title",
    description: "agentCreate.type.afterSales.description",
  },
  brand_story: {
    title: "agentCreate.type.brandStory.title",
    description: "agentCreate.type.brandStory.description",
  },
  precautions: {
    title: "agentCreate.type.precautions.title",
    description: "agentCreate.type.precautions.description",
  },
  certification: {
    title: "agentCreate.type.certification.title",
    description: "agentCreate.type.certification.description",
  },
  faq: { title: "agentCreate.type.faq.title", description: "agentCreate.type.faq.description" },
  factory: { title: "agentCreate.type.factory.title", description: "agentCreate.type.factory.description" },
  packaging: {
    title: "agentCreate.type.packaging.title",
    description: "agentCreate.type.packaging.description",
  },
  shipping: { title: "agentCreate.type.shipping.title", description: "agentCreate.type.shipping.description" },
};

export function toggleAgentImageType(
  current: readonly AgentImageTypeSelectionDraft[],
  key: AgentProductImageTypeKey,
  selected: boolean,
  defaultQuantity: number,
): AgentImageTypeSelectionDraft[] {
  const alreadySelected = current.some((item) => item.key === key);
  if (selected) {
    return alreadySelected ? [...current] : [...current, { key, quantity: defaultQuantity }];
  }
  return alreadySelected ? current.filter((item) => item.key !== key) : [...current];
}

export function updateAgentImageTypeQuantity(
  current: readonly AgentImageTypeSelectionDraft[],
  key: AgentProductImageTypeKey,
  quantity: number,
): AgentImageTypeSelectionDraft[] {
  return current.map((item) => (item.key === key ? { ...item, quantity } : item));
}

export function agentImageTotal(current: readonly AgentImageTypeSelectionDraft[]): number {
  return current.reduce((total, item) => total + item.quantity, 0);
}

export function buildAgentProductSelection(
  current: readonly AgentImageTypeSelectionDraft[],
): AgentProductSelectionV1 {
  return {
    schema_version: 1,
    image_types: current.map((item, order) => ({ ...item, order })),
  };
}

export function validateAgentProductWorkspaceInput(input: {
  name: string;
  selections: readonly AgentImageTypeSelectionDraft[];
  referenceImageCount: number;
  limits: AgentProductWorkspaceLimits | null;
}): AgentProductCreateValidationIssue | null {
  const { limits } = input;
  if (limits === null) {
    return { code: "options_unavailable" };
  }
  if (!input.name.trim()) {
    return { code: "name_required" };
  }
  if (input.selections.length < limits.min_image_types) {
    return { code: "image_type_required", minimum: limits.min_image_types };
  }
  if (new Set(input.selections.map((item) => item.key)).size !== input.selections.length) {
    return { code: "duplicate_image_type" };
  }
  if (
    input.selections.some(
      (item) =>
        !Number.isInteger(item.quantity) ||
        item.quantity < limits.min_images_per_type ||
        item.quantity > limits.max_images_per_type,
    )
  ) {
    return {
      code: "quantity_out_of_range",
      minimum: limits.min_images_per_type,
      maximum: limits.max_images_per_type,
    };
  }
  const total = agentImageTotal(input.selections);
  if (total > limits.max_total_images) {
    return { code: "total_images_exceeded", total, maximum: limits.max_total_images };
  }
  if (
    input.referenceImageCount < limits.min_reference_images ||
    input.referenceImageCount > limits.max_reference_images
  ) {
    return {
      code: "reference_count_out_of_range",
      minimum: limits.min_reference_images,
      maximum: limits.max_reference_images,
    };
  }
  return null;
}
