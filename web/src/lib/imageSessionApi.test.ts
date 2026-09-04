import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("image session API", () => {
  it("encodes the optional cursor and limit", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ items: [], next_cursor: null }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.listImageSessions({ after: "cursor=1/+", limit: 25 });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    const parsed = new URL(url, "http://localhost");
    expect(parsed.pathname).toBe("/api/image-sessions");
    expect(Object.fromEntries(parsed.searchParams)).toEqual({
      after: "cursor=1/+",
      limit: "25",
    });
  });

  it("encodes history cursor, limit, and session id", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ items: [], next_after: null }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.getImageSessionHistory("session/1+", { after: "cursor=1/+", limit: 20 });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    const parsed = new URL(url, "http://localhost");
    expect(parsed.pathname).toBe("/api/image-sessions/session%2F1%2B/history");
    expect(Object.fromEntries(parsed.searchParams)).toEqual({
      after: "cursor=1/+",
      limit: "20",
    });
  });
});
