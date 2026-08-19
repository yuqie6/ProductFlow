import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("media library API", () => {
  it("saves a generated session asset through the canonical library route", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.saveMediaLibraryAssetFromSession("session-asset/1");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/media-library/from-session",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ image_session_asset_id: "session-asset/1" }),
      }),
    );
  });

  it("sends an idempotency key when collecting assets into a product", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => [],
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.collectMediaLibraryAssetsToProduct("product-1", ["library-1"], "collect-1");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/media-library/collect",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({
          "Content-Type": "application/json",
          "Idempotency-Key": "collect-1",
        }),
        body: JSON.stringify({ product_id: "product-1", media_library_asset_ids: ["library-1"] }),
      }),
    );
  });

  it("archives an asset with expected revision", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: "asset-1", is_archived: true }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.archiveMediaLibraryAsset("asset-1", 3);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/media-library/asset-1/archive?expected_revision=3",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
      }),
    );
  });

  it("restores an asset with expected revision", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: "asset-1", is_archived: false }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.restoreMediaLibraryAsset("asset-1", 4);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/media-library/asset-1/restore?expected_revision=4",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
      }),
    );
  });

  it("moves assets with expected revisions", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => [],
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.moveMediaLibraryAssets({
      assetIds: ["asset-1", "asset-2"],
      folderId: "folder-1",
      expectedRevisions: { "asset-1": 2, "asset-2": 5 },
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/media-library/organize/move",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({
          asset_ids: ["asset-1", "asset-2"],
          folder_id: "folder-1",
          expected_revisions: { "asset-1": 2, "asset-2": 5 },
        }),
      }),
    );
  });

  it("uploads files directly to media library with optional folder id", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => [{ id: "asset-1", display_name: "test.png" }],
    });
    vi.stubGlobal("fetch", fetchMock);

    const file = new File(["dummy"], "test.png", { type: "image/png" });
    await api.uploadMediaLibraryAssets([file], "folder-1");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/media-library/upload",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
      }),
    );
  });

  it("renames and deletes folder", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: "folder-1", name: "New Name" }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.renameMediaLibraryFolder("folder-1", "Old Name", "New Name");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/media-library/folders/folder-1",
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        body: JSON.stringify({ expected_name: "Old Name", name: "New Name" }),
      }),
    );

    await api.deleteMediaLibraryFolder("folder-1");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/media-library/folders/folder-1",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
      }),
    );
  });
});


