import { describe, expect, it } from "vitest";

import {
  BRAND_REASON_EXISTS_NO_STYLE_MERGE,
  BRAND_REASON_NOT_SELECTED,
  VISUAL_INHERITANCE_PRIORITY,
  brandPlaceholderLabelKey,
  emptyReusePreview,
  hasNewerVisualVersion,
  layerLabelKey,
  overlayPayloadFromConfig,
} from "./visualReuse";

describe("visualReuse", () => {
  it("keeps IQ-CF-07 priority order", () => {
    expect(VISUAL_INHERITANCE_PRIORITY).toEqual([
      "product_override",
      "selected_visual_system_version",
      "brand_version",
      "product_default",
    ]);
  });

  it("maps layer keys and extracts overlay payload", () => {
    expect(layerLabelKey("brand_version")).toBe("visualReuse.layer.brand");
    expect(overlayPayloadFromConfig({ visual_overlay: { style: ["冷色"] } })).toEqual({
      style: ["冷色"],
    });
    expect(overlayPayloadFromConfig({})).toEqual({});
  });

  it("maps Brand B0 placeholder reasons to copy keys", () => {
    expect(brandPlaceholderLabelKey(BRAND_REASON_NOT_SELECTED)).toBe("visualReuse.brandNotSelected");
    expect(brandPlaceholderLabelKey(BRAND_REASON_EXISTS_NO_STYLE_MERGE)).toBe(
      "visualReuse.brandExistsNoMerge",
    );
    expect(brandPlaceholderLabelKey("unknown")).toBe("visualReuse.brandNotSelected");
  });

  it("emptyReusePreview uses brand_not_selected, not table-not-ready", () => {
    const preview = emptyReusePreview();
    expect(preview.brand_placeholder).toEqual({
      status: "unavailable",
      reason: BRAND_REASON_NOT_SELECTED,
      detail: "未选定品牌；继承链跳过品牌层",
    });
    expect(preview.brand_placeholder.reason).not.toBe("brand_table_not_ready");
  });

  it("detects newer version hint", () => {
    expect(hasNewerVisualVersion(null)).toBe(false);
    expect(
      hasNewerVisualVersion({
        product_id: "p1",
        selected_visual_system_version_id: "v1",
        effective_payload: {},
        layers: [],
        brand_placeholder: {
          status: "unavailable",
          reason: BRAND_REASON_NOT_SELECTED,
          detail: "未选定品牌；继承链跳过品牌层",
        },
        newer_version_available: {
          id: "v2",
          visual_system_id: "s1",
          version: 2,
          schema_version: 1,
          payload: {},
          payload_hash: "a".repeat(64),
          created_at: "2026-09-07T00:00:00Z",
        },
      }),
    ).toBe(true);
  });

  it("CF-B5: overlay payload keeps style only; brand stays unavailable placeholder", () => {
    expect(
      overlayPayloadFromConfig({
        visual_overlay: {
          style: ["冷色"],
          colors: [{ value: "#111" }],
          capacity: "600ml",
        },
      }),
    ).toEqual({
      style: ["冷色"],
      colors: [{ value: "#111" }],
    });
    expect(VISUAL_INHERITANCE_PRIORITY.indexOf("brand_version")).toBe(2);
    expect(VISUAL_INHERITANCE_PRIORITY.indexOf("product_default")).toBe(3);
  });
});
