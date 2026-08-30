/**
 * 把 Pi 已规范化的 thinking_* 事件收成有界 web projection。
 * 思考不得写入 output / output_text。redacted 与仅 signature 的块不投影。
 */

import { byteLength, MAX_THINKING_TEXT_BYTES } from "./contracts.js";

export interface ThinkingContentBlock {
  type?: string;
  thinking?: string;
  thinkingSignature?: string;
  redacted?: boolean;
}

export interface ThinkingAssistantEvent {
  type: "thinking_start" | "thinking_delta" | "thinking_end";
  contentIndex: number;
  delta?: string;
  content?: string;
  partial?: { content?: ThinkingContentBlock[] };
}

export interface ThinkingProjectionState {
  text: string;
  truncated: boolean;
  redactedIndexes: Set<number>;
}

export interface ThinkingDeltaEmit {
  delta: string;
  content_index: number;
  truncated: boolean;
}

export function createThinkingProjectionState(): ThinkingProjectionState {
  return { text: "", truncated: false, redactedIndexes: new Set() };
}

export function thinkingBlockAt(
  event: ThinkingAssistantEvent,
): ThinkingContentBlock | undefined {
  const block = event.partial?.content?.[event.contentIndex];
  if (!block || block.type !== "thinking") return undefined;
  return block;
}

export function shouldSkipThinkingProjection(block: ThinkingContentBlock | undefined): boolean {
  if (!block) return false;
  if (block.redacted === true) return true;
  const thinking = block.thinking ?? "";
  return thinking.length === 0 && Boolean(block.thinkingSignature);
}

export function appendBoundedThinking(
  current: string,
  delta: string,
  maxBytes = MAX_THINKING_TEXT_BYTES,
): { text: string; emitted: string; truncated: boolean } {
  if (byteLength(current) >= maxBytes) {
    return { text: current, emitted: "", truncated: true };
  }
  if (!delta) {
    return { text: current, emitted: "", truncated: false };
  }
  const remaining = maxBytes - byteLength(current);
  if (byteLength(delta) <= remaining) {
    return { text: current + delta, emitted: delta, truncated: false };
  }
  let emitted = delta;
  while (emitted.length > 0 && byteLength(emitted) > remaining) {
    emitted = emitted.slice(0, -1);
  }
  return { text: current + emitted, emitted, truncated: true };
}

/**
 * 把一条 Pi thinking 事件变成最多一条 `thinking.delta`。
 * thinking_end 不重放全文，避免与已经流过的 delta 重复。
 */
export function applyThinkingEvent(
  state: ThinkingProjectionState,
  event: ThinkingAssistantEvent,
): { state: ThinkingProjectionState; emit: ThinkingDeltaEmit | null } {
  const contentIndex = event.contentIndex;
  const block = thinkingBlockAt(event);
  const redactedIndexes = new Set(state.redactedIndexes);

  if (block?.redacted === true) {
    redactedIndexes.add(contentIndex);
    return { state: { ...state, redactedIndexes }, emit: null };
  }
  if (redactedIndexes.has(contentIndex)) {
    return { state: { ...state, redactedIndexes }, emit: null };
  }
  if (event.type === "thinking_end") {
    if (shouldSkipThinkingProjection(block)) {
      redactedIndexes.add(contentIndex);
      return { state: { ...state, redactedIndexes }, emit: null };
    }
    const full = (block?.thinking || event.content || "").trim();
    if (!state.text && full) {
      const result = appendBoundedThinking("", full);
      return {
        state: {
          text: result.text,
          truncated: result.truncated || state.truncated,
          redactedIndexes,
        },
        emit: {
          delta: result.emitted,
          content_index: contentIndex,
          truncated: result.truncated,
        },
      };
    }
    return { state: { ...state, redactedIndexes }, emit: null };
  }
  if (state.truncated) {
    return { state: { ...state, redactedIndexes }, emit: null };
  }
  if (event.type === "thinking_start") {
    if (shouldSkipThinkingProjection(block)) {
      redactedIndexes.add(contentIndex);
      return { state: { ...state, redactedIndexes }, emit: null };
    }
    return {
      state: { ...state, redactedIndexes },
      emit: { delta: "", content_index: contentIndex, truncated: false },
    };
  }

  const result = appendBoundedThinking(state.text, event.delta ?? "");
  if (!result.emitted && !result.truncated) {
    return { state: { ...state, redactedIndexes }, emit: null };
  }
  return {
    state: {
      text: result.text,
      truncated: result.truncated || state.truncated,
      redactedIndexes,
    },
    emit: {
      delta: result.emitted,
      content_index: contentIndex,
      truncated: result.truncated,
    },
  };
}
