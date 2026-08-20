import { describe, expect, it } from "vitest";

import { deliverySpecLabel, parseWorkflowDeliverySpec } from "./deliveryRenditions";

describe("workflow delivery spec", () => {
  it("parses the strict schema and formats a compact label", () => {
    const parsed = parseWorkflowDeliverySpec({
      width: 1600,
      height: 900,
      format: "webp",
      max_byte_size: 524288,
      fit: "cover",
      background_color: null,
      crop_anchor: "right",
    });

    expect(parsed).toEqual({
      width: 1600,
      height: 900,
      format: "webp",
      max_byte_size: 524288,
      fit: "cover",
      background_color: null,
      crop_anchor: "right",
    });
    expect(deliverySpecLabel(parsed!)).toBe("1600 x 900 WEBP");
  });

  it("rejects cross-field violations, oversized pixels, and unknown values", () => {
    expect(parseWorkflowDeliverySpec({ width: 100, height: 100, format: "png", fit: "contain", crop_anchor: "top" })).toBeNull();
    expect(parseWorkflowDeliverySpec({ width: 100, height: 100, format: "png", fit: "cover", background_color: "#FFFFFF" })).toBeNull();
    expect(parseWorkflowDeliverySpec({ width: 16384, height: 16384, format: "png", fit: "cover" })).toBeNull();
    expect(parseWorkflowDeliverySpec({ width: 100, height: 100, format: "gif", fit: "cover" })).toBeNull();
  });
});
