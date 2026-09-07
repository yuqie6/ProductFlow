/**
 * 视觉方案复用：继承优先级展示与保存/采用辅助。
 * 消费 IQ-CF-07；Brand 为显式占位，不假装多品牌实体。
 */

import type { RecipeReusePreview, VisualInheritanceLayer, VisualInheritanceView } from "../../../lib/types";
import type { TranslationKey } from "../../../lib/i18n";

export const VISUAL_INHERITANCE_PRIORITY = [
  "product_override",
  "selected_visual_system_version",
  "brand_version",
  "product_default",
] as const;

export function layerLabelKey(layer: string): TranslationKey {
  switch (layer) {
    case "product_override":
      return "visualReuse.layer.override";
    case "selected_visual_system_version":
      return "visualReuse.layer.selected";
    case "brand_version":
      return "visualReuse.layer.brand";
    case "product_default":
      return "visualReuse.layer.default";
    default:
      return "visualReuse.layer.unknown";
  }
}

export function overlayPayloadFromConfig(config: Record<string, unknown> | null | undefined): Record<string, unknown> {
  const overlay = config?.visual_overlay;
  if (!overlay || typeof overlay !== "object" || Array.isArray(overlay)) return {};
  return overlay as Record<string, unknown>;
}

export function activeLayers(view: VisualInheritanceView | null | undefined): VisualInheritanceLayer[] {
  return view?.layers ?? [];
}

export function hasNewerVisualVersion(view: VisualInheritanceView | null | undefined): boolean {
  return Boolean(view?.newer_version_available?.id);
}

export function emptyReusePreview(): RecipeReusePreview {
  return {
    inherited: [],
    pending: [],
    brand_placeholder: {
      status: "unavailable",
      reason: "brand_table_not_ready",
      detail: "",
    },
    preferred_visual_system_version_id: null,
    inheritance_priority: [...VISUAL_INHERITANCE_PRIORITY],
  };
}
