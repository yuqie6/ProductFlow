import { afterEach, describe, expect, it, vi } from "vitest";
import {
  isKnownPiAssistantEventType,
  JOURNAL_EVENT_MAX_PAYLOAD_BYTES,
  JournalStreamBuffer,
  JOURNAL_STREAM_EVENT_KINDS,
  boundedUsage,
  durableAssistantUsage,
  normalizeAssistantMessageEvent,
  PI_ASSISTANT_EVENT_TYPES,
  utf8Prefix,
} from "./pi-chunks.js";

afterEach(() => {
  vi.useRealTimers();
});

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
      action: "text.chunk",
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
    expect(
      normalizeAssistantMessageEvent({
        type: "done",
        reason: "stop",
        message: { usage: { input: 3, output: 8 } },
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

  it("records stream chunks as journal event kinds", () => {
    expect(JOURNAL_STREAM_EVENT_KINDS).toEqual(["text.chunk", "thinking.chunk", "assistant/message"]);
  });

  it("splits a large multibyte delta into bounded chunks without losing text", () => {
    const chunks: string[] = [];
    const buffer = new JournalStreamBuffer((chunk) => chunks.push(chunk.delta), {
      maxChunkBytes: 12,
      flushMS: 1_000,
    });
    const text = "商品图像".repeat(20);

    buffer.append({
      kind: "text.chunk",
      delta: text,
      step_id: "step-1",
      attempt_id: "attempt-1",
      content_index: 0,
    });
    buffer.flush();

    expect(chunks.join("")).toBe(text);
    expect(chunks.length).toBeGreaterThan(1);
    expect(chunks.every((chunk) => Buffer.byteLength(chunk, "utf8") <= 12)).toBe(true);
    expect(utf8Prefix("商品A", 7)).toBe("商品A");
    expect(utf8Prefix("商品A", 6)).toBe("商品");
  });

  it("coalesces many tiny provider deltas instead of consuming one journal sequence each", () => {
    const chunks: string[] = [];
    const buffer = new JournalStreamBuffer((chunk) => chunks.push(chunk.delta), {
      maxChunkBytes: 128,
      flushMS: 1_000,
    });

    for (let index = 0; index < 10_050; index += 1) {
      buffer.append({
        kind: "text.chunk",
        delta: "x",
        step_id: "step-1",
        attempt_id: "attempt-1",
        content_index: 0,
      });
    }
    buffer.flush();

    expect(chunks.join("")).toBe("x".repeat(10_050));
    expect(chunks).toHaveLength(Math.ceil(10_050 / 128));
  });

  it("keeps escape-heavy chunk payloads below the actual JSON byte limit", () => {
    const chunks: Array<{ delta: string; step_id: string; attempt_id: string; content_index: number }> = [];
    const buffer = new JournalStreamBuffer((chunk) => chunks.push(chunk));
    const text = "\u0000\"\\\n".repeat(20_000);

    buffer.append({
      kind: "text.chunk",
      delta: text,
      step_id: "step-escape",
      attempt_id: "attempt-escape",
      content_index: 0,
    });
    buffer.flush();

    expect(chunks.map((chunk) => chunk.delta).join("")).toBe(text);
    expect(chunks.every((chunk) => Buffer.byteLength(JSON.stringify(chunk), "utf8") <= JOURNAL_EVENT_MAX_PAYLOAD_BYTES)).toBe(true);
  });

  it("time-bounds a live chunk while merging deltas received in the same window", async () => {
    const chunks: string[] = [];
    const buffer = new JournalStreamBuffer((chunk) => chunks.push(chunk.delta), { flushMS: 5 });
    const base = { kind: "text.chunk" as const, step_id: "step-1", attempt_id: "attempt-1", content_index: 0 };

    buffer.append({ ...base, delta: "你" });
    buffer.append({ ...base, delta: "好" });
    await new Promise<void>((resolve) => setTimeout(resolve, 20));

    expect(chunks).toEqual(["你好"]);
  });

  it("keeps time-bounded chunks visible throughout a stream longer than the former soft budget", () => {
    vi.useFakeTimers();
    const chunks: string[] = [];
    const buffer = new JournalStreamBuffer((chunk) => chunks.push(chunk.delta), {
      flushMS: 25,
      maxChunkBytes: 128,
      maxEvents: 1_000,
    });
    const base = { kind: "text.chunk" as const, step_id: "step-1", attempt_id: "attempt-1", content_index: 0 };
    const windows = 800;

    for (let index = 0; index < windows; index += 1) {
      buffer.append({ ...base, delta: "x" });
      vi.advanceTimersByTime(25);
      expect(chunks).toHaveLength(index + 1);
    }

    expect(vi.getTimerCount()).toBe(0);
    expect(chunks).toHaveLength(windows);
    expect(chunks.join("")).toBe("x".repeat(windows));
  });

  it("enforces the hard event budget for size-triggered flushes", () => {
    const chunks: string[] = [];
    const errors: Error[] = [];
    const buffer = new JournalStreamBuffer((chunk) => chunks.push(chunk.delta), {
      flushMS: 1_000,
      maxChunkBytes: 1,
      maxEvents: 3,
      onError: (error) => errors.push(error),
    });
    const base = { kind: "text.chunk" as const, step_id: "step-1", attempt_id: "attempt-1", content_index: 0 };

    for (let index = 0; index < 5; index += 1) {
      buffer.append({ ...base, delta: "x" });
    }

    expect(chunks).toEqual(["x", "x", "x"]);
    expect(chunks).toHaveLength(3);
    expect(errors).toHaveLength(1);
    expect(errors[0]?.message).toContain("journal event budget of 3");
  });
});

describe("durable usage", () => {
  it("drops all-zero provider usage instead of persisting fake zeros", () => {
    expect(boundedUsage({ input: 0, output: 0, totalTokens: 0 })).toBeUndefined();
    expect(durableAssistantUsage({ input: 0, output: 0 })).toBeUndefined();
  });

  it("tags local estimates separately from provider usage", () => {
    expect(durableAssistantUsage({ input: 2, output: 3, totalTokens: 5 }, "estimated")).toEqual({
      usage: { input: 2, output: 3, total_tokens: 5 },
      usage_source: "estimated",
    });
    expect(durableAssistantUsage({ input: 2, output: 3, totalTokens: 5 }, "provider")).toEqual({
      usage: { input: 2, output: 3, total_tokens: 5 },
      usage_source: "provider",
    });
  });
});
