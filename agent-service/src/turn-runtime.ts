/**
 * 单个 Turn 的 durable execution runtime。
 *
 * 负责 lease、journal、question/resume 和终态编排；Pi session/model 由 PiSessionAdapter 提供。
 */

import { randomUUID } from "node:crypto";
import { type AgentSession, type AgentSessionEvent } from "@earendil-works/pi-coding-agent";
import {
  EVENT_SCHEMA_VERSION,
  ProductFlowError,
  isAgentEventSequenceConflict,
  type AgentExecutionLease,
  type AgentEventReceipt,
  type CheckpointKind,
  type ExecutionPhase,
  type JsonObject,
  type Scope,
  type StartTurnInput,
  type TurnAnswer,
  type TurnArtifact,
  type TurnEvent,
  type TurnQuestion,
  type TurnState,
  type ToolStepDetails,
  TurnStatus,
  isTerminalStatus,
  nowISO,
  questionAnswerToolPayload,
  safeErrorMessage,
  sha256,
  toolKind,
} from "./contracts.js";
import type { AgentEventInput, EffectReconciliation, ProductFlowClient } from "./productflow.js";
import { RuntimeError, type TurnStore } from "./store.js";
import { type ToolRuntime } from "./tools.js";
import {
  compactAskUserSummary,
  continueAgentSession,
  injectAskUserToolResult,
  storedAnswerFromEvents,
  type StoredQuestionAnswer,
} from "./question-resume.js";
import {
  boundedUsage,
  JournalStreamBuffer,
  normalizeAssistantMessageEvent,
  reportUnknownPiAssistantEvent,
  type AssistantFinishPayload,
  type JournalStreamChunk,
} from "./pi-chunks.js";
import { isJournalFlushBarrier, JournalEventBatcher } from "./journal-publisher.js";
import type { PiRuntimeManager } from "./runtime-manager.js";
import { PiSessionAdapter } from "./pi-runtime.js";
import { DEPLOYED_HARNESS } from "./harness.js";
import type { EvolutionTrace } from "./evolution-traces.js";
import {
  artifactFromPendingApproval,
  eventReceiptMatches,
  hasUnresolvedApproval,
  isRetryableJournalBatchError,
  toAgentEventInput,
} from "./runtime-journal.js";
import {
  applyThinkingEvent,
  createThinkingProjectionState,
  type ThinkingAssistantEvent,
  type ThinkingProjectionState,
} from "./thinking-projection.js";
import {
  mergeToolStepDetails,
  resultMetaFromToolResult,
  toolStepDetailsForResult,
  toolStepDetailsForStart,
  toolStepSummary,
} from "./tool-step-projection.js";

export class TurnRuntime implements ToolRuntime {
  private session?: AgentSession;
  private readonly pi: PiSessionAdapter;
  private abortController?: AbortController;
  private questionWaiter?: {
    turnID: string;
    questionID: string;
    resolve: (answer: TurnAnswer) => void;
    reject: (error: Error) => void;
    timeout?: ReturnType<typeof setTimeout>;
  };
  private pendingQuestionAnswer?: TurnAnswer;
  private questionWriteChain: Promise<unknown> = Promise.resolve();
  private artifact?: TurnArtifact;
  private workflowRunRequested = false;
  private workflowApproval?: JsonObject;
  private eventBatcher: JournalEventBatcher;
  private streamBuffer: JournalStreamBuffer;
  private output = "";
  private thinkingProjection: ThinkingProjectionState = createThinkingProjectionState();
  private unknownPiEventTypes = new Set<string>();
  private attemptID = "";
  private toolCount = 0;
  private eventChain = Promise.resolve();
  private persistenceError?: Error;
  private streamCapacityError?: Error;
  private terminalDrainFailed = false;
  private iterationError?: Error;
  private modelError?: Error;
  private activeTurnID?: string;
  private executionLease?: AgentExecutionLease;
  private executionPhase: ExecutionPhase = "claimed";
  private executionHeartbeat?: ReturnType<typeof setInterval>;
  private executionPhaseUpdates = Promise.resolve();
  private executionStopping = false;
  private executionLeaseError?: Error;
  private shutdownRequested = false;
  private shutdownWaitingInputTurnID?: string;
  private handoffRecoveryManaged = false;
  private effectUnknownError?: string;
  private readonly unknownToolStepIDs = new Set<string>();
  private readonly activeToolStepDetails = new Map<string, ToolStepDetails>();
  private readonly toolStepFailureDetails = new Map<string, ToolStepDetails>();
  private checkpointSequence = 0;
  private checkpointWrites = Promise.resolve();
  private modelRequestSequence = 0;
  private queuedEventSequence = 0;
  private currentModelRequestID?: string;
  private modelRequestStartedAt?: number;
  private readonly completedModelRequestIDs = new Set<string>();
  private currentPageType: string | null = null;
  private evolutionTrace?: EvolutionTrace;
  constructor(
    private readonly manager: PiRuntimeManager,
    readonly scope: Scope,
  ) {
    this.pi = new PiSessionAdapter(this);
    this.eventBatcher = this.createEventBatcher();
    this.streamBuffer = this.createStreamBuffer();
  }

  get client(): ProductFlowClient {
    return this.manager.productFlow;
  }

  get config(): PiRuntimeManager["config"] {
    return this.manager.config;
  }

  get store(): TurnStore {
    return this.manager.store;
  }

  get skills(): PiRuntimeManager["skills"] {
    return this.manager.skills;
  }

  get executionProjectionID(): string | null {
    return this.executionLease?.projection_id ?? null;
  }

  get currentModelRequestIDValue(): string | undefined {
    return this.currentModelRequestID;
  }

  setCurrentPageType(value: string | null): void {
    this.currentPageType = value;
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

  setHandoffRecoveryManaged(value: boolean): void {
    this.handoffRecoveryManaged = value;
  }

  /**
   * 进程崩溃后只恢复本地 journal 到 PG 的提交，不续跑模型。
   * 同一持久 owner 可以取回尚未过期的原 lease；lease 已被接管时必须服从 fencing。
   */
  async recoverDurableHandoff(
    turnID: string,
    idempotencyKey: string,
    executionID: string,
    projectionID: string,
  ): Promise<boolean> {
    this.resetTurnState();
    this.currentTurn = turnID;
    this.abortController = new AbortController();
    try {
      if (await this.confirmPublishedPrefix(turnID, executionID, projectionID)) {
        await this.manager.store.clearDurableHandoff(this.scope.run_id, turnID);
        return true;
      }
      if (await this.manager.store.reconcileConfirmedTerminal(this.scope.run_id, turnID)) {
        await this.manager.store.clearDurableHandoff(this.scope.run_id, turnID);
        return true;
      }
      let events = await this.manager.store.events(this.scope.run_id, turnID, 0);
      let unpublished = await this.manager.store.unpublishedEvents(this.scope.run_id, turnID);
      if (unpublished.length === 0 && events.some((event) => event.kind === "turn/end")) {
        await this.manager.store.clearDurableHandoff(this.scope.run_id, turnID);
        return true;
      }
      try {
        this.executionLease = await this.claimExecution(idempotencyKey, turnID);
      } catch (error) {
        if (error instanceof ProductFlowError && error.status === 409) return false;
        throw error;
      }
      this.executionPhase = this.executionLease.phase;
      await this.manager.store.saveDurableHandoff(this.scope.run_id, turnID, this.executionLease);
      this.startExecutionHeartbeat();
      if (await this.syncRecoveryEvents(turnID)) return true;
      await this.flushPublishedEvents();
      events = await this.manager.store.events(this.scope.run_id, turnID, 0);
      unpublished = await this.manager.store.unpublishedEvents(this.scope.run_id, turnID);
      if (unpublished.length > 0) {
        throw new RuntimeError(503, "event_handoff_incomplete", "Agent event handoff did not drain completely");
      }
      const state = await this.manager.store.getState(this.scope.run_id, turnID);
      if (events.some((event) => event.kind === "turn/end")) {
        await this.updateExecutionPhase("terminal");
      } else if (storedAnswerFromEvents(events)) {
        await this.manager.store.updateState(this.scope.run_id, turnID, {
          status: "queued",
          question: undefined,
          execution_attempt: undefined,
          execution_fencing_token: undefined,
        });
      } else if (state.status === "requires_input" && events.some((event) => event.kind === "question/requested")) {
        await this.updateExecutionPhase("waiting_input");
      } else if (hasUnresolvedApproval(events)) {
        await this.updateExecutionPhase("external_job");
        await this.manager.store.updateState(this.scope.run_id, turnID, {
          status: "awaiting_confirmation",
          artifact: artifactFromPendingApproval(events),
        });
      } else {
        // The WAL prefix is drained, but only Go may decide the terminal state of
        // a lost in-flight execution after the lease expires.
        this.executionLease = undefined;
      }
      await this.flushPublishedEvents();
      return true;
    } catch (error) {
      if (!isAgentEventSequenceConflict(error)) throw error;
      await this.abandonUnpublishedJournal(turnID);
      await this.manager.store.clearDurableHandoff(this.scope.run_id, turnID);
      return true;
    } finally {
      await this.cleanupAfterTurn();
    }
  }

  private async confirmPublishedPrefix(turnID: string, executionID: string, projectionID: string): Promise<boolean> {
    const probe = await this.client.confirmTurnEvents(
      this.scope.conversation_id,
      executionID,
      { events: [] },
      this.signal,
    );
    if (probe.status !== "confirmed" || probe.confirmed_through !== 0 || probe.items.length !== 0) {
      throw new RuntimeError(502, "event_confirmation_mismatch", "ProductFlow returned an invalid empty Agent event confirmation");
    }
    if (await this.adoptConfirmedTerminal(turnID, executionID, projectionID, probe)) return true;
    while (true) {
      const events = (await this.manager.store.unpublishedEvents(this.scope.run_id, turnID)).slice(0, 250);
      if (events.length === 0) {
        const published = await this.manager.store.publishedThrough(this.scope.run_id, turnID);
        if (Number.isInteger(probe.persisted_through) && probe.persisted_through > published) {
          await this.abandonUnpublishedJournal(turnID);
          return true;
        }
        return false;
      }
      const inputs = events.map(toAgentEventInput);
      let confirmation: Awaited<ReturnType<ProductFlowClient["confirmTurnEvents"]>>;
      try {
        confirmation = await this.client.confirmTurnEvents(
          this.scope.conversation_id,
          executionID,
          { events: inputs },
          this.signal,
        );
      } catch (error) {
        if (!isAgentEventSequenceConflict(error)) throw error;
        const matched = await this.confirmMatchingUnpublishedPrefix(turnID, executionID, projectionID, events);
        if (matched === "adopted") return true;
        if (matched === "missing") return false;
        if (matched === "confirmed") continue;
        if (await this.adoptConfirmedTerminal(turnID, executionID, projectionID, probe)) return true;
        await this.abandonUnpublishedJournal(turnID);
        return true;
      }
      const firstSequence = events[0]?.sequence ?? 1;
      const lastSequence = events.at(-1)?.sequence ?? 0;
      if (
        (confirmation.status !== "confirmed" && confirmation.status !== "missing") ||
        !Number.isInteger(confirmation.persisted_through) ||
        confirmation.persisted_through < 0 ||
        confirmation.confirmed_through < firstSequence - 1 ||
        confirmation.confirmed_through > lastSequence
      ) {
        throw new RuntimeError(502, "event_confirmation_mismatch", "ProductFlow returned an invalid Agent event confirmation");
      }
      const confirmedCount = confirmation.confirmed_through - firstSequence + 1;
      if (
        confirmation.items.length !== confirmedCount ||
        confirmation.items.some((receipt, index) => (
          !eventReceiptMatches(receipt, events[index]!, executionID, projectionID)
        ))
      ) {
        throw new RuntimeError(502, "event_confirmation_mismatch", "ProductFlow returned mismatched Agent event confirmations");
      }
      if (confirmedCount > 0) {
        await this.manager.store.markEventsPublished(this.scope.run_id, turnID, confirmation.confirmed_through);
      }
      if (await this.adoptConfirmedTerminal(turnID, executionID, projectionID, confirmation)) return true;
      if (confirmation.status === "missing") return false;
      if (confirmation.confirmed_through !== lastSequence) {
        throw new RuntimeError(502, "event_confirmation_mismatch", "ProductFlow returned an incomplete Agent event confirmation");
      }
    }
  }

  private async confirmMatchingUnpublishedPrefix(
    turnID: string,
    executionID: string,
    projectionID: string,
    events: readonly TurnEvent[],
  ): Promise<"adopted" | "missing" | "conflict" | "confirmed"> {
    const firstSequence = events[0]?.sequence ?? 1;
    let confirmedThrough = firstSequence - 1;
    for (const event of events) {
      try {
        const confirmation = await this.client.confirmTurnEvents(
          this.scope.conversation_id,
          executionID,
          { events: [toAgentEventInput(event)] },
          this.signal,
        );
        if (confirmation.status === "missing") {
          if (confirmedThrough >= firstSequence) {
            await this.manager.store.markEventsPublished(this.scope.run_id, turnID, confirmedThrough);
          }
          return "missing";
        }
        if (
          confirmation.status !== "confirmed"
          || confirmation.confirmed_through !== event.sequence
          || confirmation.items.length !== 1
          || !eventReceiptMatches(confirmation.items[0]!, event, executionID, projectionID)
        ) {
          throw new RuntimeError(502, "event_confirmation_mismatch", "ProductFlow returned mismatched Agent event confirmations");
        }
        confirmedThrough = event.sequence;
        if (await this.adoptConfirmedTerminal(turnID, executionID, projectionID, confirmation)) {
          await this.manager.store.markEventsPublished(this.scope.run_id, turnID, confirmedThrough);
          return "adopted";
        }
      } catch (error) {
        if (!isAgentEventSequenceConflict(error)) throw error;
        if (confirmedThrough >= firstSequence) {
          await this.manager.store.markEventsPublished(this.scope.run_id, turnID, confirmedThrough);
        }
        return "conflict";
      }
    }
    if (confirmedThrough >= firstSequence) {
      await this.manager.store.markEventsPublished(this.scope.run_id, turnID, confirmedThrough);
    }
    return "confirmed";
  }

  /** 本地未发布后缀与 PG 分叉时，不以错误内容覆盖权威 journal，只把本地 Turn 收成 unknown。 */
  private async abandonUnpublishedJournal(turnID: string): Promise<void> {
    const state = await this.manager.store.getState(this.scope.run_id, turnID);
    const events = await this.manager.store.events(this.scope.run_id, turnID, 0);
    if (isTerminalStatus(state.status) && events.some((event) => event.kind === "turn/end")) return;
    const summary = await this.manager.store.journalText(this.scope.run_id, turnID);
    const error = state.error?.trim() || "Agent event sequence 已绑定不同内容";
    if (!events.some((event) => event.kind === "turn/end")) {
      await this.manager.store.appendLocalEvent(this.scope.run_id, turnID, "turn/end", {
        reason: "unknown",
        reason_code: "execution_interrupted",
        status: "unknown",
        error,
      });
    }
    if (isTerminalStatus(state.status)) return;
    await this.manager.store.updateState(this.scope.run_id, turnID, {
      status: "unknown",
      output: summary.output || state.output,
      thinking: summary.thinking || state.thinking,
      error,
      question: undefined,
      finished_at: nowISO(),
    });
  }

  private async adoptConfirmedTerminal(
    turnID: string,
    executionID: string,
    projectionID: string,
    confirmation: Awaited<ReturnType<ProductFlowClient["confirmTurnEvents"]>>,
  ): Promise<boolean> {
    if (!Number.isInteger(confirmation.persisted_through) || confirmation.persisted_through < 0) {
      throw new RuntimeError(502, "event_confirmation_mismatch", "ProductFlow returned an invalid persisted journal head");
    }
    const terminal = confirmation.terminal;
    if (!terminal) return false;
    if (
      terminal.kind !== "turn/end" ||
      terminal.execution_id !== executionID ||
      terminal.projection_id !== projectionID ||
      terminal.schema_version !== EVENT_SCHEMA_VERSION ||
      terminal.sequence > confirmation.persisted_through ||
      typeof terminal.output !== "string" ||
      typeof terminal.thinking !== "string" ||
      typeof terminal.error !== "string" ||
      typeof terminal.finished_at !== "string"
    ) {
      throw new RuntimeError(502, "event_confirmation_mismatch", "ProductFlow returned an invalid terminal confirmation");
    }
    if (!await this.manager.store.reconcileConfirmedTerminal(this.scope.run_id, turnID)) {
      await this.manager.store.adoptAuthoritativeTerminal(this.scope.run_id, turnID, terminal);
    }
    return true;
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
      this.executionPhase = this.executionLease.phase;
      executionClaimed = true;
      this.evolutionTrace = this.manager.evolutionTraces.start({
        runID: this.scope.run_id, turnID, attempt: this.executionLease.attempt,
        fencingToken: this.executionLease.fencing_token,
        harnessHash: DEPLOYED_HARNESS.hash, skillHash: this.skills.hash,
      }, this.skills.names);
      await this.manager.store.saveDurableHandoff(this.scope.run_id, turnID, this.executionLease);
      this.startExecutionHeartbeat();
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
      await this.appendJournalEvent(turnID, "turn/start", {
        status: "running",
        attempt_id: this.attemptID,
      });
      const runtimeContext = await this.client.runtimeContext(this.scope.conversation_id, this.scope.task_id, this.signal);
      const { session, model, images } = await this.pi.createSession(
        turnID,
        runtimeContext,
        initial.input,
        (event) => this.handleSessionEvent(turnID, event),
      );
      this.session = session;
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
      this.flushStreamChunks();
      await this.eventChain;
      if (this.persistenceError) throw this.persistenceError;
      if (this.streamCapacityError) throw this.streamCapacityError;
      if (this.iterationError) throw this.iterationError;
      if (this.modelError) throw this.modelError;
      const current = await this.manager.store.getState(this.scope.run_id, turnID);
      if (this.shutdownRequested && current.status === "requires_input" && this.shutdownWaitingInputTurnID === turnID) {
        await this.updateExecutionPhase("waiting_input");
      } else if (this.shutdownRequested) {
        await this.finishTurn(turnID, "unknown", {
          output: this.output,
          error: "Agent service stopped before this Turn reached a provable terminal state",
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
        await this.finishTurn(turnID, "succeeded", { output: this.output });
      }
    } catch (error) {
      if (!executionClaimed) {
        if (error instanceof ProductFlowError && error.status >= 400 && error.status < 500) {
          await this.writeJournalTerminal(turnID, "failed", {
            output: this.output,
            error: safeErrorMessage(error),
          });
        }
        return;
      }
      await this.eventChain;
      const current = await this.manager.store.getState(this.scope.run_id, turnID).catch(() => initial);
      if (this.shutdownRequested && current.status === "requires_input" && this.shutdownWaitingInputTurnID === turnID) {
        await this.updateExecutionPhase("waiting_input");
      } else if (this.shutdownRequested) {
        await this.finishTurn(turnID, "unknown", {
          output: this.output,
          error: "Agent service stopped before this Turn reached a provable terminal state",
        });
      } else if (this.effectUnknownError) {
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
      try {
        await this.cleanupAfterTurn();
      } finally {
        this.evolutionTrace?.finish();
        this.evolutionTrace = undefined;
      }
    }
  }

  async cancelQueuedTurn(turnID: string): Promise<TurnState> {
    const initial = await this.manager.store.getState(this.scope.run_id, turnID);
    if (initial.status !== "queued" && initial.status !== "cancel_requested") {
      if (isTerminalStatus(initial.status)) return initial;
      throw new RuntimeError(409, "turn_not_queued", "the Agent Turn is no longer queued");
    }
    this.resetTurnState();
    this.currentTurn = turnID;
    this.abortController = new AbortController();
    let executionClaimed = false;
    try {
      this.executionLease = await this.claimExecution(initial.input.idempotency_key, turnID);
      this.executionPhase = this.executionLease.phase;
      executionClaimed = true;
      await this.manager.store.saveDurableHandoff(this.scope.run_id, turnID, this.executionLease);
      this.startExecutionHeartbeat();
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
    await this.publishUnpublishedEvents(turnID);
    await this.flushPublishedEvents();
    if (this.persistenceError) throw this.persistenceError;
  }

  private async syncRecoveryEvents(turnID: string): Promise<boolean> {
    const events = await this.manager.store.unpublishedEvents(this.scope.run_id, turnID);
    for (const event of events) {
      if (event.sequence > this.queuedEventSequence) {
        this.queuedEventSequence = event.sequence;
        await this.publishDurableEvent(event);
      }
      if (event.kind !== "turn/end") continue;
      await this.flushPublishedEvents();
      if (await this.manager.store.reconcileConfirmedTerminal(this.scope.run_id, turnID)) return true;
    }
    return false;
  }

  /** 写入 ProductFlow checkpoint；追加失败即丢失租约并中止。 */
  checkpoint(kind: CheckpointKind, payload: JsonObject): Promise<void> {
    const operation = this.checkpointWrites.then(() => this.appendCheckpoint(kind, payload));
    this.checkpointWrites = operation.catch(() => undefined);
    return operation;
  }

  private async appendCheckpoint(kind: CheckpointKind, payload: JsonObject): Promise<void> {
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

  async reconcileEffect(toolCallID: string): Promise<EffectReconciliation> {
    const lease = this.executionLease;
    if (!lease || this.executionLeaseError || this.executionStopping) {
      throw this.executionLeaseError ?? new RuntimeError(409, "execution_unavailable", "Agent execution lease is unavailable");
    }
    return this.client.reconcileTurnEffect(
      this.scope.conversation_id,
      lease.execution_id,
      { owner_id: lease.owner_id, lease_token: lease.lease_token, tool_call_id: toolCallID },
      this.signal,
    );
  }

  /** PG 接受 turn/end 的事务就是唯一终态提交点；checkpoint 不再重复提交终态。 */
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
    this.flushStreamChunks();
    await this.eventChain;
    const reasonCode = status === "failed"
      ? "provider_failed"
      : status === "unknown" && this.effectUnknownError
        ? "effect_unknown"
        : status === "unknown"
          ? "execution_interrupted"
          : undefined;
    try {
      await this.writeJournalTerminal(turnID, status, {
        ...details,
        thinking: this.thinkingProjection.text,
        ...(reasonCode ? { reason_code: reasonCode } : {}),
      });
    } catch (error) {
      this.persistenceError ??= error instanceof Error ? error : new Error("Agent event persistence failed");
      this.terminalDrainFailed = true;
      if (status !== "unknown") {
        await this.recordLocalUnknownTerminal(
          turnID,
          safeErrorMessage(this.persistenceError),
          details.output ?? "",
        );
      } else {
        await this.manager.store.updateState(this.scope.run_id, turnID, {
          status: "unknown",
          output: details.output ?? "",
          thinking: this.thinkingProjection.text,
          error: safeErrorMessage(this.persistenceError),
          finished_at: nowISO(),
        });
      }
    }
    this.executionPhase = "terminal";
  }

  private async recordLocalUnknownTerminal(turnID: string, error: string, output: string): Promise<void> {
    await this.manager.store.appendLocalEvent(this.scope.run_id, turnID, "turn/end", {
      reason: "unknown",
      reason_code: "persistence_failed",
      status: "unknown",
      error,
    });
    await this.manager.store.updateState(this.scope.run_id, turnID, {
      status: "unknown",
      output,
      thinking: this.thinkingProjection.text,
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
    this.executionPhase = phase;
    const operation = this.executionPhaseUpdates.then(() => this.sendLatestExecutionPhase());
    this.executionPhaseUpdates = operation.catch(() => undefined);
    return operation;
  }

  private async sendLatestExecutionPhase(): Promise<void> {
    const lease = this.executionLease;
    if (!lease || this.executionStopping || this.executionLeaseError) return;
    const phase = this.executionPhase;
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
    await this.executionPhaseUpdates.catch(() => undefined);
    const lease = this.executionLease;
    this.executionLease = undefined;
    if (!lease) return;
    const turnID = this.currentTurn;
    if (turnID && (await this.manager.store.unpublishedEvents(this.scope.run_id, turnID)).length > 0) {
      const state = await this.manager.store.getState(this.scope.run_id, turnID);
      if (!this.handoffRecoveryManaged) {
        this.manager.scheduleDurableHandoffRecovery(
          this,
          turnID,
          state.input.idempotency_key,
          lease.execution_id,
          lease.projection_id,
        );
      }
      return;
    }
    try {
      await this.client.releaseTurnExecution(this.scope.conversation_id, lease.execution_id, {
        owner_id: lease.owner_id,
        lease_token: lease.lease_token,
        phase: this.executionPhase,
      });
      if (turnID) await this.manager.store.clearDurableHandoff(this.scope.run_id, turnID);
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
    await this.writeJournalTerminal(turnID, "canceled", {
      output: state.output,
    });
    return this.manager.store.getState(this.scope.run_id, turnID);
  }

  async close(): Promise<void> {
    this.shutdownRequested = true;
    this.shutdownWaitingInputTurnID = this.questionWaiter?.turnID;
    this.flushStreamChunks();
    await this.eventChain;
    this.clearQuestionTimeout();
    this.questionWaiter?.reject(new RuntimeError(503, "closed", "Agent runtime is shutting down"));
    this.questionWaiter = undefined;
    this.abortController?.abort();
    await this.session?.abort().catch(() => undefined);
    this.session?.dispose();
    this.streamBuffer.dispose();
    this.eventBatcher.dispose();
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
    const events = await this.manager.store.events(this.scope.run_id, turnID, 0);
    const answeredSequence = [...events].reverse().find((event) => event.kind === "question/answered" && event.payload.question_id === waiter.questionID)?.sequence ?? 0;
    const existingResume = events.some((event) => event.kind === "turn/resume_requested" && event.sequence > answeredSequence);
    if (!existingResume) {
      await this.appendJournalEvent(turnID, "turn/resume_requested", { status: "running" }, true);
    }
    await this.flushPublishedEvents();
    this.pendingQuestionAnswer = undefined;
    this.clearQuestionTimeout();
    await this.manager.store.updateState(this.scope.run_id, turnID, { status: "running", question: undefined });
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
    // Pi may attach its tool-result continuation on a later microtask. Keep the
    // shutdown rejection observed immediately while preserving rejection for
    // the actual caller awaiting this same promise.
    void answerPromise.catch(() => undefined);
    await this.updateExecutionPhase("waiting_input");
    await this.checkpoint("question_required", { question: question as unknown as JsonObject });
    await this.manager.store.updateState(this.scope.run_id, state.turn_id, {
      status: "requires_input",
      question,
      output: this.output,
      thinking: this.thinkingProjection.text,
    });
    await this.appendJournalEvent(state.turn_id, "question/requested", question as never);
    if (this.persistenceError) throw this.persistenceError;
    return answerPromise;
  }

  answerQuestion(turnID: string, questionID: string, answer: TurnAnswer): Promise<TurnState> {
    return this.serializeQuestionWrite(() => this.persistQuestionAnswer(turnID, questionID, answer));
  }

  private async persistQuestionAnswer(turnID: string, questionID: string, answer: TurnAnswer): Promise<TurnState> {
    const state = await this.manager.store.getState(this.scope.run_id, turnID);
    const waiter = this.questionWaiter;
    if (this.pendingQuestionAnswer && waiter?.turnID === turnID && waiter.questionID === questionID) {
      if (JSON.stringify(this.pendingQuestionAnswer) !== JSON.stringify(answer)) {
        throw new RuntimeError(409, "question_already_answered", "the question already has a different answer");
      }
      return state;
    }
    const stored = await this.storedQuestionAnswer(turnID, questionID);
    if (stored) {
      if (JSON.stringify(stored.answer) !== JSON.stringify(answer)) {
        throw new RuntimeError(409, "question_already_answered", "the question already has a different answer");
      }
      if (state.status === "requires_input" && state.question?.id === questionID) {
        if (waiter?.turnID === turnID && waiter.questionID === questionID) {
          this.clearQuestionTimeout();
          this.pendingQuestionAnswer = answer;
        }
        return this.manager.store.updateState(this.scope.run_id, turnID, {
          status: "queued",
          question: undefined,
          ...(!waiter ? { execution_attempt: undefined, execution_fencing_token: undefined } : {}),
        });
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
    const answerPayload = {
      question_id: questionID,
      answer: answer as never,
    };
    if (waiter) {
      await this.appendJournalEvent(turnID, "question/answered", answerPayload);
      this.clearQuestionTimeout();
      this.pendingQuestionAnswer = answer;
    } else {
      await this.manager.store.appendLocalEvent(this.scope.run_id, turnID, "question/answered", answerPayload);
    }
    await this.manager.store.updateState(this.scope.run_id, turnID, {
      status: "queued",
      question: undefined,
      ...(!waiter ? { execution_attempt: undefined, execution_fencing_token: undefined } : {}),
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

  async appendJournalEvent(
    turnID: string,
    kind: string,
    payload: JsonObject,
    ignorable = false,
  ): Promise<TurnEvent> {
    const event = await this.manager.store.appendEvent(this.scope.run_id, turnID, kind, payload, ignorable);
    await this.publishUnpublishedEvents(turnID);
    return event;
  }

  async setJournalToolStep(turnID: string, step: Parameters<TurnStore["setToolStep"]>[2]): Promise<TurnState> {
    const state = await this.manager.store.setToolStep(this.scope.run_id, turnID, step);
    await this.publishUnpublishedEvents(turnID);
    return state;
  }

  private async writeJournalTerminal(
    turnID: string,
    status: Parameters<TurnStore["terminal"]>[2],
    details: Parameters<TurnStore["terminal"]>[3],
  ): Promise<TurnState> {
    const state = await this.manager.store.terminal(this.scope.run_id, turnID, status, details);
    await this.publishUnpublishedEvents(turnID);
    this.evolutionTrace?.confirmTerminal(state.status);
    return state;
  }

  private async publishUnpublishedEvents(turnID: string): Promise<void> {
    const lease = this.executionLease;
    if (!lease || lease.harness_turn_id !== turnID) return;
    const events = await this.manager.store.unpublishedEvents(this.scope.run_id, turnID);
    for (const event of events) {
      if (event.sequence <= this.queuedEventSequence) continue;
      this.queuedEventSequence = event.sequence;
      await this.publishDurableEvent(event);
    }
  }

  private createEventBatcher(): JournalEventBatcher {
    return new JournalEventBatcher(
      (events) => this.appendPublishedBatch(events),
      {
        onBackgroundError: (error) => {
          this.persistenceError ??= error;
          this.abortController?.abort();
          void this.session?.abort();
        },
      },
    );
  }

  private async appendPublishedBatch(events: readonly TurnEvent[]): Promise<void> {
    const lease = this.executionLease;
    if (!lease) throw new RuntimeError(409, "execution_unavailable", "Agent execution lease is unavailable");
    const inputs: AgentEventInput[] = events.map(toAgentEventInput);
    let receipts: AgentEventReceipt[] | undefined;
    for (let attempt = 0; ; attempt += 1) {
      try {
        receipts = await this.client.appendTurnEvents(
          this.scope.conversation_id,
          lease.execution_id,
          { owner_id: lease.owner_id, lease_token: lease.lease_token, events: inputs },
        );
        break;
      } catch (error: unknown) {
        if (!isRetryableJournalBatchError(error) || attempt >= 8 || this.shutdownRequested) throw error;
        await this.waitForRetry(attempt);
      }
    }
    if (!receipts) throw new RuntimeError(502, "event_receipt_mismatch", "ProductFlow returned no Agent event receipts");
    if (
      receipts.length !== events.length ||
      receipts.some((receipt, index) => !eventReceiptMatches(
        receipt,
        events[index]!,
        lease.execution_id,
        lease.projection_id,
      ))
    ) {
      throw new RuntimeError(502, "event_receipt_mismatch", "ProductFlow returned mismatched Agent event receipts");
    }
    const last = events.at(-1);
    if (last) await this.manager.store.markEventsPublished(this.scope.run_id, last.turn_id, last.sequence);
  }

  private async waitForRetry(attempt: number): Promise<void> {
    const delay = Math.min(30_000, 250 * 2 ** Math.min(attempt, 7));
    await new Promise<void>((resolve) => {
      const timer = setTimeout(resolve, delay);
      timer.unref?.();
    });
  }

  private createStreamBuffer(): JournalStreamBuffer {
    return new JournalStreamBuffer(
      (chunk) => this.emitStreamChunk(chunk),
      {
        onError: (error) => {
          this.streamCapacityError ??= error;
          this.persistenceError ??= error;
          this.abortController?.abort();
          void this.session?.abort();
        },
      },
    );
  }

  private emitStreamChunk(chunk: JournalStreamChunk): void {
    const { kind, ...payload } = chunk;
    this.enqueue(() => this.appendJournalEvent(this.currentTurnID(), kind, payload));
  }

  private flushStreamChunks(): void {
    this.streamBuffer.flush();
  }

  async publishDurableEvent(event: TurnEvent): Promise<void> {
    const lease = this.executionLease;
    // 本 runtime 持有活动 Turn 租约时，run 上可能还有另一个 queued Turn。
    // 那个 queued 事件等自己 claim 后再回放；用当前租约发布会把活动执行 fence 掉。
    if (lease && lease.harness_turn_id !== event.turn_id) return;
    if (!lease) {
      if (event.kind === "turn/resume_requested" || event.kind === "turn/cancel_requested") {
        return;
      }
      this.persistenceError ??= new Error("Agent event was emitted without an execution lease");
      return;
    }
    await this.eventBatcher.enqueue(event, isJournalFlushBarrier(event.kind));
  }

  private async flushPublishedEvents(): Promise<void> {
    await this.eventBatcher.drain();
  }

  /** 与 ProductFlow prepare/apply/reconcile 共用的单次工具调用幂等键。 */
  idempotencyKey(toolCallID: string): string {
    return `pi:${this.scope.run_id}:${this.currentTurnID()}:${toolCallID}`.slice(0, 200);
  }

  private handleSessionEvent(turnID: string, event: AgentSessionEvent): void {
      if (event.type === "message_update") {
        this.projectAssistantMessageEvent(turnID, event.assistantMessageEvent);
        return;
      }
      if (event.type === "message_end" && event.message.role === "assistant" && event.message.stopReason === "error") {
        this.recordAssistantMessage(turnID, "error", boundedUsage(event.message.usage));
        this.modelError = new Error(event.message.errorMessage || "Pi provider request failed");
        return;
      }
      if (event.type === "message_end" && event.message.role === "assistant") {
        this.recordAssistantMessage(turnID, event.message.stopReason || "stop", boundedUsage(event.message.usage));
        return;
      }
      if (event.type === "tool_execution_start") {
        this.evolutionTrace?.toolStart(event.toolName, event.toolCallId, event.args);
        this.flushStreamChunks();
        void this.updateExecutionPhase("tool").catch(() => undefined);
        this.toolCount += 1;
        if (this.toolCount > this.manager.config.maxIterations) {
          this.iterationError = new Error(`Pi Agent exceeded the maximum tool iteration limit of ${this.manager.config.maxIterations}`);
          this.abortController?.abort();
          void this.session?.abort();
          return;
        }
        const details = toolStepDetailsForStart(event.toolName, event.args);
        this.activeToolStepDetails.set(event.toolCallId, details);
        this.enqueue(() =>
          this.setJournalToolStep(turnID, {
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
        this.evolutionTrace?.toolEnd(event.toolName, event.toolCallId, event.isError);
        this.flushStreamChunks();
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
          this.setJournalToolStep(turnID, {
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
  }

  private enqueue(operation: () => Promise<unknown>): void {
    this.eventChain = this.eventChain
      .then(operation)
      .catch((error: unknown) => {
        this.persistenceError = error instanceof Error ? error : new Error("Agent event persistence failed");
      })
      .then(() => undefined);
  }

  async checkpointModelRequest(): Promise<string> {
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
      provider: this.pi.model?.provider ?? "unknown",
      model: this.pi.model?.id ?? "unknown",
      harness_hash: DEPLOYED_HARNESS.hash,
      skill_catalog_hash: this.manager.skills.hash,
      model_configuration: this.pi.requestConfiguration,
      execution_mode: "foreground",
    });
    this.modelRequestSequence = sequence;
    this.currentModelRequestID = requestID;
    this.modelRequestStartedAt = Date.now();
    this.evolutionTrace?.modelStart(requestID, this.pi.model?.provider, this.pi.model?.id);
    return requestID;
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

  private serializeQuestionWrite<T>(write: () => Promise<T>): Promise<T> {
    const result = this.questionWriteChain.then(write);
    this.questionWriteChain = result.catch(() => undefined);
    return result;
  }

  private expireQuestionWaiter(turnID: string, questionID: string): Promise<void> {
    return this.serializeQuestionWrite(() => this.expirePendingQuestion(turnID, questionID));
  }

  private async expirePendingQuestion(turnID: string, questionID: string): Promise<void> {
    const waiter = this.questionWaiter;
    if (!waiter || waiter.turnID !== turnID || waiter.questionID !== questionID) return;
    if (this.pendingQuestionAnswer) return;
    const stored = await this.storedQuestionAnswer(turnID, questionID);
    if (stored || this.pendingQuestionAnswer) return;
    if (this.questionWaiter !== waiter) return;
    const state = await this.manager.store.getState(this.scope.run_id, turnID);
    if (state.status !== "requires_input") return;
    this.clearQuestionTimeout();
    this.questionWaiter = undefined;
    this.pendingQuestionAnswer = undefined;
    await this.updateExecutionPhase("model");
    await this.manager.store.updateState(this.scope.run_id, turnID, { status: "running", question: undefined });
    await this.appendJournalEvent(turnID, "question/answered", {
      question_id: questionID,
      answer: { skip: true },
      status: "no_answer",
    });
    waiter.resolve({ skip: true });
  }

  private async storedQuestionAnswer(turnID: string, questionID?: string): Promise<StoredQuestionAnswer | null> {
    const events = await this.manager.store.events(this.scope.run_id, turnID, 0);
    return storedAnswerFromEvents(events, questionID);
  }

  async hasStoredQuestionAnswer(turnID: string): Promise<boolean> {
    return (await this.storedQuestionAnswer(turnID)) !== null;
  }

  private async continueWithStoredQuestionAnswer(
    session: AgentSession,
    turnID: string,
    stored: StoredQuestionAnswer,
  ): Promise<void> {
    const { questionID, answer } = stored;
    const toolCallId = await injectAskUserToolResult(session, questionID, answer);
    if (toolCallId) {
      await this.setJournalToolStep(turnID, {
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
    if (mapped.action === "text.chunk") {
      this.output += mapped.delta;
      this.streamBuffer.append({
        kind: "text.chunk",
        delta: mapped.delta,
        step_id: `pi_${turnID}`,
        attempt_id: this.attemptID,
        content_index: mapped.contentIndex,
      });
      return;
    }
    this.recordAssistantMessage(turnID, mapped.reason, mapped.usage);
  }

  private recordAssistantMessage(turnID: string, reason: string, usage?: AssistantFinishPayload["usage"]): void {
    const modelRequestID = this.currentModelRequestID;
    if (modelRequestID && this.completedModelRequestIDs.has(modelRequestID)) return;
    if (modelRequestID) this.completedModelRequestIDs.add(modelRequestID);
    const startedAt = this.modelRequestStartedAt;
    const durationMS = startedAt !== undefined ? Math.max(0, Date.now() - startedAt) : undefined;
    const finish: AssistantFinishPayload = {
      reason,
      attempt_id: this.attemptID,
      ...(modelRequestID ? { model_request_id: modelRequestID } : {}),
      ...(durationMS !== undefined ? { duration_ms: durationMS } : {}),
      ...(usage ? { usage, usage_source: "provider" } : {}),
    };
    this.evolutionTrace?.modelEnd(modelRequestID, reason, durationMS, usage);
    this.flushStreamChunks();
    this.enqueue(() => this.appendJournalEvent(turnID, "assistant/message", {
      ...finish,
      interrupted: finish.reason === "aborted",
    } as never));
  }

  private projectThinking(turnID: string, event: ThinkingAssistantEvent): void {
    const { state, emit } = applyThinkingEvent(this.thinkingProjection, event);
    this.thinkingProjection = state;
    if (!emit) return;
    this.streamBuffer.append({
      kind: "thinking.chunk",
      delta: emit.delta,
      step_id: `pi_${turnID}`,
      attempt_id: this.attemptID,
      content_index: emit.content_index,
      ...(emit.truncated ? { truncated: true } : {}),
    });
  }

  private resetTurnState(): void {
    this.streamBuffer.dispose();
    this.eventBatcher.dispose();
    this.streamBuffer = this.createStreamBuffer();
    this.eventBatcher = this.createEventBatcher();
    this.output = "";
    this.thinkingProjection = createThinkingProjectionState();
    this.unknownPiEventTypes.clear();
    this.artifact = undefined;
    this.executionLease = undefined;
    this.checkpointSequence = 0;
    this.checkpointWrites = Promise.resolve();
    this.modelRequestSequence = 0;
    this.queuedEventSequence = 0;
    this.currentModelRequestID = undefined;
    this.modelRequestStartedAt = undefined;
    this.completedModelRequestIDs.clear();
    this.executionPhase = "claimed";
    this.executionPhaseUpdates = Promise.resolve();
    this.executionStopping = false;
    this.executionLeaseError = undefined;
    this.shutdownRequested = false;
    this.shutdownWaitingInputTurnID = undefined;
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
    this.streamCapacityError = undefined;
    this.terminalDrainFailed = false;
    this.iterationError = undefined;
    this.modelError = undefined;
    this.eventChain = Promise.resolve();
  }

  private async cleanupAfterTurn(): Promise<void> {
    await this.checkpointWrites;
    this.flushStreamChunks();
    await this.eventChain;
    // terminal 失败后队首仍保留原批次；本地只更新状态快照，cleanup
    // 不得静默重试并把一次未知提交改写成另一 PG 终态。
    if (!this.terminalDrainFailed) {
      await this.flushPublishedEvents().catch((error: unknown) => {
        this.persistenceError ??= error instanceof Error ? error : new Error("Agent event persistence failed");
      });
    }
    await this.stopExecutionHeartbeat();
    this.questionWaiter?.reject(new RuntimeError(409, "turn_finished", "Agent Turn finished before the question was answered"));
    this.clearQuestionTimeout();
    this.questionWaiter = undefined;
    this.pendingQuestionAnswer = undefined;
    this.iterationError = undefined;
    this.session?.dispose();
    this.session = undefined;
    this.abortController = undefined;
    this.currentTurn = undefined;
  }
}
