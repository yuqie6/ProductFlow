import {
  ProductFlowError,
  type JsonObject,
  type TurnArtifact,
  type TurnEvent,
} from "./contracts.js";
import type { AgentEventInput } from "./productflow.js";

export function toAgentEventInput(event: TurnEvent): AgentEventInput {
  return {
    sequence: event.sequence,
    schema_version: event.schema_version,
    run_id: event.run_id,
    turn_id: event.turn_id,
    kind: event.kind,
    ...(event.ignorable ? { ignorable: true } : {}),
    payload: event.payload,
    created_at: event.created_at,
  };
}

export function eventReceiptMatches(
  receipt: { sequence: number; kind: string; execution_id: string; projection_id: string; schema_version: number; ignorable?: boolean },
  event: TurnEvent,
  executionID: string,
  projectionID: string,
): boolean {
  return receipt.sequence === event.sequence
    && receipt.kind === event.kind
    && receipt.execution_id === executionID
    && receipt.projection_id === projectionID
    && receipt.schema_version === event.schema_version
    && Boolean(receipt.ignorable) === Boolean(event.ignorable);
}

export function hasUnresolvedApproval(events: readonly TurnEvent[]): boolean {
  let pending = false;
  for (const event of events) {
    if (event.kind === "approval/requested") pending = true;
    if (event.kind === "approval/resolved") pending = false;
  }
  return pending;
}

export function artifactFromPendingApproval(events: readonly TurnEvent[]): TurnArtifact | undefined {
  const requested = [...events].reverse().find((event) => event.kind === "approval/requested");
  const value = requested?.payload.artifact;
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  if (
    typeof value.name !== "string" ||
    typeof value.step_id !== "string" ||
    !value.value ||
    typeof value.value !== "object" ||
    Array.isArray(value.value)
  ) return undefined;
  return { name: value.name, step_id: value.step_id, value: value.value as JsonObject };
}

export function isRetryableJournalBatchError(error: unknown): boolean {
  return error instanceof ProductFlowError && error.status >= 500;
}
