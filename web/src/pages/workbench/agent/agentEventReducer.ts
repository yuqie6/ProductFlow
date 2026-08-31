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
import { AGENT_TOOL_STEP_KINDS } from "./toolManifest.generated";

const AGENT_TOOL_STEP_KIND_SET = new Set<AgentToolStepKind>(AGENT_TOOL_STEP_KINDS);
const AGENT_TOOL_STEP_STATUSES = new Set<AgentToolStepStatus>([
  "running",
  "succeeded",
  "failed",
  "unknown",
]);
const UTF8_ENCODER = new TextEncoder();

export const AGENT_TERMINAL_EVENT_KINDS = [
  "turn.completed",
  "turn.awaiting_confirmation",
  "turn.failed",
  "turn.canceled",
  "turn.unknown",
] as const;

export type AgentTerminalEventKind = (typeof AGENT_TERMINAL_EVENT_KINDS)[number];

export const AGENT_UI_EVENT_KINDS = [
  "turn.started",
  "item.started",
  "item.delta",
  "item.completed",
  "approval.requested",
  "approval.resolved",
  "agent.ignored",
  ...AGENT_TERMINAL_EVENT_KINDS,
] as const;
const AGENT_UI_EVENT_KIND_SET = new Set<string>(AGENT_UI_EVENT_KINDS);

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

export type AgentTurnBlock =
  | {
    type: "thinking";
    key: string;
    attempt_id: string;
    content_index: number;
    text: string;
    truncated: boolean;
  }
  | {
    type: "text";
    key: string;
    attempt_id: string;
    content_index: number;
    text: string;
  }
  | {
    type: "tool";
    key: string;
    step_id: string;
  };

export interface AgentTurnEventState {
  turn_key: string;
  last_sequence: number;
  attempts: Record<string, AgentAttemptBuffer>;
  attempt_order: string[];
  tool_steps: Record<string, AgentLiveToolStep>;
  tool_step_order: string[];
  blocks: AgentTurnBlock[];
  current_attempt_id: string | null;
  question: AgentQuestion | null;
  question_answered: boolean;
  terminal_kind: AgentTerminalEventKind | null;
  text_settled: boolean;
  protocol_error: string | null;
  approval: Record<string, unknown> | null;
  approval_resolved: boolean;
}

export type AgentTurnEventAction =
  | { type: "reset"; turn_key: string }
  | { type: "event"; event: AgentTurnEvent };

export interface AgentTurnEventScope {
  run_id: string | null;
  turn_id: string;
}

export class AgentEventProtocolError extends Error { }

export function createAgentTurnEventState(turnKey: string): AgentTurnEventState {
  return {
    turn_key: turnKey,
    last_sequence: 0,
    attempts: {},
    attempt_order: [],
    tool_steps: {},
    tool_step_order: [],
    blocks: [],
    current_attempt_id: null,
    question: null,
    question_answered: false,
    terminal_kind: null,
    text_settled: false,
    protocol_error: null,
    approval: null,
    approval_resolved: false,
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
  if (!AGENT_UI_EVENT_KIND_SET.has(value.kind)) {
    throw new AgentEventProtocolError("Agent UI 事件 kind 不受支持");
  }
  if (!isRecord(value.payload)) {
    throw new AgentEventProtocolError("Agent 事件 payload 必须是对象");
  }
  if (value.kind === "item.delta") {
    const item = parseItemDeltaPayload(value.payload);
    if (item.item_kind === "assistant_text") {
      parseAssistantItemDeltaPayload(value.payload);
    } else if (item.item_kind === "thinking") {
      parseThinkingItemDeltaPayload(value.payload);
    } else {
      throw new AgentEventProtocolError("item.delta item_kind 无效");
    }
  }
  if (value.kind === "approval.requested" && value.payload.approval_kind === "question") {
    parseAgentQuestion(value.payload);
  }
  if (value.kind === "item.started" && value.payload.item_kind === "step") {
    parseStableStepStartedPayload(value.payload);
  } else if ((value.kind === "item.started" || value.kind === "item.completed") && hasToolStepPayload(value.payload)) {
    parseAgentToolStep(value.payload);
  }
  if (value.kind === "item.completed" && !hasToolStepPayload(value.payload)) {
    parseStableAssistantCompletedPayload(value.payload);
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
    case "item.delta": {
      const payload = parseItemDeltaPayload(event.payload);
      if (payload.item_kind === "assistant_text") {
        return appendTextPayload(next, state, payload, event.sequence);
      }
      if (payload.item_kind === "thinking") {
        return {
          ...next,
          blocks: appendStreamBlock(
            state.blocks,
            "thinking",
            payload.attempt_id,
            payload.content_index,
            payload.delta,
            payload.truncated,
          ),
        };
      }
      return next;
    }
    case "item.started":
    case "item.completed": {
      if (event.kind === "item.started" && event.payload.item_kind === "step") {
        return next;
      }
      if (!hasToolStepPayload(event.payload)) {
        const completed = parseStableAssistantCompletedPayload(event.payload);
        return {
          ...next,
          text_settled: true,
          blocks: completed.text === undefined
            ? state.blocks
            : settleAssistantText(state.blocks, completed),
        };
      }
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
        blocks: existing
          ? replaceToolBlock(state.blocks, step.step_id)
          : [...state.blocks, { type: "tool", key: `tool:${step.step_id}`, step_id: step.step_id }],
      };
    }
    case "approval.requested": {
      const approval = event.payload;
      if (approval.approval_kind === "question") {
        return {
          ...next,
          approval,
          approval_resolved: false,
          question: parseAgentQuestion(approval),
          question_answered: false,
        };
      }
      return { ...next, approval, approval_resolved: false };
    }
    case "approval.resolved":
      return { ...next, approval: event.payload, approval_resolved: true, question: null, question_answered: true };
    case "turn.completed":
    case "turn.awaiting_confirmation":
    case "turn.failed":
    case "turn.canceled":
    case "turn.unknown":
      return { ...next, terminal_kind: event.kind, text_settled: true };
    default:
      return next;
  }
}

export function currentAgentAttempt(state: AgentTurnEventState): AgentAttemptBuffer | null {
  return state.current_attempt_id ? state.attempts[state.current_attempt_id] ?? null : null;
}

export const AGENT_SETTLED_EVENT_KINDS = [
  "turn.completed",
  "turn.failed",
  "turn.canceled",
  "turn.unknown",
] as const;

export function isAgentTurnTerminal(status: AgentTurnStatus): boolean {
  return (
    status === "awaiting_confirmation" ||
    status === "succeeded" ||
    status === "failed" ||
    status === "canceled" ||
    status === "unknown"
  );
}

export function isAgentTurnSettled(status: AgentTurnStatus): boolean {
  return status === "succeeded" || status === "failed" || status === "canceled" || status === "unknown";
}

export function agentTurnNeedsEventStream(turn: AgentTurn | null): boolean {
  return Boolean(turn?.harness_turn_id);
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

export function selectAgentTurnBlocks(
  turn: AgentTurn,
  eventState: AgentTurnEventState | null,
): AgentTurnBlock[] {
  if (eventState?.turn_key === turn.id && eventState.blocks.length > 0) {
    return canonicalizeTerminalBlocks(turn, eventState.blocks);
  }
  return snapshotTurnBlocks(turn);
}

export function splitAgentTurnProcess(blocks: readonly AgentTurnBlock[]): {
  process: AgentTurnBlock[];
  body: Extract<AgentTurnBlock, { type: "text" }> | null;
} {
  let lastTextIndex = -1;
  for (let index = blocks.length - 1; index >= 0; index -= 1) {
    const block = blocks[index];
    if (block.type === "text" && block.text.trim()) {
      lastTextIndex = index;
      break;
    }
  }
  if (lastTextIndex === -1) {
    return { process: [...blocks], body: null };
  }
  const body = blocks[lastTextIndex];
  if (body.type !== "text") {
    return { process: [...blocks], body: null };
  }
  return { process: blocks.slice(0, lastTextIndex), body };
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

function parseAssistantItemDeltaPayload(payload: Record<string, unknown>): {
  delta: string;
  step_id: string;
  attempt_id: string;
  content_index: number;
} {
  if (
    typeof payload.delta !== "string" ||
    typeof payload.step_id !== "string" ||
    !payload.step_id ||
    typeof payload.attempt_id !== "string" ||
    !payload.attempt_id
  ) {
    throw new AgentEventProtocolError("item.delta assistant payload 无效");
  }
  return {
    delta: payload.delta,
    step_id: payload.step_id,
    attempt_id: payload.attempt_id,
    content_index: parseContentIndex(payload.content_index, "item.delta"),
  };
}

function parseItemDeltaPayload(payload: Record<string, unknown>): {
  item_id: string;
  item_kind: string;
  delta: string;
  attempt_id: string;
  step_id: string;
  content_index: number;
  truncated: boolean;
} {
  if (
    typeof payload.item_id !== "string" || !payload.item_id ||
    typeof payload.item_kind !== "string" || !payload.item_kind ||
    typeof payload.delta !== "string"
  ) {
    throw new AgentEventProtocolError("item.delta payload 无效");
  }
  return {
    item_id: payload.item_id,
    item_kind: payload.item_kind,
    delta: payload.delta,
    attempt_id: typeof payload.attempt_id === "string" && payload.attempt_id ? payload.attempt_id : payload.item_id,
    step_id: typeof payload.step_id === "string" && payload.step_id ? payload.step_id : payload.item_id,
    content_index: parseContentIndex(payload.content_index, "item.delta"),
    truncated: payload.truncated === true,
  };
}

function hasToolStepPayload(payload: Record<string, unknown>): boolean {
  return typeof payload.step_id === "string" && typeof payload.summary === "string" && typeof payload.status === "string";
}

function parseStableAssistantCompletedPayload(payload: Record<string, unknown>): {
  item_id: string;
  text?: string;
  attempt_id: string;
  content_index: number;
} {
  if (payload.item_kind !== "assistant_text" || typeof payload.item_id !== "string" || !payload.item_id) {
    throw new AgentEventProtocolError("item.completed payload 无效");
  }
  if (payload.text !== undefined && typeof payload.text !== "string") {
    throw new AgentEventProtocolError("item.completed assistant payload 无效");
  }
  return {
    item_id: payload.item_id,
    ...(payload.text !== undefined ? { text: payload.text } : {}),
    attempt_id: typeof payload.attempt_id === "string" && payload.attempt_id ? payload.attempt_id : payload.item_id,
    content_index: parseContentIndex(payload.content_index, "item.completed"),
  };
}

function settleAssistantText(
  blocks: AgentTurnBlock[],
  completed: { item_id: string; text?: string; attempt_id: string; content_index: number },
): AgentTurnBlock[] {
  if (completed.text === undefined) return blocks;
  for (let index = blocks.length - 1; index >= 0; index -= 1) {
    const block = blocks[index];
    if (block.type !== "text") continue;
    if (block.attempt_id === completed.attempt_id || block.content_index === completed.content_index) {
      if (block.text === completed.text) return blocks;
      const next = blocks.slice();
      next[index] = { ...block, text: completed.text };
      return next;
    }
  }
  if (!completed.text) return blocks;
  return [
    ...blocks,
    {
      type: "text",
      key: `text:${completed.item_id}`,
      attempt_id: completed.attempt_id,
      content_index: completed.content_index,
      text: completed.text,
    },
  ];
}

function appendTextPayload(
  next: AgentTurnEventState,
  state: AgentTurnEventState,
  payload: { delta: string; attempt_id: string; step_id: string; content_index: number },
  sequence: number,
): AgentTurnEventState {
  const existing = state.attempts[payload.attempt_id];
  if (existing && existing.step_id !== payload.step_id) {
    return { ...next, protocol_error: "同一 Agent attempt 返回了不一致的 step_id" };
  }
  const attempt: AgentAttemptBuffer = existing
    ? { ...existing, text: existing.text + payload.delta, last_sequence: sequence }
    : {
      attempt_id: payload.attempt_id,
      step_id: payload.step_id,
      text: payload.delta,
      first_sequence: sequence,
      last_sequence: sequence,
    };
  return {
    ...next,
    attempts: { ...state.attempts, [payload.attempt_id]: attempt },
    attempt_order: existing ? state.attempt_order : [...state.attempt_order, payload.attempt_id],
    current_attempt_id: payload.attempt_id,
    blocks: appendStreamBlock(state.blocks, "text", payload.attempt_id, payload.content_index, payload.delta),
  };
}

function replaceToolBlock(blocks: AgentTurnBlock[], stepID: string): AgentTurnBlock[] {
  if (blocks.some((block) => block.type === "tool" && block.step_id === stepID)) return blocks;
  return [...blocks, { type: "tool", key: `tool:${stepID}`, step_id: stepID }];
}

function parseThinkingItemDeltaPayload(payload: Record<string, unknown>): {
  delta: string;
  step_id: string;
  attempt_id: string;
  content_index: number;
  truncated: boolean;
} {
  if (
    typeof payload.delta !== "string" ||
    typeof payload.step_id !== "string" ||
    !payload.step_id ||
    typeof payload.attempt_id !== "string" ||
    !payload.attempt_id
  ) {
    throw new AgentEventProtocolError("item.delta thinking payload 无效");
  }
  if (payload.truncated !== undefined && typeof payload.truncated !== "boolean") {
    throw new AgentEventProtocolError("item.delta thinking payload 无效");
  }
  return {
    delta: payload.delta,
    step_id: payload.step_id,
    attempt_id: payload.attempt_id,
    content_index: parseContentIndex(payload.content_index, "item.delta"),
    truncated: payload.truncated === true,
  };
}

function parseContentIndex(value: unknown, kind: string): number {
  if (value === undefined) return 0;
  if (!Number.isSafeInteger(value) || (value as number) < 0 || (value as number) > 10_000) {
    throw new AgentEventProtocolError(`${kind} payload 无效`);
  }
  return value as number;
}

function appendStreamBlock(
  blocks: AgentTurnBlock[],
  type: "thinking" | "text",
  attemptId: string,
  contentIndex: number,
  delta: string,
  truncated = false,
): AgentTurnBlock[] {
  const last = blocks[blocks.length - 1];
  if (
    last &&
    last.type === type &&
    last.attempt_id === attemptId &&
    last.content_index === contentIndex
  ) {
    if (last.type === "thinking") {
      return [
        ...blocks.slice(0, -1),
        {
          ...last,
          text: last.text + delta,
          truncated: last.truncated || truncated,
        },
      ];
    }
    return [...blocks.slice(0, -1), { ...last, text: last.text + delta }];
  }
  const key = `${type}:${attemptId}:${blocks.length}`;
  if (type === "thinking") {
    return [
      ...blocks,
      {
        type: "thinking",
        key,
        attempt_id: attemptId,
        content_index: contentIndex,
        text: delta,
        truncated,
      },
    ];
  }
  return [
    ...blocks,
    {
      type: "text",
      key,
      attempt_id: attemptId,
      content_index: contentIndex,
      text: delta,
    },
  ];
}

function canonicalizeTerminalBlocks(turn: AgentTurn, blocks: AgentTurnBlock[]): AgentTurnBlock[] {
  if (!isAgentTurnTerminal(turn.status)) {
    return blocks;
  }
  const output = turn.output_text ?? "";
  const lastTextIndex = lastNonEmptyTextIndex(blocks);
  if (lastTextIndex >= 0) {
    const last = blocks[lastTextIndex];
    if (last.type !== "text" || last.text === output) {
      return blocks;
    }
    const next = blocks.slice();
    next[lastTextIndex] = { ...last, text: output };
    return next;
  }
  if (!output) {
    return blocks;
  }
  return [
    ...blocks,
    {
      type: "text",
      key: "text:snapshot",
      attempt_id: "snapshot",
      content_index: 0,
      text: output,
    },
  ];
}

function snapshotTurnBlocks(turn: AgentTurn): AgentTurnBlock[] {
  const blocks: AgentTurnBlock[] = [];
  if (turn.thinking_text) {
    blocks.push({
      type: "thinking",
      key: "thinking:snapshot",
      attempt_id: "snapshot",
      content_index: 0,
      text: turn.thinking_text,
      truncated: false,
    });
  }
  for (const step of turn.tool_steps ?? []) {
    blocks.push({ type: "tool", key: `tool:${step.step_id}`, step_id: step.step_id });
  }
  if (turn.output_text) {
    blocks.push({
      type: "text",
      key: "text:snapshot",
      attempt_id: "snapshot",
      content_index: 0,
      text: turn.output_text,
    });
  }
  return blocks;
}

function lastNonEmptyTextIndex(blocks: readonly AgentTurnBlock[]): number {
  for (let index = blocks.length - 1; index >= 0; index -= 1) {
    const block = blocks[index];
    if (block.type === "text" && block.text.trim()) {
      return index;
    }
  }
  return -1;
}

function parseAgentToolStep(payload: Record<string, unknown>): AgentToolStep {
  const keys = Object.keys(payload);
  const allowedKeys = new Set(["item_id", "item_kind", "step_id", "kind", "summary", "status", "tool_name", "details", "meta"]);
  if (
    keys.length < 4 ||
    !keys.every((key) => allowedKeys.has(key)) ||
    typeof payload.step_id !== "string" ||
    !payload.step_id.trim() ||
    UTF8_ENCODER.encode(payload.step_id).byteLength > 200 ||
    typeof payload.kind !== "string" ||
    !AGENT_TOOL_STEP_KIND_SET.has(payload.kind as AgentToolStepKind) ||
    typeof payload.summary !== "string" ||
    !payload.summary.trim() ||
    /[\r\n]/u.test(payload.summary) ||
    UTF8_ENCODER.encode(payload.summary).byteLength > 160 ||
    typeof payload.status !== "string" ||
    !AGENT_TOOL_STEP_STATUSES.has(payload.status as AgentToolStepStatus)
  ) {
    throw new AgentEventProtocolError("tool_call item payload 无效");
  }
  const toolName = readOptionalDetailString(payload.tool_name, 120, false, "tool_name");
  const details = parseAgentToolStepDetails(payload.details);
  const meta = parseAgentToolMeta(payload.meta);
  return {
    step_id: payload.step_id,
    kind: payload.kind as AgentToolStepKind,
    summary: payload.summary,
    status: payload.status as AgentToolStepStatus,
    ...(toolName ? { tool_name: toolName } : {}),
    ...(details ? { details } : {}),
    ...(meta ? { meta } : {}),
  };
}

function parseStableStepStartedPayload(payload: Record<string, unknown>): void {
  if (
    Object.keys(payload).some((key) => !["item_id", "item_kind", "step_id"].includes(key)) ||
    payload.item_kind !== "step" ||
    typeof payload.item_id !== "string" ||
    !payload.item_id.trim() ||
    typeof payload.step_id !== "string" ||
    !payload.step_id.trim()
  ) {
    throw new AgentEventProtocolError("item.started step payload 无效");
  }
}

function parseAgentToolMeta(value: unknown): AgentToolStepDetails | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value) || UTF8_ENCODER.encode(JSON.stringify(value)).byteLength > 16 * 1024) {
    throw new AgentEventProtocolError("tool_call item meta 无效");
  }
  const rest: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value)) {
    if (key === "schema_version" || key === "kind") continue;
    rest[key] = item;
  }
  if (value.schema_version !== undefined && value.schema_version !== 1) {
    throw new AgentEventProtocolError("tool_call item meta schema_version 无效");
  }
  if (value.kind !== undefined && (typeof value.kind !== "string" || !value.kind.trim() || value.kind.length > 80)) {
    throw new AgentEventProtocolError("tool_call item meta kind 无效");
  }
  return parseAgentToolStepDetails(Object.keys(rest).length ? rest : undefined);
}

function parseAgentToolStepDetails(value: unknown): AgentToolStepDetails | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value) || UTF8_ENCODER.encode(JSON.stringify(value)).byteLength > 16 * 1024) {
    throw new AgentEventProtocolError("tool_call item details 无效");
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
    "truncated",
    "pending_confirmation",
    "reconciled",
    "response_format",
    "item_count",
    "node_count",
    "group_count",
    "asset_count",
    "operation_summaries",
    "affected_node_ids",
    "affected_edge_ids",
    "affected_group_ids",
    "workflow_id",
    "workflow_title",
    "run_id",
    "proposal_id",
    "product_id",
    "request_id",
    "expected_workflow_revision",
    "product_workspace_created",
    "summary",
    "artifact_name",
  ]);
  if (!Object.keys(value).every((key) => allowedKeys.has(key))) {
    throw new AgentEventProtocolError("tool_call item details 无效");
  }

  const phase = readOptionalDetailString(value.phase, 32, false, "phase");
  if (phase !== undefined && !new Set(["skill_load", "context_injection", "question", "tool_result"]).has(phase)) {
    throw new AgentEventProtocolError("tool_call item details phase 无效");
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
    ["workflow_id", 64, false],
    ["workflow_title", 240, false],
    ["run_id", 64, false],
    ["proposal_id", 64, false],
    ["product_id", 64, false],
    ["request_id", 64, false],
    ["summary", 240, false],
    ["artifact_name", 120, false],
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
    ["affected_node_ids", 20],
    ["affected_edge_ids", 20],
    ["affected_group_ids", 20],
  ] as const) {
    const field = readOptionalDetailStringList(value[key], maximum, key);
    if (field !== undefined) details[key] = field;
  }
  const operationSummaries = readOptionalDetailStringList(value.operation_summaries, 16, "operation_summaries", false);
  if (operationSummaries !== undefined) details.operation_summaries = operationSummaries;
  for (const [key, maximum] of [
    ["selected_asset_count", 100],
    ["visible_asset_count", 100],
    ["context_bytes", 64 * 1024],
    ["item_count", 128],
    ["node_count", 10_000],
    ["group_count", 10_000],
    ["asset_count", 100],
  ] as const) {
    const field = readOptionalDetailInteger(value[key], maximum, key);
    if (field !== undefined) details[key] = field;
  }
  if (value.expected_workflow_revision !== undefined) {
    const field = readOptionalDetailInteger(
      value.expected_workflow_revision,
      1_000_000,
      "expected_workflow_revision",
    );
    if (field === undefined || field < 1) {
      throw new AgentEventProtocolError("tool_call item details expected_workflow_revision 无效");
    }
    details.expected_workflow_revision = field;
  }
  if (value.response_format !== undefined) {
    if (value.response_format !== "concise" && value.response_format !== "detailed") {
      throw new AgentEventProtocolError("tool_call item details response_format 无效");
    }
    details.response_format = value.response_format;
  }
  for (const key of ["truncated", "pending_confirmation", "reconciled", "product_workspace_created"] as const) {
    if (value[key] === undefined) continue;
    if (typeof value[key] !== "boolean") throw new AgentEventProtocolError(`tool_call item details ${key} 无效`);
    details[key] = value[key];
  }
  if (value.retryable !== undefined) {
    if (typeof value.retryable !== "boolean") throw new AgentEventProtocolError("tool_call item details retryable 无效");
    details.retryable = value.retryable;
  }
  if (value.instruction_truncated !== undefined) {
    if (typeof value.instruction_truncated !== "boolean") {
      throw new AgentEventProtocolError("tool_call item details instruction_truncated 无效");
    }
    details.instruction_truncated = value.instruction_truncated;
  }
  if (value.validation_issues !== undefined) {
    if (!Array.isArray(value.validation_issues) || value.validation_issues.length > 8) {
      throw new AgentEventProtocolError("tool_call item validation_issues 无效");
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
        throw new AgentEventProtocolError("tool_call item validation_issues 无效");
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
    throw new AgentEventProtocolError(`tool_call item details ${fieldName} 无效`);
  }
  return value;
}

function readOptionalDetailStringList(
  value: unknown,
  maximum: number,
  fieldName: string,
  unique = true,
): string[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length > maximum) {
    throw new AgentEventProtocolError(`tool_call item details ${fieldName} 无效`);
  }
  const values = value.map((item) => {
    if (typeof item !== "string" || !item.trim() || item.length > 160 || /[\r\n]/u.test(item)) {
      throw new AgentEventProtocolError(`tool_call item details ${fieldName} 无效`);
    }
    return item;
  });
  if (unique && new Set(values).size !== values.length) {
    throw new AgentEventProtocolError(`tool_call item details ${fieldName} 不能重复`);
  }
  return values;
}

function readOptionalDetailInteger(value: unknown, maximum: number, fieldName: string): number | undefined {
  if (value === undefined) return undefined;
  if (!Number.isSafeInteger(value) || (value as number) < 0 || (value as number) > maximum) {
    throw new AgentEventProtocolError(`tool_call item details ${fieldName} 无效`);
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
    throw new AgentEventProtocolError("approval question payload 无效");
  }
  const options = optionsValue.map((option) => {
    if (
      !isRecord(option) ||
      typeof option.label !== "string" ||
      !option.label ||
      (option.description !== undefined && typeof option.description !== "string")
    ) {
      throw new AgentEventProtocolError("approval question option 无效");
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
