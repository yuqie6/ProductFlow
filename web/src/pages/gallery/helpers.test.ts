import { describe, expect, it } from "vitest";

import type { GalleryEntry } from "../../lib/types";
import { galleryEntryAspectRatio, galleryEntrySizeLabel, galleryTileLayout, selectGalleryEntry } from "./helpers";

const createdAt = "2026-04-28T00:00:00Z";
const gridRowUnitPx = 8;
const gridGapPx = 16;
const regularSquareTileWidthPx = 308;
const featuredSquareTileWidthPx = 420;

function renderedGridHeight(rowSpan: number): number {
  return rowSpan * gridRowUnitPx + (rowSpan - 1) * gridGapPx;
}

function entry(overrides: Partial<GalleryEntry>): GalleryEntry {
  return {
    id: "gallery-1",
    image_session_asset_id: "asset-1",
    image_session_round_id: "round-1",
    image_session_id: "session-1",
    image_session_title: "session",
    product_id: null,
    product_name: null,
    image: {
      id: "asset-1",
      kind: "generated_image",
      original_filename: "asset.png",
      mime_type: "image/png",
      download_url: "/download",
      preview_url: "/preview",
      thumbnail_url: "/thumb",
      created_at: createdAt,
    },
    prompt: "prompt",
    size: "2048x2048",
    actual_size: "1024x1024",
    model_name: "mock",
    provider_name: "mock",
    prompt_version: "v1",
    provider_response_id: null,
    image_generation_call_id: null,
    generation_group_id: null,
    candidate_index: 1,
    candidate_count: 1,
    base_asset_id: null,
    selected_reference_asset_ids: [],
    provider_notes: [],
    created_at: createdAt,
    ...overrides,
  };
}

describe("gallery helpers", () => {
  it("formats requested and actual size metadata", () => {
    expect(galleryEntrySizeLabel(entry({}))).toBe("实际 1024x1024 · 请求 2048x2048");
    expect(galleryEntrySizeLabel(entry({ size: "1024x1024", actual_size: "1024x1024" }))).toBe("1024x1024");
    expect(galleryEntrySizeLabel(entry({ size: null, actual_size: null }))).toBe("尺寸未知");
  });

  it("keeps a selected entry when present and falls back to the newest list item", () => {
    const first = entry({ id: "gallery-1" });
    const second = entry({ id: "gallery-2" });
    expect(selectGalleryEntry([first, second], "gallery-2")).toBe(second);
    expect(selectGalleryEntry([first, second], "missing")).toBe(first);
    expect(selectGalleryEntry([], "gallery-1")).toBeNull();
  });

  it("derives a bounded aspect ratio from actual size before requested size", () => {
    expect(galleryEntryAspectRatio(entry({ actual_size: "1536x1024", size: "1024x1024" }))).toBeCloseTo(1.5);
    expect(galleryEntryAspectRatio(entry({ actual_size: null, size: "1024x2048" }))).toBeCloseTo(0.72);
    expect(galleryEntryAspectRatio(entry({ actual_size: "4096x1024", size: "1024x1024" }))).toBeCloseTo(1.85);
    expect(galleryEntryAspectRatio(entry({ actual_size: null, size: "bad-size" }))).toBeCloseTo(0.8);
  });

  it("keeps tile layout stable for the same entry and varies featured placement by id", () => {
    const regular = galleryTileLayout(entry({ id: "gallery-1", actual_size: "1600x1000" }), 0);
    expect(galleryTileLayout(entry({ id: "gallery-1", actual_size: "1600x1000" }), 0)).toEqual(regular);
    expect(regular.aspectRatio).toBe("1.6000");
    expect(regular.className).toBe("lg:col-span-3");
    expect(regular.rowSpan).toBeGreaterThan(0);

    const possibleClassNames = new Set(
      Array.from({ length: 24 }, (_, index) =>
        galleryTileLayout(entry({ id: `gallery-${index}`, actual_size: "1600x1000" }), index).className,
      ),
    );
    expect(possibleClassNames.has("sm:col-span-2 lg:col-span-4")).toBe(true);
  });

  it("makes tall images occupy more grid rows than landscape images", () => {
    const landscape = galleryTileLayout(entry({ id: "layout-landscape", actual_size: "1600x1000" }), 0);
    const portrait = galleryTileLayout(entry({ id: "layout-portrait", actual_size: "900x1400" }), 0);

    expect(portrait.rowSpan).toBeGreaterThan(landscape.rowSpan);
  });

  it("keeps square tile heights close to their real gap-aware image width", () => {
    const square = galleryTileLayout(entry({ id: "gallery-1", actual_size: "1024x1024" }), 0);
    const featuredSquare = galleryTileLayout(entry({ id: "gallery-6", actual_size: "1024x1024" }), 6);

    expect(square.className).toBe("lg:col-span-3");
    expect(Math.abs(renderedGridHeight(square.rowSpan) - regularSquareTileWidthPx)).toBeLessThanOrEqual(gridGapPx);
    expect(featuredSquare.className).toBe("sm:col-span-2 lg:col-span-4");
    expect(Math.abs(renderedGridHeight(featuredSquare.rowSpan) - featuredSquareTileWidthPx)).toBeLessThanOrEqual(
      gridGapPx,
    );
  });

  it("uses the measured grid width instead of assuming a full-width desktop grid", () => {
    const portraitAtFullWidth = galleryTileLayout(entry({ id: "layout-portrait", actual_size: "900x1400" }), 0, 1280);
    const portraitAtNarrowWidth = galleryTileLayout(entry({ id: "layout-portrait", actual_size: "900x1400" }), 0, 1040);

    expect(portraitAtNarrowWidth.rowSpan).toBeLessThan(portraitAtFullWidth.rowSpan);
  });
});
