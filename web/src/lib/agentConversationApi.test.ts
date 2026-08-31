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
    await api.ensureAgentWorkbench("product/1", "session/1");

    expect(fetchMock.mock.calls.map(([url, init]) => [url, init?.method ?? "GET"])).toEqual([
      ["/api/v2/agent-sessions?include_archived=true", "GET"],
      ["/api/v2/products/product%2F1/agent-workbench?agent_session_id=session%2F1", "GET"],
      ["/api/v2/products/product%2F1/agent-workbench?agent_session_id=session%2F1", "POST"],
    ]);
    expect((fetchMock.mock.calls[2]?.[1] as RequestInit).headers).toEqual({
      "Content-Type": "application/json",
      "Idempotency-Key": "agent-workbench:product/1",
    });
  });

  it("owns the Agent control-plane SSE URL", () => {
    expect(api.agentControlEventsUrl()).toBe("/api/v2/agent-control/events");
  });

  it("encodes Agent Session mutation ids and preserves request methods", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.createAgentSession();
    await api.renameAgentSession("session/1", "春季素材 v2");
    await api.archiveAgentSession("session/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/agent-sessions",
      "/api/v2/agent-sessions/session%2F1",
      "/api/v2/agent-sessions/session%2F1/archive",
    ]);
    expect(fetchMock.mock.calls[0]?.[1]?.body).toBeUndefined();
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
    await api.getMediaLibraryAsset("asset/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/products/product%2F1/agent-conversations/conversation%2F1/turns?limit=50&after=cursor%2B%2F%3D",
      "/api/v2/products/product%2F1/agent-conversations/conversation%2F1/turns/projection%2F1",
      "/api/media-library/asset%2F1",
    ]);
    expect(api.getAgentTurnEventsUrl("product/1", "conversation/1", "projection/1", 17)).toBe(
      "/api/v2/products/product%2F1/agent-conversations/conversation%2F1/turns/projection%2F1/events?after=17",
    );
    expect(api.getGlobalAgentTurnEventsUrl("conversation/1", "projection/1", 17)).toBe(
      "/api/v2/agent-conversations/conversation%2F1/turns/projection%2F1/events?after=17",
    );
    expect(api.getProductImageAssetMediaUrl("asset/1", "thumbnail")).toBe(
      "/api/v2/product-image-assets/asset%2F1/download?variant=thumbnail",
    );
    expect(api.getMediaLibraryAssetMediaUrl("asset/1", "thumbnail")).toBe(
      "/api/media-library/asset%2F1/download?variant=thumbnail",
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

  it("encodes workflow run request identity and keeps confirmation controls on the product route", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => null,
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.getAgentWorkflowRunRequest("product/1", "conversation/1");
    await api.confirmAgentWorkflowRunRequest("product/1", "conversation/1", "request/1");
    await api.cancelAgentWorkflowRunRequest("product/1", "conversation/1", "request/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/products/product%2F1/agent-conversations/conversation%2F1/workflow-run-request",
      "/api/v2/products/product%2F1/agent-conversations/conversation%2F1/workflow-run-request/request%2F1/confirm",
      "/api/v2/products/product%2F1/agent-conversations/conversation%2F1/workflow-run-request/request%2F1/cancel",
    ]);
    expect(fetchMock.mock.calls.map(([, init]) => init?.method)).toEqual([
      undefined,
      "POST",
      "POST",
    ]);
  });

  it("uses the global conversation route while preserving page context and task identity", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.listGlobalAgentTurns("conversation/1", { after: "cursor+/=", limit: 10, taskId: "task/1" });
    await api.submitGlobalAgentTurn("conversation/1", {
      input_text: "检查全局素材",
      asset_ids: ["media-asset-1"],
      idempotency_key: "global-turn-1",
      task_id: "task/1",
      page_context: {
        route: "/media-library",
        page_type: "media_library",
        selected_asset_ids: ["media-asset-1"],
        visible_asset_ids: [],
        filters: {},
        captured_at: "2026-08-17T00:00:00Z",
      },
    });
    await api.getGlobalWorkflowRunRequest("conversation/1", "task/1");
    await api.confirmGlobalWorkflowRunRequest("conversation/1", "request/1");
    await api.cancelGlobalWorkflowRunRequest("conversation/1", "request/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/agent-conversations/conversation%2F1/turns?limit=10&after=cursor%2B%2F%3D&task_id=task%2F1",
      "/api/v2/agent-conversations/conversation%2F1/turns",
      "/api/v2/agent-conversations/conversation%2F1/workflow-run-request?task_id=task%2F1",
      "/api/v2/agent-conversations/conversation%2F1/workflow-run-request/request%2F1/confirm",
      "/api/v2/agent-conversations/conversation%2F1/workflow-run-request/request%2F1/cancel",
    ]);
    expect(fetchMock.mock.calls[1][1]?.body).toBe(
      JSON.stringify({
        input_text: "检查全局素材",
        asset_ids: ["media-asset-1"],
        idempotency_key: "global-turn-1",
        task_id: "task/1",
        page_context: {
          route: "/media-library",
          page_type: "media_library",
          selected_asset_ids: ["media-asset-1"],
          visible_asset_ids: [],
          filters: {},
          captured_at: "2026-08-17T00:00:00Z",
        },
      }),
    );
    expect(fetchMock.mock.calls.slice(2).map(([, init]) => init?.method)).toEqual([
      undefined,
      "POST",
      "POST",
    ]);
  });

  it("reads and confirms a global media organization Draft with an idempotent request", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.getGlobalLibraryOrganizationDraft("conversation/1");
    await api.confirmGlobalLibraryOrganizationDraft("conversation/1", 3, "confirm-1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/agent-conversations/conversation%2F1/library-organization-draft",
      "/api/v2/agent-conversations/conversation%2F1/library-organization-draft/confirm",
    ]);
    expect(fetchMock.mock.calls[1][1]?.method).toBe("POST");
    expect(fetchMock.mock.calls[1][1]?.body).toBe(
      JSON.stringify({ expected_draft_version: 3, idempotency_key: "confirm-1" }),
    );
  });

  it("reads, completes, and controls Agent Tasks through encoded ids", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.getAgentTask("task/1");
    await api.completeAgentTask("task/1");
    await api.pauseAgentTask("task/1");
    await api.resumeAgentTask("task/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/agent-tasks/task%2F1",
      "/api/v2/agent-tasks/task%2F1/complete",
      "/api/v2/agent-tasks/task%2F1/pause",
      "/api/v2/agent-tasks/task%2F1/resume",
    ]);
    expect(fetchMock.mock.calls.map(([, init]) => init?.method ?? "GET")).toEqual([
      "GET",
      "POST",
      "POST",
      "POST",
    ]);
  });
});
