import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { GalleryAsset } from "../../../lib/types";
import {
  IMAGE_EXPLORER_MAX_SELECTION,
  assetCanReadMedia,
  decodeAssetDragPayload,
  encodeAssetDragPayload,
  flattenGalleryAssetPages,
  formatAssetByteSize,
  galleryAssetsQueryKey,
  imageExplorerViewStorageKey,
  isWideImageExplorer,
  readImageExplorerView,
  selectLoadedAssets,
  toggleAssetSelection,
  writeImageExplorerView,
} from "./explorerState";

function asset(id: string, verificationStatus: GalleryAsset["verification_status"] = "verified"): GalleryAsset {
  return {
    id,
    product_id: "product-1",
    media_object_id: `media-${id}`,
    origin_type: "upload",
    display_name: id,
    original_filename: `${id}.png`,
    image_type_key: null,
    user_folder_id: null,
    user_folder_name: null,
    image_type_title: null,
    generation: null,
    parent_asset_id: null,
    source_image_session_asset_id: null,
    mime_type: "image/png",
    byte_size: 1024,
    width: 100,
    height: 100,
    verification_status: verificationStatus,
    download_url: "/download",
    preview_url: "/preview",
    thumbnail_url: "/thumbnail",
    created_at: "2026-08-12T00:00:00Z",
    updated_at: "2026-08-12T00:00:00Z",
  };
}

describe("product image explorer state", () => {
  const storage = new Map<string, string>();
  beforeEach(() => {
    storage.clear();
    vi.stubGlobal("window", {
      localStorage: {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => storage.set(key, value),
      },
    });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("uses the exact 440px directory layout boundary", () => {
    expect(isWideImageExplorer(439)).toBe(false);
    expect(isWideImageExplorer(440)).toBe(true);
  });

  it("keeps view preference scoped to the product owner", () => {
    expect(readImageExplorerView("product-1")).toBe("grid");
    writeImageExplorerView("product-1", "list");
    expect(readImageExplorerView("product-1")).toBe("list");
    expect(readImageExplorerView("product-2")).toBe("grid");
    expect(imageExplorerViewStorageKey("product-1")).toContain("product-1");
  });

  it("bounds loaded selection to the archive and move API limit", () => {
    const assets = Array.from({ length: IMAGE_EXPLORER_MAX_SELECTION + 5 }, (_, index) => asset(`asset-${index}`));
    const selected = selectLoadedAssets(assets);
    expect(selected.size).toBe(IMAGE_EXPLORER_MAX_SELECTION);
    expect(toggleAssetSelection(selected, "asset-extra").has("asset-extra")).toBe(false);
    expect(toggleAssetSelection(selected, "asset-0").has("asset-0")).toBe(false);
  });

  it("formats media evidence and disables bytes for unverified assets", () => {
    expect(formatAssetByteSize(512)).toBe("512 B");
    expect(formatAssetByteSize(1536)).toBe("1.5 KiB");
    expect(formatAssetByteSize(2 * 1024 * 1024)).toBe("2.0 MiB");
    expect(formatAssetByteSize(null)).toBe("--");
    expect(assetCanReadMedia(asset("verified"))).toBe(true);
    expect(assetCanReadMedia(asset("missing", "missing"))).toBe(false);
    expect(assetCanReadMedia(asset("pending", "legacy_pending"))).toBe(false);
  });

  it("accepts only bounded unique drag payloads", () => {
    expect(decodeAssetDragPayload(encodeAssetDragPayload(["a", "b"]))).toEqual(["a", "b"]);
    expect(decodeAssetDragPayload("not-json")).toEqual([]);
    expect(decodeAssetDragPayload('["a","a"]')).toEqual([]);
    expect(decodeAssetDragPayload(JSON.stringify(Array.from({ length: 101 }, (_, index) => String(index))))).toEqual([]);
  });

  it("flattens only pages that have actually been fetched", () => {
    const firstPage = { items: [asset("a")], next_cursor: "cursor-1" };
    const secondPage = { items: [asset("b")], next_cursor: null };
    expect(flattenGalleryAssetPages([firstPage])).toEqual([firstPage.items[0]]);
    expect(flattenGalleryAssetPages([firstPage, secondPage])).toEqual([firstPage.items[0], secondPage.items[0]]);
  });

  it("changes the cache identity for directory, search, and sort", () => {
    const base = galleryAssetsQueryKey("product-1", { kind: "all", key: null }, "", "created_desc");
    expect(galleryAssetsQueryKey("product-1", { kind: "uploads", key: null }, "", "created_desc")).not.toEqual(base);
    expect(galleryAssetsQueryKey("product-1", { kind: "all", key: null }, "orange", "created_desc")).not.toEqual(base);
    expect(galleryAssetsQueryKey("product-1", { kind: "all", key: null }, "", "name_asc")).not.toEqual(base);
  });
});
