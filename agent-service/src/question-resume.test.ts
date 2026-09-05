import { describe, expect, it } from "vitest";
import type { TurnAnswer, TurnEvent } from "./contracts.js";
import {
  buildAskUserToolResultMessage,
  compactAskUserSummary,
  injectAskUserToolResult,
  lastMessageIsAskUserResult,
  pendingAskUserCall,
  QUESTION_WAIT_EXPIRED_MESSAGE,
  storedAnswerFromEvents,
} from "./question-resume.js";
import { RuntimeError } from "./store.js";

const assistantAskUser = {
  role: "assistant",
  content: [{ type: "toolCall", id: "call-ask-1", name: "ask_user", arguments: {} }],
};

describe("question-resume helpers", () => {
  it("reads the latest stored question answer from events", () => {
    expect(
      storedAnswerFromEvents([
        { sequence: 1, kind: "question/requested", payload: { id: "question-1" }, created_at: "", run_id: "", turn_id: "", schema_version: 1 },
        {
          sequence: 2,
          kind: "question/answered",
          payload: { question_id: "question-1", answer: { text: "筋膜枪" } },
          created_at: "",
          run_id: "",
          turn_id: "",
          schema_version: 1,
        },
      ]),
    ).toEqual({ questionID: "question-1", answer: { text: "筋膜枪" } });
  });

  it("looks up the current question by default and never resumes it with an older answer", () => {
    const event = (kind: string, payload: TurnEvent["payload"]): TurnEvent => ({
      schema_version: 1, run_id: "run", turn_id: "turn", sequence: 1, created_at: "", kind, payload,
    });
    const events = [
      event("question/requested", { id: "q1" }),
      event("question/answered", { question_id: "q1", answer: { text: "first" } }),
      event("question/requested", { id: "q2" }),
    ];
    expect(storedAnswerFromEvents(events)).toBeNull();
    expect(storedAnswerFromEvents(events, "missing")).toBeNull();
    expect(storedAnswerFromEvents(events, "q1")).toEqual({ questionID: "q1", answer: { text: "first" } });
    events.push(event("question/answered", { question_id: "q2", answer: { option: 1 } }));
    events.push(event("question/answered", { question_id: "q1", answer: { text: "first" } }));
    expect(storedAnswerFromEvents(events)).toEqual({ questionID: "q2", answer: { option: 1 } });
  });

  it("finds an unanswered ask_user tool call and ignores completed ones", () => {
    expect(pendingAskUserCall([assistantAskUser])).toEqual({ id: "call-ask-1" });
    expect(
      pendingAskUserCall([
        assistantAskUser,
        { role: "toolResult", toolCallId: "call-ask-1", toolName: "ask_user" },
      ]),
    ).toBeNull();
    expect(
      lastMessageIsAskUserResult([
        assistantAskUser,
        { role: "toolResult", toolCallId: "call-ask-1", toolName: "ask_user" },
      ]),
    ).toBe(true);
  });

  it("injects ask_user toolResult without a user prompt", async () => {
    const persisted: unknown[] = [];
    const messages = [{ ...assistantAskUser }];
    const continueCalls: number[] = [];
    const promptCalls: string[] = [];
    const session = {
      agent: {
        state: { messages },
        continue: async () => {
          continueCalls.push(1);
        },
      },
      sessionManager: {
        appendMessage: (message: unknown) => {
          persisted.push(message);
        },
      },
      prompt: async (text: string) => {
        promptCalls.push(text);
      },
    };
    const answer: TurnAnswer = { text: "筋膜枪" };
    await injectAskUserToolResult(session as never, "question-1", answer);
    expect(promptCalls).toEqual([]);
    expect(messages.at(-1)).toMatchObject({
      role: "toolResult",
      toolCallId: "call-ask-1",
      toolName: "ask_user",
    });
    expect(JSON.parse((messages.at(-1) as unknown as { content: Array<{ text: string }> }).content[0].text)).toEqual({
      accepted: true,
      answer: { text: "筋膜枪" },
    });
    expect(persisted).toHaveLength(1);
    expect(buildAskUserToolResultMessage("call-ask-1", "question-1", { skip: true }).content[0].text).toContain(
      "no_answer",
    );
    expect(continueCalls).toEqual([]);
  });

  it("rejects inject when the session has no pending ask_user", async () => {
    const session = {
      agent: { state: { messages: [{ role: "user", content: "hello" }] } },
      sessionManager: { appendMessage: () => undefined },
    };
    await expect(injectAskUserToolResult(session as never, "question-1", { text: "筋膜枪" })).rejects.toEqual(
      expect.objectContaining({
        name: RuntimeError.name,
        status: 409,
        code: "question_wait_expired",
        message: QUESTION_WAIT_EXPIRED_MESSAGE,
      }),
    );
  });

  it("summarizes skip and option answers for the tool step row", () => {
    const skipResult = {
      content: [{ type: "text", text: JSON.stringify({ accepted: false, status: "no_answer" }) }],
    };
    const optionResult = {
      content: [{ type: "text", text: JSON.stringify({ accepted: true, answer: { option: 0 } }) }],
    };
    expect(compactAskUserSummary(skipResult)).toBe("已跳过");
    expect(compactAskUserSummary(optionResult, ["筋膜枪", "稍后再说"])).toBe("筋膜枪");
  });
});
