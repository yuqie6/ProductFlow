import type {
  AgentQuestion,
  AgentToolStep,
  AgentToolStepDetails,
  AgentToolStepKind,
  AgentToolStepStatus,
  AgentTurn,
  AgentTurnEvent,
  AgentTurnStatus,
} from "../../../lib/types";

const AGENT_TOOL_STEP_KINDS = new Set<AgentToolStepKind>([
  "load_skill",
  "inject_context",
  "ask_question",
  "inspect_image",
  "propose_draft",
  "inspect_context",
  "read_history",
  "organize_assets",
  "request_workflow_run",
  "create_product",
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
  run_id: string | null;
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
  if (
    typeof value.run_id !== "string" ||
    !value.run_id ||
    (expectedScope.run_id !== null && value.run_id !== expectedScope.run_id) ||
    value.turn_id !== expectedScope.turn_id
  ) {
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
      return { ...next, question: null, question_answered: true };
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
  const allowedKeys = new Set(["step_id", "kind", "summary", "status", "tool_name", "details"]);
  if (
    keys.length < 4 ||
    !keys.every((key) => allowedKeys.has(key)) ||
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
  const toolName = readOptionalDetailString(payload.tool_name, 120, false, "tool_name");
  const details = parseAgentToolStepDetails(payload.details);
  return {
    step_id: payload.step_id,
    kind: payload.kind as AgentToolStepKind,
    summary: payload.summary,
    status: payload.status as AgentToolStepStatus,
    ...(toolName ? { tool_name: toolName } : {}),
    ...(details ? { details } : {}),
  };
}

function parseAgentToolStepDetails(value: unknown): AgentToolStepDetails | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value) || UTF8_ENCODER.encode(JSON.stringify(value)).byteLength > 16 * 1024) {
    throw new AgentEventProtocolError("tool.step details 无效");
  }
  const allowedKeys = new Set([
    "phase",
    "skill_name",
    "resource_path",
    "instruction_excerpt",
    "instruction_truncated",
    "context_sections",
    "runtime_context_keys",
    "contract_fields",
    "page_route",
    "page_type",
    "selected_asset_count",
    "visible_asset_count",
    "context_bytes",
    "input_summary",
    "output_summary",
    "error_code",
    "error_message",
    "retryable",
    "validation_issues",
    "question_id",
    "question_header",
    "question_text",
    "option_labels",
  ]);
  if (!Object.keys(value).every((key) => allowedKeys.has(key))) {
    throw new AgentEventProtocolError("tool.step details 无效");
  }

  const phase = readOptionalDetailString(value.phase, 32, false, "phase");
  if (phase !== undefined && !new Set(["skill_load", "context_injection", "question", "tool_result"]).has(phase)) {
    throw new AgentEventProtocolError("tool.step details phase 无效");
  }
  const details: AgentToolStepDetails = phase
    ? { phase: phase as AgentToolStepDetails["phase"] }
    : {};
  const stringFields = [
    ["skill_name", 64, false],
    ["resource_path", 256, false],
    ["instruction_excerpt", 12000, true],
    ["page_route", 512, false],
    ["page_type", 80, false],
    ["input_summary", 240, false],
    ["output_summary", 240, false],
    ["error_code", 120, false],
    ["error_message", 1000, false],
    ["question_id", 120, false],
    ["question_header", 32, false],
    ["question_text", 2000, true],
  ] as const;
  for (const [key, maximum, allowNewline] of stringFields) {
    const field = readOptionalDetailString(value[key], maximum, allowNewline, key);
    if (field !== undefined) details[key] = field;
  }

  for (const [key, maximum] of [
    ["context_sections", 8],
    ["runtime_context_keys", 32],
    ["contract_fields", 16],
    ["option_labels", 5],
  ] as const) {
    const field = readOptionalDetailStringList(value[key], maximum, key);
    if (field !== undefined) details[key] = field;
  }
  for (const [key, maximum] of [
    ["selected_asset_count", 100],
    ["visible_asset_count", 100],
    ["context_bytes", 64 * 1024],
  ] as const) {
    const field = readOptionalDetailInteger(value[key], maximum, key);
    if (field !== undefined) details[key] = field;
  }
  if (value.retryable !== undefined) {
    if (typeof value.retryable !== "boolean") throw new AgentEventProtocolError("tool.step details retryable 无效");
    details.retryable = value.retryable;
  }
  if (value.instruction_truncated !== undefined) {
    if (typeof value.instruction_truncated !== "boolean") {
      throw new AgentEventProtocolError("tool.step details instruction_truncated 无效");
    }
    details.instruction_truncated = value.instruction_truncated;
  }
  if (value.validation_issues !== undefined) {
    if (!Array.isArray(value.validation_issues) || value.validation_issues.length > 8) {
      throw new AgentEventProtocolError("tool.step validation_issues 无效");
    }
    details.validation_issues = value.validation_issues.map((issue) => {
      if (
        !isRecord(issue) ||
        Object.keys(issue).some((key) => !["path", "message"].includes(key)) ||
        Object.keys(issue).length !== 2 ||
        typeof issue.path !== "string" ||
        !issue.path.trim() ||
        issue.path.length > 200 ||
        /[\r\n]/u.test(issue.path) ||
        typeof issue.message !== "string" ||
        !issue.message.trim() ||
        issue.message.length > 500 ||
        /[\r\n]/u.test(issue.message)
      ) {
        throw new AgentEventProtocolError("tool.step validation_issues 无效");
      }
      return { path: issue.path, message: issue.message };
    });
  }
  return Object.keys(details).length > 0 ? details : undefined;
}

function readOptionalDetailString(
  value: unknown,
  maximum: number,
  allowNewline: boolean,
  fieldName: string,
): string | undefined {
  if (value === undefined) return undefined;
  if (
    typeof value !== "string" ||
    !value.trim() ||
    value.length > maximum ||
    (!allowNewline && /[\r\n]/u.test(value))
  ) {
    throw new AgentEventProtocolError(`tool.step details ${fieldName} 无效`);
  }
  return value;
}

function readOptionalDetailStringList(value: unknown, maximum: number, fieldName: string): string[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length > maximum) {
    throw new AgentEventProtocolError(`tool.step details ${fieldName} 无效`);
  }
  const values = value.map((item) => {
    if (typeof item !== "string" || !item.trim() || item.length > 160 || /[\r\n]/u.test(item)) {
      throw new AgentEventProtocolError(`tool.step details ${fieldName} 无效`);
    }
    return item;
  });
  if (new Set(values).size !== values.length) {
    throw new AgentEventProtocolError(`tool.step details ${fieldName} 不能重复`);
  }
  return values;
}

function readOptionalDetailInteger(value: unknown, maximum: number, fieldName: string): number | undefined {
  if (value === undefined) return undefined;
  if (!Number.isSafeInteger(value) || (value as number) < 0 || (value as number) > maximum) {
    throw new AgentEventProtocolError(`tool.step details ${fieldName} 无效`);
  }
  return value as number;
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
