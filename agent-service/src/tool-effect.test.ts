import { describe, expect, it, vi } from "vitest";
import { MAX_CHECKPOINT_PAYLOAD_BYTES, ProductFlowError } from "./contracts.js";
import {
  TOOL_EFFECT_INTENT_KEYS,
  TOOL_EFFECT_INTENT_SCHEMA_VERSION,
  buildToolEffectIntent,
  withEffect,
  type EffectRuntime,
} from "./tool-effect.js";

describe("tool_effect_intent schema v1", () => {
  it("freezes identity fields and a bounded request_payload without flattening", () => {
    const intent = buildToolEffectIntent("create_product_workspace_v1", "call-1", "key-1", {
      name: "春季新品",
    });
    expect(Object.keys(intent).sort()).toEqual([...TOOL_EFFECT_INTENT_KEYS].sort());
    expect(intent).toEqual({
      schema_version: TOOL_EFFECT_INTENT_SCHEMA_VERSION,
      tool_name: "create_product_workspace_v1",
      tool_call_id: "call-1",
      idempotency_key: "key-1",
      recovery_policy: "reconcile_then_retry",
      request_payload: { name: "春季新品" },
    });
    expect(intent).not.toHaveProperty("request");
    expect(intent).not.toHaveProperty("name");
    expect(Buffer.byteLength(JSON.stringify(intent), "utf8")).toBeLessThanOrEqual(MAX_CHECKPOINT_PAYLOAD_BYTES);
  });

  it("rejects secrets, image bytes, and raw HTTP responses", () => {
    expect(() => buildToolEffectIntent("create_product_workspace_v1", "call-1", "key-1", { api_key: "secret" })).toThrow(
      /secrets, image bytes, or raw HTTP responses/,
    );
    expect(() =>
      buildToolEffectIntent("create_product_workspace_v1", "call-1", "key-1", {
        preview: "data:image/png;base64,aaaa",
      }),
    ).toThrow(/secrets, image bytes, or raw HTTP responses/);
    expect(() =>
      buildToolEffectIntent("create_product_workspace_v1", "call-1", "key-1", {
        nested: { raw_response: { status: 200, body: "{}" } },
      }),
    ).toThrow(/secrets, image bytes, or raw HTTP responses/);
  });

  it("rejects payloads that exceed the checkpoint byte limit", () => {
    expect(() =>
      buildToolEffectIntent("apply_graph_change_set_v1", "call-1", "key-1", {
        change_set: { summary: "x".repeat(MAX_CHECKPOINT_PAYLOAD_BYTES) },
      }),
    ).toThrow(/checkpoint payload limit/);
  });
});

describe("applied effect persistence boundary", () => {
  it.each(["checkpoint", "encoding"])("does not reconcile or rewrite an applied effect after %s failure", async (failure) => {
    const error = new ProductFlowError(503, "persistence_unavailable", "result unavailable");
    const checkpoint = vi.fn<EffectRuntime["checkpoint"]>(async (kind) => {
      if (failure === "checkpoint" && kind === "tool_effect_result") throw error;
    });
    const runtime: EffectRuntime = {
      checkpoint,
      reconcileEffect: vi.fn(),
      markEffectUnknown: vi.fn(),
      requestApproval: vi.fn(),
      idempotencyKey: () => "key-1",
    };
    const mutate = vi.fn(async () => ({ product_id: "product-1" }));
    await expect(withEffect(runtime, "create_product_workspace_v1", "call-1", {
      mutate,
      unknownReason: "business effect unknown",
      resultMeta: () => {
        if (failure === "encoding") throw error;
        return {};
      },
    })).rejects.toBe(error);
    expect(mutate).toHaveBeenCalledTimes(1);
    expect(checkpoint.mock.calls.map(([, payload]) => payload.result)).toEqual([undefined, "applied"]);
    expect(runtime.reconcileEffect).not.toHaveBeenCalled();
    expect(runtime.markEffectUnknown).not.toHaveBeenCalled();
  });
});
