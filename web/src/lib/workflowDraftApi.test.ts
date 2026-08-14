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
    expect(api.workflowRevealEventsUrl("materialization/1", 8)).toContain("materialization%2F1");
  });

  it("streams reveal bytes with credentials and both replay cursor forms", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: null,
      text: async () => "id: 9\n\n",
    });
    vi.stubGlobal("fetch", fetchMock);
    const chunks: string[] = [];

    await api.streamWorkflowRevealEvents("materialization-1", {
      after: 8,
      onChunk: (chunk) => chunks.push(chunk),
    });

    expect(chunks).toEqual(["id: 9\n\n"]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v2/workflow-materializations/materialization-1/reveal-events?after=8",
      expect.objectContaining({
        credentials: "include",
        headers: { Accept: "text/event-stream", "Last-Event-ID": "8" },
      }),
    );
  });

  it("confirms the exact draft revision currently under review", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: "draft-1", current_version: 4 }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.confirmWorkflowDraft("product-1", "draft-1", 4);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v2/products/product-1/workflow-drafts/draft-1/confirm",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ expected_draft_version: 4 }),
      }),
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

  it("lists and cancels v2 node runs through stable encoded ids", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ items: [] }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.listWorkflowNodeRunsV2("node/1", 12);
    await api.cancelWorkflowNodeRunV2("run/1");

    expect(fetchMock.mock.calls[0]).toEqual([
      "/api/v2/workflow-nodes/node%2F1/runs?limit=12",
      expect.objectContaining({ credentials: "include" }),
    ]);
    expect(fetchMock.mock.calls[1]).toEqual([
      "/api/v2/workflow-node-runs/run%2F1/cancel",
      expect.objectContaining({ method: "POST", credentials: "include" }),
    ]);
  });

  it("submits one scoped v2 workflow run and exposes workflow-level controls", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ items: [] }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.runWorkflowV2("product/1", "workflow/1");
    await api.getWorkflowRunV2("product/1", "workflow/1", "run/1");
    await api.listWorkflowRunsV2("product/1", "workflow/1", 12);
    await api.cancelWorkflowRunV2("product/1", "workflow/1", "run/1");
    await api.retryWorkflowRunV2("product/1", "workflow/1", "run/1");

    expect(fetchMock.mock.calls).toEqual([
      [
        "/api/v2/products/product%2F1/workflows/workflow%2F1/runs",
        expect.objectContaining({ method: "POST", credentials: "include" }),
      ],
      [
        "/api/v2/products/product%2F1/workflows/workflow%2F1/runs/run%2F1",
        expect.objectContaining({ credentials: "include" }),
      ],
      [
        "/api/v2/products/product%2F1/workflows/workflow%2F1/runs?limit=12",
        expect.objectContaining({ credentials: "include" }),
      ],
      [
        "/api/v2/products/product%2F1/workflows/workflow%2F1/runs/run%2F1/cancel",
        expect.objectContaining({ method: "POST", credentials: "include" }),
      ],
      [
        "/api/v2/products/product%2F1/workflows/workflow%2F1/runs/run%2F1/retry",
        expect.objectContaining({ method: "POST", credentials: "include" }),
      ],
    ]);
  });

  it("reads and updates a v2 node through the scoped typed endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ workflow_edit_version: 4, node: { id: "node/1" } }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const update = {
      node_type: "image_generation" as const,
      expected_edit_version: 4,
      title: "首屏海报图 1",
      variation_instruction: "保持结构，调整光位",
      generation_spec: {
        aspect_ratio: "1:1",
        resolution_tier: "high" as const,
        quality_intent: "high" as const,
        reference_fidelity: "high" as const,
        background_intent: "opaque" as const,
        text_policy: "required" as const,
        text_language: "zh-CN",
      },
      delivery_spec: null,
    };

    await api.getWorkflowNodeDetailV2("product/1", "workflow/1", "node/1");
    await api.updateWorkflowNodeV2("product/1", "workflow/1", "node/1", update);

    const url = "/api/v2/products/product%2F1/workflows/workflow%2F1/nodes/node%2F1";
    expect(fetchMock.mock.calls[0]?.[0]).toBe(url);
    expect(fetchMock.mock.calls[1]).toEqual([
      url,
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        body: JSON.stringify(update),
      }),
    ]);
  });

  it("creates, lists, reads, and retries delivery renditions through stable asset and job ids", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      json: async () => ({ id: "job-1", status: "queued" }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const spec = { width: 1600, height: 900, format: "webp" as const, fit: "cover" as const };

    await api.createDeliveryRendition("source/1", spec);
    await api.listDeliveryRenditions("source/1");
    await api.getDeliveryRenditionJob("job/1");
    await api.retryDeliveryRenditionJob("job/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/product-image-assets/source%2F1/renditions",
      "/api/v2/product-image-assets/source%2F1/renditions",
      "/api/v2/delivery-rendition-jobs/job%2F1",
      "/api/v2/delivery-rendition-jobs/job%2F1/retry",
    ]);
    expect(fetchMock.mock.calls[0]?.[1]).toEqual(expect.objectContaining({
      method: "POST",
      credentials: "include",
      body: JSON.stringify(spec),
    }));
    expect(fetchMock.mock.calls[3]?.[1]).toEqual(expect.objectContaining({
      method: "POST",
      credentials: "include",
    }));
  });
});
