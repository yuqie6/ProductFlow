/**
 * 创建与画布镜头用的图种家族。
 *
 * 证据类持久化为未绑定的 `image_asset` 占位。可生成类（摄影+信息图）持久化为一组：一个提示词节点加 N 个生图节点。
 */

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

/** 证据类图种不会生成提示词/生图节点对。 */
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
