/**
 * 把挂起的 ask_user 收成 toolResult，再在同一 Pi session 上 continue。
 * 不能再 prompt 一条用户消息，否则 transcript 会多一条 user。
 */

import type { AgentSession } from "@earendil-works/pi-coding-agent";
import { questionAnswerToolPayload, type JsonObject, type TurnAnswer, type TurnEvent } from "./contracts.js";
import { RuntimeError } from "./store.js";

export const QUESTION_WAIT_EXPIRED_MESSAGE = "提问等待已失效";

interface ToolCallPart {
  type: "toolCall";
  id: string;
  name: string;
}

interface SessionMessage {
  role: string;
  content?: unknown;
  toolCallId?: string;
  toolName?: string;
}

export function storedAnswerFromEvents(events: readonly TurnEvent[]): TurnAnswer | null {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (event.kind !== "question.answered") continue;
    return parseStoredAnswer(event.payload.answer);
  }
  return null;
}

export function parseStoredAnswer(value: unknown): TurnAnswer | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const body = value as Record<string, unknown>;
  if (body.skip === true) return { skip: true };
  if (typeof body.option === "number" && Number.isInteger(body.option) && body.option >= 0) {
    return { option: body.option };
  }
  if (typeof body.text === "string" && body.text.trim()) {
    return { text: body.text };
  }
  return null;
}

export function pendingAskUserCall(messages: readonly SessionMessage[]): { id: string } | null {
  const answered = new Set<string>();
  for (const message of messages) {
    if (message.role === "toolResult" && message.toolName === "ask_user" && typeof message.toolCallId === "string") {
      answered.add(message.toolCallId);
    }
  }
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (message.role !== "assistant" || !Array.isArray(message.content)) continue;
    for (let partIndex = message.content.length - 1; partIndex >= 0; partIndex -= 1) {
      const part = message.content[partIndex];
      if (!isToolCallPart(part) || part.name !== "ask_user" || answered.has(part.id)) continue;
      return { id: part.id };
    }
  }
  return null;
}

export function lastMessageIsAskUserResult(messages: readonly SessionMessage[]): boolean {
  const last = messages[messages.length - 1];
  return last?.role === "toolResult" && last.toolName === "ask_user";
}

export function buildAskUserToolResultMessage(toolCallId: string, questionId: string, answer: TurnAnswer): {
  role: "toolResult";
  toolCallId: string;
  toolName: "ask_user";
  content: Array<{ type: "text"; text: string }>;
  details: JsonObject;
  isError: false;
  timestamp: number;
} {
  const payload = questionAnswerToolPayload(answer);
  return {
    role: "toolResult",
    toolCallId,
    toolName: "ask_user",
    content: [{ type: "text", text: JSON.stringify(payload) }],
    details: { question_id: questionId },
    isError: false,
    timestamp: Date.now(),
  };
}

export function compactAskUserSummary(result: unknown, optionLabels: readonly string[] = []): string {
  const parsed = parseAskUserResultPayload(result);
  if (!parsed) return "用户回答已保存";
  if (parsed.accepted === false) return "已跳过";
  const answer = parsed.answer;
  if (!answer) return "用户回答已保存";
  if (typeof answer.text === "string" && answer.text.trim()) return answer.text.trim();
  if (typeof answer.option === "number" && Number.isInteger(answer.option) && answer.option >= 0) {
    const label = optionLabels[answer.option]?.trim();
    if (label) return label;
    return `选项 ${answer.option + 1}`;
  }
  return "用户回答已保存";
}

function parseAskUserResultPayload(result: unknown): {
  accepted?: boolean;
  answer?: { text?: unknown; option?: unknown };
} | null {
  const record = asRecord(result);
  const content = Array.isArray(record?.content) ? record.content : [];
  const textPart = content.find((part) => asRecord(part)?.type === "text");
  const raw = asRecord(textPart)?.text;
  if (typeof raw !== "string" || !raw.trim()) return asRecord(record?.details) as never;
  try {
    const parsed = JSON.parse(raw) as unknown;
    return asRecord(parsed) as { accepted?: boolean; answer?: { text?: unknown; option?: unknown } };
  } catch {
    return null;
  }
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function isToolCallPart(value: unknown): value is ToolCallPart {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const part = value as Record<string, unknown>;
  return part.type === "toolCall" && typeof part.id === "string" && typeof part.name === "string";
}

export async function injectAskUserToolResult(
  session: AgentSession,
  questionId: string,
  answer: TurnAnswer,
): Promise<string | null> {
  const messages = session.agent.state.messages as SessionMessage[];
  if (lastMessageIsAskUserResult(messages)) return null;
  const pending = pendingAskUserCall(messages);
  if (!pending) {
    throw new RuntimeError(409, "question_wait_expired", QUESTION_WAIT_EXPIRED_MESSAGE);
  }
  const toolResult = buildAskUserToolResultMessage(pending.id, questionId, answer);
  messages.push(toolResult);
  session.sessionManager.appendMessage(toolResult);
  return pending.id;
}

export async function continueAgentSession(session: AgentSession): Promise<void> {
  await session.agent.continue();
  await session.agent.waitForIdle();
}
