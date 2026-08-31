import { describe, expect, it } from "vitest";
import type { TurnAnswer } from "./contracts.js";
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
        { sequence: 1, kind: "question/requested", payload: {}, created_at: "", run_id: "", turn_id: "", schema_version: 1 },
        {
          sequence: 2,
          kind: "question/answered",
          payload: { answer: { text: "筋膜枪" } },
          created_at: "",
          run_id: "",
          turn_id: "",
          schema_version: 1,
        },
      ]),
    ).toEqual({ text: "筋膜枪" });
    expect(
      storedAnswerFromEvents([
        {
          sequence: 1,
          kind: "question.answered",
          payload: { answer: { option: 0 } },
          created_at: "",
          run_id: "",
          turn_id: "",
          schema_version: 1,
        },
      ]),
    ).toEqual({ option: 0 });
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
