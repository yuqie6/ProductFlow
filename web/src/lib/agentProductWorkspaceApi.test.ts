import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("Agent product workspace API", () => {
  it("loads the backend-owned options catalog and read-only workbench bootstrap", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.getAgentProductWorkspaceOptions();
    await api.getAgentWorkbench("product/1");
    await api.getActiveProductWorkflowV2("product-1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/agent-product-workspaces/options",
      "/api/v2/products/product%2F1/agent-workbench",
      "/api/v2/products/product-1/workflow",
    ]);
  });

  it("owns the multipart field names and keeps one stable idempotency header", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({ product: { id: "product-1" } }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const front = new File(["front"], "front.png", { type: "image/png" });
    const detail = new File(["detail"], "detail.webp", { type: "image/webp" });

    await api.createAgentProductWorkspace({
      name: "硬质刀具收纳套装",
      selection: {
        schema_version: 1,
        image_types: [
          { key: "scene", quantity: 3, order: 0 },
          { key: "hero", quantity: 2, order: 1 },
        ],
      },
      images: [front, detail],
      idempotency_key: "agent-create-1",
      agent_session_id: "session-1",
    });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v2/agent-product-workspaces");
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("include");
    expect(init.headers).toEqual({ "Idempotency-Key": "agent-create-1" });
    expect(init.body).toBeInstanceOf(FormData);
    const formData = init.body as FormData;
    expect(formData.get("name")).toBe("硬质刀具收纳套装");
    expect(JSON.parse(String(formData.get("selection")))).toEqual({
      schema_version: 1,
      image_types: [
        { key: "scene", quantity: 3, order: 0 },
        { key: "hero", quantity: 2, order: 1 },
      ],
    });
    expect(formData.getAll("images")).toEqual([front, detail]);
    expect(formData.get("agent_session_id")).toBe("session-1");
  });

  it("supports draft creation, workspace recovery, and bounded intake finalization", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);
    const front = new File(["front"], "front.png", { type: "image/png" });

    await api.createAgentProductDraftWorkspace({
      name: "硬质刀具收纳套装",
      idempotency_key: "draft-create-1",
      agent_session_id: "session-1",
    });
    await api.getAgentProductWorkspace("conversation/1");
    await api.finalizeAgentProductWorkspaceIntake({
      conversation_id: "conversation/1",
      selection: {
        schema_version: 1,
        image_types: [{ key: "hero", quantity: 2, order: 0 }],
      },
      images: [front],
      idempotency_key: "intake-finalize-1",
    });

    const [draftUrl, draftInit] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(draftUrl).toBe("/api/v2/agent-product-workspaces/drafts");
    expect(draftInit.headers).toEqual({
      "Content-Type": "application/json",
      "Idempotency-Key": "draft-create-1",
    });
    expect(JSON.parse(String(draftInit.body))).toEqual({
      name: "硬质刀具收纳套装",
      agent_session_id: "session-1",
    });
    expect(fetchMock.mock.calls[1][0]).toBe(
      "/api/v2/agent-product-workspaces/conversation%2F1",
    );
    const [intakeUrl, intakeInit] = fetchMock.mock.calls[2] as [string, RequestInit];
    expect(intakeUrl).toBe(
      "/api/v2/agent-product-workspaces/conversation%2F1/intake",
    );
    expect(intakeInit.headers).toEqual({ "Idempotency-Key": "intake-finalize-1" });
    const formData = intakeInit.body as FormData;
    expect(JSON.parse(String(formData.get("selection")))).toEqual({
      schema_version: 1,
      image_types: [{ key: "hero", quantity: 2, order: 0 }],
    });
    expect(formData.getAll("images")).toEqual([front]);
  });
});
