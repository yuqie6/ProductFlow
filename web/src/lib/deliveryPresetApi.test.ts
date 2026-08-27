import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("delivery preset API", () => {
  it("reads the v3 catalog and returns the runtime-parsed order", async () => {
    const fetchMock = vi.fn().mockResolvedValue(okResponse(validCatalog()));
    vi.stubGlobal("fetch", fetchMock);

    const catalog = await api.getDeliveryPresets();

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v3/delivery-presets",
      expect.objectContaining({ credentials: "include" }),
    );
    expect(catalog.supports_custom).toBe(true);
    expect(catalog.items.map((item) => item.key)).toEqual([
      "taobao_tmall_hero",
      "jd_hero",
      "amazon_hero",
      "detail_portrait",
      "scene_landscape",
    ]);
    expect(catalog.items[0]?.delivery_spec.max_byte_size).toBeNull();
  });

  it("rejects drifted or provider-bearing wire payloads before rendering", async () => {
    const malformedPayloads: unknown[] = [
      { ...validCatalog(), supports_custom: false },
      { ...validCatalog(), unexpected: "field" },
      {
        ...validCatalog(),
        items: validCatalog().items.map((item, index) => index === 0
          ? { ...item, delivery_spec: { ...item.delivery_spec, provider: "openai" } }
          : item),
      },
      {
        ...validCatalog(),
        items: validCatalog().items.slice(0, 4),
      },
    ];

    for (const payload of malformedPayloads) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(okResponse(payload)));
      await expect(api.getDeliveryPresets()).rejects.toMatchObject({ status: 502 });
      vi.unstubAllGlobals();
    }
  });
});

function okResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function validCatalog() {
  return {
    supports_custom: true,
    items: [
      preset("taobao_tmall_hero", "淘宝/天猫首屏", "3:4", "hero", 1200, 1600),
      preset("jd_hero", "京东主图", "1:1", "hero", 1200, 1200),
      preset("amazon_hero", "Amazon 主图", "1:1", "hero", 1200, 1200),
      preset("detail_portrait", "详情竖图", "3:4", "detail", 1200, 1600),
      preset("scene_landscape", "场景横图", "4:3", "scene", 1600, 1200),
    ],
  };
}

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
    disclaimer: "Templates provide convenient defaults.",
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
