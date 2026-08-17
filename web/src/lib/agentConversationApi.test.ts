import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("Agent conversation API", () => {
  it("keeps Agent Session listing and workbench selection identities in the URL", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ items: [] }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.listAgentSessions(true);
    await api.getAgentWorkbench("product/1", "session/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/agent-sessions?include_archived=true",
      "/api/v2/products/product%2F1/agent-workbench?agent_session_id=session%2F1",
    ]);
  });

  it("encodes Agent Session mutation ids and preserves request methods", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.createAgentSession({ title: "春季素材" });
    await api.renameAgentSession("session/1", "春季素材 v2");
    await api.archiveAgentSession("session/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/agent-sessions",
      "/api/v2/agent-sessions/session%2F1",
      "/api/v2/agent-sessions/session%2F1/archive",
    ]);
    expect(fetchMock.mock.calls.map(([, init]) => init?.method)).toEqual(["POST", "PATCH", "POST"]);
  });

  it("owns encoded Turn pagination, detail, and SSE URLs", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ items: [], next_cursor: null }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.listAgentTurns("product/1", "conversation/1", { after: "cursor+/=", limit: 50 });
    await api.getAgentTurn("product/1", "conversation/1", "projection/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/products/product%2F1/agent-conversations/conversation%2F1/turns?limit=50&after=cursor%2B%2F%3D",
      "/api/v2/products/product%2F1/agent-conversations/conversation%2F1/turns/projection%2F1",
    ]);
    expect(api.getAgentTurnEventsUrl("product/1", "conversation/1", "projection/1", 17)).toBe(
      "/api/v2/products/product%2F1/agent-conversations/conversation%2F1/turns/projection%2F1/events?after=17",
    );
    expect(api.getProductImageAssetMediaUrl("asset/1", "thumbnail")).toBe(
      "/api/v2/product-image-assets/asset%2F1/download?variant=thumbnail",
    );
  });

  it("serializes submit, answer, resume, and cancel controls exactly once", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.submitAgentTurn("product-1", "conversation-1", {
      input_text: "补充价格为 299 元",
      asset_ids: ["asset-1"],
      idempotency_key: "message-1",
    });
    await api.answerAgentQuestion(
      "product-1",
      "conversation-1",
      "projection-1",
      "question-1",
      { option: 0 },
    );
    await api.resumeAgentTurn("product-1", "conversation-1", "projection-1");
    await api.cancelAgentTurn("product-1", "conversation-1", "projection-1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/products/product-1/agent-conversations/conversation-1/turns",
      "/api/v2/products/product-1/agent-conversations/conversation-1/turns/projection-1/questions/question-1/answer",
      "/api/v2/products/product-1/agent-conversations/conversation-1/turns/projection-1/resume",
      "/api/v2/products/product-1/agent-conversations/conversation-1/turns/projection-1/cancel",
    ]);
    expect(fetchMock.mock.calls.map(([, init]) => init?.method)).toEqual([
      "POST",
      "POST",
      "POST",
      "POST",
    ]);
    expect(fetchMock.mock.calls[0][1]?.body).toBe(
      JSON.stringify({
        input_text: "补充价格为 299 元",
        asset_ids: ["asset-1"],
        idempotency_key: "message-1",
      }),
    );
    expect(fetchMock.mock.calls[1][1]?.body).toBe(JSON.stringify({ option: 0 }));
  });
});
