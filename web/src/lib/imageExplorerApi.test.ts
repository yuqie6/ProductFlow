import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("product image explorer API", () => {
  it("encodes directory, search, sort, cursor, and page size with URLSearchParams", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ items: [], next_cursor: null }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.listGalleryAssets("product 1", {
      directory_kind: "user_folder",
      directory_key: "folder/1",
      q: "橙色 & blue",
      sort: "name_desc",
      after: "cursor=1/+",
      limit: 25,
    });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    const parsed = new URL(url, "http://localhost");
    expect(parsed.pathname).toBe("/api/v2/products/product%201/image-assets");
    expect(Object.fromEntries(parsed.searchParams)).toEqual({
      directory_kind: "user_folder",
      directory_key: "folder/1",
      q: "橙色 & blue",
      sort: "name_desc",
      after: "cursor=1/+",
      limit: "25",
    });
  });

  it("sends one atomic move payload with expected folder ids", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ items: [], next_cursor: null }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.moveGalleryAssets("product-1", {
      items: [
        { asset_id: "asset-1", expected_folder_id: null },
        { asset_id: "asset-2", expected_folder_id: "folder-old" },
      ],
      folder_id: "folder-new",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v2/products/product-1/image-assets/move",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({
          items: [
            { asset_id: "asset-1", expected_folder_id: null },
            { asset_id: "asset-2", expected_folder_id: "folder-old" },
          ],
          folder_id: "folder-new",
        }),
      }),
    );
  });

  it("requests the archive as a credentialed blob without routing through JSON decoding", async () => {
    const blob = new Blob(["zip"], { type: "application/zip" });
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, blob: async () => blob });
    vi.stubGlobal("fetch", fetchMock);

    await expect(api.downloadGalleryArchive("product-1", ["asset-1"])).resolves.toBe(blob);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v2/products/product-1/image-assets/download-archive",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ asset_ids: ["asset-1"] }),
      }),
    );
  });
});
