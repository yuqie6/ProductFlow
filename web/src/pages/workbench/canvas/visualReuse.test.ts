import { describe, expect, it } from "vitest";

import {
  VISUAL_INHERITANCE_PRIORITY,
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

  it("detects newer version hint", () => {
    expect(hasNewerVisualVersion(null)).toBe(false);
    expect(
      hasNewerVisualVersion({
        product_id: "p1",
        selected_visual_system_version_id: "v1",
        effective_payload: {},
        layers: [],
        brand_placeholder: { status: "unavailable", reason: "brand_table_not_ready", detail: "" },
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
});
