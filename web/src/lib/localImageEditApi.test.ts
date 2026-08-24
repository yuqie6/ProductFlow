import { afterEach, describe, expect, it, vi } from "vitest";

import { api, ApiError } from "./api";
import { parseLocalImageEditTask } from "./localImageEdits";
import type { LocalImageEditMaskGeometry } from "./types";

afterEach(() => vi.unstubAllGlobals());

describe("local image edit API", () => {
  it("reads capability and encodes list/get task paths", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okResponse(capability()))
      .mockResolvedValueOnce(okResponse({ items: [task()] }))
      .mockResolvedValueOnce(okResponse(task()));
    vi.stubGlobal("fetch", fetchMock);

    await expect(api.getLocalImageEditCapability()).resolves.toMatchObject({ supported: true });
    await api.listLocalImageEdits("product / one", 500);
    await api.getLocalImageEdit("product / one", "task/one");

    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/v3/local-image-edits/capability");
    expect(fetchMock.mock.calls[1]?.[0]).toBe("/api/v3/products/product%20%2F%20one/image-edits?limit=100");
    expect(fetchMock.mock.calls[2]?.[0]).toBe("/api/v3/products/product%20%2F%20one/image-edits/task%2Fone");
    expect(fetchMock.mock.calls[0]?.[1]).toEqual(expect.objectContaining({ credentials: "include" }));
  });

  it("sends only canonical multipart fields and action bodies", async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(okResponse(task())));
    vi.stubGlobal("fetch", fetchMock);
    const mask = new Blob(["png"], { type: "image/png" });

    await api.createLocalImageEdit("product/one", {
      source_asset_id: "source-1",
      operation: "replace_text",
      mask,
      mask_geometry: geometry(),
      reference_asset_ids: ["reference-1"],
      source_text: "old",
      replacement_text: "new",
      target_node_id: "node/1",
    });
    const createInit = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(createInit.method).toBe("POST");
    expect(createInit.body).toBeInstanceOf(FormData);
    const form = createInit.body as FormData;
    expect([...form.keys()].sort()).toEqual([
      "mask",
      "mask_geometry_json",
      "operation",
      "reference_asset_ids_json",
      "replacement_text",
      "source_asset_id",
      "source_text",
      "target_node_id",
    ]);
    expect(form.get("source_asset_id")).toBe("source-1");
    expect(form.get("mask_geometry")).toBeNull();
    expect(form.get("reference_asset_ids")).toBeNull();
    expect(form.get("mask_geometry_json")).toBe(JSON.stringify(geometry()));
    expect(form.get("reference_asset_ids_json")).toBe(JSON.stringify(["reference-1"]));
    expect((form.get("mask") as File).type).toBe("image/png");

    await api.updateLocalImageEdit("product/one", "task/one", {
      expected_revision: 2,
      operation: "inpaint",
      mask,
      mask_geometry: geometry(),
      instruction: "fix",
    });
    const updateInit = fetchMock.mock.calls[1]?.[1] as RequestInit;
    expect(updateInit.method).toBe("PATCH");
    expect(updateInit.body).toBeInstanceOf(FormData);
    const updateForm = updateInit.body as FormData;
    expect(updateForm.get("expected_revision")).toBe("2");
    expect(updateForm.get("source_asset_id")).toBeNull();

    await api.submitLocalImageEdit("product/one", "task/one", "key-1");
    await api.cancelLocalImageEdit("product/one", "task/one", 2);
    await api.retryLocalImageEdit("product/one", "task/one", 3);
    await api.adoptLocalImageEdit("product/one", "task/one", "artifact-1");
    await api.revertLocalImageEdit("product/one", "task/one", "event/1", "artifact-2");

    expect(fetchMock.mock.calls.slice(2).map(([url, init]) => [url, init?.body])).toEqual([
      ["/api/v3/products/product%2Fone/image-edits/task%2Fone/submit", JSON.stringify({ idempotency_key: "key-1" })],
      ["/api/v3/products/product%2Fone/image-edits/task%2Fone/cancel", JSON.stringify({ expected_revision: 2 })],
      ["/api/v3/products/product%2Fone/image-edits/task%2Fone/retry", JSON.stringify({ expected_revision: 3 })],
      ["/api/v3/products/product%2Fone/image-edits/task%2Fone/adopt", JSON.stringify({ expected_current_artifact_id: "artifact-1" })],
      ["/api/v3/products/product%2Fone/image-edits/task%2Fone/adoptions/event%2F1/revert", JSON.stringify({ expected_current_artifact_id: "artifact-2" })],
    ]);
  });

  it("fails closed on malformed capability, task, and nested asset payloads", async () => {
    const malformedCapability = { ...capability(), operations: ["generate"] };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(okResponse(malformedCapability)));
    await expect(api.getLocalImageEditCapability()).rejects.toMatchObject({ status: 502 });

    const malformedTask = { ...task(), status: "unknown_status" };
    expect(parseLocalImageEditTask(malformedTask)).toBeNull();
    expect(parseLocalImageEditTask({ ...task(), source_asset: { ...asset(), thumbnail_url: null } })).toBeNull();

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(okResponse(malformedTask)));
    await expect(api.getLocalImageEdit("product-1", "task-1")).rejects.toBeInstanceOf(ApiError);
  });
});

function okResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function capability() {
  return {
    provider_name: "openai_images",
    supported: true,
    mode: "masked_edit",
    operations: ["remove", "replace_text", "inpaint"],
    requires_mask: true,
    max_reference_images: 6,
    reason: null,
  };
}

function geometry(): LocalImageEditMaskGeometry {
  return {
    source_width: 1200,
    source_height: 800,
    viewport_width: 600,
    viewport_height: 400,
    viewport_to_source: [2, 0, 0, 2, 0, 0],
    transform_direction: "viewport_to_source",
  };
}

function asset() {
  return {
    id: "asset-1",
    product_id: "product-1",
    media_object_id: "media-1",
    origin_type: "upload",
    display_name: "Source",
    original_filename: "source.png",
    image_type_key: null,
    user_folder_id: null,
    parent_asset_id: null,
    source_image_session_asset_id: null,
    source_library_asset_id: null,
    mime_type: "image/png",
    byte_size: 3,
    width: 1200,
    height: 800,
    verification_status: "verified",
    download_url: "/download/source",
    preview_url: "/preview/source",
    thumbnail_url: "/thumbnail/source",
    created_at: "2026-08-24T00:00:00Z",
    updated_at: "2026-08-24T00:00:00Z",
  };
}

function task() {
  return {
    id: "task-1",
    product_id: "product-1",
    status: "queued",
    revision: 2,
    operation: "inpaint",
    instruction: "remove mark",
    source_text: null,
    replacement_text: null,
    mask_geometry: geometry(),
    source_media_sha256: "sha256-source",
    source_asset: asset(),
    result_asset: null,
    references: [],
    reference_asset_ids: [],
    target_graph_id: "graph-1",
    target_node_id: "node-1",
    target_graph_revision: 4,
    source_artifact_id: "artifact-1",
    source_artifact_asset_id: "asset-1",
    source_artifact_input_digest: "digest-1",
    idempotency_key: "key-1",
    request_hash: "hash-1",
    requested_provider_name: "openai_images",
    requested_local_edit_mode: "masked_edit",
    attempts: 1,
    active_attempt_id: "attempt-1",
    progress_phase: "queued",
    failure_reason: null,
    is_retryable: false,
    is_cancelable: true,
    provider_name: "openai_images",
    provider_model: "gpt-image-1",
    provider_response_id: null,
    provider_status: "queued",
    provider_attempts: [],
    adoption_events: [],
    created_at: "2026-08-24T00:00:00Z",
    updated_at: "2026-08-24T00:00:01Z",
    queued_at: "2026-08-24T00:00:01Z",
    started_at: null,
    finished_at: null,
  };
}
