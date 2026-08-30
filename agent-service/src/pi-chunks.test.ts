import { describe, expect, it } from "vitest";
import {
  isDurableTurnEventKind,
  isKnownPiAssistantEventType,
  LIVE_ONLY_EVENT_KINDS,
  normalizeAssistantMessageEvent,
  PI_ASSISTANT_EVENT_TYPES,
} from "./pi-chunks.js";

describe("Pi assistantMessageEvent inventory", () => {
  it("lists the Pi 0.83 assistant stream types", () => {
    expect(PI_ASSISTANT_EVENT_TYPES).toEqual([
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
    ]);
  });

  it("maps text and thinking into ProductFlow UI chunks", () => {
    expect(normalizeAssistantMessageEvent({ type: "text_delta", delta: "你好", contentIndex: 1 })).toEqual({
      action: "text.delta",
      delta: "你好",
      contentIndex: 1,
    });
    const thinking = normalizeAssistantMessageEvent({
      type: "thinking_delta",
      delta: "先看约束",
      contentIndex: 0,
    });
    expect(thinking).toMatchObject({ action: "thinking", event: { type: "thinking_delta", delta: "先看约束" } });
  });

  it("emits a bounded finish from done/error and drops raw toolcall args", () => {
    expect(
      normalizeAssistantMessageEvent({
        type: "done",
        reason: "stop",
        message: { usage: { input: 3, output: 8, totalTokens: 11 } },
      }),
    ).toEqual({
      action: "finish",
      reason: "stop",
      usage: { input: 3, output: 8, total_tokens: 11 },
    });
    expect(normalizeAssistantMessageEvent({ type: "toolcall_delta", delta: '{"name":' })).toEqual({
      action: "ignore",
      type: "toolcall_delta",
    });
    expect(normalizeAssistantMessageEvent({ type: "text_start", contentIndex: 0 })).toEqual({
      action: "ignore",
      type: "text_start",
    });
  });

  it("drops unknown types instead of forwarding them into the UI journal", () => {
    expect(isKnownPiAssistantEventType("response.output_text.delta")).toBe(false);
    expect(normalizeAssistantMessageEvent({ type: "response.output_text.delta" })).toEqual({
      action: "unknown",
      type: "response.output_text.delta",
    });
    expect(normalizeAssistantMessageEvent({ type: "" })).toEqual({ action: "unknown", type: "" });
  });

  it("keeps high-frequency chunks out of the durable PostgreSQL journal", () => {
    expect(LIVE_ONLY_EVENT_KINDS).toEqual(["text.delta", "thinking.delta", "assistant.finish"]);
    expect(isDurableTurnEventKind("text.delta")).toBe(false);
    expect(isDurableTurnEventKind("thinking.delta")).toBe(false);
    expect(isDurableTurnEventKind("assistant.finish")).toBe(false);
    expect(isDurableTurnEventKind("tool.step")).toBe(true);
    expect(isDurableTurnEventKind("turn.succeeded")).toBe(true);
    expect(isDurableTurnEventKind("question.required")).toBe(true);
  });
});
