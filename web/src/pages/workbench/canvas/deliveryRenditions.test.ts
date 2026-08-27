import { describe, expect, it } from "vitest";

import {
  deliverySpecLabel,
  parseDeliveryPresetCatalog,
  parseWorkflowDeliverySpec,
  replaceDeliverySpec,
} from "./deliveryRenditions";

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

  it("accepts the exact server preset catalog and normalizes its delivery specs", () => {
    const catalog = parseDeliveryPresetCatalog({
      supports_custom: true,
      items: [
        preset("taobao_tmall_hero", "淘宝/天猫首屏", "3:4", "hero", 1200, 1600),
        preset("jd_hero", "京东主图", "1:1", "hero", 1200, 1200),
        preset("amazon_hero", "Amazon 主图", "1:1", "hero", 1200, 1200),
        preset("detail_portrait", "详情竖图", "3:4", "detail", 1200, 1600),
        preset("scene_landscape", "场景横图", "4:3", "scene", 1600, 1200),
      ],
    });

    expect(catalog?.items.map((item) => item.key)).toEqual([
      "taobao_tmall_hero",
      "jd_hero",
      "amazon_hero",
      "detail_portrait",
      "scene_landscape",
    ]);
    expect(catalog?.items[0]?.delivery_spec).toEqual({
      width: 1200,
      height: 1600,
      format: "png",
      max_byte_size: null,
      fit: "contain",
      background_color: null,
      crop_anchor: null,
    });
  });

  it("fails closed for catalog drift, malicious fields, and an unstable preset order", () => {
    const valid = {
      supports_custom: true,
      items: [
        preset("taobao_tmall_hero", "淘宝/天猫首屏", "3:4", "hero", 1200, 1600),
        preset("jd_hero", "京东主图", "1:1", "hero", 1200, 1200),
        preset("amazon_hero", "Amazon 主图", "1:1", "hero", 1200, 1200),
        preset("detail_portrait", "详情竖图", "3:4", "detail", 1200, 1600),
        preset("scene_landscape", "场景横图", "4:3", "scene", 1600, 1200),
      ],
    };
    expect(parseDeliveryPresetCatalog({ ...valid, supports_custom: false })).toBeNull();
    expect(parseDeliveryPresetCatalog({ ...valid, provider: "openai" })).toBeNull();
    expect(parseDeliveryPresetCatalog({
      ...valid,
      items: valid.items.map((item, index) => index === 0
        ? { ...item, delivery_spec: { ...item.delivery_spec, provider: "openai" } }
        : item),
    })).toBeNull();
    expect(parseDeliveryPresetCatalog({ ...valid, items: [...valid.items].reverse() })).toBeNull();
  });

  it("replaces only delivery_spec while preserving the editor draft", () => {
    const existing = {
      title: "未保存标题",
      generation_spec: { aspect_ratio: "4:5", resolution_tier: "high" },
      provider: "must-remain-untouched",
      visual_overlay: { style: ["clean"] },
    };
    const next = replaceDeliverySpec(existing, {
      width: 1200,
      height: 1600,
      format: "png",
      max_byte_size: null,
      fit: "contain",
      background_color: null,
      crop_anchor: null,
    });

    expect(next).toEqual({
      ...existing,
      delivery_spec: {
        width: 1200,
        height: 1600,
        format: "png",
        max_byte_size: null,
        fit: "contain",
        background_color: null,
        crop_anchor: null,
      },
    });
    expect(next).not.toBe(existing);
    expect(existing).not.toHaveProperty("delivery_spec");
  });
});

function preset(
  key: string,
  title: string,
  aspectRatio: string,
  applicableImageType: string,
  width: number,
  height: number,
) {
  return {
    key,
    title,
    aspect_ratio: aspectRatio,
    applicable_image_type: applicableImageType,
    reviewed_at: "2026-08-24",
    source: "docs/ARCHITECTURE.md §7",
    disclaimer: "模板只是便捷默认值，不构成平台合规保证。",
    delivery_spec: {
      width,
      height,
      format: "png",
      max_byte_size: null,
      fit: "contain",
      background_color: null,
      crop_anchor: null,
    },
  };
}
