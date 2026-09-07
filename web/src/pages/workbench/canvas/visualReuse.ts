/**
 * 视觉方案复用：继承优先级展示与保存/采用辅助。
 * 消费 IQ-CF-07；Brand 层诚实占位（未选定 / 未合并风格），不假装已合并品牌色。
 */

import type { RecipeReusePreview, VisualInheritanceLayer, VisualInheritanceView } from "../../../lib/types";
import type { TranslationKey } from "../../../lib/i18n";

/** 与 go/internal/visualsystem BrandReason* 对齐（B0）。 */
export const BRAND_REASON_NOT_SELECTED = "brand_not_selected";
export const BRAND_REASON_EXISTS_NO_STYLE_MERGE = "brand_exists_no_style_merge";

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

/** 品牌占位 reason → 文案键；缺省按未选定。 */
export function brandPlaceholderLabelKey(reason: string): TranslationKey {
  switch (reason) {
    case BRAND_REASON_EXISTS_NO_STYLE_MERGE:
      return "visualReuse.brandExistsNoMerge";
    case BRAND_REASON_NOT_SELECTED:
    default:
      return "visualReuse.brandNotSelected";
  }
}

/** 仅保留风格链字段（style/colors）；事实/身份不得进入商品覆盖载荷。 */
export function overlayPayloadFromConfig(config: Record<string, unknown> | null | undefined): Record<string, unknown> {
  const overlay = config?.visual_overlay;
  if (!overlay || typeof overlay !== "object" || Array.isArray(overlay)) return {};
  const raw = overlay as Record<string, unknown>;
  const out: Record<string, unknown> = {};
  if ("style" in raw) out.style = raw.style;
  if ("colors" in raw) out.colors = raw.colors;
  return out;
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
      reason: BRAND_REASON_NOT_SELECTED,
      detail: "未选定品牌；继承链跳过品牌层",
    },
    preferred_visual_system_version_id: null,
    inheritance_priority: [...VISUAL_INHERITANCE_PRIORITY],
  };
}
