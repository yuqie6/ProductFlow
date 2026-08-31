/**
 * 单个 Agent-service 进程的交互式 Pi Turn 运行时。
 *
 * 业务权威仍是 PostgreSQL。这里的 session/event 文件只服务当前 Turn，
 * 不能证明后台恢复、副作用对账或多实例 claim。无法证明的副作用记 unknown，
 * 不能猜成 failed。
 */

import { randomUUID } from "node:crypto";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import {
  createAgentSession,
  DefaultResourceLoader,
  ModelRuntime,
  SessionManager,
  SettingsManager,
  type AgentSession,
  type AgentSessionEvent,
  type InlineExtension,
  type ToolDefinition,
} from "@earendil-works/pi-coding-agent";
import type { ImageContent, Model } from "@earendil-works/pi-ai/compat";
import {
  API_VERSION,
  type AgentExecutionLease,
  CONTEXT_SCHEMA_VERSION,
  EVENT_SCHEMA_VERSION,
  MAX_DYNAMIC_CONTEXT_BYTES,
  PI_SDK_VERSION,
  ProductFlowError,
  ProductFlowContract,
  RUNTIME_NAME,
  RuntimeContext,
  RuntimeStatus,
  type CheckpointKind,
  type ExecutionPhase,
  type JsonObject,
  Scope,
  StartTurnInput,
  TurnAnswer,
  TurnArtifact,
  TurnEvent,
  TurnQuestion,
  TurnState,
  type ToolStepDetails,
  type ToolStepKind,
  TurnStatus,
  TOOL_CONTRACT_VERSION,
  resolvedToolContractVersion,
  byteLength,
  isTerminalStatus,
  nowISO,
  questionAnswerToolPayload,
  safeErrorMessage,
  sameRuntimeScope,
  sha256,
  toolKind,
  validatePageContext,
  validateScope,
} from "./contracts.js";
import { Config } from "./config.js";
import { ProductFlowClient } from "./productflow.js";
import { loadRuntimePolicy } from "./runtime-policy.js";
import { PRODUCTFLOW_SKILL_TOOL_NAME, SkillCatalog } from "./skills.js";
import { RuntimeError, TurnStore } from "./store.js";
import { createProductFlowTools, ToolRuntime } from "./tools.js";
import {
  compactAskUserSummary,
  continueAgentSession,
  injectAskUserToolResult,
  storedAnswerFromEvents,
  QUESTION_WAIT_EXPIRED_MESSAGE,
} from "./question-resume.js";
import {
  isDurableTurnEventKind,
  normalizeAssistantMessageEvent,
  reportUnknownPiAssistantEvent,
  type AssistantFinishPayload,
} from "./pi-chunks.js";
import {
  applyThinkingEvent,
  createThinkingProjectionState,
  type ThinkingAssistantEvent,
  type ThinkingProjectionState,
} from "./thinking-projection.js";

const RUNTIME_VERSION = "0.1.0";
const MAX_INPUT_TEXT_BYTES = 64 << 10;
const RUNTIME_POLICY = loadRuntimePolicy();

interface ProviderRequestOptions {
  reasoningSummary: string | null;
  textVerbosity: string | null;
  serviceTier: string | null;
}

interface ProviderRequestBoundary {
  beforeRequest(): Promise<string>;
  currentRequestID(): string | undefined;
}

export interface RuntimeLookup {
  conversationID?: string;
  taskID?: string;
}

export interface StartRequest {
  lookup: RuntimeLookup;
  input: StartTurnInput;
  // ProductFlow 把尚未进模型的 queued Turn 交给另一实例时，可能带上原 harness Turn ID。
  turnID?: string;
}

export class PiRuntimeManager {
  private readonly runs = new Map<string, RunRuntime>();
  private readonly pending: Array<{ runtime: RunRuntime; turnID: string }> = [];
  private readonly scheduled = new Set<string>();
  readonly instanceID = randomUUID();
  private running = 0;
  private closed = false;

  constructor(
    readonly config: Config,
    readonly store: TurnStore,
    readonly productFlow: ProductFlowClient,
    readonly skills: SkillCatalog,
  ) { }

  /** 只把本地能证明尚未被 claim 的 Turn 重新入队。 */
  async recoverAfterRestart(): Promise<RuntimeRecoverySummary> {
    this.assertOpen();
    const recovered = await this.store.recoverAfterRestart();
    for (const candidate of recovered.queued) {
      const runtime = await this.runtimeFor(candidate.scope);
      this.enqueue(runtime, candidate.turnID);
    }
    return {
      queued_turns: recovered.queued.length,
      deferred_turns: recovered.deferred,
      waiting_input_turns: recovered.waitingInput,
      restored_terminal_turns: recovered.restoredTerminal,
      unknown_turns: recovered.unknown,
    };
  }

  /** 按幂等键创建或回放 Turn；仍是 queued 才入队。 */
  async start(request: StartRequest): Promise<TurnState> {
    this.assertOpen();
    const scope = await this.loadScope(request.lookup);
    validatePageContext(request.input.page_context);
    if (!request.input.input_text.trim() || Buffer.byteLength(request.input.input_text, "utf8") > MAX_INPUT_TEXT_BYTES) {
      throw new RuntimeError(400, "invalid_argument", "input_text is required and exceeds the limit");
    }
    const runtime = await this.runtimeFor(scope);
    const result = await this.store.createTurn(scope, request.input, request.turnID);
    if (result.created || result.state.status === "queued") this.enqueue(runtime, result.state.turn_id);
    return result.state;
  }

  async publishDurableEvent(scope: Scope, event: TurnEvent): Promise<void> {
    const runtime = this.runs.get(scope.run_id);
    if (!runtime) return;
    await runtime.publishDurableEvent(event);
  }

  async get(request: RuntimeLookup, turnID: string): Promise<TurnState> {
    const runtime = await this.runtimeForLookup(request);
    return this.store.getState(runtime.scope.run_id, turnID);
  }

  /**
   * 取消进行中的 Turn。没有进程内等待者的问题在本地取消；
   * 没有等待者的 queued Turn 先 claim 再取消，让 ProductFlow 落到终态 checkpoint。
   */
  async cancel(request: RuntimeLookup, turnID: string): Promise<TurnState> {
    const runtime = await this.runtimeForLookup(request);
    const state = await this.store.getState(runtime.scope.run_id, turnID);
    if (isTerminalStatus(state.status)) return state;
    if (state.status === "requires_input" && !runtime.hasQuestionWaiter(turnID)) {
      return runtime.cancelWaitingInputTurn(turnID);
    }
    if (state.status === "queued" && !runtime.hasQuestionWaiter(turnID) && runtime.canCancelQueuedTurn()) {
      return this.cancelQueuedTurn(runtime, turnID);
    }
    await this.store.updateState(runtime.scope.run_id, turnID, { status: "cancel_requested" });
    await this.store.appendEvent(runtime.scope.run_id, turnID, "turn.cancel_requested", { status: "cancel_requested" });
    if (state.status === "queued") this.enqueue(runtime, turnID);
    runtime.cancel(turnID);
    return this.store.getState(runtime.scope.run_id, turnID);
  }

  /** 只有 queued Turn，或已写入问题答案时才能 resume。活 waiter 在进程内续跑；无 waiter 时用 toolResult 续同一 session。 */
  async resume(request: RuntimeLookup, turnID: string): Promise<TurnState> {
    const runtime = await this.runtimeForLookup(request);
    const state = await this.store.getState(runtime.scope.run_id, turnID);
    if (isTerminalStatus(state.status)) return state;
    if (state.status === "queued" && runtime.hasQuestionWaiter(turnID)) {
      await runtime.resumeQuestion(turnID);
      return this.store.getState(runtime.scope.run_id, turnID);
    }
    if (state.status === "queued") {
      await this.store.appendEvent(runtime.scope.run_id, turnID, "turn.resume_requested", { status: "queued" });
      this.enqueue(runtime, turnID);
      return state;
    }
    if (state.status === "requires_input") {
      if (!runtime.hasQuestionWaiter(turnID)) {
        throw new RuntimeError(409, "not_resumable", "this question is no longer attached to a live Pi turn");
      }
      return state;
    }
    throw new RuntimeError(409, "not_resumable", "the Agent Turn is already running or has no resumable checkpoint");
  }

  async answerQuestion(request: RuntimeLookup, turnID: string, questionID: string, answer: TurnAnswer): Promise<TurnState> {
    const runtime = await this.runtimeForLookup(request);
    return runtime.answerQuestion(turnID, questionID, answer);
  }

  async events(request: RuntimeLookup, turnID: string, after: number): Promise<TurnStateAndEvents> {
    const runtime = await this.runtimeForLookup(request);
    return {
      state: await this.store.getState(runtime.scope.run_id, turnID),
      events: await this.store.events(runtime.scope.run_id, turnID, after),
    };
  }

  async *streamEvents(
    request: RuntimeLookup,
    turnID: string,
    after: number,
    signal: AbortSignal,
  ): AsyncGenerator<TurnEvent | null> {
    const runtime = await this.runtimeForLookup(request);
    const runID = runtime.scope.run_id;
    let cursor = after;
    while (!signal.aborted) {
      const events = await this.store.events(runID, turnID, cursor);
      for (const event of events) {
        cursor = Math.max(cursor, event.sequence);
        yield event;
        const kindStatus = event.kind.startsWith("turn.") ? event.kind.slice("turn.".length) : "";
        if (kindStatus && isTerminalStatus(kindStatus as TurnStatus)) return;
      }
      const state = await this.store.getState(runID, turnID);
      if (isTerminalStatus(state.status)) return;
      const woke = await this.store.waitForEvents(runID, turnID, cursor, {
        signal,
        timeoutMS: this.config.heartbeatIntervalMS,
      });
      if (!woke && !signal.aborted) yield null;
    }
  }

  /** `background_durable_tasks` 固定为 false：本进程只做交互式 Turn。 */
  health(): RuntimeStatus & { active_turns: number; queued_turns: number } {
    return {
      runtime: RUNTIME_NAME,
      runtime_version: RUNTIME_VERSION,
      pi_sdk_version: PI_SDK_VERSION,
      api_version: API_VERSION,
      tool_contract_version: TOOL_CONTRACT_VERSION,
      context_schema_version: CONTEXT_SCHEMA_VERSION,
      skill_catalog_hash: this.skills.hash,
      os_tools: [],
      background_durable_tasks: false,
      active_turns: this.running,
      queued_turns: this.pending.length,
    };
  }

  async close(): Promise<void> {
    this.closed = true;
    for (const runtime of this.runs.values()) runtime.close();
    this.pending.length = 0;
  }

  private async runtimeForLookup(lookup: RuntimeLookup): Promise<RunRuntime> {
    if (!lookup.conversationID && !lookup.taskID) {
      throw new RuntimeError(400, "invalid_argument", "conversation_id or task_id is required");
    }
    return this.runtimeFor(await this.loadScope(lookup));
  }

  private async runtimeFor(scope: Scope): Promise<RunRuntime> {
    const existing = this.runs.get(scope.run_id);
    if (existing) {
      if (!sameRuntimeScope(existing.scope, scope)) {
        throw new RuntimeError(409, "conflict", "runtime scope conflicts with the ProductFlow contract");
      }
      return existing;
    }
    const runtime = new RunRuntime(this, scope);
    this.runs.set(scope.run_id, runtime);
    try {
      await this.store.ensureRun(scope);
      return runtime;
    } catch (error) {
      if (this.runs.get(scope.run_id) === runtime) this.runs.delete(scope.run_id);
      throw error;
    }
  }

  /** 每个 ProductFlow run 同时只跑一个 Turn，其余留在 pending。 */
  private enqueue(runtime: RunRuntime, turnID: string): void {
    const key = `${runtime.scope.run_id}:${turnID}`;
    if (this.closed || this.scheduled.has(key)) return;
    this.scheduled.add(key);
    this.pending.push({ runtime, turnID });
    void this.drain();
  }

  private async drain(): Promise<void> {
    while (!this.closed && this.running < this.config.maxConcurrentTurns && this.pending.length > 0) {
      const index = this.pending.findIndex((candidate) => candidate.runtime.canStartTurn());
      if (index < 0) return;
      const [next] = this.pending.splice(index, 1);
      next.runtime.beginTurn(next.turnID);
      this.running += 1;
      void next.runtime
        .execute(next.turnID)
        .catch(() => undefined)
        .finally(() => {
          next.runtime.endTurn(next.turnID);
          this.running -= 1;
          this.scheduled.delete(`${next.runtime.scope.run_id}:${next.turnID}`);
          void this.drain();
        });
    }
  }

  private async cancelQueuedTurn(runtime: RunRuntime, turnID: string): Promise<TurnState> {
    const pendingIndex = this.pending.findIndex((candidate) => candidate.runtime === runtime && candidate.turnID === turnID);
    if (pendingIndex >= 0) this.pending.splice(pendingIndex, 1);
    runtime.beginTurn(turnID);
    this.running += 1;
    try {
      return await runtime.cancelQueuedTurn(turnID);
    } finally {
      runtime.endTurn(turnID);
      this.running -= 1;
      this.scheduled.delete(`${runtime.scope.run_id}:${turnID}`);
      void this.drain();
    }
  }

  /** Scope 只来自 ProductFlow contract，不来自本地文件。 */
  private async loadScope(lookup: RuntimeLookup): Promise<Scope> {
    const contract = lookup.taskID
      ? await this.productFlow.taskContract(lookup.taskID)
      : await this.productFlow.conversationContract(lookup.conversationID!);
    return scopeFromContract(contract, lookup);
  }

  private assertOpen(): void {
    if (this.closed) throw new RuntimeError(503, "closed", "Agent runtime is shutting down");
  }
}

export interface TurnStateAndEvents {
  state: TurnState;
  events: Awaited<ReturnType<TurnStore["events"]>>;
}

export interface RuntimeRecoverySummary {
  queued_turns: number;
  deferred_turns: number;
  waiting_input_turns: number;
  restored_terminal_turns: number;
  unknown_turns: number;
}

class RunRuntime implements ToolRuntime {
  private modelRuntime?: ModelRuntime;
  private model?: Model<any>;
  private session?: AgentSession;
  private abortController?: AbortController;
  private questionWaiter?: {
    turnID: string;
    questionID: string;
    resolve: (answer: TurnAnswer) => void;
    reject: (error: Error) => void;
    timeout?: ReturnType<typeof setTimeout>;
  };
  private pendingQuestionAnswer?: TurnAnswer;
  private artifact?: TurnArtifact;
  private workflowRunRequested = false;
  private workflowApproval?: JsonObject;
  private output = "";
  private thinkingProjection: ThinkingProjectionState = createThinkingProjectionState();
  private unknownPiEventTypes = new Set<string>();
  private attemptID = "";
  private toolCount = 0;
  private eventChain = Promise.resolve();
  private persistenceError?: Error;
  private iterationError?: Error;
  private modelError?: Error;
  private activeTurnID?: string;
  private executionLease?: AgentExecutionLease;
  private executionPhase: ExecutionPhase = "claimed";
  private executionHeartbeat?: ReturnType<typeof setInterval>;
  private executionStopping = false;
  private executionLeaseError?: Error;
  private effectUnknownError?: string;
  private readonly unknownToolStepIDs = new Set<string>();
  private readonly activeToolStepDetails = new Map<string, ToolStepDetails>();
  private readonly toolStepFailureDetails = new Map<string, ToolStepDetails>();
  private checkpointSequence = 0;
  private modelRequestSequence = 0;
  private currentModelRequestID?: string;
  private currentPageType: string | null = null;
  private providerRequestOptions: ProviderRequestOptions = {
    reasoningSummary: null,
    textVerbosity: null,
    serviceTier: null,
  };

  constructor(
    private readonly manager: PiRuntimeManager,
    readonly scope: Scope,
  ) { }

  get client(): ProductFlowClient {
    return this.manager.productFlow;
  }

  loadSkill(name: string, resourcePath?: string): Promise<string> {
    return this.manager.skills.load(name, resourcePath);
  }

  get signal(): AbortSignal {
    return this.abortController?.signal ?? AbortSignal.timeout(this.manager.config.requestTimeoutMS);
  }

  get pageType(): string | null {
    return this.currentPageType;
  }

  canStartTurn(): boolean {
    return this.activeTurnID === undefined;
  }

  beginTurn(turnID: string): void {
    if (!this.canStartTurn()) throw new Error("ProductFlow run already has an active Pi Turn");
    this.activeTurnID = turnID;
  }

  endTurn(turnID: string): void {
    if (this.activeTurnID === turnID) this.activeTurnID = undefined;
  }

  /**
   * 模型运行前先 claim ProductFlow 执行租约。无法证明的工具副作用记 unknown。
   * 待确认只来自跑图请求或全局素材整理。
   */
  async execute(turnID: string): Promise<void> {
    const initial = await this.manager.store.getState(this.scope.run_id, turnID);
    if (initial.status !== "queued" && initial.status !== "cancel_requested") return;
    this.resetTurnState();
    this.output = initial.output ?? "";
    if (initial.thinking) {
      this.thinkingProjection = { ...createThinkingProjectionState(), text: initial.thinking };
    }
    this.currentTurn = turnID;
    this.abortController = new AbortController();
    let executionClaimed = false;
    try {
      this.executionLease = await this.claimExecution(initial.input.idempotency_key, turnID);
      executionClaimed = true;
      this.attemptID = `${this.executionLease.attempt}:${randomUUID()}`;
      await this.manager.store.updateState(this.scope.run_id, turnID, {
        execution_attempt: this.executionLease.attempt,
        execution_fencing_token: this.executionLease.fencing_token,
      });
      await this.syncDurableEvents(turnID);
      const beforeModel = await this.manager.store.getState(this.scope.run_id, turnID);
      if (beforeModel.status === "cancel_requested" || this.abortController.signal.aborted) {
        await this.updateExecutionPhase("terminal");
        await this.finishTurn(turnID, "canceled", { output: this.output });
        return;
      }
      await this.updateExecutionPhase("model");
      this.startExecutionHeartbeat();
      await this.manager.store.updateState(this.scope.run_id, turnID, {
        status: "running",
        execution_attempt: this.executionLease.attempt,
        execution_fencing_token: this.executionLease.fencing_token,
        started_at: nowISO(),
      });
      await this.manager.store.appendEvent(this.scope.run_id, turnID, "turn.started", { status: "running" });
      const runtimeContext = await this.client.runtimeContext(this.scope.conversation_id, this.scope.task_id, this.signal);
      const images = await this.loadInputImages(initial.input);
      const { session, model } = await this.createSession(turnID, runtimeContext, initial.input.page_context, images);
      this.session = session;
      this.model = model;
      const storedAnswer = await this.storedQuestionAnswer(turnID);
      if (storedAnswer) {
        await this.continueWithStoredQuestionAnswer(session, turnID, storedAnswer);
      } else {
        const prompt = initial.input.asset_ids.length > 0
          ? `${initial.input.input_text}\n\nProductFlow selected asset IDs for this turn (inspect with the matching ProductFlow tool when needed): ${initial.input.asset_ids.join(", ")}`
          : initial.input.input_text;
        await session.prompt(prompt, {
          images: images.length > 0 && model.input.includes("image") ? images : undefined,
          source: "rpc",
        });
      }
      await session.waitForIdle();
      await this.eventChain;
      if (this.persistenceError) throw this.persistenceError;
      if (this.iterationError) throw this.iterationError;
      if (this.modelError) throw this.modelError;
      const current = await this.manager.store.getState(this.scope.run_id, turnID);
      if (current.status === "cancel_requested" || (this.abortController.signal.aborted && !this.workflowRunRequested && !this.artifact)) {
        await this.finishTurn(turnID, "canceled", { output: this.output });
      } else if (this.workflowRunRequested || (this.artifact && this.scope.scope_type === "global")) {
        await this.finishTurn(turnID, "awaiting_confirmation", {
          output: this.output,
          ...(this.artifact ? { artifact: this.artifact } : {}),
          ...(this.workflowApproval ? { approval: this.workflowApproval } : {}),
        });
      } else {
        await this.finishTurn(turnID, "succeeded", { output: this.output });
      }
    } catch (error) {
      if (!executionClaimed) {
        if (error instanceof ProductFlowError && error.status >= 400 && error.status < 500) {
          await this.manager.store.terminal(this.scope.run_id, turnID, "failed", {
            output: this.output,
            error: safeErrorMessage(error),
          });
        }
        return;
      }
      await this.eventChain;
      const current = await this.manager.store.getState(this.scope.run_id, turnID).catch(() => initial);
      if (this.effectUnknownError) {
        await this.finishTurn(turnID, "unknown", {
          output: this.output,
          error: this.effectUnknownError,
        });
      } else if (this.executionLeaseError) {
        await this.finishTurn(turnID, "unknown", {
          output: this.output,
          error: "Agent execution lease was lost before this Turn reached a provable terminal state",
        });
      } else if (error instanceof RuntimeError && error.code === "question_wait_expired") {
        await this.finishTurn(turnID, "unknown", {
          output: this.output,
          error: error.message,
        });
      } else if (this.iterationError) {
        await this.finishTurn(turnID, "failed", {
          output: this.output,
          error: safeErrorMessage(this.iterationError),
        });
      } else if (this.modelError) {
        await this.finishTurn(turnID, "failed", {
          output: this.output,
          error: safeErrorMessage(this.modelError),
        });
      } else if (this.persistenceError) {
        await this.finishTurn(turnID, "unknown", {
          output: this.output,
          error: safeErrorMessage(this.persistenceError),
        });
      } else if (current.status === "cancel_requested" || (this.abortController.signal.aborted && !this.workflowRunRequested && !this.artifact)) {
        await this.finishTurn(turnID, "canceled", { output: this.output });
      } else if (this.workflowRunRequested || (this.artifact && this.scope.scope_type === "global")) {
        await this.finishTurn(turnID, "awaiting_confirmation", {
          output: this.output,
          ...(this.artifact ? { artifact: this.artifact } : {}),
          ...(this.workflowApproval ? { approval: this.workflowApproval } : {}),
        });
      } else {
        await this.finishTurn(turnID, "failed", {
          output: this.output,
          error: safeErrorMessage(error),
        });
      }
    } finally {
      await this.cleanupAfterTurn();
    }
  }

  async cancelQueuedTurn(turnID: string): Promise<TurnState> {
    const initial = await this.manager.store.getState(this.scope.run_id, turnID);
    if (initial.status !== "queued") {
      if (isTerminalStatus(initial.status)) return initial;
      throw new RuntimeError(409, "turn_not_queued", "the Agent Turn is no longer queued");
    }
    this.resetTurnState();
    this.currentTurn = turnID;
    this.abortController = new AbortController();
    let executionClaimed = false;
    try {
      this.executionLease = await this.claimExecution(initial.input.idempotency_key, turnID);
      executionClaimed = true;
      this.attemptID = `${this.executionLease.attempt}:${randomUUID()}`;
      await this.manager.store.updateState(this.scope.run_id, turnID, {
        execution_attempt: this.executionLease.attempt,
        execution_fencing_token: this.executionLease.fencing_token,
      });
      await this.syncDurableEvents(turnID);
      await this.updateExecutionPhase("terminal");
      await this.finishTurn(turnID, "canceled", { output: initial.output });
      return this.manager.store.getState(this.scope.run_id, turnID);
    } catch (error) {
      if (!executionClaimed) throw error;
      await this.eventChain;
      const terminalError = this.effectUnknownError
        ?? (this.executionLeaseError
          ? "Agent execution lease was lost before this Turn reached a provable terminal state"
          : this.persistenceError
            ? safeErrorMessage(this.persistenceError)
            : safeErrorMessage(error));
      await this.finishTurn(turnID, "unknown", { output: initial.output, error: terminalError });
      return this.manager.store.getState(this.scope.run_id, turnID);
    } finally {
      await this.cleanupAfterTurn();
    }
  }

  /** 租约归 ProductFlow。只有尚未证明 claim 的 5xx 才能重试。 */
  private async claimExecution(idempotencyKey: string, turnID: string): Promise<AgentExecutionLease> {
    for (let attempt = 0; attempt < 3; attempt += 1) {
      try {
        return await this.client.claimTurnExecution(
          this.scope.conversation_id,
          {
            task_id: this.scope.task_id,
            idempotency_key: idempotencyKey,
            harness_turn_id: turnID,
            owner_id: this.manager.instanceID,
          },
          this.signal,
        );
      } catch (error) {
        if (!(error instanceof ProductFlowError) || error.status < 500 || attempt === 2) throw error;
        await new Promise<void>((resolve, reject) => {
          let settled = false;
          const onAbort = () => {
            if (settled) return;
            settled = true;
            clearTimeout(timer);
            this.signal.removeEventListener("abort", onAbort);
            reject(new RuntimeError(499, "canceled", "Agent Turn was canceled"));
          };
          const timer = setTimeout(() => {
            if (settled) return;
            settled = true;
            this.signal.removeEventListener("abort", onAbort);
            resolve();
          }, 500 * 2 ** attempt);
          this.signal.addEventListener("abort", onAbort, { once: true });
          timer.unref?.();
        });
      }
    }
    throw new Error("Agent execution claim exhausted retries");
  }

  private async syncDurableEvents(turnID: string): Promise<void> {
    const events = await this.manager.store.events(this.scope.run_id, turnID, 0);
    for (const event of events) {
      if (!isDurableTurnEventKind(event.kind)) continue;
      await this.publishDurableEvent(event);
    }
    if (this.persistenceError) throw this.persistenceError;
  }

  /** 写入 ProductFlow checkpoint；追加失败即丢失租约并中止。 */
  async checkpoint(kind: CheckpointKind, payload: JsonObject): Promise<void> {
    const lease = this.executionLease;
    if (!lease || this.executionLeaseError || this.executionStopping) {
      throw this.executionLeaseError ?? new RuntimeError(409, "execution_unavailable", "Agent execution lease is unavailable");
    }
    const sequence = this.checkpointSequence + 1;
    try {
      await this.client.appendTurnCheckpoint(
        this.scope.conversation_id,
        lease.execution_id,
        {
          owner_id: lease.owner_id,
          lease_token: lease.lease_token,
          sequence,
          kind,
          payload,
        },
        this.signal,
      );
      this.checkpointSequence = sequence;
    } catch (error) {
      this.executionLeaseError = error instanceof Error ? error : new Error("Agent checkpoint persistence failed");
      this.abortController?.abort();
      void this.session?.abort();
      throw this.executionLeaseError;
    }
  }

  /**
   * 先写本地终态快照，再写 ProductFlow checkpoint。
   * checkpoint 或事件发布失败时，快照升级为 unknown。
   */
  private async finishTurn(
    turnID: string,
    status: Extract<TurnStatus, "succeeded" | "failed" | "canceled" | "unknown" | "awaiting_confirmation">,
    details: {
      output?: string;
      error?: string;
      question?: TurnQuestion;
      artifact?: TurnArtifact;
      approval?: JsonObject;
    },
  ): Promise<void> {
    await this.manager.store.terminal(this.scope.run_id, turnID, status, {
      ...details,
      thinking: this.thinkingProjection.text,
    });
    let terminalStatus = status;
    let terminalError = details.error;
    if (this.persistenceError && status !== "unknown") {
      terminalStatus = "unknown";
      terminalError = safeErrorMessage(this.persistenceError);
      await this.recordLocalUnknownTerminal(turnID, details.output ?? "", terminalError);
    }
    this.executionPhase = "terminal";
    try {
      await this.checkpoint("terminal", {
        status: terminalStatus,
        ...(details.output ? { output: details.output } : {}),
        ...(terminalError ? { error: terminalError } : {}),
        ...(details.artifact ? { artifact: details.artifact as unknown as JsonObject } : {}),
      });
    } catch (error) {
      if (terminalStatus !== "unknown") {
        terminalStatus = "unknown";
        terminalError = safeErrorMessage(error);
        await this.recordLocalUnknownTerminal(turnID, details.output ?? "", terminalError);
      }
    }
  }

  private async recordLocalUnknownTerminal(turnID: string, output: string, error: string): Promise<void> {
    await this.manager.store.appendEvent(this.scope.run_id, turnID, "turn.unknown", {
      status: "unknown",
      output,
      error,
    });
    await this.manager.store.updateState(this.scope.run_id, turnID, {
      status: "unknown",
      error,
      finished_at: nowISO(),
    });
  }

  private startExecutionHeartbeat(): void {
    if (this.executionHeartbeat) clearInterval(this.executionHeartbeat);
    this.executionHeartbeat = setInterval(() => {
      void this.updateExecutionPhase(this.executionPhase).catch(() => undefined);
    }, 20_000);
    this.executionHeartbeat.unref?.();
  }

  private async updateExecutionPhase(phase: ExecutionPhase): Promise<void> {
    const lease = this.executionLease;
    if (!lease || this.executionStopping || this.executionLeaseError) return;
    this.executionPhase = phase;
    try {
      const refreshed = await this.client.heartbeatTurnExecution(
        this.scope.conversation_id,
        lease.execution_id,
        {
          owner_id: lease.owner_id,
          lease_token: lease.lease_token,
          phase,
        },
      );
      if (!this.executionStopping && this.executionLease === lease) this.executionLease = refreshed;
    } catch (error) {
      if (this.executionStopping) return;
      this.executionLeaseError = error instanceof Error ? error : new Error("Agent execution lease was lost");
      this.abortController?.abort();
      void this.session?.abort();
      throw this.executionLeaseError;
    }
  }

  private async stopExecutionHeartbeat(): Promise<void> {
    this.executionStopping = true;
    if (this.executionHeartbeat) {
      clearInterval(this.executionHeartbeat);
      this.executionHeartbeat = undefined;
    }
    const lease = this.executionLease;
    this.executionLease = undefined;
    if (!lease) return;
    try {
      await this.client.releaseTurnExecution(this.scope.conversation_id, lease.execution_id, {
        owner_id: lease.owner_id,
        lease_token: lease.lease_token,
        phase: this.executionPhase,
      });
    } catch {
      // 过期租约由 ProductFlow 的 durable 扫描恢复。
    }
  }

  canCancelQueuedTurn(): boolean {
    return this.activeTurnID === undefined;
  }

  cancel(turnID: string): void {
    if (this.currentTurn !== turnID) return;
    if (this.questionWaiter?.turnID === turnID) {
      this.clearQuestionTimeout();
      this.questionWaiter.reject(new RuntimeError(499, "canceled", "Agent Turn was canceled"));
      this.questionWaiter = undefined;
    }
    this.abortController?.abort();
    void this.session?.abort();
  }

  /** 只有没有活着的 Pi 等待者时，才能取消停住的问题。 */
  async cancelWaitingInputTurn(turnID: string): Promise<TurnState> {
    const state = await this.manager.store.getState(this.scope.run_id, turnID);
    if (state.status !== "requires_input" || this.hasQuestionWaiter(turnID)) {
      throw new RuntimeError(409, "question_not_cancellable", "the question is attached to a live Pi turn");
    }
    await this.manager.store.terminal(this.scope.run_id, turnID, "canceled", {
      output: state.output,
    });
    return this.manager.store.getState(this.scope.run_id, turnID);
  }

  close(): void {
    this.abortController?.abort();
    this.session?.dispose();
    this.clearQuestionTimeout();
    this.questionWaiter?.reject(new RuntimeError(503, "closed", "Agent runtime is shutting down"));
    this.questionWaiter = undefined;
  }

  hasQuestionWaiter(turnID: string): boolean {
    return this.questionWaiter?.turnID === turnID;
  }

  async resumeQuestion(turnID: string): Promise<void> {
    await this.updateExecutionPhase("model");
    const waiter = this.questionWaiter;
    const answer = this.pendingQuestionAnswer;
    if (!waiter || waiter.turnID !== turnID || !answer) {
      throw new RuntimeError(409, "not_resumable", "the answered question is no longer attached to a live Pi turn");
    }
    this.pendingQuestionAnswer = undefined;
    this.clearQuestionTimeout();
    await this.manager.store.updateState(this.scope.run_id, turnID, { status: "running", question: undefined });
    await this.manager.store.appendEvent(this.scope.run_id, turnID, "turn.resume_requested", { status: "running" });
    this.questionWaiter = undefined;
    waiter.resolve(answer);
  }

  async askUser(question: TurnQuestion): Promise<TurnAnswer> {
    const state = await this.manager.store.getState(this.scope.run_id, this.currentTurnID());
    if (this.questionWaiter) throw new Error("Pi requested more than one unanswered question");
    const answerPromise = new Promise<TurnAnswer>((resolve, reject) => {
      this.questionWaiter = {
        turnID: state.turn_id,
        questionID: question.id,
        resolve,
        reject,
        timeout: setTimeout(() => {
          void this.expireQuestionWaiter(state.turn_id, question.id);
        }, this.manager.config.questionTimeoutMS),
      };
      this.questionWaiter.timeout?.unref?.();
    });
    await this.updateExecutionPhase("waiting_input");
    await this.checkpoint("question_required", { question: question as unknown as JsonObject });
    await this.manager.store.updateState(this.scope.run_id, state.turn_id, { status: "requires_input", question });
    await this.manager.store.appendEvent(this.scope.run_id, state.turn_id, "turn.requires_input", {
      status: "requires_input",
      question: question as never,
    });
    await this.manager.store.appendEvent(this.scope.run_id, state.turn_id, "question.required", question as never);
    if (this.persistenceError) throw this.persistenceError;
    return answerPromise;
  }

  async answerQuestion(turnID: string, questionID: string, answer: TurnAnswer): Promise<TurnState> {
    const state = await this.manager.store.getState(this.scope.run_id, turnID);
    const waiter = this.questionWaiter;
    if (this.pendingQuestionAnswer && waiter?.turnID === turnID && waiter.questionID === questionID) {
      if (JSON.stringify(this.pendingQuestionAnswer) !== JSON.stringify(answer)) {
        throw new RuntimeError(409, "question_already_answered", "the question already has a different answer");
      }
      return state;
    }
    const stored = await this.storedQuestionAnswer(turnID);
    if (stored) {
      if (JSON.stringify(stored) !== JSON.stringify(answer)) {
        throw new RuntimeError(409, "question_already_answered", "the question already has a different answer");
      }
      return state;
    }
    if (state.status !== "requires_input" || !state.question || state.question.id !== questionID) {
      throw new RuntimeError(409, "question_expired", "the requested question is no longer active");
    }
    if ("skip" in answer && answer.skip) {
      // skip / 超时：不校验选项和文本
    } else if ("option" in answer) {
      const option = answer.option;
      if (option === undefined || !Number.isInteger(option) || option < 0 || option >= state.question.options.length) {
        throw new RuntimeError(400, "invalid_argument", "question option index is out of range");
      }
    } else if (!answer.text.trim() || Buffer.byteLength(answer.text, "utf8") > 4000) {
      throw new RuntimeError(400, "invalid_argument", "question text answer is empty or exceeds the limit");
    }
    if (waiter && (waiter.turnID !== turnID || waiter.questionID !== questionID)) {
      throw new RuntimeError(409, "not_resumable", "the question is no longer attached to a live Pi turn");
    }
    this.clearQuestionTimeout();
    if (waiter) this.pendingQuestionAnswer = answer;
    await this.manager.store.updateState(this.scope.run_id, turnID, { status: "queued", question: undefined });
    await this.manager.store.appendEvent(this.scope.run_id, turnID, "question.answered", {
      question_id: questionID,
      answer: answer as never,
    });
    return this.manager.store.getState(this.scope.run_id, turnID);
  }

  requestApproval(approval: JsonObject): void {
    const kind = typeof approval.approval_kind === "string" ? approval.approval_kind : "";
    if (kind === "artifact") {
      if (this.artifact) throw new Error("Pi proposed more than one ProductFlow artifact in one turn");
      const artifact = approval.artifact;
      if (!artifact || typeof artifact !== "object" || Array.isArray(artifact)) {
        throw new Error("ProductFlow artifact approval is missing artifact payload");
      }
      const record = artifact as JsonObject;
      const value = record.value;
      this.artifact = {
        name: String(record.name ?? ""),
        value: value && typeof value === "object" && !Array.isArray(value) ? value as JsonObject : {},
        step_id: String(record.step_id ?? approval.approval_id ?? ""),
      };
      if (!this.artifact.name || !this.artifact.step_id) {
        throw new Error("ProductFlow artifact approval is incomplete");
      }
    } else {
      this.workflowRunRequested = true;
      this.workflowApproval = approval;
      void this.updateExecutionPhase("external_job").catch(() => undefined);
    }
    // Approval pauses model generation, while the execution signal must remain live
    // long enough to persist tool/result, approval/requested and turn/end.
    void this.session?.abort();
  }

  emitApproval(approval: JsonObject): void {
    const turnID = this.activeTurnID;
    if (!turnID) return;
    this.enqueue(() =>
      this.manager.store.appendEvent(this.scope.run_id, turnID, "approval/requested", approval),
    );
  }

  /** ProductFlow 副作用无法证明时，把 Turn 中止为 unknown。 */
  markEffectUnknown(toolCallID: string, reason = "ProductFlow side effect result is unknown"): void {
    this.effectUnknownError = reason;
    this.unknownToolStepIDs.add(toolCallID);
    this.abortController?.abort();
    void this.session?.abort();
  }

  recordToolFailure(toolCallID: string, details: ToolStepDetails): void {
    this.toolStepFailureDetails.set(toolCallID, details);
  }

  async publishDurableEvent(event: TurnEvent): Promise<void> {
    const lease = this.executionLease;
    // 本 runtime 持有活动 Turn 租约时，run 上可能还有另一个 queued Turn。
    // 那个 queued 事件等自己 claim 后再回放；用当前租约发布会把活动执行 fence 掉。
    if (lease && lease.harness_turn_id !== event.turn_id) return;
    if (!lease) {
      if (event.kind === "turn.queued" || event.kind === "turn.resume_requested" || event.kind === "turn.cancel_requested") {
        return;
      }
      this.persistenceError ??= new Error("Agent event was emitted without an execution lease");
      return;
    }
    try {
      await this.client.appendTurnEvent(
        this.scope.conversation_id,
        lease.execution_id,
        {
          owner_id: lease.owner_id,
          lease_token: lease.lease_token,
          sequence: event.sequence,
          schema_version: event.schema_version,
          run_id: event.run_id,
          turn_id: event.turn_id,
          kind: event.kind,
          payload: event.payload,
          created_at: event.created_at,
        },
        this.signal,
      );
    } catch (error) {
      this.persistenceError ??= error instanceof Error ? error : new Error("Agent event persistence failed");
    }
  }

  /** 与 ProductFlow prepare/apply/reconcile 共用的单次工具调用幂等键。 */
  idempotencyKey(toolCallID: string): string {
    return `pi:${this.scope.run_id}:${this.currentTurnID()}:${toolCallID}`.slice(0, 200);
  }

  private async createSession(
    turnID: string,
    runtimeContext: RuntimeContext,
    pageContext: StartTurnInput["page_context"],
    images: ImageContent[],
  ): Promise<{ session: AgentSession; model: Model<any> }> {
    const { runtime, model, thinkingLevel } = await this.ensureModel();
    const workspace = await this.manager.store.workspace(this.scope.run_id);
    await mkdir(join(workspace, ".pi-agent"), { recursive: true, mode: 0o700 });
    const settingsManager = SettingsManager.inMemory({
      compaction: {
        enabled: true,
        reserveTokens: Math.max(1_024, this.manager.config.modelContextWindow - this.manager.config.autoCompactTokenLimit),
        keepRecentTokens: Math.min(16_000, Math.floor(this.manager.config.autoCompactTokenLimit / 4)),
      },
      httpIdleTimeoutMs: this.manager.config.providerRequestTimeoutMS,
      retry: {
        enabled: false,
        provider: {
          timeoutMs: this.manager.config.providerRequestTimeoutMS,
          maxRetries: 0,
        },
      },
      enableAnalytics: false,
      enableInstallTelemetry: false,
    });
    const staticPrompt = [
      this.scope.system_prompt,
      RUNTIME_POLICY,
      this.scope.task_goal?.trim() ? `Authoritative ProductFlow Task goal:\n${this.scope.task_goal.trim()}` : "",
      `Runtime: ${RUNTIME_NAME}; API contract: ${API_VERSION}; context schema: ${CONTEXT_SCHEMA_VERSION}; skill catalog: ${this.manager.skills.hash}.`,
      this.manager.skills.promptForScope(this.scope.scope_type),
    ]
      .filter(Boolean)
      .join("\n\n");
    const contextStepID = `context_${turnID}`;
    let contextDetails = buildContextStepDetails(this.scope, runtimeContext, pageContext, images.length, this.manager.skills.hash, 0);
    try {
      const dynamicContext = buildDynamicContext(
        runtimeContext,
        pageContext,
        images,
        this.scope,
        this.manager.skills.hash,
      );
      contextDetails = {
        ...contextDetails,
        context_bytes: byteLength(dynamicContext),
      };
      await this.manager.store.setToolStep(this.scope.run_id, turnID, {
        step_id: contextStepID,
        kind: "inject_context",
        summary: "注入本轮运行时、页面和选中图片上下文",
        status: "running",
        tool_name: "productflow_context_injection",
        details: contextDetails,
      });
      const resourceLoader = new DefaultResourceLoader({
        cwd: workspace,
        agentDir: join(workspace, ".pi-agent"),
        settingsManager,
        noExtensions: true,
        noSkills: true,
        extensionFactories: [
          providerRequestExtension(this.providerRequestOptions, {
            beforeRequest: () => this.checkpointModelRequest(),
            currentRequestID: () => this.currentModelRequestID,
          }),
        ],
        noPromptTemplates: true,
        noThemes: true,
        noContextFiles: true,
        systemPrompt: staticPrompt,
        systemPromptOverride: (base) => `${base ?? staticPrompt}\n\n${dynamicContext}`,
      });
      await resourceLoader.reload();
      const sessionManager = SessionManager.continueRecent(workspace, this.manager.store.sessionDir(this.scope.run_id));
      this.currentPageType = pageContext?.page_type?.trim() || null;
      const tools = createProductFlowTools(this);
      const result = await createAgentSession({
        cwd: workspace,
        agentDir: join(workspace, ".pi-agent"),
        modelRuntime: runtime,
        model,
        thinkingLevel,
        noTools: "all",
        tools: tools.map((tool) => tool.name),
        customTools: tools,
        resourceLoader,
        sessionManager,
        settingsManager,
      });
      await this.manager.store.setToolStep(this.scope.run_id, turnID, {
        step_id: contextStepID,
        kind: "inject_context",
        summary: "注入本轮运行时、页面和选中图片上下文",
        status: "succeeded",
        tool_name: "productflow_context_injection",
        details: {
          ...contextDetails,
          output_summary: "已注入有界 Agent contract、Skill catalog、运行时、页面和选中图片上下文。",
        },
      });
      this.bindSessionEvents(result.session, turnID);
      return { session: result.session, model };
    } catch (error) {
      try {
        await this.manager.store.setToolStep(this.scope.run_id, turnID, {
          step_id: contextStepID,
          kind: "inject_context",
          summary: "注入本轮运行时、页面和选中图片上下文",
          status: "failed",
          tool_name: "productflow_context_injection",
          details: {
            ...contextDetails,
            error_code: error instanceof ProductFlowError ? error.code : "context_injection_failed",
            error_message: safeErrorMessage(error),
            retryable: true,
          },
        });
      } catch {
        // 诊断步骤写不进去时，保留最初的会话创建失败
      }
      throw error;
    }
  }

  private bindSessionEvents(session: AgentSession, turnID: string): void {
    session.subscribe((event: AgentSessionEvent) => {
      if (event.type === "message_update") {
        this.projectAssistantMessageEvent(turnID, event.assistantMessageEvent);
        return;
      }
      if (event.type === "message_end" && event.message.role === "assistant" && event.message.stopReason === "error") {
        this.modelError = new Error(event.message.errorMessage || "Pi provider request failed");
        return;
      }
      if (event.type === "tool_execution_start") {
        void this.updateExecutionPhase("tool").catch(() => undefined);
        this.toolCount += 1;
        if (this.toolCount > this.manager.config.maxIterations) {
          this.iterationError = new Error(`Pi Agent exceeded the maximum tool iteration limit of ${this.manager.config.maxIterations}`);
          this.abortController?.abort();
          void session.abort();
          return;
        }
        const details = toolStepDetailsForStart(event.toolName, event.args);
        this.activeToolStepDetails.set(event.toolCallId, details);
        this.enqueue(() =>
          this.manager.store.setToolStep(this.scope.run_id, turnID, {
            step_id: event.toolCallId,
            kind: toolKind(event.toolName),
            summary: toolStepSummary(event.toolName),
            status: "running",
            tool_name: event.toolName,
            ...(details ? { details } : {}),
          }),
        );
        return;
      }
      if (event.type === "tool_execution_end") {
        void this.updateExecutionPhase("model").catch(() => undefined);
      }
      if (event.type === "tool_execution_end") {
        const startedDetails = this.activeToolStepDetails.get(event.toolCallId);
        const failureDetails = this.toolStepFailureDetails.get(event.toolCallId);
        const resultDetails = toolStepDetailsForResult(event.toolName, event.result, event.isError);
        const details = mergeToolStepDetails(startedDetails, resultDetails, failureDetails);
        if (details && event.toolName === "ask_user" && !event.isError) {
          details.output_summary = compactAskUserSummary(event.result, startedDetails?.option_labels ?? []);
        }
        this.activeToolStepDetails.delete(event.toolCallId);
        this.toolStepFailureDetails.delete(event.toolCallId);
        const meta = resultMetaFromToolResult(event.result);
        this.enqueue(() =>
          this.manager.store.setToolStep(this.scope.run_id, turnID, {
            step_id: event.toolCallId,
            kind: toolKind(event.toolName),
            summary: event.toolName === "ask_user" && details?.output_summary
              ? details.output_summary
              : toolStepSummary(event.toolName),
            status: this.unknownToolStepIDs.has(event.toolCallId)
              ? "unknown"
              : event.isError
                ? "failed"
                : "succeeded",
            tool_name: event.toolName,
            ...(details ? { details } : {}),
            ...(meta ? { meta } : {}),
          }),
        );
      }
    });
  }

  private enqueue(operation: () => Promise<unknown>): void {
    this.eventChain = this.eventChain
      .then(operation)
      .catch((error: unknown) => {
        this.persistenceError = error instanceof Error ? error : new Error("Agent event persistence failed");
      })
      .then(() => undefined);
  }

  private async ensureModel(): Promise<{ runtime: ModelRuntime; model: Model<any>; thinkingLevel: ReturnType<typeof thinkingLevel> }> {
    if (this.modelRuntime && this.model) {
      return { runtime: this.modelRuntime, model: this.model, thinkingLevel: thinkingLevel(this.providerReasoningEffort) };
    }
    const provider = await this.client.providerConfig(this.signal);
    if (provider.schema_version !== 1) {
      throw new ProductFlowError(502, "provider_contract_mismatch", "ProductFlow returned an unsupported provider config schema");
    }
    const providerKind = provider.provider_kind.trim();
    const apiKey = this.manager.config.providerAPIKey || provider.api_key.trim();
    const baseURL = this.manager.config.providerBaseURL || provider.base_url;
    const modelID = this.manager.config.providerModel || provider.model.trim();
    const reasoningEffort = this.manager.config.providerReasoningEffort || provider.reasoning_effort;
    if (!providerKind || !modelID) throw new ProductFlowError(502, "provider_config_invalid", "ProductFlow provider configuration is incomplete");
    if (providerKind === "mock") throw new ProductFlowError(503, "provider_unavailable", "mock Agent provider cannot run the Pi production adapter");
    if (providerKind === "openai" && !apiKey) throw new ProductFlowError(503, "provider_config_invalid", "ProductFlow provider configuration has no API key");
    const runtime = await ModelRuntime.create({
      authPath: join(await this.manager.store.workspace(this.scope.run_id), ".pi-agent", "auth.json"),
      modelsPath: join(await this.manager.store.workspace(this.scope.run_id), ".pi-agent", "models.json"),
      allowModelNetwork: false,
    });
    const api = providerApi(providerKind);
    runtime.registerProvider(providerKind, {
      ...(baseURL ? { baseUrl: baseURL } : {}),
      api,
    });
    if (apiKey) await runtime.setRuntimeApiKey(providerKind, apiKey, { allowNetwork: false });
    let model = runtime.getModel(providerKind, modelID);
    if (!model) {
      runtime.registerProvider(providerKind, {
        ...(baseURL ? { baseUrl: baseURL } : {}),
        api,
        models: [
          {
            id: modelID,
            name: modelID,
            api,
            reasoning: true,
            input: ["text", "image"],
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            contextWindow: this.manager.config.modelContextWindow,
            maxTokens: Math.min(32_000, this.manager.config.modelContextWindow),
          },
        ],
      });
      model = runtime.getModel(providerKind, modelID);
    }
    if (!model) throw new ProductFlowError(502, "model_unavailable", `Pi cannot resolve model ${providerKind}/${modelID}`);
    this.providerReasoningEffort = reasoningEffort;
    this.providerRequestOptions = {
      reasoningSummary: this.manager.config.providerReasoningSummary || provider.reasoning_summary,
      textVerbosity: this.manager.config.providerTextVerbosity || provider.text_verbosity,
      serviceTier: this.manager.config.providerServiceTier || provider.service_tier,
    };
    this.modelRuntime = runtime;
    this.model = model;
    return { runtime, model, thinkingLevel: thinkingLevel(reasoningEffort) };
  }

  private async checkpointModelRequest(): Promise<string> {
    const lease = this.executionLease;
    if (!lease) throw new RuntimeError(409, "execution_unavailable", "Agent execution lease is unavailable");
    const sequence = this.modelRequestSequence + 1;
    const requestID = `model:${sha256(
      `${this.scope.run_id}:${this.currentTurnID()}:${lease.attempt}:${lease.fencing_token}:${sequence}`,
    ).slice(0, 32)}`;
    await this.checkpoint("before_model_request", {
      attempt: lease.attempt,
      fencing_token: lease.fencing_token,
      model_request_id: requestID,
      model_request_sequence: sequence,
    });
    this.modelRequestSequence = sequence;
    this.currentModelRequestID = requestID;
    return requestID;
  }

  private providerReasoningEffort: string | null = null;

  private async loadInputImages(input: StartTurnInput): Promise<ImageContent[]> {
    const images: ImageContent[] = [];
    let totalBytes = 0;
    for (const assetID of input.asset_ids) {
      const content = await this.client.assetContent(
        this.scope.conversation_id,
        assetID,
        this.scope.scope_type === "global",
        this.signal,
      );
      totalBytes += content.sizeBytes;
      if (totalBytes > 20 << 20) throw new ProductFlowError(413, "image_limit", "selected assets exceed the Turn image byte limit");
      images.push({ type: "image", data: content.data, mimeType: content.mediaType });
    }
    return images;
  }

  private currentTurnID(): string {
    const current = this.currentTurn;
    if (!current) throw new Error("Pi tool executed outside an active turn");
    return current;
  }

  private currentTurn?: string;

  private clearQuestionTimeout(): void {
    const timeout = this.questionWaiter?.timeout;
    if (timeout !== undefined) clearTimeout(timeout);
    if (this.questionWaiter) this.questionWaiter.timeout = undefined;
  }

  private async expireQuestionWaiter(turnID: string, questionID: string): Promise<void> {
    const waiter = this.questionWaiter;
    if (!waiter || waiter.turnID !== turnID || waiter.questionID !== questionID) return;
    if (this.pendingQuestionAnswer) return;
    const stored = await this.storedQuestionAnswer(turnID);
    if (stored || this.pendingQuestionAnswer) return;
    if (this.questionWaiter !== waiter) return;
    const state = await this.manager.store.getState(this.scope.run_id, turnID);
    if (state.status !== "requires_input") return;
    this.clearQuestionTimeout();
    this.questionWaiter = undefined;
    this.pendingQuestionAnswer = undefined;
    await this.updateExecutionPhase("model");
    await this.manager.store.updateState(this.scope.run_id, turnID, { status: "running", question: undefined });
    await this.manager.store.appendEvent(this.scope.run_id, turnID, "question.answered", {
      question_id: questionID,
      answer: { skip: true },
      status: "no_answer",
    });
    waiter.resolve({ skip: true });
  }

  private async storedQuestionAnswer(turnID: string): Promise<TurnAnswer | null> {
    const events = await this.manager.store.events(this.scope.run_id, turnID, 0);
    return storedAnswerFromEvents(events);
  }

  private async continueWithStoredQuestionAnswer(
    session: AgentSession,
    turnID: string,
    answer: TurnAnswer,
  ): Promise<void> {
    const events = await this.manager.store.events(this.scope.run_id, turnID, 0);
    const answered = [...events].reverse().find((event) => event.kind === "question.answered");
    const questionID = typeof answered?.payload.question_id === "string"
      ? answered.payload.question_id
      : "";
    if (!questionID) {
      throw new RuntimeError(409, "question_wait_expired", QUESTION_WAIT_EXPIRED_MESSAGE);
    }
    const toolCallId = await injectAskUserToolResult(session, questionID, answer);
    if (toolCallId) {
      await this.manager.store.setToolStep(this.scope.run_id, turnID, {
        step_id: toolCallId,
        kind: toolKind("ask_user"),
        summary: compactAskUserSummary(questionAnswerToolPayload(answer), []),
        status: "succeeded",
        tool_name: "ask_user",
      });
    }
    await continueAgentSession(session);
  }

  private projectAssistantMessageEvent(turnID: string, raw: { type: string }): void {
    const mapped = normalizeAssistantMessageEvent(raw);
    if (mapped.action === "unknown") {
      if (!this.unknownPiEventTypes.has(mapped.type)) {
        this.unknownPiEventTypes.add(mapped.type);
        reportUnknownPiAssistantEvent(mapped.type);
      }
      return;
    }
    if (mapped.action === "ignore") return;
    if (mapped.action === "thinking") {
      this.projectThinking(turnID, mapped.event);
      return;
    }
    if (mapped.action === "text.delta") {
      this.output += mapped.delta;
      this.enqueue(async () => {
        await this.manager.store.appendEvent(this.scope.run_id, turnID, "text.delta", {
          delta: mapped.delta,
          step_id: `pi_${turnID}`,
          attempt_id: this.attemptID,
          content_index: mapped.contentIndex,
        });
        await this.manager.store.updateState(this.scope.run_id, turnID, { output: this.output });
      });
      return;
    }
    const finish: AssistantFinishPayload = {
      reason: mapped.reason,
      attempt_id: this.attemptID,
      ...(mapped.usage ? { usage: mapped.usage } : {}),
    };
    this.enqueue(() => this.manager.store.appendEvent(this.scope.run_id, turnID, "assistant.finish", finish as never));
  }

  private projectThinking(turnID: string, event: ThinkingAssistantEvent): void {
    const { state, emit } = applyThinkingEvent(this.thinkingProjection, event);
    this.thinkingProjection = state;
    if (!emit) return;
    this.enqueue(async () => {
      await this.manager.store.appendEvent(this.scope.run_id, turnID, "thinking.delta", {
        delta: emit.delta,
        step_id: `pi_${turnID}`,
        attempt_id: this.attemptID,
        content_index: emit.content_index,
        ...(emit.truncated ? { truncated: true } : {}),
      });
      await this.manager.store.updateState(this.scope.run_id, turnID, { thinking: this.thinkingProjection.text });
    });
  }

  private resetTurnState(): void {
    this.output = "";
    this.thinkingProjection = createThinkingProjectionState();
    this.unknownPiEventTypes.clear();
    this.artifact = undefined;
    this.executionLease = undefined;
    this.checkpointSequence = 0;
    this.modelRequestSequence = 0;
    this.currentModelRequestID = undefined;
    this.executionPhase = "claimed";
    this.executionStopping = false;
    this.executionLeaseError = undefined;
    this.effectUnknownError = undefined;
    this.unknownToolStepIDs.clear();
    this.activeToolStepDetails.clear();
    this.toolStepFailureDetails.clear();
    if (this.executionHeartbeat) {
      clearInterval(this.executionHeartbeat);
      this.executionHeartbeat = undefined;
    }
    this.workflowRunRequested = false;
    this.workflowApproval = undefined;
    this.currentPageType = null;
    this.attemptID = randomUUID();
    this.toolCount = 0;
    this.persistenceError = undefined;
    this.iterationError = undefined;
    this.modelError = undefined;
    this.eventChain = Promise.resolve();
  }

  private async cleanupAfterTurn(): Promise<void> {
    await this.stopExecutionHeartbeat();
    this.questionWaiter?.reject(new RuntimeError(409, "turn_finished", "Agent Turn finished before the question was answered"));
    this.clearQuestionTimeout();
    this.questionWaiter = undefined;
    this.pendingQuestionAnswer = undefined;
    this.iterationError = undefined;
    this.session?.dispose();
    this.session = undefined;
    this.abortController = undefined;
    this.model = undefined;
    this.currentTurn = undefined;
  }
}

function scopeFromContract(contract: ProductFlowContract, lookup: RuntimeLookup): Scope {
  const expectedVersion = resolvedToolContractVersion(contract.draft_schema);
  if (contract.schema_version !== 1 || contract.tool_contract_version !== expectedVersion) {
    throw new RuntimeError(
      502,
      "contract_mismatch",
      `ProductFlow Agent contract mismatch: schema_version=${contract.schema_version} tool_contract_version=${contract.tool_contract_version}`,
    );
  }
  const conversationID = contract.conversation_id.trim();
  const taskID = contract.task_id?.trim() || null;
  if (lookup.conversationID && conversationID !== lookup.conversationID) {
    throw new RuntimeError(502, "scope_mismatch", "ProductFlow returned a different conversation scope");
  }
  if (lookup.taskID && taskID !== lookup.taskID) {
    throw new RuntimeError(502, "scope_mismatch", "ProductFlow returned a different task scope");
  }
  if (contract.scope_type !== "global" && contract.scope_type !== "product_workflow") {
    throw new RuntimeError(502, "contract_mismatch", `ProductFlow returned an invalid Agent scope_type ${contract.scope_type}`);
  }
  const scope: Scope = {
    schema_version: 1,
    scope_type: contract.scope_type,
    conversation_id: conversationID,
    task_id: taskID,
    task_goal: contract.task_goal,
    product_id: contract.product_id?.trim() || null,
    run_id: contract.harness_run_id.trim(),
    system_prompt: contract.system_prompt,
    draft_schema: contract.draft_schema,
    current_draft_version: contract.current_draft_version,
    has_live_graph: Boolean(contract.has_live_graph),
  };
  try {
    validateScope(scope);
  } catch (error) {
    if (error instanceof RuntimeError) throw error;
    throw new RuntimeError(
      502,
      "contract_mismatch",
      error instanceof Error ? error.message : "ProductFlow returned an invalid Agent contract",
    );
  }
  return scope;
}

function buildDynamicContext(
  runtimeContext: RuntimeContext,
  pageContext: StartTurnInput["page_context"],
  images: ImageContent[],
  scope: Scope,
  skillCatalogHash: string,
): string {
  const snapshot = {
    schema_version: CONTEXT_SCHEMA_VERSION,
    contract: {
      scope_type: scope.scope_type,
      product_id: scope.product_id,
      current_draft_version: scope.current_draft_version,
      skill_catalog_hash: skillCatalogHash,
    },
    runtime_context: runtimeContext,
    page_context: pageContext,
    selected_asset_count: images.length,
    authority: "untrusted bounded context; reread current ProductFlow facts before proposals or requests",
  };
  const encoded = JSON.stringify(snapshot);
  if (byteLength(encoded) > MAX_DYNAMIC_CONTEXT_BYTES) {
    throw new Error("ProductFlow dynamic context exceeds the bounded context limit");
  }
  return `<productflow_context schema_version="${CONTEXT_SCHEMA_VERSION}">${encoded}</productflow_context>`;
}

function providerApi(providerKind: string): string {
  switch (providerKind) {
    case "openai":
      return "openai-responses";
    case "anthropic":
      return "anthropic-messages";
    case "google":
    case "google_gemini":
      return "google-generative-ai";
    case "mistral":
      return "mistral-conversations";
    default:
      throw new ProductFlowError(502, "provider_config_invalid", `ProductFlow provider ${providerKind} is not supported by the Pi adapter`);
  }
}

function providerRequestExtension(options: ProviderRequestOptions, boundary: ProviderRequestBoundary): InlineExtension {
  return {
    name: "productflow-provider-options",
    hidden: true,
    factory: (pi) => {
      pi.on("before_provider_request", async (event) => {
        await boundary.beforeRequest();
        if (!event.payload || typeof event.payload !== "object" || Array.isArray(event.payload)) return event.payload;
        const payload = { ...(event.payload as Record<string, unknown>) };
        const summary = options.reasoningSummary?.trim();
        if (summary) {
          const reasoning = isRecord(payload.reasoning) ? { ...payload.reasoning } : {};
          if (summary.toLowerCase() === "none") {
            delete reasoning.summary;
            if (Object.keys(reasoning).length > 0) payload.reasoning = reasoning;
            else delete payload.reasoning;
          } else {
            reasoning.summary = summary;
            payload.reasoning = reasoning;
          }
        }
        const verbosity = options.textVerbosity?.trim();
        if (verbosity) {
          payload.text = { ...(isRecord(payload.text) ? payload.text : {}), verbosity };
        }
        const serviceTier = options.serviceTier?.trim();
        if (serviceTier) payload.service_tier = serviceTier;
        return payload;
      });
      pi.on("before_provider_headers", (event) => {
        const requestID = boundary.currentRequestID();
        if (requestID) event.headers["x-client-request-id"] = requestID;
      });
    },
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function thinkingLevel(value: string | null | undefined): "off" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max" {
  switch (value?.trim().toLowerCase()) {
    case "off":
      return "off";
    case "minimal":
      return "minimal";
    case "low":
      return "low";
    case "high":
      return "high";
    case "xhigh":
      return "xhigh";
    case "max":
      return "max";
    default:
      return "medium";
  }
}

const TOOL_STEP_SUMMARIES: Record<ToolStepKind, string> = {
  load_skill: "加载版本化 ProductFlow Skill 指令",
  inject_context: "注入本轮 ProductFlow 上下文",
  ask_question: "等待用户回答结构化问题",
  inspect_image: "检查选中的商品图片",
  propose_draft: "提交完整 ProductFlow 草案",
  inspect_context: "读取 ProductFlow 当前上下文",
  read_history: "读取有界历史信息",
  organize_assets: "整理 ProductFlow 素材",
  request_workflow_run: "请求执行工作流并等待确认",
  create_product: "创建商品工作区",
  apply_graph: "立即写入 live graph ChangeSet",
  propose_graph: "提交未应用的图提案",
  focus_canvas: "聚焦 live graph 画布选区",
  expand_intake: "写入商品 intake 并展开出生图",
  discard_proposal: "丢弃未应用的图提案",
  cancel_run: "取消进行中的工作流运行",
};

function toolStepSummary(name: string): string {
  return TOOL_STEP_SUMMARIES[toolKind(name)];
}

function buildContextStepDetails(
  scope: Scope,
  runtimeContext: RuntimeContext,
  pageContext: StartTurnInput["page_context"],
  selectedAssetCount: number,
  skillCatalogHash: string,
  contextBytes: number,
): ToolStepDetails {
  return {
    phase: "context_injection",
    context_sections: ["productflow_contract", "skill_catalog", "runtime_context", "page_context", "selected_assets"],
    runtime_context_keys: Object.keys(runtimeContext).sort().slice(0, 32),
    contract_fields: [
      "scope_type",
      "product_id",
      "current_draft_version",
      "skill_catalog_hash",
    ],
    ...(pageContext ? { page_route: pageContext.route, page_type: pageContext.page_type } : {}),
    selected_asset_count: selectedAssetCount,
    ...(pageContext ? { visible_asset_count: pageContext.visible_asset_ids.length } : {}),
    context_bytes: contextBytes,
    input_summary: `注入 ${scope.scope_type} contract、Skill catalog ${skillCatalogHash.slice(0, 12)} 和当前页面摘要。`,
  };
}

function toolStepDetailsForStart(name: string, args: unknown): ToolStepDetails {
  const argumentsObject = isRecord(args) ? args : {};
  switch (toolKind(name)) {
    case "load_skill":
      return {
        phase: "skill_load",
        skill_name: safeDetailString(argumentsObject.skill_name, 64),
        resource_path: safeDetailString(argumentsObject.resource_path, 256),
        input_summary: "读取一个精确匹配的版本化 Skill 或静态参考。",
      };
    case "ask_question": {
      const options = Array.isArray(argumentsObject.options)
        ? argumentsObject.options.flatMap((option) => {
          if (!isRecord(option)) return [];
          const label = safeDetailString(option.label, 80);
          return label ? [label] : [];
        })
        : [];
      return {
        phase: "question",
        question_header: safeDetailString(argumentsObject.header, 32),
        question_text: safeDetailString(argumentsObject.question, 2000, true),
        ...(options.length ? { option_labels: options.slice(0, 5) } : {}),
        input_summary: "向用户提出一个会影响结果的结构化选择。",
      };
    }
    case "inspect_context":
      return { phase: "tool_result", input_summary: "读取当前 ProductFlow 商品、事实、草案或运行上下文。" };
    case "inspect_image":
      return { phase: "tool_result", input_summary: "读取明确选中的已核验图片信息。" };
    case "propose_draft":
      return { phase: "tool_result", input_summary: "提交完整草案，由 ProductFlow Schema 和业务规则校验。" };
    case "read_history":
      return { phase: "tool_result", input_summary: "读取有界的历史摘要或归档信息。" };
    case "organize_assets":
      return { phase: "tool_result", input_summary: "准备或执行受限的素材整理操作。" };
    case "request_workflow_run":
      return { phase: "tool_result", input_summary: "准备工作流执行请求，等待用户确认。" };
    case "create_product":
      return { phase: "tool_result", input_summary: "创建商品工作区。" };
    case "apply_graph":
      return { phase: "tool_result", input_summary: "立即把 Graph Command 写入 live graph。" };
    case "propose_graph":
      return { phase: "tool_result", input_summary: "提交未应用的图提案，等待确认。" };
    case "focus_canvas":
      return { phase: "tool_result", input_summary: "请求画布聚焦到指定节点、边或分组。" };
    case "expand_intake":
      return { phase: "tool_result", input_summary: "写入商品 intake 并展开摄影/信息图模板。" };
    case "discard_proposal":
      return { phase: "tool_result", input_summary: "丢弃当前未应用的图提案。" };
    case "cancel_run":
      return { phase: "tool_result", input_summary: "取消仍在运行的工作流。" };
    case "inject_context":
      return { phase: "context_injection" };
    default:
      return { phase: "tool_result", input_summary: `执行 ProductFlow 工具 ${name}。` };
  }
}

export function toolStepDetailsForResult(name: string, result: unknown, isError: boolean): ToolStepDetails | undefined {
  const resultObject = isRecord(result) ? result : {};
  const resultDetails = isRecord(resultObject.details) ? resultObject.details : {};
  const kind = toolKind(name);
  if (isError) {
    return {
      phase: kind === "ask_question" ? "question" : kind === "load_skill" ? "skill_load" : "tool_result",
      output_summary: "工具调用失败，详情见错误信息。",
    };
  }
  const journalMeta = projectJournalMeta(resultDetails);
  switch (kind) {
    case "load_skill": {
      const instructionExcerpt = safeDetailString(resultDetails.instruction_excerpt, 12_000, true);
      return {
        ...journalMeta,
        phase: "skill_load",
        ...(safeDetailString(resultDetails.skill_name, 64) ? { skill_name: safeDetailString(resultDetails.skill_name, 64) } : {}),
        ...(safeDetailString(resultDetails.resource_path, 256)
          ? { resource_path: safeDetailString(resultDetails.resource_path, 256) }
          : {}),
        ...(instructionExcerpt ? { instruction_excerpt: instructionExcerpt } : {}),
        ...(typeof resultDetails.instruction_truncated === "boolean"
          ? { instruction_truncated: resultDetails.instruction_truncated }
          : {}),
        output_summary: "已加载版本化 Skill 指令；完整内容已提供给模型。",
      };
    }
    case "ask_question":
      return {
        ...journalMeta,
        phase: "question",
        ...(safeDetailString(resultDetails.question_id, 120)
          ? { question_id: safeDetailString(resultDetails.question_id, 120) }
          : {}),
        output_summary: compactAskUserSummary(result, []),
      };
    case "inspect_context": {
      const includesNodeCatalog =
        name === "get_product_workflow_context_v1" || name === "inspect_global_workflow_context_v1";
      return {
        ...journalMeta,
        phase: "tool_result",
        ...(includesNodeCatalog
          ? {
            context_sections: [
              "product_facts",
              "intake",
              "live_graph",
              "verified_reference_assets",
              "node_catalog",
            ],
          }
          : {}),
        output_summary: includesNodeCatalog
          ? "已读取当前商品事实、intake、live graph、参考资产和 Node Catalog config_fields；Inspector 与节点配置写入以此为唯一来源。"
          : "已读取有界 ProductFlow 上下文。",
      };
    }
    case "inspect_image":
      return { ...journalMeta, phase: "tool_result", output_summary: "已读取选中图片的有界检查结果。" };
    case "propose_draft":
      return { ...journalMeta, phase: "tool_result", output_summary: "后端已接受完整草案，当前等待用户确认。" };
    case "read_history":
      return { ...journalMeta, phase: "tool_result", output_summary: "已读取有界历史摘要。" };
    case "organize_assets":
      return { ...journalMeta, phase: "tool_result", output_summary: "素材操作已返回 ProductFlow 结果。" };
    case "request_workflow_run":
      return { ...journalMeta, phase: "tool_result", output_summary: "执行请求已准备，当前等待用户确认。" };
    case "create_product":
      return { ...journalMeta, phase: "tool_result", output_summary: "商品工作区创建结果已返回。" };
    case "apply_graph":
      return { ...journalMeta, phase: "tool_result", output_summary: "Graph Command 已写入 live graph。" };
    case "propose_graph":
      return { ...journalMeta, phase: "tool_result", output_summary: "图提案已作为未应用幽灵预览提交。" };
    case "focus_canvas":
      return { ...journalMeta, phase: "tool_result", output_summary: "画布聚焦请求已记录。" };
    case "expand_intake":
      return { ...journalMeta, phase: "tool_result", output_summary: "商品 intake 已写入，出生图已展开。" };
    case "discard_proposal":
      return { ...journalMeta, phase: "tool_result", output_summary: "未应用的图提案已丢弃。" };
    case "cancel_run":
      return { ...journalMeta, phase: "tool_result", output_summary: "工作流取消请求已提交。" };
    case "inject_context":
      return { ...journalMeta, phase: "context_injection", output_summary: "上下文已注入模型会话。" };
  }
}

function projectJournalMeta(details: Record<string, unknown>): ToolStepDetails {
  const projected: ToolStepDetails = {};
  if (typeof details.truncated === "boolean") projected.truncated = details.truncated;
  if (typeof details.pending_confirmation === "boolean") projected.pending_confirmation = details.pending_confirmation;
  if (typeof details.reconciled === "boolean") projected.reconciled = details.reconciled;
  if (details.response_format === "concise" || details.response_format === "detailed") {
    projected.response_format = details.response_format;
  }
  copyJournalInteger(projected, details, "item_count", 128);
  copyJournalInteger(projected, details, "node_count", 10_000);
  copyJournalInteger(projected, details, "group_count", 10_000);
  copyJournalInteger(projected, details, "asset_count", 100);
  copyJournalInteger(projected, details, "expected_workflow_revision", 1_000_000, 1);
  const workflowID = safeDetailString(details.workflow_id, 64);
  if (workflowID) projected.workflow_id = workflowID;
  const workflowTitle = safeDetailString(details.workflow_title, 240);
  if (workflowTitle) projected.workflow_title = workflowTitle;
  const summary = safeDetailString(details.summary, 240);
  if (summary) projected.summary = summary;
  const runID = safeDetailString(details.run_id, 64);
  if (runID) projected.run_id = runID;
  const proposalID = safeDetailString(details.proposal_id, 64);
  if (proposalID) projected.proposal_id = proposalID;
  const productID = safeDetailString(details.product_id, 64);
  if (productID) projected.product_id = productID;
  const requestID = safeDetailString(details.request_id, 64);
  if (requestID) projected.request_id = requestID;
  const artifactName = safeDetailString(details.artifact_name, 120);
  if (artifactName) projected.artifact_name = artifactName;
  if (typeof details.product_workspace_created === "boolean") {
    projected.product_workspace_created = details.product_workspace_created;
  }
  const operations = journalStringList(details.operation_summaries, 16, 160, false);
  if (operations) projected.operation_summaries = operations;
  const nodes = journalStringList(details.affected_node_ids, 20, 80, true);
  if (nodes) projected.affected_node_ids = nodes;
  const edges = journalStringList(details.affected_edge_ids, 20, 80, true);
  if (edges) projected.affected_edge_ids = edges;
  const groups = journalStringList(details.affected_group_ids, 20, 80, true);
  if (groups) projected.affected_group_ids = groups;
  return projected;
}

function copyJournalInteger(
  target: ToolStepDetails,
  details: Record<string, unknown>,
  key: "item_count" | "node_count" | "group_count" | "asset_count" | "expected_workflow_revision",
  maximum: number,
  minimum = 0,
): void {
  const value = details[key];
  if (typeof value === "number" && Number.isInteger(value) && value >= minimum && value <= maximum) {
    target[key] = value;
  }
}

function journalStringList(
  value: unknown,
  maxItems: number,
  maxLength: number,
  unique: boolean,
): string[] | undefined {
  if (!Array.isArray(value) || value.length === 0) return undefined;
  const items = value.flatMap((item) => {
    if (typeof item !== "string") return [];
    const trimmed = item.trim();
    if (!trimmed || trimmed.length > maxLength || /[\r\n]/u.test(trimmed)) return [];
    return [trimmed];
  }).slice(0, maxItems);
  const result = unique ? [...new Set(items)] : items;
  return result.length ? result : undefined;
}

function resultMetaFromToolResult(result: unknown): JsonObject | undefined {
  if (!isRecord(result) || !isRecord(result.details)) return undefined;
  const projected = projectJournalMeta(result.details);
  return Object.keys(projected).length ? projected as JsonObject : undefined;
}

function mergeToolStepDetails(
  started: ToolStepDetails | undefined,
  result: ToolStepDetails | undefined,
  failure: ToolStepDetails | undefined,
): ToolStepDetails | undefined {
  if (!started && !result && !failure) return undefined;
  const phase = started?.phase ?? result?.phase ?? failure?.phase;
  return {
    ...started,
    ...result,
    ...failure,
    ...(phase ? { phase } : {}),
  };
}

function safeDetailString(value: unknown, maximum: number, allowNewline = false): string | undefined {
  if (
    typeof value !== "string" ||
    !value.trim() ||
    value.length > maximum ||
    (!allowNewline && /[\r\n]/u.test(value))
  ) {
    return undefined;
  }
  return value;
}
