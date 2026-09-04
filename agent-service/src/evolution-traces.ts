import { randomUUID } from "node:crypto";
import { appendFile, mkdir, readdir, stat, unlink, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { isTerminalStatus, sha256, type TurnStatus } from "./contracts.js";
import { GRAPH_COMMAND_OPS } from "./graph-command-schema.js";
import { TOOL_MANIFEST } from "./tool-manifest.js";
import type { AssistantFinishPayload } from "./pi-chunks.js";

export const TRACE_LIMITS = Object.freeze({ recordBytes: 4096, traceBytes: 65536, files: 256, pending: 128 });
interface Limits { recordBytes: number; traceBytes: number; files: number; pending: number }
type Fields = Record<string, string | number | boolean | null | string[]>;
const ownedFile = /^\d{13}-[a-f0-9-]{36}\.jsonl$/u;
const tools = new Set<string>(TOOL_MANIFEST.map((tool) => tool.name));
const ops = new Set<string>(GRAPH_COMMAND_OPS);

export interface TraceIdentity {
  runID: string;
  turnID: string;
  attempt: number;
  fencingToken: number;
  harnessHash: string;
  skillHash: string;
}

interface FileEntry { path: string; bytes: number; modified: number; trace?: EvolutionTrace }
interface WriteJob { trace: EvolutionTrace; line: string; first: boolean }

/** Best-effort diagnostics have their own bounded queue, never the journal's promise chain. */
export class EvolutionTraceStore {
  private readonly queue: WriteJob[] = [];
  private readonly files = new Map<string, FileEntry>();
  private worker?: Promise<void>;
  private initialized = false;
  private closed = false;
  private pending = 0;
  private dropped = 0;
  private errors = 0;
  private evicted = 0;

  constructor(readonly root?: string, private readonly limits: Limits = TRACE_LIMITS) {
    if (Object.values(limits).some((n) => !Number.isSafeInteger(n) || n <= 0) || limits.traceBytes < limits.recordBytes * 2) {
      throw new Error("Invalid evolution trace limits");
    }
  }

  start(identity: TraceIdentity, skills: readonly string[]): EvolutionTrace | undefined {
    if (!this.root || this.closed) return undefined;
    return new EvolutionTrace(this, this.limits, skills, identity);
  }

  health() {
    return { enabled: Boolean(this.root), pending_records: this.pending, dropped_records: this.dropped, io_errors: this.errors, evicted_traces: this.evicted };
  }

  enqueue(trace: EvolutionTrace, line: string, first: boolean): boolean {
    if (this.closed || this.pending >= this.limits.pending) return false;
    this.pending += 1;
    trace.pending += 1;
    this.queue.push({ trace, line, first });
    this.kick();
    return true;
  }

  noteDrop(): void { this.dropped += 1; }

  async drain(): Promise<void> {
    while (this.worker) await this.worker;
  }

  async close(): Promise<void> {
    this.closed = true;
    await this.drain();
  }

  private kick(): void {
    if (this.worker) return;
    this.worker = this.writePending().finally(() => {
      this.worker = undefined;
      if (this.queue.length) this.kick();
    });
  }

  private async writePending(): Promise<void> {
    for (let job = this.queue.shift(); job; job = this.queue.shift()) {
      const { trace, line, first } = job;
      try {
        if (trace.ioFailed) continue;
        await this.initialize();
        const path = join(this.root!, trace.fileName);
        if (first) {
          if (!await this.makeRoom()) {
            trace.ioFailed = true;
            this.noteDrop();
            continue;
          }
          // A failed first write can leave a partial file; retain its capacity reservation.
          const file = { path, bytes: this.limits.traceBytes, modified: Date.now(), trace };
          this.files.set(trace.fileName, file);
          await writeFile(path, line, { encoding: "utf8", flag: "wx", mode: 0o600 });
          file.bytes = Buffer.byteLength(line);
        } else {
          await appendFile(path, line, { encoding: "utf8", flag: "a" });
          this.files.get(trace.fileName)!.bytes += Buffer.byteLength(line);
        }
      } catch {
        trace.ioFailed = true;
        this.errors += 1;
      } finally {
        trace.pending -= 1;
        this.pending -= 1;
      }
    }
  }

  private async initialize(): Promise<void> {
    if (this.initialized) return;
    await mkdir(this.root!, { recursive: true, mode: 0o700 });
    for (const item of await readdir(this.root!, { withFileTypes: true })) {
      if (!item.isFile() || !ownedFile.test(item.name)) continue;
      const path = join(this.root!, item.name);
      const info = await stat(path);
      this.files.set(item.name, { path, bytes: info.size, modified: info.mtimeMs });
    }
    this.initialized = true;
  }

  private async makeRoom(): Promise<boolean> {
    const reserved = (file: FileEntry) => file.trace && (!file.trace.closed || file.trace.pending > 0) ? this.limits.traceBytes : file.bytes;
    let total = [...this.files.values()].reduce((sum, file) => sum + reserved(file), 0);
    const candidates = [...this.files.entries()]
      .filter(([, file]) => !file.trace || (file.trace.closed && file.trace.pending === 0))
      .sort((a, b) => a[1].modified - b[1].modified);
    while (this.files.size >= this.limits.files || total + this.limits.traceBytes > this.limits.files * this.limits.traceBytes) {
      const candidate = candidates.shift();
      if (!candidate) return false;
      try {
        await unlink(candidate[1].path);
      } catch (error) {
        if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) throw error;
      }
      total -= reserved(candidate[1]);
      this.files.delete(candidate[0]);
      this.evicted += 1;
    }
    return true;
  }
}

export class EvolutionTrace {
  readonly fileName = `${Date.now()}-${randomUUID()}.jsonl`;
  pending = 0;
  closed = false;
  ioFailed = false;
  private bytes = 0;
  private sequence = 0;
  private dropped = 0;
  private terminal: string | null = null;
  private readonly skills: Set<string>;

  constructor(private readonly store: EvolutionTraceStore, private readonly limits: Limits, skills: readonly string[], identity: TraceIdentity) {
    this.skills = new Set(skills);
    this.emit("attempt_start", {
      turn_key: key(`${identity.runID}:${identity.turnID}`),
      attempt: integer(identity.attempt), fencing_token: integer(identity.fencingToken),
      harness_hash: digest(identity.harnessHash), skill_catalog_hash: digest(identity.skillHash),
      content_policy: "structural_only",
    });
  }

  modelStart(id: string, provider: unknown, model: unknown): void {
    this.emit("model_start", { request_key: key(id), provider: identifier(provider), model: identifier(model) });
  }

  modelEnd(id: string | undefined, reason: string, duration: number | undefined, usage?: AssistantFinishPayload["usage"]): void {
    this.emit("model_end", {
      request_key: key(id), reason: ["stop", "length", "toolUse", "error", "aborted"].includes(reason) ? reason : "other",
      duration_ms: integer(duration), input_tokens: integer(usage?.input), output_tokens: integer(usage?.output), total_tokens: integer(usage?.total_tokens),
    });
  }

  toolStart(name: string, id: string, args: unknown): void {
    const input = object(args);
    const operations = Array.isArray(input.operations) ? input.operations : [];
    this.emit("tool_start", {
      tool: tools.has(name) ? name : "unknown", call_key: key(id),
      base_graph_revision: integer(input.base_graph_revision), op_count: operations.length,
      ops_truncated: operations.length > 32,
      ops: operations.slice(0, 32).map((item) => {
        const op = object(item).op;
        return typeof op === "string" && ops.has(op) ? op : "unknown";
      }),
      skill: typeof input.skill_name === "string" && this.skills.has(input.skill_name) ? input.skill_name : null,
      option_count: Array.isArray(input.options) ? input.options.length : null,
    });
  }

  toolEnd(name: string, id: string, failed: boolean): void {
    this.emit("tool_end", { tool: tools.has(name) ? name : "unknown", call_key: key(id), failed });
  }

  confirmTerminal(status: TurnStatus): void {
    this.terminal = isTerminalStatus(status) ? status : null;
    this.emit("terminal", { status: this.terminal });
  }

  finish(): void {
    if (this.closed) return;
    this.emit("attempt_end", { terminal: this.terminal, complete: this.terminal !== null && this.dropped === 0 && !this.ioFailed }, true);
    this.closed = true;
  }

  private emit(kind: string, fields: Fields, footer = false): void {
    if (this.closed || this.ioFailed) return;
    const first = this.sequence === 0;
    const line = JSON.stringify({ schema_version: 1, sequence: ++this.sequence, at: new Date().toISOString(), kind, ...fields, dropped_records: this.dropped }) + "\n";
    const bytes = Buffer.byteLength(line);
    const budget = this.limits.traceBytes - (footer ? 0 : this.limits.recordBytes);
    if (bytes > this.limits.recordBytes || this.bytes + bytes > budget || !this.store.enqueue(this, line, first)) {
      this.dropped += 1;
      this.store.noteDrop();
      if (first) this.ioFailed = true;
      return;
    }
    this.bytes += bytes;
  }
}

function object(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}
function integer(value: unknown): number | null {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : null;
}
function digest(value: string): string | null {
  return /^[a-f0-9]{64}$/u.test(value) ? value : null;
}
function key(value: string | undefined): string | null {
  return typeof value === "string" && value.length <= 4096 ? sha256(value) : null;
}
function identifier(value: unknown): string | null {
  return typeof value === "string" && /^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,159}$/u.test(value) ? value : null;
}
