import { describe, expect, it } from "vitest";
import { byteLength, MAX_THINKING_TEXT_BYTES } from "./contracts.js";
import {
  appendBoundedThinking,
  applyThinkingEvent,
  createThinkingProjectionState,
  shouldSkipThinkingProjection,
  type ThinkingAssistantEvent,
} from "./thinking-projection.js";

function event(
  type: ThinkingAssistantEvent["type"],
  contentIndex: number,
  extras: Partial<ThinkingAssistantEvent> = {},
): ThinkingAssistantEvent {
  return { type, contentIndex, ...extras };
}

describe("thinking projection", () => {
  it("emits an empty thinking.delta on start so the UI can show a thinking row", () => {
    const { emit, state } = applyThinkingEvent(
      createThinkingProjectionState(),
      event("thinking_start", 0, {
        partial: { content: [{ type: "thinking", thinking: "" }] },
      }),
    );
    expect(emit).toEqual({ delta: "", content_index: 0, truncated: false });
    expect(state.text).toBe("");
  });

  it("accumulates thinking deltas without treating them as output text", () => {
    let current = createThinkingProjectionState();
    current = applyThinkingEvent(current, event("thinking_start", 0)).state;
    const first = applyThinkingEvent(current, event("thinking_delta", 0, { delta: "先看约束" }));
    const second = applyThinkingEvent(first.state, event("thinking_delta", 0, { delta: "再给结论" }));
    expect(first.emit?.delta).toBe("先看约束");
    expect(second.emit?.delta).toBe("再给结论");
    expect(second.state.text).toBe("先看约束再给结论");
  });

  it("does not re-emit thinking_end content after streamed deltas", () => {
    let current = createThinkingProjectionState();
    current = applyThinkingEvent(current, event("thinking_start", 0)).state;
    current = applyThinkingEvent(current, event("thinking_delta", 0, { delta: "内部推理" })).state;
    const ended = applyThinkingEvent(
      current,
      event("thinking_end", 0, {
        content: "内部推理",
        partial: { content: [{ type: "thinking", thinking: "内部推理" }] },
      }),
    );
    expect(ended.emit).toBeNull();
    expect(ended.state.text).toBe("内部推理");
  });

  it("does not project redacted thinking", () => {
    const started = applyThinkingEvent(
      createThinkingProjectionState(),
      event("thinking_start", 0, {
        partial: { content: [{ type: "thinking", thinking: "", redacted: true, thinkingSignature: "sig" }] },
      }),
    );
    const delta = applyThinkingEvent(
      started.state,
      event("thinking_delta", 0, {
        delta: "secret",
        partial: { content: [{ type: "thinking", thinking: "secret", redacted: true, thinkingSignature: "sig" }] },
      }),
    );
    expect(started.emit).toBeNull();
    expect(delta.emit).toBeNull();
    expect(delta.state.text).toBe("");
  });

  it("does not project signature-only thinking_start", () => {
    const started = applyThinkingEvent(
      createThinkingProjectionState(),
      event("thinking_start", 0, {
        partial: { content: [{ type: "thinking", thinking: "", thinkingSignature: "opaque" }] },
      }),
    );
    expect(started.emit).toBeNull();
    expect(started.state.redactedIndexes.has(0)).toBe(true);
  });

  it("backfills thinking_end when no deltas were streamed", () => {
    const ended = applyThinkingEvent(
      createThinkingProjectionState(),
      event("thinking_end", 0, {
        content: "只在结束时给出思考",
        partial: { content: [{ type: "thinking", thinking: "只在结束时给出思考" }] },
      }),
    );
    expect(ended.emit?.delta).toBe("只在结束时给出思考");
    expect(ended.state.text).toBe("只在结束时给出思考");
  });

  it("does not project signature-only thinking_end", () => {
    const ended = applyThinkingEvent(
      createThinkingProjectionState(),
      event("thinking_end", 0, {
        content: "",
        partial: { content: [{ type: "thinking", thinking: "", thinkingSignature: "opaque" }] },
      }),
    );
    expect(ended.emit).toBeNull();
    expect(shouldSkipThinkingProjection({ type: "thinking", thinking: "", thinkingSignature: "opaque" })).toBe(true);
  });

  it("truncates accumulated thinking at the bounded byte limit", () => {
    const prefix = "x".repeat(MAX_THINKING_TEXT_BYTES - 8);
    const { text, emitted, truncated } = appendBoundedThinking(prefix, "yyyyyyyyyyyy");
    expect(truncated).toBe(true);
    expect(byteLength(text)).toBe(MAX_THINKING_TEXT_BYTES);
    expect(emitted.length).toBeGreaterThan(0);
    expect(byteLength(prefix + emitted)).toBe(MAX_THINKING_TEXT_BYTES);

    let current = createThinkingProjectionState();
    current = { ...current, text: prefix };
    const result = applyThinkingEvent(current, event("thinking_delta", 1, { delta: "yyyyyyyyyyyy" }));
    expect(result.emit?.truncated).toBe(true);
    expect(byteLength(result.state.text)).toBe(MAX_THINKING_TEXT_BYTES);
    const skipped = applyThinkingEvent(result.state, event("thinking_delta", 1, { delta: "more" }));
    expect(skipped.emit).toBeNull();
  });
});
