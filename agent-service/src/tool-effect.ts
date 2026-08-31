/**
 * 工具效果的唯一包装器。
 *
 * mutate：intent checkpoint → 执行 → 4xx=failed / 5xx=对账 → applied/not_applied/conflict/unknown。
 * ui_effect：执行且写 checkpoint，不对账、不把 5xx 标成 unknown。
 * approval 只是清单上的效果等级。本函数不根据 effect 自动 requestApproval：
 * 需要中止 Turn 的调用方在 onApplied 里显式 requestApproval。
 */

import type { AgentToolResult } from "@earendil-works/pi-coding-agent";
import {
  CheckpointKind,
  JsonObject,
  ProductFlowError,
} from "./contracts.js";
import { ReconcileResult } from "./productflow.js";
import { toolManifestEntry, toolRecoveryPolicy, type ToolName } from "./tool-manifest.js";
import { encodeToolResult } from "./tool-result.js";

export interface EffectRuntime {
  checkpoint(kind: CheckpointKind, payload: JsonObject): Promise<void>;
  markEffectUnknown(toolCallID: string, reason?: string): void;
  requestApproval(approval: JsonObject): void;
  idempotencyKey(toolCallID: string): string;
}

export interface EffectOptions {
  mutate: (idempotencyKey: string) => Promise<unknown>;
  reconcile?: (idempotencyKey: string) => Promise<ReconcileResult>;
  unknownReason: string;
  intentPayload?: JsonObject;
  meta?: JsonObject;
  resultMeta?: (result: unknown) => JsonObject;
  afterAppliedCheckpoints?: Array<{ kind: CheckpointKind; payload: JsonObject }>;
  onApplied?: (result: unknown, idempotencyKey: string) => void;
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
  const intent: JsonObject = {
    tool_name: toolName,
    tool_call_id: toolCallID,
    idempotency_key: idempotencyKey,
    recovery_policy: toolRecoveryPolicy(toolName),
    request: options.intentPayload ?? {},
    ...(options.intentPayload ?? {}),
  };
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
    return encodeToolResult(toolName, result, {
      ...(options.meta ?? {}),
      ...(options.resultMeta?.(result) ?? {}),
      ...(extra.reconciliation_state ? { reconciled: true } : {}),
    });
  };

  try {
    const result = await options.mutate(idempotencyKey);
    return await finishApplied(result);
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
    const reconcile = options.reconcile;
    if (!reconcile) {
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
      throw error;
    }
    let reconciled: ReconcileResult;
    try {
      reconciled = await reconcile(idempotencyKey);
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
    if (reconciled.state === "applied") {
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
      return await finishApplied(reconciled.result, { reconciliation_state: "applied" });
    }
    if (reconciled.state === "not_applied" && toolRecoveryPolicy(toolName) === "reconcile_then_retry") {
      try {
        return await finishApplied(await options.mutate(idempotencyKey), {
          reconciliation_state: "not_applied_then_retried",
        });
      } catch (retryError) {
        if (!(retryError instanceof ProductFlowError) || retryError.status < 500) {
          await runtime.checkpoint("tool_effect_result", {
            tool_name: toolName,
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "failed",
            reconciliation_state: "not_applied",
          });
          throw retryError;
        }
        let retriedReconciliation: ReconcileResult;
        try {
          retriedReconciliation = await options.reconcile!(idempotencyKey);
        } catch {
          await recordUnknown(runtime, toolCallID, {
            tool_name: toolName,
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "unknown",
            reconciliation_state: "unavailable",
          }, options.unknownReason);
          throw retryError;
        }
        if (retriedReconciliation.state === "applied" && retriedReconciliation.result !== undefined) {
          return await finishApplied(retriedReconciliation.result, { reconciliation_state: "applied" });
        }
        if (retriedReconciliation.state === "not_applied" || retriedReconciliation.state === "conflict") {
          await runtime.checkpoint("tool_effect_result", {
            tool_name: toolName,
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "failed",
            reconciliation_state: retriedReconciliation.state,
          });
          throw retryError;
        }
        await recordUnknown(runtime, toolCallID, {
          tool_name: toolName,
          tool_call_id: toolCallID,
          idempotency_key: idempotencyKey,
          result: "unknown",
          reconciliation_state: "unknown",
        }, options.unknownReason);
        throw retryError;
      }
    }
    if (reconciled.state === "not_applied" || reconciled.state === "conflict") {
      await runtime.checkpoint("tool_effect_result", {
        tool_name: toolName,
        tool_call_id: toolCallID,
        idempotency_key: idempotencyKey,
        result: "failed",
        reconciliation_state: reconciled.state,
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
        reconciliation_state: isReconcileState(reconciled.state) ? reconciled.state : "invalid",
      },
      options.unknownReason,
    );
    throw error;
  }
}

function isReconcileState(value: string): value is "applied" | "not_applied" | "conflict" | "unknown" {
  return value === "applied" || value === "not_applied" || value === "conflict" || value === "unknown";
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
