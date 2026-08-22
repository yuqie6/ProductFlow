import type { AgentProductImageTypeKey } from "./types";

export type ImageTypeFamily = "photography" | "infographic" | "evidence";

export const IMAGE_TYPE_FAMILY_ORDER: ImageTypeFamily[] = ["photography", "infographic", "evidence"];

export const IMAGE_TYPE_FAMILY_BY_KEY: Record<AgentProductImageTypeKey, ImageTypeFamily> = {
  hero: "photography",
  scene: "photography",
  detail: "photography",
  sku: "photography",
  packaging: "photography",
  selling_point: "infographic",
  dimensions: "infographic",
  specifications: "infographic",
  after_sales: "infographic",
  precautions: "infographic",
  faq: "infographic",
  shipping: "infographic",
  brand_story: "infographic",
  certification: "evidence",
  factory: "evidence",
};

export function imageTypeFamily(key: AgentProductImageTypeKey): ImageTypeFamily {
  return IMAGE_TYPE_FAMILY_BY_KEY[key];
}

export function isEvidenceImageType(key: AgentProductImageTypeKey): boolean {
  return imageTypeFamily(key) === "evidence";
}

export function isGeneratingImageType(key: AgentProductImageTypeKey): boolean {
  return !isEvidenceImageType(key);
}

export function generatingImageTypeKeys(): AgentProductImageTypeKey[] {
  return (Object.keys(IMAGE_TYPE_FAMILY_BY_KEY) as AgentProductImageTypeKey[]).filter(isGeneratingImageType);
}

export const IMAGE_TYPE_ASPECT_RATIOS: Record<AgentProductImageTypeKey, string> = {
  hero: "3:4",
  selling_point: "3:4",
  scene: "4:3",
  detail: "1:1",
  sku: "1:1",
  dimensions: "3:4",
  specifications: "3:4",
  after_sales: "3:4",
  brand_story: "16:9",
  precautions: "3:4",
  certification: "1:1",
  faq: "3:4",
  factory: "16:9",
  packaging: "1:1",
  shipping: "3:4",
};

export function defaultAspectRatioForType(key: AgentProductImageTypeKey): string {
  return IMAGE_TYPE_ASPECT_RATIOS[key];
}
