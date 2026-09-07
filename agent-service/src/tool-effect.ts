/**
 * 工具效果的唯一包装器。
 *
 * mutate：intent checkpoint → 单次执行 → 4xx=failed / 5xx=查询 Go effect reconciler → applied/failed/unknown。
 * recovery_policy 只由 Go 解释；Node 不按 not_applied 重试 mutation。
 * ui_effect：执行且写 checkpoint，不对账、不把 5xx 标成 unknown。
 * approval 只是清单上的效果等级。本函数不根据 effect 自动 requestApproval：
 * 需要中止 Turn 的调用方在 onApplied 里显式 requestApproval。
 *
 * tool_effect_intent schema v1 只保存 tool_name、tool_call_id、idempotency_key、
 * recovery_policy 和有界 request_payload；禁止密钥、图片字节和原始 HTTP 响应。
 */

import type { AgentToolResult } from "@earendil-works/pi-coding-agent";
import {
  CheckpointKind,
  JsonObject,
  JsonValue,
  MAX_CHECKPOINT_PAYLOAD_BYTES,
  ProductFlowError,
} from "./contracts.js";
import type { EffectReconciliation } from "./productflow.js";
import { toolManifestEntry, toolRecoveryPolicy, type ToolName, type ToolRecoveryPolicy } from "./tool-manifest.js";
import { encodeToolResult } from "./tool-result.js";

export const TOOL_EFFECT_INTENT_SCHEMA_VERSION = 1 as const;
export const TOOL_EFFECT_INTENT_KEYS = [
  "schema_version",
  "tool_name",
  "tool_call_id",
  "idempotency_key",
  "recovery_policy",
  "request_payload",
] as const;

const FORBIDDEN_INTENT_KEY =
  /^(authorization|cookie|set-cookie|api[_-]?key|access[_-]?key|secret|password|passwd|token|bearer|raw_response|http_response|response_body|response_headers|raw_http|image_bytes|image_data|file_bytes)$/iu;

export interface EffectRuntime {
  checkpoint(kind: CheckpointKind, payload: JsonObject): Promise<void>;
  reconcileEffect(toolCallID: string): Promise<EffectReconciliation>;
  markEffectUnknown(toolCallID: string, reason?: string): void;
  requestApproval(approval: JsonObject): void;
  idempotencyKey(toolCallID: string): string;
}

export interface EffectOptions {
  mutate: (idempotencyKey: string) => Promise<unknown>;
  unknownReason: string;
  intentPayload?: JsonObject;
  meta?: JsonObject;
  resultMeta?: (result: unknown) => JsonObject;
  afterAppliedCheckpoints?: Array<{ kind: CheckpointKind; payload: JsonObject }>;
  /** Approval effects end the current Pi tool batch after the result is persisted. */
  terminate?: boolean;
  onApplied?: (result: unknown, idempotencyKey: string) => void;
}

export interface ToolEffectIntentV1 extends JsonObject {
  schema_version: typeof TOOL_EFFECT_INTENT_SCHEMA_VERSION;
  tool_name: ToolName;
  tool_call_id: string;
  idempotency_key: string;
  recovery_policy: ToolRecoveryPolicy;
  request_payload: JsonObject;
}

export function buildToolEffectIntent(
  toolName: ToolName,
  toolCallID: string,
  idempotencyKey: string,
  requestPayload: JsonObject = {},
): ToolEffectIntentV1 {
  const entry = toolManifestEntry(toolName);
  if (!entry) throw new Error(`Unknown ProductFlow tool manifest entry: ${toolName}`);
  const intent: ToolEffectIntentV1 = {
    schema_version: TOOL_EFFECT_INTENT_SCHEMA_VERSION,
    tool_name: toolName,
    tool_call_id: toolCallID,
    idempotency_key: idempotencyKey,
    recovery_policy: toolRecoveryPolicy(toolName),
    request_payload: sanitizeRequestPayload(requestPayload),
  };
  const encoded = JSON.stringify(intent);
  if (Buffer.byteLength(encoded, "utf8") > MAX_CHECKPOINT_PAYLOAD_BYTES) {
    throw new Error("tool_effect_intent exceeds the checkpoint payload limit");
  }
  return JSON.parse(encoded) as ToolEffectIntentV1;
}

export async function withEffect(
  runtime: EffectRuntime,
  toolName: ToolName,
  toolCallID: string,
  options: EffectOptions,
): Promise<AgentToolResult<JsonObject>> {
  const entry = toolManifestEntry(toolName);
  if (!entry) throw new Error(`Unknown ProductFlow tool manifest entry: ${toolName}`);
  const idempotencyKey = runtime.idempotencyKey(toolCallID);
  const intent = buildToolEffectIntent(toolName, toolCallID, idempotencyKey, options.intentPayload ?? {});
  await runtime.checkpoint("tool_effect_intent", intent);

  const finishApplied = async (result: unknown, extra: JsonObject = {}): Promise<AgentToolResult<JsonObject>> => {
    for (const checkpoint of options.afterAppliedCheckpoints ?? []) {
      await runtime.checkpoint(checkpoint.kind, checkpoint.payload);
    }
    await runtime.checkpoint("tool_effect_result", {
      tool_name: toolName,
      tool_call_id: toolCallID,
      idempotency_key: idempotencyKey,
      result: "applied",
      ...extra,
    });
    options.onApplied?.(result, idempotencyKey);
    const encoded = encodeToolResult(toolName, result, {
      ...(options.meta ?? {}),
      ...(options.resultMeta?.(result) ?? {}),
      ...(extra.reconciliation_state ? { reconciled: true } : {}),
    });
    return options.terminate ? { ...encoded, terminate: true } : encoded;
  };

  let result: unknown;
  try {
    result = await options.mutate(idempotencyKey);
  } catch (error) {
    if (entry.effect === "ui_effect" || !(error instanceof ProductFlowError) || error.status < 500) {
      await runtime.checkpoint("tool_effect_result", {
        tool_name: toolName,
        tool_call_id: toolCallID,
        idempotency_key: idempotencyKey,
        result: "failed",
      });
      throw error;
    }
    let reconciled: EffectReconciliation;
    try {
      reconciled = await runtime.reconcileEffect(toolCallID);
    } catch (reconcileError) {
      await recordUnknown(
        runtime,
        toolCallID,
        {
          tool_name: toolName,
          tool_call_id: toolCallID,
          idempotency_key: idempotencyKey,
          result: "unknown",
          reconciliation_state: "unavailable",
        },
        options.unknownReason,
      );
      throw reconcileError;
    }
    if (reconciled.effect_result === "applied") {
      if (reconciled.result === undefined) {
        await recordUnknown(
          runtime,
          toolCallID,
          {
            tool_name: toolName,
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "unknown",
            reconciliation_state: "invalid_applied_result",
          },
          options.unknownReason,
        );
        throw new ProductFlowError(502, "reconciliation_invalid", "ProductFlow reconciliation returned an incomplete result");
      }
      return await finishApplied(reconciled.result, { reconciliation_state: reconciled.reconciliation_state });
    }
    if (reconciled.effect_result === "failed") {
      await runtime.checkpoint("tool_effect_result", {
        tool_name: toolName,
        tool_call_id: toolCallID,
        idempotency_key: idempotencyKey,
        result: "failed",
        reconciliation_state: reconciled.reconciliation_state,
      });
      throw error;
    }
    await recordUnknown(
      runtime,
      toolCallID,
      {
        tool_name: toolName,
        tool_call_id: toolCallID,
        idempotency_key: idempotencyKey,
        result: "unknown",
        reconciliation_state: reconciled.reconciliation_state,
      },
      options.unknownReason,
    );
    throw error;
  }
  // The business effect is already applied. Checkpoint/encoding failures must
  // propagate without reclassifying it as failed or reconciling the mutation.
  return await finishApplied(result);
}

function sanitizeRequestPayload(value: JsonObject): JsonObject {
  return walkRequestPayload(value) as JsonObject;
}

function walkRequestPayload(value: JsonValue): JsonValue {
  if (value === null || typeof value === "boolean" || typeof value === "number") return value;
  if (typeof value === "string") {
    if (value.startsWith("data:image/")) {
      throw new Error("tool_effect_intent must not store secrets, image bytes, or raw HTTP responses");
    }
    return value;
  }
  if (Array.isArray(value)) return value.map((item) => walkRequestPayload(item));
  if (!value || typeof value !== "object") {
    throw new Error("tool_effect_intent request_payload must be JSON");
  }
  const out: JsonObject = {};
  for (const [key, child] of Object.entries(value)) {
    if (FORBIDDEN_INTENT_KEY.test(key)) {
      throw new Error("tool_effect_intent must not store secrets, image bytes, or raw HTTP responses");
    }
    out[key] = walkRequestPayload(child);
  }
  return out;
}

async function recordUnknown(
  runtime: EffectRuntime,
  toolCallID: string,
  payload: JsonObject,
  reason: string,
): Promise<void> {
  try {
    await runtime.checkpoint("tool_effect_result", payload);
  } catch {
    // checkpoint 写不进去时，unknown 标记本身仍是权威。
  } finally {
    runtime.markEffectUnknown(toolCallID, reason);
  }
}
