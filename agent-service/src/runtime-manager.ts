/**
 * Agent-service 进程级 Turn 调度器。
 *
 * 这里只拥有 queue、并发 admission、restart handoff 调度和 HTTP-facing control；
 * PostgreSQL lease/journal 与 Pi session 行为由下层 runtime 执行。
 */

import { randomUUID } from "node:crypto";
import {
  API_VERSION,
  CONTEXT_SCHEMA_VERSION,
  PI_SDK_VERSION,
  RUNTIME_NAME,
  TOOL_CONTRACT_VERSION,
  type RuntimeStatus,
  type Scope,
  type StartTurnInput,
  type TurnAnswer,
  type TurnState,
  isTerminalStatus,
  safeErrorMessage,
  sameRuntimeScope,
  validatePageContext,
} from "./contracts.js";
import type { Config } from "./config.js";
import type { ProductFlowClient } from "./productflow.js";
import { TurnRuntime } from "./turn-runtime.js";
import { scopeFromContract } from "./runtime-scope.js";
import type { SkillCatalog } from "./skills.js";
import { RuntimeError, type TurnStore } from "./store.js";

const RUNTIME_VERSION = "0.1.0";
const MAX_INPUT_TEXT_BYTES = 64 << 10;

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
  private readonly runs = new Map<string, TurnRuntime>();
  private readonly pending: Array<{ runtime: TurnRuntime; turnID: string }> = [];
  private readonly scheduled = new Set<string>();
  private readonly activeExecutions = new Set<Promise<void>>();
  private readonly handoffRetryTimers = new Map<string, ReturnType<typeof setTimeout>>();
  readonly instanceID: string;
  private running = 0;
  private closed = false;

  constructor(
    readonly config: Config,
    readonly store: TurnStore,
    readonly productFlow: ProductFlowClient,
    readonly skills: SkillCatalog,
    instanceID: string = randomUUID(),
  ) {
    this.instanceID = instanceID;
  }

  /** 只把本地能证明尚未被 claim 的 Turn 重新入队。 */
  async recoverAfterRestart(): Promise<RuntimeRecoverySummary> {
    this.assertOpen();
    let replayedHandoffs = 0;
    for (const candidate of await this.store.durableHandoffCandidates()) {
      try {
        const runtime = await this.runtimeFor(candidate.scope);
        const recovered = await runtime.recoverDurableHandoff(
          candidate.turnID,
          candidate.idempotencyKey,
          candidate.executionID,
          candidate.projectionID,
        );
        if (recovered) {
          replayedHandoffs += 1;
        } else {
          this.scheduleDurableHandoffRecovery(
            runtime,
            candidate.turnID,
            candidate.idempotencyKey,
            candidate.executionID,
            candidate.projectionID,
          );
        }
      } catch (error) {
        process.stderr.write(
          `Agent durable handoff recovery failed for turn ${candidate.turnID}: ${safeErrorMessage(error)}\n`,
        );
      }
    }
    const recovered = await this.store.recoverAfterRestart();
    for (const candidate of recovered.queued) {
      const runtime = await this.runtimeFor(candidate.scope);
      this.enqueue(runtime, candidate.turnID);
    }
    return {
      replayed_handoffs: replayedHandoffs,
      queued_turns: recovered.queued.length,
      deferred_turns: recovered.deferred,
      waiting_input_turns: recovered.waitingInput,
      restored_terminal_turns: recovered.restoredTerminal,
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
    if ((state.status === "requires_input" || state.status === "cancel_requested") && !runtime.hasQuestionWaiter(turnID)) {
      const events = await this.store.events(runtime.scope.run_id, turnID, 0);
      if (!events.some((event) => event.kind === "turn/cancel_requested")) {
        await this.store.appendLocalEvent(runtime.scope.run_id, turnID, "turn/cancel_requested", { status: "cancel_requested" });
      }
      await this.store.updateState(runtime.scope.run_id, turnID, { status: "cancel_requested", question: undefined });
      if (!runtime.canCancelQueuedTurn()) {
        this.enqueue(runtime, turnID);
        return this.store.getState(runtime.scope.run_id, turnID);
      }
      return this.cancelQueuedTurn(runtime, turnID);
    }
    if (state.status === "queued" && !runtime.hasQuestionWaiter(turnID) && runtime.canCancelQueuedTurn()) {
      return this.cancelQueuedTurn(runtime, turnID);
    }
    await this.store.updateState(runtime.scope.run_id, turnID, { status: "cancel_requested" });
    await runtime.appendJournalEvent(turnID, "turn/cancel_requested", { status: "cancel_requested" }, true);
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
      if (runtime.hasQuestionWaiter(turnID)) {
        await runtime.appendJournalEvent(turnID, "turn/resume_requested", { status: "queued" }, true);
      } else {
        await this.store.appendLocalEvent(runtime.scope.run_id, turnID, "turn/resume_requested", { status: "queued" });
      }
      this.enqueue(runtime, turnID);
      return state;
    }
    if (state.status === "requires_input") {
      if (!runtime.hasQuestionWaiter(turnID)) {
        if (!await runtime.hasStoredQuestionAnswer(turnID)) {
          throw new RuntimeError(409, "not_resumable", "this question is no longer attached to a live Pi turn");
        }
        await this.store.updateState(runtime.scope.run_id, turnID, { status: "queued", question: undefined });
        await this.store.appendLocalEvent(runtime.scope.run_id, turnID, "turn/resume_requested", { status: "queued" });
        this.enqueue(runtime, turnID);
        return this.store.getState(runtime.scope.run_id, turnID);
      }
      return state;
    }
    throw new RuntimeError(409, "not_resumable", "the Agent Turn is already running or has no resumable checkpoint");
  }

  async answerQuestion(request: RuntimeLookup, turnID: string, questionID: string, answer: TurnAnswer): Promise<TurnState> {
    const runtime = await this.runtimeForLookup(request);
    return runtime.answerQuestion(turnID, questionID, answer);
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
    for (const timer of this.handoffRetryTimers.values()) clearTimeout(timer);
    this.handoffRetryTimers.clear();
    await Promise.allSettled([...this.runs.values()].map((runtime) => runtime.close()));
    this.pending.length = 0;
    await Promise.allSettled([...this.activeExecutions]);
  }

  private async runtimeForLookup(lookup: RuntimeLookup): Promise<TurnRuntime> {
    if (!lookup.conversationID && !lookup.taskID) {
      throw new RuntimeError(400, "invalid_argument", "conversation_id or task_id is required");
    }
    return this.runtimeFor(await this.loadScope(lookup));
  }

  scheduleDurableHandoffRecovery(
    runtime: TurnRuntime,
    turnID: string,
    idempotencyKey: string,
    executionID: string,
    projectionID: string,
    retry = 0,
  ): void {
    if (this.closed) return;
    const key = `${runtime.scope.run_id}:${turnID}`;
    if (this.handoffRetryTimers.has(key)) return;
    const delay = Math.min(30_000, 250 * 2 ** Math.min(retry, 7));
    const timer = setTimeout(() => {
      this.handoffRetryTimers.delete(key);
      if (this.closed) return;
      if (!runtime.canStartTurn() || this.running >= this.config.maxConcurrentTurns) {
        this.scheduleDurableHandoffRecovery(runtime, turnID, idempotencyKey, executionID, projectionID, retry);
        return;
      }
      runtime.beginTurn(turnID);
      runtime.setHandoffRecoveryManaged(true);
      this.running += 1;
      const recovery = runtime.recoverDurableHandoff(turnID, idempotencyKey, executionID, projectionID)
        .then((recovered) => {
          if (!recovered) {
            this.scheduleDurableHandoffRecovery(runtime, turnID, idempotencyKey, executionID, projectionID, retry + 1);
          }
        })
        .catch(() => {
          this.scheduleDurableHandoffRecovery(runtime, turnID, idempotencyKey, executionID, projectionID, retry + 1);
        })
        .finally(() => {
          runtime.setHandoffRecoveryManaged(false);
          runtime.endTurn(turnID);
          this.running -= 1;
          void this.drain();
        });
      this.activeExecutions.add(recovery);
      void recovery.finally(() => this.activeExecutions.delete(recovery));
    }, delay);
    timer.unref?.();
    this.handoffRetryTimers.set(key, timer);
  }

  private async runtimeFor(scope: Scope): Promise<TurnRuntime> {
    const existing = this.runs.get(scope.run_id);
    if (existing) {
      if (!sameRuntimeScope(existing.scope, scope)) {
        throw new RuntimeError(409, "conflict", "runtime scope conflicts with the ProductFlow contract");
      }
      return existing;
    }
    const runtime = new TurnRuntime(this, scope);
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
  private enqueue(runtime: TurnRuntime, turnID: string): void {
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
      const execution = next.runtime
        .execute(next.turnID)
        .catch(() => undefined)
        .finally(() => {
          next.runtime.endTurn(next.turnID);
          this.running -= 1;
          this.scheduled.delete(`${next.runtime.scope.run_id}:${next.turnID}`);
          void this.drain();
        });
      this.activeExecutions.add(execution);
      void execution.finally(() => this.activeExecutions.delete(execution));
    }
  }

  private async cancelQueuedTurn(runtime: TurnRuntime, turnID: string): Promise<TurnState> {
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

export interface RuntimeRecoverySummary {
  replayed_handoffs: number;
  queued_turns: number;
  deferred_turns: number;
  waiting_input_turns: number;
  restored_terminal_turns: number;
}
