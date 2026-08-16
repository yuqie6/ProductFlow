import type {
  AgentQuestion,
  AgentToolStep,
  AgentToolStepKind,
  AgentToolStepStatus,
  AgentTurn,
  AgentTurnEvent,
  AgentTurnStatus,
} from "../../lib/types";

const AGENT_TOOL_STEP_KINDS = new Set<AgentToolStepKind>([
  "inspect_image",
  "propose_draft",
  "inspect_context",
  "read_history",
  "organize_assets",
]);
const AGENT_TOOL_STEP_STATUSES = new Set<AgentToolStepStatus>([
  "running",
  "succeeded",
  "failed",
  "unknown",
]);
const UTF8_ENCODER = new TextEncoder();

export const AGENT_TERMINAL_EVENT_KINDS = [
  "turn.awaiting_confirmation",
  "turn.succeeded",
  "turn.failed",
  "turn.canceled",
  "turn.unknown",
] as const;

export type AgentTerminalEventKind = (typeof AGENT_TERMINAL_EVENT_KINDS)[number];

export interface AgentAttemptBuffer {
  attempt_id: string;
  step_id: string;
  text: string;
  first_sequence: number;
  last_sequence: number;
}

export interface AgentLiveToolStep {
  step: AgentToolStep;
  sequence: number;
}

export interface AgentTurnEventState {
  turn_key: string;
  last_sequence: number;
  attempts: Record<string, AgentAttemptBuffer>;
  attempt_order: string[];
  tool_steps: Record<string, AgentLiveToolStep>;
  tool_step_order: string[];
  current_attempt_id: string | null;
  question: AgentQuestion | null;
  question_answered: boolean;
  resume_requested: boolean;
  cancel_requested: boolean;
  artifact_sequence: number | null;
  terminal_kind: AgentTerminalEventKind | null;
  protocol_error: string | null;
}

export type AgentTurnEventAction =
  | { type: "reset"; turn_key: string }
  | { type: "event"; event: AgentTurnEvent };

export interface AgentTurnEventScope {
  run_id: string;
  turn_id: string;
}

export class AgentEventProtocolError extends Error {}

export function createAgentTurnEventState(turnKey: string): AgentTurnEventState {
  return {
    turn_key: turnKey,
    last_sequence: 0,
    attempts: {},
    attempt_order: [],
    tool_steps: {},
    tool_step_order: [],
    current_attempt_id: null,
    question: null,
    question_answered: false,
    resume_requested: false,
    cancel_requested: false,
    artifact_sequence: null,
    terminal_kind: null,
    protocol_error: null,
  };
}

export function parseAgentTurnEvent(
  raw: string,
  eventKind: string,
  expectedScope: AgentTurnEventScope,
): AgentTurnEvent {
  let value: unknown;
  try {
    value = JSON.parse(raw) as unknown;
  } catch {
    throw new AgentEventProtocolError("Agent 事件不是有效 JSON");
  }
  if (!isRecord(value)) {
    throw new AgentEventProtocolError("Agent 事件必须是对象");
  }
  if (value.schema_version !== 1) {
    throw new AgentEventProtocolError("Agent 事件 schema_version 不受支持");
  }
  if (value.run_id !== expectedScope.run_id || value.turn_id !== expectedScope.turn_id) {
    throw new AgentEventProtocolError("Agent 事件作用域与当前 Turn 不匹配");
  }
  if (!Number.isSafeInteger(value.sequence) || (value.sequence as number) <= 0) {
    throw new AgentEventProtocolError("Agent 事件 sequence 无效");
  }
  if (typeof value.created_at !== "string" || !value.created_at) {
    throw new AgentEventProtocolError("Agent 事件 created_at 无效");
  }
  if (typeof value.kind !== "string" || value.kind !== eventKind) {
    throw new AgentEventProtocolError("Agent 事件 kind 与 SSE 类型不匹配");
  }
  if (!isRecord(value.payload)) {
    throw new AgentEventProtocolError("Agent 事件 payload 必须是对象");
  }
  if (value.kind === "text.delta") {
    parseTextDeltaPayload(value.payload);
  }
  if (value.kind === "question.required") {
    parseAgentQuestion(value.payload);
  }
  if (value.kind === "tool.step") {
    parseAgentToolStep(value.payload);
  }
  return value as unknown as AgentTurnEvent;
}

export function agentEventReducer(
  state: AgentTurnEventState,
  action: AgentTurnEventAction,
): AgentTurnEventState {
  if (action.type === "reset") {
    return !action.turn_key || action.turn_key === state.turn_key
      ? state
      : createAgentTurnEventState(action.turn_key);
  }
  const { event } = action;
  if (event.sequence <= state.last_sequence) {
    return state;
  }
  const next = { ...state, last_sequence: event.sequence };
  switch (event.kind) {
    case "text.delta": {
      const payload = parseTextDeltaPayload(event.payload);
      const existing = state.attempts[payload.attempt_id];
      if (existing && existing.step_id !== payload.step_id) {
        return {
          ...next,
          protocol_error: "同一 Agent attempt 返回了不一致的 step_id",
        };
      }
      const attempt: AgentAttemptBuffer = existing
        ? {
            ...existing,
            text: existing.text + payload.delta,
            last_sequence: event.sequence,
          }
        : {
            attempt_id: payload.attempt_id,
            step_id: payload.step_id,
            text: payload.delta,
            first_sequence: event.sequence,
            last_sequence: event.sequence,
          };
      return {
        ...next,
        attempts: { ...state.attempts, [payload.attempt_id]: attempt },
        attempt_order: existing
          ? state.attempt_order
          : [...state.attempt_order, payload.attempt_id],
        current_attempt_id: payload.attempt_id,
      };
    }
    case "tool.step": {
      const step = parseAgentToolStep(event.payload);
      const existing = state.tool_steps[step.step_id];
      return {
        ...next,
        tool_steps: {
          ...state.tool_steps,
          [step.step_id]: { step, sequence: event.sequence },
        },
        tool_step_order: existing
          ? state.tool_step_order
          : [...state.tool_step_order, step.step_id],
      };
    }
    case "question.required":
      return {
        ...next,
        question: parseAgentQuestion(event.payload),
        question_answered: false,
        resume_requested: false,
      };
    case "question.answered":
      return { ...next, question_answered: true };
    case "turn.resume_requested":
      return { ...next, resume_requested: true };
    case "turn.cancel_requested":
      return { ...next, cancel_requested: true };
    case "artifact.proposed":
      return { ...next, artifact_sequence: event.sequence };
    case "turn.awaiting_confirmation":
    case "turn.succeeded":
    case "turn.failed":
    case "turn.canceled":
    case "turn.unknown":
      return { ...next, terminal_kind: event.kind };
    default:
      return next;
  }
}

export function currentAgentAttempt(state: AgentTurnEventState): AgentAttemptBuffer | null {
  return state.current_attempt_id ? state.attempts[state.current_attempt_id] ?? null : null;
}

export function isAgentTurnTerminal(status: AgentTurnStatus): boolean {
  return (
    status === "awaiting_confirmation" ||
    status === "succeeded" ||
    status === "failed" ||
    status === "canceled" ||
    status === "unknown"
  );
}

export function agentTurnNeedsEventStream(turn: AgentTurn | null): boolean {
  return Boolean(turn?.harness_turn_id && !isAgentTurnTerminal(turn.status));
}

export function selectAgentAssistantText(
  turn: AgentTurn,
  eventState: AgentTurnEventState | null,
): string {
  if (isAgentTurnTerminal(turn.status)) {
    return turn.output_text ?? "";
  }
  if (eventState?.turn_key === turn.id) {
    return currentAgentAttempt(eventState)?.text ?? turn.output_text ?? "";
  }
  return turn.output_text ?? "";
}

export function selectAgentToolSteps(
  turn: AgentTurn | null | undefined,
  eventState: AgentTurnEventState | null | undefined,
): AgentToolStep[] {
  if (!turn) {
    return [];
  }
  const snapshotById = new Map<string, AgentToolStep>();
  for (const step of turn.tool_steps ?? []) {
    if (!snapshotById.has(step.step_id)) {
      snapshotById.set(step.step_id, step);
    }
  }
  if (!eventState || eventState.turn_key !== turn.id) {
    return [...snapshotById.values()];
  }
  const merged = [...snapshotById.entries()].map(
    ([stepId, step]) => eventState.tool_steps[stepId]?.step ?? step,
  );
  for (const stepId of eventState.tool_step_order) {
    if (!snapshotById.has(stepId)) {
      const live = eventState.tool_steps[stepId];
      if (live) {
        merged.push(live.step);
      }
    }
  }
  return merged;
}

function parseTextDeltaPayload(payload: Record<string, unknown>): {
  delta: string;
  step_id: string;
  attempt_id: string;
} {
  if (
    typeof payload.delta !== "string" ||
    typeof payload.step_id !== "string" ||
    !payload.step_id ||
    typeof payload.attempt_id !== "string" ||
    !payload.attempt_id
  ) {
    throw new AgentEventProtocolError("text.delta payload 无效");
  }
  return {
    delta: payload.delta,
    step_id: payload.step_id,
    attempt_id: payload.attempt_id,
  };
}

function parseAgentToolStep(payload: Record<string, unknown>): AgentToolStep {
  const keys = Object.keys(payload);
  if (
    keys.length !== 4 ||
    !keys.every((key) => ["step_id", "kind", "summary", "status"].includes(key)) ||
    typeof payload.step_id !== "string" ||
    !payload.step_id.trim() ||
    UTF8_ENCODER.encode(payload.step_id).byteLength > 200 ||
    typeof payload.kind !== "string" ||
    !AGENT_TOOL_STEP_KINDS.has(payload.kind as AgentToolStepKind) ||
    typeof payload.summary !== "string" ||
    !payload.summary.trim() ||
    /[\r\n]/u.test(payload.summary) ||
    UTF8_ENCODER.encode(payload.summary).byteLength > 160 ||
    typeof payload.status !== "string" ||
    !AGENT_TOOL_STEP_STATUSES.has(payload.status as AgentToolStepStatus)
  ) {
    throw new AgentEventProtocolError("tool.step payload 无效");
  }
  return {
    step_id: payload.step_id,
    kind: payload.kind as AgentToolStepKind,
    summary: payload.summary,
    status: payload.status as AgentToolStepStatus,
  };
}

function parseAgentQuestion(payload: Record<string, unknown>): AgentQuestion {
  const optionsValue = payload.options ?? [];
  if (
    typeof payload.id !== "string" ||
    !payload.id ||
    typeof payload.header !== "string" ||
    typeof payload.question !== "string" ||
    !payload.question ||
    !Array.isArray(optionsValue)
  ) {
    throw new AgentEventProtocolError("question.required payload 无效");
  }
  const options = optionsValue.map((option) => {
    if (
      !isRecord(option) ||
      typeof option.label !== "string" ||
      !option.label ||
      (option.description !== undefined && typeof option.description !== "string")
    ) {
      throw new AgentEventProtocolError("question.required option 无效");
    }
    return {
      label: option.label,
      ...(option.description ? { description: option.description } : {}),
    };
  });
  return {
    id: payload.id,
    header: payload.header,
    question: payload.question,
    options,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
