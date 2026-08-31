/**
 * Pi 0.83 `assistantMessageEvent` → ProductFlow UI chunk 归一。
 *
 * 对照 `@earendil-works/pi-ai` AssistantMessageEvent。
 * Pi 原始流在这里归一，随后由 RunRuntime 写入 append-only journal。
 * toolcall_* 的原始 arguments 不进日志；工具卡使用有界的 call/result meta。
 */

import type { ThinkingAssistantEvent } from "./thinking-projection.js";

/** 高频内容也属于 journal；仅在浏览器装配层按 animation frame 发布。 */
export const JOURNAL_STREAM_EVENT_KINDS = ["text.chunk", "thinking.chunk", "assistant/message"] as const;

export const PI_ASSISTANT_EVENT_TYPES = [
  "start",
  "text_start",
  "text_delta",
  "text_end",
  "thinking_start",
  "thinking_delta",
  "thinking_end",
  "toolcall_start",
  "toolcall_delta",
  "toolcall_end",
  "done",
  "error",
] as const;

export type PiAssistantEventType = (typeof PI_ASSISTANT_EVENT_TYPES)[number];

const PI_ASSISTANT_EVENT_TYPE_SET = new Set<string>(PI_ASSISTANT_EVENT_TYPES);

export interface PiAssistantMessageEvent {
  type: string;
  contentIndex?: number;
  delta?: string;
  content?: string;
  reason?: string;
  message?: PiAssistantMessage;
  error?: PiAssistantMessage;
  partial?: unknown;
}

export interface PiAssistantMessage {
  usage?: PiUsage;
  stopReason?: string;
  errorMessage?: string;
}

export interface PiUsage {
  input?: number;
  output?: number;
  totalTokens?: number;
}

export interface AssistantFinishPayload {
  reason: string;
  attempt_id: string;
  model_request_id?: string;
  usage?: {
    input: number;
    output: number;
    total_tokens: number;
  };
}

export type NormalizedAssistantEvent =
  | { action: "text.chunk"; delta: string; contentIndex: number }
  | { action: "thinking"; event: ThinkingAssistantEvent }
  | { action: "finish"; reason: string; usage: AssistantFinishPayload["usage"] }
  | { action: "ignore"; type: string }
  | { action: "unknown"; type: string };

const IGNORED_ASSISTANT_TYPES = new Set<string>([
  "start",
  "text_start",
  "text_end",
  "toolcall_start",
  "toolcall_delta",
  "toolcall_end",
]);

export function isKnownPiAssistantEventType(type: string): type is PiAssistantEventType {
  return PI_ASSISTANT_EVENT_TYPE_SET.has(type);
}

/**
 * 把一条 Pi assistantMessageEvent 收成 ProductFlow UI 动作。
 * 未登记的 type 返回 `unknown`，调用方必须记日志且不得写入 UI journal。
 */
export function normalizeAssistantMessageEvent(event: PiAssistantMessageEvent): NormalizedAssistantEvent {
  const type = typeof event.type === "string" ? event.type : "";
  if (!type) return { action: "unknown", type: "" };
  if (type === "text_delta") {
    return {
      action: "text.chunk",
      delta: typeof event.delta === "string" ? event.delta : "",
      contentIndex: numberOrZero(event.contentIndex),
    };
  }
  if (type === "thinking_start" || type === "thinking_delta" || type === "thinking_end") {
    return {
      action: "thinking",
      event: event as ThinkingAssistantEvent,
    };
  }
  if (type === "done") {
    return {
      action: "finish",
      reason: typeof event.reason === "string" && event.reason ? event.reason : "stop",
      usage: boundedUsage(event.message?.usage),
    };
  }
  if (type === "error") {
    const reason = event.error?.stopReason === "aborted" ? "aborted" : "error";
    return {
      action: "finish",
      reason,
      usage: boundedUsage(event.error?.usage),
    };
  }
  if (IGNORED_ASSISTANT_TYPES.has(type) || isKnownPiAssistantEventType(type)) {
    return { action: "ignore", type };
  }
  return { action: "unknown", type };
}

export function reportUnknownPiAssistantEvent(type: string): void {
  process.stderr.write(`[productflow-pi] dropping unknown assistantMessageEvent type=${type || "<empty>"}\n`);
}

export function boundedUsage(usage: PiUsage | undefined): AssistantFinishPayload["usage"] {
  if (!usage) return undefined;
  const input = integerOrZero(usage.input);
  const output = integerOrZero(usage.output);
  const total = integerOrZero(usage.totalTokens) || input + output;
  if (input === 0 && output === 0 && total === 0) return undefined;
  return { input, output, total_tokens: total };
}

function numberOrZero(value: unknown): number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : 0;
}

function integerOrZero(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? Math.floor(value) : 0;
}
