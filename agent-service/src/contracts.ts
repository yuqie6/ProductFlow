/**
 * Pi 运行时与 ProductFlow Agent 适配器共用的线上合同。
 *
 * unknown 是终态：运行时无法证明成功或失败。
 * `harness_run_id` 是 AgentSession/task run id 的兼容列名。
 * 页面上下文只是环境快照，不得改写任务目标。
 */

import { createHash } from "node:crypto";

export const API_VERSION = "v1alpha1" as const;
export const EVENT_SCHEMA_VERSION = 1 as const;
export const RUNTIME_NAME = "productflow-pi" as const;
export const PI_SDK_VERSION = "0.83.0" as const;
export const TOOL_CONTRACT_VERSION = 15 as const;
export const CONTEXT_SCHEMA_VERSION = 1 as const;
/** 必须与后端 AGENT_CONTEXT_MAX_BYTES 对齐。 */
export const MAX_PRODUCT_CONTEXT_BYTES = 512 << 10;
export const MAX_DYNAMIC_CONTEXT_BYTES = 64 << 10;

/** unknown 是终态：运行时无法证明成功或失败。 */
export const TURN_STATUSES = [
  "queued",
  "running",
  "requires_input",
  "awaiting_confirmation",
  "succeeded",
  "failed",
  "cancel_requested",
  "canceled",
  "unknown",
] as const;
export type TurnStatus = (typeof TURN_STATUSES)[number];

export const EXECUTION_PHASES = ["claimed", "model", "tool", "waiting_input", "external_job", "terminal"] as const;
export type ExecutionPhase = (typeof EXECUTION_PHASES)[number];

/** ProductFlow 签发的租约。fencing_token 让丢失所有权的写入失效。 */
export interface AgentExecutionLease {
  execution_id: string;
  projection_id: string;
  harness_turn_id: string;
  owner_id: string;
  lease_token: string;
  attempt: number;
  fencing_token: number;
  phase: ExecutionPhase;
  lease_expires_at: string;
}

export const CHECKPOINT_KINDS = [
  "before_model_request",
  "tool_effect_intent",
  "tool_effect_result",
  "question_required",
  "external_job_submitted",
  "terminal",
] as const;
export type CheckpointKind = (typeof CHECKPOINT_KINDS)[number];

export interface AgentCheckpointReceipt {
  id: string;
  projection_id: string;
  execution_id: string;
  attempt: number;
  fencing_token: number;
  sequence: number;
  kind: CheckpointKind;
  created_at: string;
}

export interface AgentEventReceipt {
  id: string;
  projection_id: string;
  execution_id: string;
  sequence: number;
  schema_version: 1;
  kind: string;
  created_at: string;
}

export const TOOL_STEP_KINDS = [
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
  "apply_graph",
  "propose_graph",
] as const;
export type ToolStepKind = (typeof TOOL_STEP_KINDS)[number];

export const TOOL_STEP_STATUSES = ["running", "succeeded", "failed", "unknown"] as const;
export type ToolStepStatus = (typeof TOOL_STEP_STATUSES)[number];

export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };
export type JsonObject = { [key: string]: JsonValue };

export interface PageContext {
  snapshot_id: string;
  route: string;
  page_type: string;
  product_id: string | null;
  workflow_id: string | null;
  selected_asset_ids: string[];
  visible_asset_ids: string[];
  filters: Record<string, string>;
  workflow_revision: number | null;
  library_revision: number | null;
  digest: string;
  captured_at: string;
}

export interface RuntimeContext {
  schema_version: number;
  session_id: string;
  conversation_id: string;
  task_id: string | null;
  session_summary: string | null;
  task_summary: string | null;
  [key: string]: JsonValue;
}

export interface ProviderConfig {
  schema_version: number;
  provider_kind: string;
  api_key: string;
  base_url: string | null;
  model: string;
  reasoning_effort: string | null;
  reasoning_summary: string | null;
  text_verbosity: string | null;
  service_tier: string | null;
}

export interface ProductFlowContract {
  schema_version: number;
  scope_type: "global" | "product_workflow" | string;
  conversation_id: string;
  task_id: string | null;
  task_goal: string | null;
  product_id: string | null;
  /** AgentSession/task run id 的兼容持久化列名。 */
  harness_run_id: string;
  current_draft_version: number;
  system_prompt: string;
  draft_kind: string;
  draft_schema: JsonObject;
  tool_contract_version: number;
  has_live_graph?: boolean;
}

export interface Scope {
  schema_version: 1;
  scope_type: "global" | "product_workflow";
  conversation_id: string;
  task_id: string | null;
  task_goal: string | null;
  product_id: string | null;
  run_id: string;
  system_prompt: string;
  draft_schema: JsonObject;
  current_draft_version: number;
  has_live_graph: boolean;
}

export interface StartTurnInput {
  input_text: string;
  asset_ids: string[];
  idempotency_key: string;
  page_context: PageContext | null;
}

export interface TurnQuestionOption {
  label: string;
  description?: string;
}

export interface TurnQuestion {
  id: string;
  header: string;
  question: string;
  options: TurnQuestionOption[];
}

export type TurnAnswer = { option: number; text?: never } | { option?: never; text: string };

export interface TurnArtifact {
  name: string;
  value: JsonObject;
  step_id: string;
}

export const TOOL_STEP_DETAIL_PHASES = ["skill_load", "context_injection", "question", "tool_result"] as const;
export type ToolStepDetailPhase = (typeof TOOL_STEP_DETAIL_PHASES)[number];

export interface ToolStepValidationIssue {
  path: string;
  message: string;
}

/** 工具行的有界安全元数据。不要写入原始工具参数或 Draft 载荷。 */
export interface ToolStepDetails {
  phase?: ToolStepDetailPhase;
  skill_name?: string;
  resource_path?: string;
  instruction_excerpt?: string;
  instruction_truncated?: boolean;
  context_sections?: string[];
  runtime_context_keys?: string[];
  contract_fields?: string[];
  page_route?: string;
  page_type?: string;
  selected_asset_count?: number;
  visible_asset_count?: number;
  context_bytes?: number;
  input_summary?: string;
  output_summary?: string;
  error_code?: string;
  error_message?: string;
  retryable?: boolean;
  validation_issues?: ToolStepValidationIssue[];
  question_id?: string;
  question_header?: string;
  question_text?: string;
  option_labels?: string[];
}

export interface ToolStep {
  step_id: string;
  kind: ToolStepKind;
  summary: string;
  status: ToolStepStatus;
  tool_name?: string;
  details?: ToolStepDetails;
}

export interface TurnState {
  api_version: typeof API_VERSION;
  run_id: string;
  turn_id: string;
  status: TurnStatus;
  execution_attempt?: number;
  execution_fencing_token?: number;
  input: StartTurnInput;
  question?: TurnQuestion;
  artifact?: TurnArtifact;
  tool_steps?: ToolStep[];
  output: string;
  error: string;
  created_at: string;
  updated_at: string;
  started_at: string | null;
  finished_at: string | null;
}

export interface TurnEvent {
  schema_version: typeof EVENT_SCHEMA_VERSION;
  run_id: string;
  turn_id: string;
  sequence: number;
  created_at: string;
  kind: string;
  payload: JsonObject;
}

export interface TextDeltaPayload extends JsonObject {
  delta: string;
  step_id: string;
  attempt_id: string;
}

export interface RuntimeStatus {
  runtime: typeof RUNTIME_NAME;
  runtime_version: string;
  pi_sdk_version: typeof PI_SDK_VERSION;
  api_version: typeof API_VERSION;
  tool_contract_version: typeof TOOL_CONTRACT_VERSION;
  context_schema_version: typeof CONTEXT_SCHEMA_VERSION;
  skill_catalog_hash: string;
  os_tools: string[];
  /** main 上固定为 false：后台 durable Task 仍是实验方向。 */
  background_durable_tasks: false;
}

export function sha256(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

export function nowISO(): string {
  return new Date().toISOString();
}

export function byteLength(value: string): number {
  return Buffer.byteLength(value, "utf8");
}

/** 给后续 Turn 用的环境页面快照，不改写任务目标。 */
export function validatePageContext(value: PageContext | null): void {
  if (value === null) return;
  if (!value.snapshot_id.trim() || byteLength(value.snapshot_id) > 64) {
    throw new Error("page_context.snapshot_id is required");
  }
  if (!value.route.trim() || byteLength(value.route) > 512 || !value.page_type.trim() || byteLength(value.page_type) > 80) {
    throw new Error("page_context route and page_type are invalid");
  }
  if (value.selected_asset_ids.length > 100 || value.visible_asset_ids.length > 100) {
    throw new Error("page_context asset IDs exceed the limit");
  }
  if (Object.keys(value.filters).length > 20 || !value.captured_at.trim()) {
    throw new Error("page_context filters or captured_at are invalid");
  }
  if (!/^[0-9a-f]{64}$/u.test(value.digest)) {
    throw new Error("page_context.digest is invalid");
  }
  if (byteLength(JSON.stringify(value)) > MAX_DYNAMIC_CONTEXT_BYTES) {
    throw new Error("page_context exceeds the bounded context limit");
  }
}

export function validateScope(scope: Scope): void {
  if (scope.schema_version !== 1 || !scope.run_id || !scope.conversation_id) {
    throw new Error("ProductFlow returned an invalid Agent contract");
  }
  if (scope.scope_type === "product_workflow") {
    if (!scope.product_id) {
      throw new Error("ProductFlow returned an incomplete product Agent contract");
    }
  } else if (scope.scope_type === "global") {
    if (scope.product_id !== null) {
      throw new Error("ProductFlow global Agent contract must not include product scope IDs");
    }
  } else {
    throw new Error(`ProductFlow returned an invalid Agent scope_type ${scope.scope_type}`);
  }
}

/** run 身份不含合同刷新字段：prompt、live graph、Draft schema 和当前 Draft 版本。 */
export function sameRuntimeScope(left: Scope, right: Scope): boolean {
  return (
    left.schema_version === right.schema_version &&
    left.scope_type === right.scope_type &&
    left.conversation_id === right.conversation_id &&
    left.task_id === right.task_id &&
    left.product_id === right.product_id &&
    left.run_id === right.run_id
  );
}

export function safeErrorMessage(error: unknown): string {
  if (error instanceof ProductFlowError) return error.message.slice(0, 1000);
  if (error instanceof Error) return error.message.slice(0, 1000);
  return "Agent runtime failed";
}

export class ProductFlowError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: JsonObject;

  constructor(status: number, code: string, message: string, details?: JsonObject) {
    super(message);
    this.name = "ProductFlowError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

/** 对本交互运行时，unknown 和 awaiting_confirmation 都是终态。 */
export function isTerminalStatus(status: TurnStatus): boolean {
  return ["succeeded", "failed", "canceled", "unknown", "awaiting_confirmation"].includes(status);
}

export function toolKind(name: string): ToolStepKind {
  if (name === "load_productflow_skill") return "load_skill";
  if (name === "ask_user") return "ask_question";
  if (name === "apply_graph_change_set_v1") return "apply_graph";
  if (name === "propose_graph_change_set_v1" || name === "discard_workflow_proposal_v1" || name.includes("graph_proposal")) return "propose_graph";
  if (name === "cancel_workflow_run_v1") return "request_workflow_run";
  if (name.includes("draft")) return "propose_draft";
  if (name.includes("request_workflow_run")) return "request_workflow_run";
  if (name.includes("workspace")) return "create_product";
  if (name.includes("asset") || name.includes("media")) return name.includes("inspect") ? "inspect_image" : "organize_assets";
  if (name.includes("context") || name.includes("product") || name.includes("workflow")) return "inspect_context";
  if (name.includes("run")) return "read_history";
  return "inspect_context";
}
