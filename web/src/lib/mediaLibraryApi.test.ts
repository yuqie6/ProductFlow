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
});
