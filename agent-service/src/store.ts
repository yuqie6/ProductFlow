/**
 * 本 Agent-service 实例的文件型 Turn 存储。
 *
 * session/event 文件是交互式运行时状态。业务权威仍是 ProductFlow PostgreSQL：
 * 重启不得把已经 claim 的 Turn 再入队；无法证明的进行中 Turn 记 unknown，不是 failed。
 */

import { mkdir, readdir, readFile, rename, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { randomUUID } from "node:crypto";
import {
  API_VERSION,
  EVENT_SCHEMA_VERSION,
  JsonObject,
  JsonValue,
  Scope,
  StartTurnInput,
  TurnArtifact,
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

export interface TurnRecoveryCandidate {
  scope: Scope;
  turnID: string;
}

export interface TurnRecoverySummary {
  queued: TurnRecoveryCandidate[];
  deferred: number;
  waitingInput: number;
  restoredTerminal: number;
  unknown: number;
}

export type DurableEventPublisher = (scope: Scope, event: TurnEvent) => Promise<void>;

const RESTART_UNKNOWN_ERROR = "Agent service restarted before this Turn reached a provable terminal state";

/** 本进程的 Turn 文件。不是第二份业务 transcript。 */
export class TurnStore {
  private readonly locks = new Map<string, Promise<void>>();
  private readonly eventCache = new Map<string, PersistedEvents>();
  private readonly dirtyEvents = new Set<string>();
  private eventPublisher?: DurableEventPublisher;

  constructor(readonly root: string) {}

  setEventPublisher(publisher: DurableEventPublisher): void {
    this.eventPublisher = publisher;
  }

  async init(): Promise<void> {
    await mkdir(join(this.root, "runs"), { recursive: true, mode: 0o700 });
    await mkdir(join(this.root, "sessions"), { recursive: true, mode: 0o700 });
    await mkdir(join(this.root, "workspaces"), { recursive: true, mode: 0o700 });
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
        await this.updateStateUnlocked(scope.run_id, state.turn_id, {
          status,
          output: current.output,
          error: stringPayload(terminalEvent.payload.error) ?? "",
          artifact,
          finished_at: terminalEvent.created_at,
          question: undefined,
        });
        return "restored_terminal";
      }
      if (isTerminalStatus(current.status)) return "terminal";
      if (
        current.status === "queued" &&
        (current.execution_attempt !== undefined || current.execution_fencing_token !== undefined)
      ) {
        // claim 之后执行阶段归 ProductFlow。本地 queued 快照可能落后于 durable 阶段，
        // 只有从未被碰过的 queued Turn 才可以在 Agent 本地重新入队。
        return "deferred";
      }
      if (
        current.status === "requires_input" &&
        current.question &&
        events.some((event) => event.kind === "question/requested")
      ) {
        return "waiting_input";
      }
      if (current.status === "queued") {
        const hasQuestionContinuation = events.some(
          (event) => event.kind === "question/answered",
        );
        if (!hasQuestionContinuation) return "queued";
      }
      const recoveredToolSteps = unknownRunningToolSteps(current.tool_steps);
      for (const step of recoveredToolSteps ?? []) {
        if (current.tool_steps?.some((candidate) => candidate.step_id === step.step_id && candidate.status !== step.status)) {
          await this.appendEventUnlocked(
            scope.run_id,
            state.turn_id,
            "tool/result",
            step as unknown as JsonObject,
            false,
            false,
          );
        }
      }
      await this.appendEventUnlocked(scope.run_id, state.turn_id, "turn/end", {
        reason: "unknown",
        status: "unknown",
        error: RESTART_UNKNOWN_ERROR,
      }, false, false);
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

  /** PG publisher 已失败时仍要在本地留下诚实的 unknown 终态，且不能再次调用同一失败 publisher。 */
  async appendLocalEvent(runID: string, turnID: string, kind: string, payload: JsonObject): Promise<TurnEvent> {
    return this.serial(runID + ":" + turnID, () => this.appendEventUnlocked(runID, turnID, kind, payload, false, false));
  }

  async events(runID: string, turnID: string, after: number): Promise<TurnEvent[]> {
    const events = await this.loadEventsRecord(runID, turnID);
    return events.items.filter((event) => event.sequence > after);
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
      if (details.artifact) {
        await this.appendEventUnlocked(runID, turnID, "approval/requested", {
          approval_id: details.artifact.step_id,
          approval_kind: "artifact",
          artifact: details.artifact as unknown as JsonObject,
        });
      }
      if (details.approval) {
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
    return join(this.root, "runs", runID, "events", `${turnID}.json`);
  }

  private async readJSON<T>(path: string): Promise<T> {
    return JSON.parse(await readFile(path, "utf8")) as T;
  }

  /** 原子替换，避免崩溃留下写到一半的 Turn 文件。 */
  private async writeJSON(path: string, value: unknown): Promise<void> {
    await mkdir(dirname(path), { recursive: true, mode: 0o700 });
    const temporary = `${path}.${randomUUID()}.tmp`;
    await writeFile(temporary, `${JSON.stringify(value)}\n`, { mode: 0o600 });
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
    publish = true,
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
    events.sequence = event.sequence;
    events.items.push(event);
    this.eventCache.set(key, events);
    this.dirtyEvents.add(key);
    await this.flushEventsUnlocked(runID, turnID);
    if (publish && this.eventPublisher) {
      const record = await this.loadRun(runID);
      await this.eventPublisher(record.scope, event);
    }
    return event;
  }

  private async loadEventsRecord(runID: string, turnID: string): Promise<PersistedEvents> {
    const key = this.eventKey(runID, turnID);
    const cached = this.eventCache.get(key);
    if (cached) return cached;
    try {
      const events = await this.readJSON<PersistedEvents>(this.eventsPath(runID, turnID));
      this.eventCache.set(key, events);
      return events;
    } catch (error: unknown) {
      if (!isENOENT(error)) throw error;
      const empty: PersistedEvents = { sequence: 0, items: [] };
      this.eventCache.set(key, empty);
      return empty;
    }
  }

  private async flushEventsUnlocked(runID: string, turnID: string): Promise<void> {
    const key = this.eventKey(runID, turnID);
    if (!this.dirtyEvents.has(key)) return;
    const events = this.eventCache.get(key);
    if (!events) {
      this.dirtyEvents.delete(key);
      return;
    }
    await this.writeJSON(this.eventsPath(runID, turnID), events);
    this.dirtyEvents.delete(key);
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
	const reason = typeof event.payload.reason === "string" ? event.payload.reason : event.payload.status;
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
