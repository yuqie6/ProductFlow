import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { randomUUID } from "node:crypto";
import {
  API_VERSION,
  EVENT_SCHEMA_VERSION,
  JsonObject,
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

interface EventWaiter {
  after: number;
  resolve: (hasEvents: boolean) => void;
  reject: (error: unknown) => void;
  timer: ReturnType<typeof setTimeout> | undefined;
  signal: AbortSignal | undefined;
  onAbort: (() => void) | undefined;
}

export class TurnStore {
  private readonly locks = new Map<string, Promise<void>>();
  private readonly eventWaiters = new Map<string, Set<EventWaiter>>();

  constructor(readonly root: string) {}

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

  async createTurn(scope: Scope, input: StartTurnInput): Promise<{ state: TurnState; created: boolean }> {
    return this.serial(`run:${scope.run_id}`, async () => {
      const record = await this.ensureRunUnlocked(scope);
      const existingID = record.idempotency[input.idempotency_key];
      if (existingID) {
        const existing = await this.getState(scope.run_id, existingID);
        if (JSON.stringify(existing.input) !== JSON.stringify(input)) {
          throw new RuntimeError(409, "idempotency_conflict", "idempotency_key was already used with different input");
        }
        return { state: existing, created: false };
      }
      const turnID = randomUUID();
      const now = nowISO();
      const state: TurnState = {
        api_version: API_VERSION,
        run_id: scope.run_id,
        turn_id: turnID,
        status: "queued",
        input,
        tool_steps: [],
        output: "",
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
      await this.appendEvent(scope.run_id, turnID, "turn.queued", { status: "queued" });
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

  async updateState(
    runID: string,
    turnID: string,
    patch: Partial<Pick<TurnState, "status" | "question" | "artifact" | "tool_steps" | "output" | "error" | "started_at" | "finished_at">>,
  ): Promise<TurnState> {
    return this.serial(runID + ":" + turnID, () => this.updateStateUnlocked(runID, turnID, patch));
  }

  async appendEvent(runID: string, turnID: string, kind: string, payload: JsonObject): Promise<TurnEvent> {
    return this.serial(runID + ":" + turnID, () => this.appendEventUnlocked(runID, turnID, kind, payload));
  }

  async events(runID: string, turnID: string, after: number): Promise<TurnEvent[]> {
    try {
      const events = await this.readJSON<PersistedEvents>(this.eventsPath(runID, turnID));
      return events.items.filter((event) => event.sequence > after);
    } catch (error: unknown) {
      if (isENOENT(error)) return [];
      throw error;
    }
  }

  async waitForEvents(
    runID: string,
    turnID: string,
    after: number,
    options: { signal?: AbortSignal; timeoutMS?: number } = {},
  ): Promise<boolean> {
    if ((await this.events(runID, turnID, after)).length > 0) return true;
    if (options.signal?.aborted) return false;

    const key = this.eventKey(runID, turnID);
    return new Promise<boolean>((resolve, reject) => {
      let settled = false;
      const waiters = this.eventWaiters.get(key) ?? new Set<EventWaiter>();
      const waiter: EventWaiter = {
        after,
        resolve: (hasEvents) => finish(hasEvents),
        reject: (error) => finish(undefined, error),
        timer: undefined,
        signal: options.signal,
        onAbort: undefined,
      };
      const finish = (hasEvents?: boolean, error?: unknown) => {
        if (settled) return;
        settled = true;
        waiters.delete(waiter);
        if (waiters.size === 0) this.eventWaiters.delete(key);
        if (waiter.timer !== undefined) clearTimeout(waiter.timer);
        if (waiter.signal && waiter.onAbort) waiter.signal.removeEventListener("abort", waiter.onAbort);
        if (error !== undefined) reject(error);
        else resolve(Boolean(hasEvents));
      };
      waiter.onAbort = () => finish(false);
      waiters.add(waiter);
      this.eventWaiters.set(key, waiters);
      if (options.signal) options.signal.addEventListener("abort", waiter.onAbort, { once: true });
      if (options.timeoutMS !== undefined && options.timeoutMS > 0) {
        waiter.timer = setTimeout(() => finish(false), options.timeoutMS);
      }
      void this.events(runID, turnID, after)
        .then((items) => {
          if (items.length > 0) finish(true);
        })
        .catch((error: unknown) => finish(undefined, error));
    });
  }

  async setToolStep(runID: string, turnID: string, step: ToolStep): Promise<TurnState> {
    return this.serial(this.eventKey(runID, turnID), async () => {
      const state = await this.getState(runID, turnID);
      const steps = [...(state.tool_steps ?? [])];
      const index = steps.findIndex((candidate) => candidate.step_id === step.step_id);
      if (index >= 0) steps[index] = step;
      else steps.push(step);
      await this.appendEventUnlocked(runID, turnID, "tool.step", step as unknown as JsonObject);
      return this.updateStateUnlocked(runID, turnID, { tool_steps: steps });
    });
  }

  async terminal(
    runID: string,
    turnID: string,
    status: Extract<TurnStatus, "succeeded" | "failed" | "canceled" | "unknown" | "awaiting_confirmation">,
    details: { output?: string; error?: string; question?: TurnQuestion; artifact?: TurnArtifact },
  ): Promise<TurnState> {
    return this.serial(this.eventKey(runID, turnID), async () => {
      const current = await this.getState(runID, turnID);
      const output = details.output ?? stateOutput(current);
      const error = details.error ?? "";
      await this.appendEventUnlocked(runID, turnID, `turn.${status}`, {
        status,
        output,
        error,
        ...(details.question ? { question: details.question as unknown as JsonObject } : {}),
        ...(details.artifact ? { artifact: details.artifact as unknown as JsonObject } : {}),
      });
      if (details.question) await this.appendEventUnlocked(runID, turnID, "question.required", details.question as unknown as JsonObject);
      if (details.artifact) await this.appendEventUnlocked(runID, turnID, "artifact.proposed", details.artifact as unknown as JsonObject);
      return this.updateStateUnlocked(runID, turnID, {
        status,
        output,
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

  private async writeJSON(path: string, value: unknown): Promise<void> {
    await mkdir(dirname(path), { recursive: true, mode: 0o700 });
    const temporary = `${path}.${randomUUID()}.tmp`;
    await writeFile(temporary, `${JSON.stringify(value)}\n`, { mode: 0o600 });
    await rename(temporary, path);
  }

  private async updateStateUnlocked(
    runID: string,
    turnID: string,
    patch: Partial<Pick<TurnState, "status" | "question" | "artifact" | "tool_steps" | "output" | "error" | "started_at" | "finished_at">>,
  ): Promise<TurnState> {
    const state = await this.getState(runID, turnID);
    const next: TurnState = { ...state, ...patch, updated_at: nowISO() };
    await this.writeJSON(this.statePath(runID, turnID), next);
    return next;
  }

  private async appendEventUnlocked(runID: string, turnID: string, kind: string, payload: JsonObject): Promise<TurnEvent> {
    const path = this.eventsPath(runID, turnID);
    let events: PersistedEvents = { sequence: 0, items: [] };
    try {
      events = await this.readJSON<PersistedEvents>(path);
    } catch (error: unknown) {
      if (!isENOENT(error)) throw error;
    }
    const event: TurnEvent = {
      schema_version: EVENT_SCHEMA_VERSION,
      run_id: runID,
      turn_id: turnID,
      sequence: events.sequence + 1,
      created_at: nowISO(),
      kind,
      payload,
    };
    events.sequence = event.sequence;
    events.items.push(event);
    await this.writeJSON(path, events);
    this.notifyEventWaiters(runID, turnID, event.sequence);
    return event;
  }

  private notifyEventWaiters(runID: string, turnID: string, sequence: number): void {
    const waiters = this.eventWaiters.get(this.eventKey(runID, turnID));
    if (!waiters) return;
    for (const waiter of [...waiters]) {
      if (sequence > waiter.after) waiter.resolve(true);
    }
  }

  private eventKey(runID: string, turnID: string): string {
    return `${runID}:${turnID}`;
  }

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

function isENOENT(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && "code" in error && error.code === "ENOENT");
}
