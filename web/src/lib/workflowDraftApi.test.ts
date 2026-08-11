import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("workflow draft API contract", () => {
  it("builds a reveal SSE URL with an optional replay cursor", () => {
    expect(api.workflowRevealEventsUrl("materialization-1")).toBe(
      "/api/v2/workflow-materializations/materialization-1/reveal-events",
    );
    expect(api.workflowRevealEventsUrl("materialization-1", 8)).toBe(
      "/api/v2/workflow-materializations/materialization-1/reveal-events?after=8",
    );
  });

  it("sends explicit draft and workflow revisions to the materialization endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        id: "materialization-1",
        created: true,
        workflow: {},
        reveal_events_url: "/api/v2/workflow-materializations/materialization-1/reveal-events",
      }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.materializeWorkflowDraft("product-1", "draft-1", {
      expected_draft_version: 3,
      expected_workflow_revision: 2,
      idempotency_key: "request-1",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v2/products/product-1/workflow-drafts/draft-1/materialize",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({
          expected_draft_version: 3,
          expected_workflow_revision: 2,
          idempotency_key: "request-1",
        }),
      }),
    );
  });

  it("submits exactly one v2 workflow node run through the dedicated endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      json: async () => ({ created: true, node_run: { id: "node-run-1" } }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.runWorkflowNodeV2("node-1");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v2/workflow-nodes/node-1/run",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
      }),
    );
  });

  it("queries requested, effective, and actual node-run evidence by stable id", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: "node-run-1", status: "succeeded" }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.getWorkflowNodeRunV2("node-run-1");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v2/workflow-node-runs/node-run-1",
      expect.objectContaining({ credentials: "include" }),
    );
  });
});
