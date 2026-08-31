/**
 * 本 Agent-service 实例的文件型 Turn 存储。
 *
 * session/event 文件是交互式运行时状态。业务权威仍是 ProductFlow PostgreSQL：
 * 重启不得把已经 claim 的 Turn 再入队；无法证明的进行中 Turn 记 unknown，不是 failed。
 */

import { appendFile, mkdir, open, readdir, readFile, rename, truncate, unlink, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { randomUUID } from "node:crypto";
import { spawn } from "node:child_process";
import {
  API_VERSION,
  EVENT_SCHEMA_VERSION,
  JsonObject,
  JsonValue,
  Scope,
  StartTurnInput,
  TurnArtifact,
  AgentExecutionLease,
  TurnEvent,
  TurnQuestion,
  TurnState,
  TurnStatus,
  ToolStep,
  isTerminalStatus,
  nowISO,
  sameRuntimeScope,
} from "./contracts.js";
import { JOURNAL_EVENT_MAX_PAYLOAD_BYTES, JOURNAL_EVENT_MAX_SEQUENCE } from "./pi-chunks.js";

export class RuntimeError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "RuntimeError";
    this.status = status;
    this.code = code;
  }
}

interface RunRecord {
  scope: Scope;
  turn_ids: string[];
  idempotency: Record<string, string>;
  last_turn_id: string | null;
}

interface PersistedEvents {
  sequence: number;
  items: TurnEvent[];
}

interface PublishedEventAck {
  sequence: number;
}

export interface TurnRecoveryCandidate {
  scope: Scope;
  turnID: string;
}

export interface DurableHandoffCandidate extends TurnRecoveryCandidate {
  idempotencyKey: string;
  executionID: string;
  projectionID: string;
}

export interface ProcessLockHandle {
  processID: number;
  release(): Promise<void>;
}

interface DurableHandoffRecord {
  schema_version: 1;
  execution_id: string;
  projection_id: string;
  attempt: number;
  fencing_token: number;
}

export interface TurnRecoverySummary {
  queued: TurnRecoveryCandidate[];
  deferred: number;
  waitingInput: number;
  restoredTerminal: number;
  unknown: number;
}

const RESTART_UNKNOWN_ERROR = "Agent service restarted before this Turn reached a provable terminal state";

/** 本进程的 Turn 文件。不是第二份业务 transcript。 */
export class TurnStore {
  private readonly locks = new Map<string, Promise<void>>();
  private readonly eventCache = new Map<string, PersistedEvents>();
  private readonly initializedEventAcks = new Set<string>();

  constructor(readonly root: string) { }

  async init(): Promise<void> {
    await mkdir(join(this.root, "runs"), { recursive: true, mode: 0o700 });
    await mkdir(join(this.root, "sessions"), { recursive: true, mode: 0o700 });
    await mkdir(join(this.root, "workspaces"), { recursive: true, mode: 0o700 });
  }

  /** 同一数据目录使用稳定 owner identity，进程重启后才能取回尚未过期的原 lease。 */
  async publisherID(): Promise<string> {
    const path = join(this.root, "publisher.json");
    try {
      const record = await this.readJSON<{ id: string }>(path);
      if (typeof record.id !== "string" || record.id.trim() === "") throw new Error("Agent publisher identity is invalid");
      return record.id;
    } catch (error: unknown) {
      if (!isENOENT(error)) throw error;
      const id = randomUUID();
      try {
        await writeFile(path, `${JSON.stringify({ id })}\n`, { encoding: "utf8", mode: 0o600, flag: "wx" });
        return id;
      } catch (createError: unknown) {
        if (!isEEXIST(createError)) throw createError;
        const record = await this.readJSON<{ id: string }>(path);
        if (typeof record.id !== "string" || record.id.trim() === "") throw new Error("Agent publisher identity is invalid");
        return record.id;
      }
    }
  }

  /** 同一数据目录只能由一个 Agent 进程持有，避免 stable owner 被并发进程共享。 */
  async acquireProcessLock(onLost?: (error: Error) => void): Promise<ProcessLockHandle> {
    const path = join(this.root, "agent-service.lock");
    const lockProcess = spawn("flock", ["-n", "-F", path, "sh", "-c", "printf ready; while read line; do :; done"], {
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stderr = "";
    let stdout = "";
    let ready = false;
    let released = false;
    lockProcess.stderr.on("data", (chunk: Buffer) => {
      stderr += chunk.toString("utf8");
    });
    await new Promise<void>((resolve, reject) => {
      let settled = false;
      const finish = (operation: () => void) => {
        if (settled) return;
        settled = true;
        operation();
      };
      lockProcess.once("error", (error) => finish(() => reject(error)));
      lockProcess.once("exit", (code) => {
        finish(() => reject(
          code === 1
            ? new RuntimeError(409, "data_root_locked", "Agent data root is already owned by another process")
            : new Error(`Agent data root lock failed: ${stderr.trim() || `flock exited ${code}`}`),
        ));
      });
      lockProcess.stdout.on("data", (chunk: Buffer) => {
        stdout += chunk.toString("utf8");
        if (!ready && stdout.startsWith("ready")) {
          ready = true;
          finish(resolve);
        }
      });
    });
    lockProcess.once("exit", (code, signal) => {
      if (released) return;
      onLost?.(new Error(`Agent data root lock was lost: ${stderr.trim() || signal || `flock exited ${code}`}`));
    });
    return {
      processID: lockProcess.pid!,
      release: async () => {
        if (released) return;
        released = true;
        if (lockProcess.exitCode !== null || lockProcess.signalCode !== null) return;
        const exited = new Promise<void>((resolve) => lockProcess.once("exit", () => resolve()));
        lockProcess.stdin.end();
        await exited;
      },
    };
  }

  /** 只返回已经 claim 且仍有本地未确认事件的 Turn；queued 事件会在正常 claim 后同步。 */
  async durableHandoffCandidates(): Promise<DurableHandoffCandidate[]> {
    const candidates: DurableHandoffCandidate[] = [];
    let runEntries: import("node:fs").Dirent[] = [];
    try {
      runEntries = await readdir(join(this.root, "runs"), { withFileTypes: true });
    } catch (error: unknown) {
      if (isENOENT(error)) return candidates;
      throw error;
    }
    for (const entry of runEntries.filter((candidate) => candidate.isDirectory()).sort((left, right) => left.name.localeCompare(right.name))) {
      const record = await this.loadRun(entry.name);
      for (const turnID of record.turn_ids) {
        const state = await this.getState(record.scope.run_id, turnID);
        if (state.execution_attempt === undefined && state.execution_fencing_token === undefined) continue;
        const handoff = await this.loadDurableHandoff(record.scope.run_id, turnID);
        if (!handoff || !await this.hasPublishedAck(record.scope.run_id, turnID)) continue;
        candidates.push({
          scope: record.scope,
          turnID,
          idempotencyKey: state.input.idempotency_key,
          executionID: handoff.execution_id,
          projectionID: handoff.projection_id,
        });
      }
    }
    return candidates;
  }

  async loadRun(runID: string): Promise<RunRecord> {
    return this.readJSON<RunRecord>(this.runPath(runID)).catch((error: unknown) => {
      if (isENOENT(error)) throw new RuntimeError(404, "not_found", "Agent run does not exist");
      throw error;
    });
  }

  async ensureRun(scope: Scope): Promise<RunRecord> {
    return this.serial(`run:${scope.run_id}`, () => this.ensureRunUnlocked(scope));
  }

  private async ensureRunUnlocked(scope: Scope): Promise<RunRecord> {
    const path = this.runPath(scope.run_id);
    try {
      const existing = await this.readJSON<RunRecord>(path);
      if (!sameRuntimeScope(existing.scope, scope)) {
        throw new RuntimeError(409, "conflict", "persisted Agent scope conflicts with ProductFlow contract");
      }
      return existing;
    } catch (error: unknown) {
      if (!isENOENT(error)) throw error;
      const record: RunRecord = { scope, turn_ids: [], idempotency: {}, last_turn_id: null };
      await mkdir(dirname(path), { recursive: true, mode: 0o700 });
      await this.writeJSON(path, record);
      return record;
    }
  }

  async listRunStates(runID: string): Promise<TurnState[]> {
    const record = await this.loadRun(runID);
    const result: TurnState[] = [];
    for (const turnID of record.turn_ids) {
      result.push(await this.getState(runID, turnID));
    }
    return result;
  }

  /** 扫描本地 Turn，分别标成重新入队、等待、还原终态或 unknown。 */
  async recoverAfterRestart(): Promise<TurnRecoverySummary> {
    const queued: TurnRecoveryCandidate[] = [];
    let deferred = 0;
    let waitingInput = 0;
    let restoredTerminal = 0;
    let unknown = 0;
    let runEntries: import("node:fs").Dirent[] = [];
    try {
      runEntries = await readdir(join(this.root, "runs"), { withFileTypes: true });
    } catch (error: unknown) {
      if (isENOENT(error)) return { queued, deferred, waitingInput, restoredTerminal, unknown };
      throw error;
    }
    for (const entry of runEntries.filter((candidate) => candidate.isDirectory()).sort((left, right) => left.name.localeCompare(right.name))) {
      const record = await this.loadRun(entry.name);
      for (const turnID of record.turn_ids) {
        const state = await this.getState(record.scope.run_id, turnID);
        if (isTerminalStatus(state.status)) continue;
        const events = await this.events(record.scope.run_id, turnID, 0);
        const result = await this.recoverTurnAfterRestart(record.scope, state, events);
        if (result === "queued") queued.push({ scope: record.scope, turnID });
        else if (result === "deferred") deferred += 1;
        else if (result === "waiting_input") waitingInput += 1;
        else if (result === "restored_terminal") restoredTerminal += 1;
        else if (result === "unknown") unknown += 1;
      }
    }
    return { queued, deferred, waitingInput, restoredTerminal, unknown };
  }

  /**
   * 把幂等键绑到一个 Turn。输入相同则回放；键被不同输入占用则冲突。
   */
  async createTurn(
    scope: Scope,
    input: StartTurnInput,
    requestedTurnID?: string,
  ): Promise<{ state: TurnState; created: boolean }> {
    return this.serial(`run:${scope.run_id}`, async () => {
      const record = await this.ensureRunUnlocked(scope);
      const adoptedTurnID = requestedTurnID === undefined ? undefined : normalizeTurnID(requestedTurnID);
      const existingID = record.idempotency[input.idempotency_key];
      if (existingID) {
        if (adoptedTurnID !== undefined && existingID !== adoptedTurnID) {
          throw new RuntimeError(409, "turn_identity_conflict", "idempotency_key is already bound to a different Agent Turn");
        }
        const existing = await this.getState(scope.run_id, existingID);
        if (JSON.stringify(existing.input) !== JSON.stringify(input)) {
          throw new RuntimeError(409, "idempotency_conflict", "idempotency_key was already used with different input");
        }
        return { state: existing, created: false };
      }
      const turnID = adoptedTurnID ?? randomUUID();
      if (record.turn_ids.includes(turnID)) {
        const existing = await this.getState(scope.run_id, turnID);
        if (JSON.stringify(existing.input) !== JSON.stringify(input)) {
          throw new RuntimeError(409, "turn_identity_conflict", "requested Agent Turn ID is already bound to different input");
        }
        record.idempotency[input.idempotency_key] = turnID;
        record.last_turn_id = turnID;
        await this.writeJSON(this.runPath(scope.run_id), record);
        return { state: existing, created: false };
      }
      const now = nowISO();
      const state: TurnState = {
        api_version: API_VERSION,
        run_id: scope.run_id,
        turn_id: turnID,
        status: "queued",
        input,
        tool_steps: [],
        output: "",
        thinking: "",
        error: "",
        created_at: now,
        updated_at: now,
        started_at: null,
        finished_at: null,
      };
      record.turn_ids.push(turnID);
      record.idempotency[input.idempotency_key] = turnID;
      record.last_turn_id = turnID;
      await this.writeJSON(this.runPath(scope.run_id), record);
      await this.writeJSON(this.statePath(scope.run_id, turnID), state);
      return { state, created: true };
    });
  }

  async getState(runID: string, turnID: string): Promise<TurnState> {
    try {
      return await this.readJSON<TurnState>(this.statePath(runID, turnID));
    } catch (error: unknown) {
      if (isENOENT(error)) throw new RuntimeError(404, "not_found", "Agent Turn does not exist");
      throw error;
    }
  }

  /**
   * 终态事件优先于快照。本地仍是 queued 但已被 claim 的快照交给 ProductFlow；
   * 进行中的工具步骤改成 unknown。
   */
  private async recoverTurnAfterRestart(
    scope: Scope,
    state: TurnState,
    events: TurnEvent[],
  ): Promise<"queued" | "deferred" | "waiting_input" | "terminal" | "restored_terminal" | "unknown"> {
    return this.serial(this.eventKey(scope.run_id, state.turn_id), async () => {
      const current = await this.getState(scope.run_id, state.turn_id);
      const terminalEvent = [...events].reverse().find((event) => terminalStatusFromEvent(event) !== null);
      if (terminalEvent) {
        const status = terminalStatusFromEvent(terminalEvent);
        if (status === null) return "terminal";
        if (isTerminalStatus(current.status)) return "terminal";
        const artifact = artifactFromTerminalEvents(events, terminalEvent);
        const summary = await this.journalText(scope.run_id, state.turn_id, terminalEvent.sequence);
        await this.updateStateUnlocked(scope.run_id, state.turn_id, {
          status,
          output: summary.output,
          thinking: summary.thinking,
          error: stringPayload(terminalEvent.payload.error) ?? "",
          artifact,
          finished_at: terminalEvent.created_at,
          question: undefined,
        });
        return "restored_terminal";
      }
      if (isTerminalStatus(current.status)) return "terminal";
      if (current.status === "cancel_requested") {
        if (!events.some((event) => event.kind === "turn/cancel_requested")) {
          await this.appendEventUnlocked(
            scope.run_id,
            state.turn_id,
            "turn/cancel_requested",
            { status: "cancel_requested" },
          );
        }
        return "queued";
      }
      if (
        current.status === "requires_input" &&
        current.question &&
        events.some((event) => event.kind === "question/requested")
      ) {
        return "waiting_input";
      }
      if (current.execution_attempt !== undefined || current.execution_fencing_token !== undefined) {
        // claim 之后执行阶段归 ProductFlow。本地 queued 快照可能落后于 durable 阶段，
        // 只有从未被碰过的 queued Turn 才可以在 Agent 本地重新入队。其余恢复由 PG lease/fencing 决定，
        // 本地不得追加一个无法发布的第二终态。
        return "deferred";
      }
      if (current.status === "queued") {
        return "queued";
      }
      const recoveredToolSteps = unknownRunningToolSteps(current.tool_steps);
      for (const step of recoveredToolSteps ?? []) {
        if (current.tool_steps?.some((candidate) => candidate.step_id === step.step_id && candidate.status !== step.status)) {
          await this.appendEventUnlocked(
            scope.run_id,
            state.turn_id,
            "tool/result",
            step as unknown as JsonObject,
          );
        }
      }
      await this.appendEventUnlocked(scope.run_id, state.turn_id, "turn/end", {
        reason: "unknown",
        status: "unknown",
        error: RESTART_UNKNOWN_ERROR,
      });
      await this.updateStateUnlocked(scope.run_id, state.turn_id, {
        status: "unknown",
        error: RESTART_UNKNOWN_ERROR,
        question: undefined,
        tool_steps: recoveredToolSteps,
        finished_at: nowISO(),
      });
      return "unknown";
    });
  }

  async updateState(
    runID: string,
    turnID: string,
    patch: Partial<Pick<TurnState, "status" | "execution_attempt" | "execution_fencing_token" | "question" | "artifact" | "tool_steps" | "output" | "thinking" | "error" | "started_at" | "finished_at">>,
  ): Promise<TurnState> {
    return this.serial(runID + ":" + turnID, () => this.updateStateUnlocked(runID, turnID, patch));
  }

  async appendEvent(runID: string, turnID: string, kind: string, payload: JsonObject, ignorable = false): Promise<TurnEvent> {
    return this.serial(runID + ":" + turnID, () => this.appendEventUnlocked(runID, turnID, kind, payload, ignorable));
  }

  /** 只追加本地 WAL；是否提交 PG 始终由持有 lease 的 runtime 决定。 */
  async appendLocalEvent(runID: string, turnID: string, kind: string, payload: JsonObject): Promise<TurnEvent> {
    return this.serial(runID + ":" + turnID, () => this.appendEventUnlocked(runID, turnID, kind, payload));
  }

  async events(runID: string, turnID: string, after: number): Promise<TurnEvent[]> {
    const events = await this.loadEventsRecord(runID, turnID);
    return events.items.filter((event) => event.sequence > after);
  }

  async unpublishedEvents(runID: string, turnID: string): Promise<TurnEvent[]> {
    const events = await this.loadEventsRecord(runID, turnID);
    const publishedSequence = await this.publishedSequence(runID, turnID);
    return events.items.filter((event) => event.sequence > publishedSequence);
  }

  async journalText(runID: string, turnID: string, throughSequence = Number.MAX_SAFE_INTEGER): Promise<{ output: string; thinking: string }> {
    const events = (await this.loadEventsRecord(runID, turnID)).items.filter((event) => event.sequence < throughSequence);
    let output = "";
    let thinking = "";
    for (const event of events) {
      if (event.payload.compacted === true) continue;
      if (event.kind === "text.chunk") output += stringPayload(event.payload.delta) ?? "";
      if (event.kind === "thinking.chunk") thinking += stringPayload(event.payload.delta) ?? "";
      if (event.kind === "assistant/message" && typeof event.payload.text === "string") output = event.payload.text;
    }
    return { output, thinking };
  }

  async adoptAuthoritativeTerminal(
    runID: string,
    turnID: string,
    terminal: {
      payload: JsonObject;
      projection_status: TurnStatus;
      output: string;
      thinking: string;
      error: string;
      finished_at: string;
    },
  ): Promise<void> {
    const status = terminalStatusFromPayload(terminal.payload);
    if (!status || status !== terminal.projection_status || !isTerminalStatus(terminal.projection_status)) {
      throw new RuntimeError(409, "authoritative_terminal_invalid", "ProductFlow returned an invalid terminal event");
    }
    await this.updateState(runID, turnID, {
      status,
      output: terminal.output,
      thinking: terminal.thinking,
      error: terminal.error,
      artifact: parseArtifact(terminal.payload.artifact),
      question: undefined,
      finished_at: terminal.finished_at,
    });
  }

  async saveDurableHandoff(runID: string, turnID: string, lease: AgentExecutionLease): Promise<void> {
    await this.ensurePublishedAck(runID, turnID);
    await this.writeJSON(this.handoffPath(runID, turnID), {
      schema_version: 1,
      execution_id: lease.execution_id,
      projection_id: lease.projection_id,
      attempt: lease.attempt,
      fencing_token: lease.fencing_token,
    } satisfies DurableHandoffRecord);
  }

  async clearDurableHandoff(runID: string, turnID: string): Promise<void> {
    await unlink(this.handoffPath(runID, turnID)).catch((error: unknown) => {
      if (!isENOENT(error)) throw error;
    });
  }

  /** PG 已确认终态时，移除其后仅存在于本地的 persistence-failed fallback，并恢复本地快照。 */
  async reconcileConfirmedTerminal(runID: string, turnID: string): Promise<boolean> {
    return this.serial(this.eventKey(runID, turnID), async () => {
      const confirmedThrough = await this.publishedSequence(runID, turnID);
      const events = await this.loadEventsRecord(runID, turnID);
      const terminal = [...events.items]
        .reverse()
        .find((event) => event.sequence <= confirmedThrough && terminalStatusFromEvent(event) !== null);
      if (!terminal) return false;
      const suffix = events.items.filter((event) => event.sequence > terminal.sequence);
      if (!suffix.every(isLocalPersistenceFallback)) {
        throw new RuntimeError(409, "confirmed_terminal_suffix_conflict", "Agent local journal contains events after a confirmed terminal");
      }
      if (suffix.length > 0) {
        events.items = events.items.filter((event) => event.sequence <= terminal.sequence);
        events.sequence = terminal.sequence;
        this.eventCache.set(this.eventKey(runID, turnID), events);
        await this.rewriteEventsWAL(runID, turnID, events);
      }
      const status = terminalStatusFromEvent(terminal)!;
      const summary = await this.journalText(runID, turnID, terminal.sequence);
      await this.updateStateUnlocked(runID, turnID, {
        status,
        output: summary.output,
        thinking: summary.thinking,
        error: stringPayload(terminal.payload.error) ?? "",
        artifact: artifactFromTerminalEvents(events.items, terminal),
        question: undefined,
        finished_at: terminal.created_at,
      });
      return true;
    });
  }

  private async loadDurableHandoff(runID: string, turnID: string): Promise<DurableHandoffRecord | undefined> {
    try {
      const record = await this.readJSON<DurableHandoffRecord>(this.handoffPath(runID, turnID));
      if (record.schema_version !== 1 || !record.execution_id || !record.projection_id) {
        throw new Error("Agent durable handoff record is invalid");
      }
      return record;
    } catch (error: unknown) {
      if (isENOENT(error)) return undefined;
      throw error;
    }
  }

  async publishedThrough(runID: string, turnID: string): Promise<number> {
    return this.publishedSequence(runID, turnID);
  }

  async markEventsPublished(runID: string, turnID: string, sequence: number): Promise<void> {
    await this.serial(`ack:${this.eventKey(runID, turnID)}`, async () => {
      const events = await this.loadEventsRecord(runID, turnID);
      const publishedSequence = await this.publishedSequence(runID, turnID);
      if (!Number.isInteger(sequence) || sequence < publishedSequence || sequence > events.sequence) {
        throw new RuntimeError(409, "published_sequence_invalid", "Agent published event sequence is invalid");
      }
      if (sequence === publishedSequence) return;
      await this.writeJSON(this.eventAckPath(runID, turnID), { sequence } satisfies PublishedEventAck);
      this.initializedEventAcks.add(this.eventKey(runID, turnID));
    });
  }

  private async publishedSequence(runID: string, turnID: string): Promise<number> {
    try {
      const ack = await this.readJSON<PublishedEventAck>(this.eventAckPath(runID, turnID));
      if (!Number.isInteger(ack.sequence) || ack.sequence < 0) throw new Error("Agent event ACK is invalid");
      this.initializedEventAcks.add(this.eventKey(runID, turnID));
      return ack.sequence;
    } catch (error: unknown) {
      if (isENOENT(error)) return 0;
      throw error;
    }
  }

  private async hasPublishedAck(runID: string, turnID: string): Promise<boolean> {
    try {
      await this.readJSON<PublishedEventAck>(this.eventAckPath(runID, turnID));
      this.initializedEventAcks.add(this.eventKey(runID, turnID));
      return true;
    } catch (error: unknown) {
      if (isENOENT(error)) return false;
      throw error;
    }
  }

  private async ensurePublishedAck(runID: string, turnID: string): Promise<void> {
    const key = this.eventKey(runID, turnID);
    if (this.initializedEventAcks.has(key)) return;
    if (await this.hasPublishedAck(runID, turnID)) return;
    await this.writeJSON(this.eventAckPath(runID, turnID), { sequence: 0 } satisfies PublishedEventAck);
    this.initializedEventAcks.add(key);
  }

  async setToolStep(runID: string, turnID: string, step: ToolStep): Promise<TurnState> {
    return this.serial(this.eventKey(runID, turnID), async () => {
      const state = await this.getState(runID, turnID);
      const steps = [...(state.tool_steps ?? [])];
      const index = steps.findIndex((candidate) => candidate.step_id === step.step_id);
      if (index >= 0) steps[index] = step;
      else steps.push(step);
      await this.appendEventUnlocked(
        runID,
        turnID,
        step.status === "running" ? "tool/call" : "tool/result",
        step as unknown as JsonObject,
      );
      return this.updateStateUnlocked(runID, turnID, { tool_steps: steps });
    });
  }

  /** 在同一个 Turn 锁里追加终态事件并更新快照。 */
  async terminal(
    runID: string,
    turnID: string,
    status: Extract<TurnStatus, "succeeded" | "failed" | "canceled" | "unknown" | "awaiting_confirmation">,
    details: {
      output?: string;
      error?: string;
      question?: TurnQuestion;
      artifact?: TurnArtifact;
      approval?: JsonObject;
      thinking?: string;
      reason_code?: "provider_failed" | "execution_interrupted" | "effect_unknown" | "persistence_failed";
      approval_already_recorded?: boolean;
    },
  ): Promise<TurnState> {
    return this.serial(this.eventKey(runID, turnID), async () => {
      const current = await this.getState(runID, turnID);
      const output = details.output ?? stateOutput(current);
      const thinking = details.thinking ?? current.thinking ?? "";
      const error = details.error ?? "";
      if (details.question) {
        await this.appendEventUnlocked(runID, turnID, "question/requested", details.question as unknown as JsonObject);
      }
      if (details.artifact && !details.approval_already_recorded) {
        await this.appendEventUnlocked(runID, turnID, "approval/requested", {
          approval_id: details.artifact.step_id,
          approval_kind: "artifact",
          artifact: details.artifact as unknown as JsonObject,
        });
      }
      if (details.approval && !details.approval_already_recorded) {
        await this.appendEventUnlocked(runID, turnID, "approval/requested", details.approval);
      }
      await this.appendEventUnlocked(runID, turnID, "turn/end", {
        reason: status === "succeeded" ? "completed" : status,
        ...(details.reason_code ? { reason_code: details.reason_code } : {}),
        status,
        error,
        ...(details.question ? { question: details.question as unknown as JsonObject } : {}),
        ...(details.artifact ? { artifact: details.artifact as unknown as JsonObject } : {}),
      });
      return this.updateStateUnlocked(runID, turnID, {
        status,
        output,
        thinking,
        error,
        question: details.question,
        artifact: details.artifact,
        finished_at: nowISO(),
      });
    });
  }

  async workspace(runID: string): Promise<string> {
    const path = join(this.root, "workspaces", runID);
    await mkdir(path, { recursive: true, mode: 0o700 });
    return path;
  }

  sessionDir(runID: string): string {
    return join(this.root, "sessions", runID);
  }

  private runPath(runID: string): string {
    return join(this.root, "runs", runID, "run.json");
  }

  private statePath(runID: string, turnID: string): string {
    return join(this.root, "runs", runID, "turns", `${turnID}.json`);
  }

  private eventsPath(runID: string, turnID: string): string {
    return join(this.root, "runs", runID, "events", `${turnID}.jsonl`);
  }

  private eventAckPath(runID: string, turnID: string): string {
    return join(this.root, "runs", runID, "event-acks", `${turnID}.json`);
  }

  private handoffPath(runID: string, turnID: string): string {
    return join(this.root, "runs", runID, "handoffs", `${turnID}.json`);
  }

  private async readJSON<T>(path: string): Promise<T> {
    return JSON.parse(await readFile(path, "utf8")) as T;
  }

  /** 原子替换，避免崩溃留下写到一半的 Turn 文件。 */
  private async writeJSON(path: string, value: unknown): Promise<void> {
    await mkdir(dirname(path), { recursive: true, mode: 0o700 });
    const temporary = `${path}.${randomUUID()}.tmp`;
    await writeFile(temporary, `${JSON.stringify(value)}\n`, { mode: 0o600, flush: true });
    await rename(temporary, path);
  }

  private async updateStateUnlocked(
    runID: string,
    turnID: string,
    patch: Partial<Pick<TurnState, "status" | "execution_attempt" | "execution_fencing_token" | "question" | "artifact" | "tool_steps" | "output" | "thinking" | "error" | "started_at" | "finished_at">>,
  ): Promise<TurnState> {
    const state = await this.getState(runID, turnID);
    const next: TurnState = { ...state, ...patch, updated_at: nowISO() };
    await this.writeJSON(this.statePath(runID, turnID), next);
    return next;
  }

  private async appendEventUnlocked(
    runID: string,
    turnID: string,
    kind: string,
    payload: JsonObject,
    ignorable = false,
  ): Promise<TurnEvent> {
    const key = this.eventKey(runID, turnID);
    const events = await this.loadEventsRecord(runID, turnID);
    if (events.sequence >= JOURNAL_EVENT_MAX_SEQUENCE) {
      throw new RuntimeError(409, "event_sequence_exhausted", "Agent Turn journal event budget is exhausted");
    }
    if (Buffer.byteLength(JSON.stringify(payload), "utf8") > JOURNAL_EVENT_MAX_PAYLOAD_BYTES) {
      throw new RuntimeError(413, "event_payload_too_large", "Agent Turn journal event payload exceeds the limit");
    }
    const event: TurnEvent = {
      schema_version: EVENT_SCHEMA_VERSION,
      run_id: runID,
      turn_id: turnID,
      sequence: events.sequence + 1,
      created_at: nowISO(),
      kind,
      ...(ignorable ? { ignorable: true } : {}),
      payload,
    };
    await this.ensurePublishedAck(runID, turnID);
    const path = this.eventsPath(runID, turnID);
    await mkdir(dirname(path), { recursive: true, mode: 0o700 });
    await appendFile(path, `${JSON.stringify(event)}\n`, { encoding: "utf8", mode: 0o600, flush: true });
    events.sequence = event.sequence;
    events.items.push(event);
    this.eventCache.set(key, events);
    return event;
  }

  private async loadEventsRecord(runID: string, turnID: string): Promise<PersistedEvents> {
    const key = this.eventKey(runID, turnID);
    const cached = this.eventCache.get(key);
    if (cached) return cached;
    try {
      const path = this.eventsPath(runID, turnID);
      let text = await readFile(path, "utf8");
      if (text !== "" && !text.endsWith("\n")) {
        const completeLength = text.lastIndexOf("\n") + 1;
        await truncate(path, Buffer.byteLength(text.slice(0, completeLength), "utf8"));
        const handle = await open(path, "r+");
        try {
          await handle.sync();
        } finally {
          await handle.close();
        }
        text = text.slice(0, completeLength);
      }
      const items = text.trim() === ""
        ? []
        : text.trimEnd().split("\n").map((line) => {
          try {
            return JSON.parse(line) as TurnEvent;
          } catch {
            throw new RuntimeError(409, "event_journal_corrupt", "Agent Turn journal contains a corrupt record");
          }
        });
      for (let index = 0; index < items.length; index += 1) {
        if (items[index]?.sequence !== index + 1) {
          throw new RuntimeError(409, "event_journal_corrupt", "Agent Turn journal sequence is not contiguous");
        }
      }
      const events: PersistedEvents = { sequence: items.length, items };
      this.eventCache.set(key, events);
      return events;
    } catch (error: unknown) {
      if (!isENOENT(error)) throw error;
      const empty: PersistedEvents = { sequence: 0, items: [] };
      this.eventCache.set(key, empty);
      return empty;
    }
  }

  private async rewriteEventsWAL(runID: string, turnID: string, events: PersistedEvents): Promise<void> {
    const path = this.eventsPath(runID, turnID);
    await mkdir(dirname(path), { recursive: true, mode: 0o700 });
    const temporary = `${path}.${randomUUID()}.tmp`;
    const contents = events.items.map((event) => JSON.stringify(event)).join("\n");
    await writeFile(temporary, contents === "" ? "" : `${contents}\n`, { mode: 0o600, flush: true });
    await rename(temporary, path);
  }

  private eventKey(runID: string, turnID: string): string {
    return `${runID}:${turnID}`;
  }

  /** 按 run/Turn 串行写入，保证事件序号单调。 */
  private async serial<T>(key: string, operation: () => Promise<T>): Promise<T> {
    const previous = this.locks.get(key) ?? Promise.resolve();
    let release!: () => void;
    const current = new Promise<void>((resolve) => {
      release = resolve;
    });
    const queued = previous.then(() => current);
    this.locks.set(key, queued);
    await previous;
    try {
      return await operation();
    } finally {
      release();
      if (this.locks.get(key) === queued) this.locks.delete(key);
    }
  }
}

function stateOutput(state: TurnState): string {
  return state.output;
}

function terminalStatusFromEvent(event: TurnEvent): Extract<TurnStatus, "succeeded" | "failed" | "canceled" | "unknown" | "awaiting_confirmation"> | null {
  if (event.kind !== "turn/end") return null;
  return terminalStatusFromPayload(event.payload);
}

function terminalStatusFromPayload(payload: JsonObject): Extract<TurnStatus, "succeeded" | "failed" | "canceled" | "unknown" | "awaiting_confirmation"> | null {
  const reason = typeof payload.reason === "string" ? payload.reason : payload.status;
  const candidate = reason === "completed" ? "succeeded" : reason;
  if (!isTerminalStatus(candidate as TurnStatus)) return null;
  if (typeof candidate !== "string") return null;
  return candidate as Extract<TurnStatus, "succeeded" | "failed" | "canceled" | "unknown" | "awaiting_confirmation">;
}

function artifactFromTerminalEvents(events: TurnEvent[], terminalEvent: TurnEvent): TurnArtifact | undefined {
  const nested = parseArtifact(terminalEvent.payload.artifact);
  if (nested) return nested;
  const proposed = [...events].reverse().find((event) => event.kind === "approval/requested");
  if (!proposed) return undefined;
  return parseArtifact(proposed.payload.artifact) ?? parseArtifact(proposed.payload);
}

function parseArtifact(value: JsonValue | undefined): TurnArtifact | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const candidate = value as Record<string, JsonValue>;
  if (
    typeof candidate.name !== "string" ||
    typeof candidate.step_id !== "string" ||
    !candidate.value ||
    typeof candidate.value !== "object" ||
    Array.isArray(candidate.value)
  ) {
    return undefined;
  }
  return {
    name: candidate.name,
    step_id: candidate.step_id,
    value: candidate.value as JsonObject,
  };
}

function stringPayload(value: JsonValue | undefined): string | undefined {
  return typeof value === "string" ? value : undefined;
}

/** 进程重启后，进行中的工具步骤无法证明成败。 */
function unknownRunningToolSteps(steps: ToolStep[] | undefined): ToolStep[] | undefined {
  return steps?.map((step) => (step.status === "running" ? { ...step, status: "unknown" } : step));
}

function normalizeTurnID(value: string): string {
  const normalized = value.trim();
  if (!/^[A-Za-z0-9][A-Za-z0-9_-]{0,119}$/u.test(normalized)) {
    throw new RuntimeError(400, "invalid_argument", "requested Agent Turn ID is invalid");
  }
  return normalized;
}

function isENOENT(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && "code" in error && error.code === "ENOENT");
}

function isEEXIST(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && "code" in error && error.code === "EEXIST");
}

function isLocalPersistenceFallback(event: TurnEvent): boolean {
  return event.kind === "turn/end" && event.payload.status === "unknown" && event.payload.reason_code === "persistence_failed";
}
