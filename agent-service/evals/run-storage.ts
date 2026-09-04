import { appendFile, mkdir, rename, rm, writeFile } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { Value } from "typebox/value";

import { EvalTrialRecordSchema, type EvalTrialRecord } from "./schema.js";

const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

export interface EvalRunMetadata {
  schema_version: 1;
  run_id: string;
  created_at: string;
  commit: string;
  worktree_dirty: boolean;
  worktree_hash: string;
  skill_catalog_hash: string;
  harness_hash: string;
  task_set_hash: string;
  model: string;
  provider_kind: string;
  reasoning_effort: string | null;
  trials: number;
  concurrency: number;
  production_max_concurrent_turns: number;
  layers: string[];
}

export interface EvalRunFinishOptions {
  successful: boolean;
  publishLatest?: boolean;
}

export function evalStorageRoot(env = process.env): string {
  const configured = env.STORAGE_ROOT?.trim();
  return configured ? resolve(configured) : join(REPO_ROOT, "storage-dev");
}

export function newEvalRunID(now = new Date()): string {
  const timestamp = now.toISOString().replace(/[-:]/gu, "").replace(/\.\d{3}Z$/u, "Z");
  return `${timestamp}-${randomUUID().slice(0, 8)}`;
}

export class EvalRunStorage {
  readonly runDir: string;
  readonly transcriptDir: string;
  private writes = Promise.resolve();

  constructor(
    readonly runID: string,
    readonly storageRoot = evalStorageRoot(),
  ) {
    this.runDir = join(storageRoot, "agent-evals", runID);
    this.transcriptDir = join(this.runDir, "transcripts");
  }

  async init(metadata: EvalRunMetadata): Promise<void> {
    await mkdir(this.transcriptDir, { recursive: true });
    await writeJSON(join(this.runDir, "run.json"), metadata);
  }

  appendTrial(record: EvalTrialRecord): Promise<void> {
    if (!Value.Check(EvalTrialRecordSchema, record)) {
      return Promise.reject(new Error("cannot persist an invalid Agent eval TrialRecord"));
    }
    return this.enqueue(async () => {
      await appendFile(join(this.runDir, "trials.jsonl"), `${JSON.stringify(record)}\n`, "utf8");
    });
  }

  writeTranscript(taskID: string, trial: number, transcript: unknown): Promise<string> {
    const relative = join("transcripts", `${safeSegment(taskID)}-${trial}.json`);
    return this.enqueue(async () => {
      await writeJSON(join(this.runDir, relative), transcript);
      return relative;
    });
  }

  async finish(summary: unknown, options: EvalRunFinishOptions): Promise<void> {
    await this.writes;
    await writeJSONAtomic(join(this.runDir, "summary.json"), summary);
    await appendHistory(this.storageRoot, this.runID, summary);
    if (!options.publishLatest) return;
    await writeJSONAtomic(
      join(this.storageRoot, "agent-evals", "latest.json"),
      {
        schema_version: 1,
        run_id: this.runID,
        run_dir: this.runDir,
        updated_at: new Date().toISOString(),
      },
    );
  }

  private enqueue<T>(write: () => Promise<T>): Promise<T> {
    const next = this.writes.then(write, write);
    this.writes = next.then(() => undefined, () => undefined);
    return next;
  }
}

async function appendHistory(storageRoot: string, runID: string, summary: unknown): Promise<void> {
  const metrics = isRecord(summary) && isRecord(summary.metrics) ? summary.metrics : null;
  const overall = isRecord(metrics) && isRecord(metrics.overall) ? metrics.overall : null;
  const bySuite = isRecord(metrics) && Array.isArray(metrics.bySuite) ? metrics.bySuite : [];
  const line = {
    schema_version: 1,
    run_id: runID,
    created_at: new Date().toISOString(),
    passAt1: typeof overall?.passAt1 === "number" ? overall.passAt1 : null,
    bySuite: bySuite
      .filter((item): item is Record<string, unknown> => isRecord(item))
      .map((item) => ({
        name: String(item.name ?? ""),
        passAt1: typeof item.passAt1 === "number" ? item.passAt1 : 0,
        taskCount: typeof item.taskCount === "number" ? item.taskCount : 0,
      })),
  };
  await mkdir(join(storageRoot, "agent-evals"), { recursive: true });
  await appendFile(join(storageRoot, "agent-evals", "history.jsonl"), `${JSON.stringify(line)}\n`, "utf8");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function safeSegment(value: string): string {
  const safe = value.replace(/[^a-zA-Z0-9._-]+/gu, "-").replace(/^-+|-+$/gu, "");
  if (!safe) throw new Error("eval task id does not contain a safe filename character");
  return safe;
}

async function writeJSON(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

async function writeJSONAtomic(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  const temporary = `${path}.tmp-${randomUUID()}`;
  try {
    await writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`, "utf8");
    await rename(temporary, path);
  } catch (error) {
    await rm(temporary, { force: true }).catch(() => undefined);
    throw error;
  }
}
