import { describe, expect, it } from "vitest";

import type { AgentProductImageTypeKey, AgentProductWorkspaceLimits, DeliveryPresetCatalog } from "../../lib/types";
import {
  applyRecommendedImageSet,
  agentImageTotal,
  aspectRatioForSelection,
  buildAgentProductSelection,
  resolveDeliveryPresetKey,
  toggleAgentImageType,
  updateAgentImageTypeAspectRatio,
  updateAgentImageTypeQuantity,
  validateAgentProductWorkspaceInput,
  type AgentImageTypeSelectionDraft,
} from "./imageTypeSelection";

const limits: AgentProductWorkspaceLimits = {
  min_image_types: 1,
  default_images_per_type: 2,
  min_images_per_type: 1,
  max_images_per_type: 6,
  max_total_images: 30,
  min_reference_images: 1,
  max_reference_images: 6,
  allowed_image_mime_types: ["image/png", "image/jpeg", "image/webp"],
};

function validate(selections: AgentImageTypeSelectionDraft[], referenceImageCount = 1) {
  return validateAgentProductWorkspaceInput({
    name: "测试商品",
    selections,
    referenceImageCount,
    limits,
  });
}

describe("Agent product image type selection", () => {
  it("appends only available missing recommendations in the fixed order", () => {
    const recommendedLimits = { ...limits, min_images_per_type: 2 };
    const current = [
      { key: "sku" as const, quantity: 4, aspectRatio: "16:9" },
      { key: "hero" as const, quantity: 3, aspectRatio: "9:16" },
    ];
    const result = applyRecommendedImageSet({
      current,
      catalogKeys: ["hero", "detail", "selling_point", "sku"],
      limits: recommendedLimits,
    });

    expect(result).toEqual({
      ok: true,
      selections: [
        ...current,
        { key: "detail", quantity: 2, aspectRatio: "1:1" },
        { key: "selling_point", quantity: 2, aspectRatio: "3:4" },
      ],
      addedKeys: ["detail", "selling_point"],
    });
    expect(current).toEqual([
      { key: "sku", quantity: 4, aspectRatio: "16:9" },
      { key: "hero", quantity: 3, aspectRatio: "9:16" },
    ]);
  });

  it("uses the current minimum quantity and keeps the operation atomic at capacity", () => {
    const constrainedLimits = { ...limits, min_images_per_type: 2, max_total_images: 5 };
    const current = [{ key: "hero" as const, quantity: 2, aspectRatio: "4:5" }];
    const result = applyRecommendedImageSet({
      current,
      catalogKeys: ["hero", "detail", "scene", "selling_point"],
      limits: constrainedLimits,
    });

    expect(result).toEqual({
      ok: false,
      reason: "capacity",
      selections: current,
      addedKeys: [],
    });
    expect(result.selections).toBe(current);
  });

  it("reports unavailable and complete sets without mutating the current array", () => {
    const current = [{ key: "sku" as const, quantity: 2 }];
    const unavailable = applyRecommendedImageSet({ current, catalogKeys: ["sku"], limits });
    expect(unavailable).toEqual({
      ok: false,
      reason: "unavailable",
      selections: current,
      addedKeys: [],
    });

    const completeCurrent = [
      { key: "hero" as const, quantity: 2 },
      { key: "detail" as const, quantity: 1 },
      { key: "scene" as const, quantity: 1 },
      { key: "selling_point" as const, quantity: 1 },
      { key: "sku" as const, quantity: 4, aspectRatio: "16:9" },
    ];
    const complete = applyRecommendedImageSet({
      current: completeCurrent,
      catalogKeys: ["hero", "detail", "scene", "selling_point", "sku"],
      limits,
    });
    expect(complete).toEqual({
      ok: false,
      reason: "complete",
      selections: completeCurrent,
      addedKeys: [],
    });
    expect(complete.selections).toBe(completeCurrent);
  });

  it("starts empty, appends selected types, and resets a reselected type to the backend default", () => {
    let current: AgentImageTypeSelectionDraft[] = [];
    current = toggleAgentImageType(current, "hero", true, limits.default_images_per_type);
    current = updateAgentImageTypeQuantity(current, "hero", 5);
    current = toggleAgentImageType(current, "scene", true, limits.default_images_per_type);
    current = toggleAgentImageType(current, "hero", false, limits.default_images_per_type);
    current = toggleAgentImageType(current, "hero", true, limits.default_images_per_type);

    expect(current).toEqual([
      { key: "scene", quantity: 2, aspectRatio: "4:3" },
      { key: "hero", quantity: 2, aspectRatio: "3:4" },
    ]);
    expect(buildAgentProductSelection(current)).toEqual({
      schema_version: 1,
      image_types: [
        { key: "scene", quantity: 2, order: 0 },
        { key: "hero", quantity: 2, order: 1 },
      ],
    });
  });

  it("adds a selected delivery preset without changing the legacy selection shape otherwise", () => {
    const current = [{ key: "hero" as const, quantity: 2 }];
    const legacy = buildAgentProductSelection(current);

    expect(buildAgentProductSelection(current, " jd_hero ")).toEqual({
      ...legacy,
      delivery_preset_key: "jd_hero",
    });
    expect(buildAgentProductSelection(current, null)).toEqual(legacy);
  });

  it("accepts only a key that is present in the strict catalog", () => {
    const catalog: DeliveryPresetCatalog = {
      supports_custom: true,
      items: [{
        key: "jd_hero",
        title: "京东主图",
        aspect_ratio: "1:1",
        applicable_image_type: "hero",
        reviewed_at: "2026-08-24",
        source: "official",
        disclaimer: "Default only",
        delivery_spec: {
          width: 1200,
          height: 1200,
          format: "png",
          max_byte_size: null,
          fit: "contain",
          background_color: null,
          crop_anchor: null,
        },
      }],
    };

    expect(resolveDeliveryPresetKey(catalog, " jd_hero ")).toBe("jd_hero");
    expect(resolveDeliveryPresetKey(catalog, "unknown")).toBeNull();
    expect(resolveDeliveryPresetKey(null, "jd_hero")).toBeNull();
  });

  it("accepts the 1, 6, and total-30 boundaries and reports each invalid boundary", () => {
    const keys: AgentProductImageTypeKey[] = ["hero", "scene", "detail", "sku", "dimensions"];
    const exactlyThirty = keys.map((key) => ({ key, quantity: 6 }));

    expect(validate([{ key: "hero", quantity: 1 }])).toBeNull();
    expect(validate([{ key: "hero", quantity: 6 }], 6)).toBeNull();
    expect(agentImageTotal(exactlyThirty)).toBe(30);
    expect(validate(exactlyThirty)).toBeNull();
    expect(validate([])).toEqual({ code: "image_type_required", minimum: 1 });
    expect(validate([{ key: "hero", quantity: 0 }])).toEqual({
      code: "quantity_out_of_range",
      minimum: 1,
      maximum: 6,
    });
    expect(validate([{ key: "hero", quantity: 7 }])).toEqual({
      code: "quantity_out_of_range",
      minimum: 1,
      maximum: 6,
    });
    expect(validate([...exactlyThirty, { key: "shipping", quantity: 1 }])).toEqual({
      code: "total_images_exceeded",
      total: 31,
      maximum: 30,
    });
    expect(validate([{ key: "hero", quantity: 2 }], 0)).toEqual({
      code: "reference_count_out_of_range",
      minimum: 1,
      maximum: 6,
    });
    expect(validate([{ key: "hero", quantity: 2 }], 7)).toEqual({
      code: "reference_count_out_of_range",
      minimum: 1,
      maximum: 6,
    });
  });

  it("does not mutate prior selection arrays", () => {
    const initial = [{ key: "hero" as const, quantity: 2 }];
    const next = updateAgentImageTypeQuantity(initial, "hero", 3);

    expect(initial).toEqual([{ key: "hero", quantity: 2 }]);
    expect(next).toEqual([{ key: "hero", quantity: 3 }]);
  });

  it("locks evidence types to quantity 1 and excludes them from generated totals", () => {
    const selected = toggleAgentImageType([], "certification", true, 2);
    expect(selected).toEqual([{ key: "certification", quantity: 1, aspectRatio: "1:1" }]);
    expect(updateAgentImageTypeQuantity(selected, "certification", 4)).toEqual(selected);
    expect(agentImageTotal(selected)).toBe(0);
    expect(agentImageTotal([...selected, { key: "hero", quantity: 2 }])).toBe(2);
    expect(validate(selected)).toBeNull();
  });

  it("keeps aspect ratio on the selection draft and out of the Agent intake payload", () => {
    const withRatio = updateAgentImageTypeAspectRatio([{ key: "hero", quantity: 2, aspectRatio: "3:4" }], "hero", "9:16");
    expect(aspectRatioForSelection({ key: "detail", quantity: 1 })).toBe("1:1");
    expect(withRatio).toEqual([{ key: "hero", quantity: 2, aspectRatio: "9:16" }]);
    expect(buildAgentProductSelection(withRatio).image_types).toEqual([
      { key: "hero", quantity: 2, order: 0 },
    ]);
  });
});
