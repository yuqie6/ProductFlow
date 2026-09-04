import { mkdir, readFile, realpath, writeFile } from "node:fs/promises";
import { join, resolve, sep } from "node:path";
import { Type, type Static } from "typebox";
import { Value } from "typebox/value";
import type { EvalTaskSet } from "./loader.js";
import { hashCanonicalJSON } from "./provenance.js";
import { evalStorageRoot } from "./run-storage.js";
import { EvalTrialRecordSchema, type EvalTrialRecord } from "./schema.js";

const slug = Type.String({ pattern: "^[a-z0-9]+(?:-[a-z0-9]+)*$", maxLength: 120 });
const digest = Type.String({ pattern: "^[a-f0-9]{64}$" });
const purposeSchema = Type.Union([Type.Literal("development"), Type.Literal("regression"), Type.Literal("acceptance")]);
export type CollectionPurpose = Static<typeof purposeSchema>;
const groupSchema = Type.Object({
  scene_id: slug,
  source_ids: Type.Array(Type.String({ minLength: 1 }), { minItems: 1, uniqueItems: true }),
  purpose: purposeSchema,
  exposed: Type.Boolean(),
  evidence: Type.String({ minLength: 1 }),
  task_ids: Type.Array(slug, { minItems: 1, uniqueItems: true }),
}, { additionalProperties: false });
export const CollectionPlanSchema = Type.Object({
  schema_version: Type.Literal(1),
  groups: Type.Array(groupSchema, { minItems: 1 }),
}, { additionalProperties: false });
export const CollectionManifestSchema = Type.Object({
  ...CollectionPlanSchema.properties,
  task_set_hash: digest,
  hash: digest,
}, { additionalProperties: false });
export type CollectionPlan = Static<typeof CollectionPlanSchema>;
export type CollectionManifest = Static<typeof CollectionManifestSchema>;
export const CollectionRunSchema = Type.Object({
  manifest_hash: digest,
  purpose: purposeSchema,
  task_ids: Type.Array(slug, { minItems: 1, uniqueItems: true }),
}, { additionalProperties: false });
export type CollectionRun = Static<typeof CollectionRunSchema>;

const runIdentitySchema = Type.Object({
  schema_version: Type.Literal(1), run_id: Type.String({ minLength: 1 }),
  task_set_hash: digest, harness_hash: digest, skill_catalog_hash: digest,
  commit: Type.String({ minLength: 1 }), worktree_dirty: Type.Boolean(), worktree_hash: Type.String({ minLength: 1 }),
  reasoning_effort: Type.Union([Type.String(), Type.Null()]),
  model: Type.String({ minLength: 1 }), provider_kind: Type.String({ minLength: 1 }),
  trials: Type.Integer({ minimum: 1 }), layers: Type.Array(Type.Literal("l1"), { minItems: 1, maxItems: 1 }),
  collection: CollectionRunSchema,
}, { additionalProperties: true });

export function taskSetHash(taskSet: EvalTaskSet): string {
  return hashCanonicalJSON({ tasks: taskSet.tasks, worlds: Object.fromEntries(taskSet.worlds) });
}

export function freezeCollection(plan: unknown, taskSet: EvalTaskSet): CollectionManifest {
  if (!Value.Check(CollectionPlanSchema, plan)) throw new Error("Invalid collection plan");
  const scenes = new Set<string>();
  const sources = new Map<string, CollectionPurpose>();
  const origins = new Map<string, CollectionPurpose>();
  const assignments = new Map<string, CollectionPurpose>();
  const tasks = new Map(taskSet.tasks.map((task) => [task.id, task]));
  for (const group of plan.groups) {
    if (scenes.has(group.scene_id)) throw new Error("Duplicate collection scene");
    scenes.add(group.scene_id);
    if (group.exposed && group.purpose !== "development") throw new Error("Exposed scenes must remain development material");
    for (const source of group.source_ids) {
      if (sources.has(source) && sources.get(source) !== group.purpose) throw new Error("Source spans collection purposes");
      sources.set(source, group.purpose);
    }
    for (const id of group.task_ids) {
      const task = tasks.get(id);
      if (!task || assignments.has(id)) throw new Error("Unknown or duplicate collection task");
      if (origins.has(task.origin) && origins.get(task.origin) !== group.purpose) throw new Error("Task origin spans collection purposes");
      origins.set(task.origin, group.purpose);
      assignments.set(id, group.purpose);
    }
  }
  if (assignments.size !== tasks.size) throw new Error("Collection plan must cover the complete task set");
  const groups = plan.groups.map((group) => ({
    ...group, source_ids: [...group.source_ids].sort(), task_ids: [...group.task_ids].sort(),
  })).sort((a, b) => a.scene_id.localeCompare(b.scene_id));
  const body = { schema_version: 1 as const, groups, task_set_hash: taskSetHash(taskSet) };
  return { ...body, hash: hashCanonicalJSON(body) };
}

export async function loadCollection(path: string, taskSet: EvalTaskSet): Promise<CollectionManifest> {
  const manifest: unknown = JSON.parse(await readFile(path, "utf8"));
  if (!Value.Check(CollectionManifestSchema, manifest)) throw new Error("Invalid collection manifest");
  const expected = freezeCollection({ schema_version: manifest.schema_version, groups: manifest.groups }, taskSet);
  if (hashCanonicalJSON(expected) !== hashCanonicalJSON(manifest)) throw new Error("Collection identity or current task contents changed");
  return expected;
}

export function selectCollection(manifest: CollectionManifest, purpose: string, taskSet: EvalTaskSet) {
  if (!Value.Check(purposeSchema, purpose)) throw new Error("Unknown collection purpose");
  if (manifest.task_set_hash !== taskSetHash(taskSet)) throw new Error("Collection task set mismatch");
  const ids = new Set(manifest.groups.filter((group) => group.purpose === purpose).flatMap((group) => group.task_ids));
  const tasks = taskSet.tasks.filter((task) => ids.has(task.id) && task.layers.includes("l1"));
  if (!tasks.length) throw new Error("Collection has no L1 tasks; no acceptance evidence exists");
  return { tasks, identity: { manifest_hash: manifest.hash, purpose, task_ids: tasks.map((task) => task.id) } satisfies CollectionRun };
}

export async function persistCollection(manifest: CollectionManifest, storageRoot = evalStorageRoot()): Promise<string> {
  const root = join(storageRoot, "agent-evals", "collections");
  await mkdir(root, { recursive: true, mode: 0o700 });
  const path = join(root, `${manifest.hash}.json`);
  await writeFile(path, `${JSON.stringify(manifest, null, 2)}\n`, { flag: "wx", mode: 0o600 });
  return path;
}

/** Trusted evaluator boundary. Only the returned development packet may reach a proposer. */
export async function exportDevelopment(runID: string, manifestPath: string, taskSet: EvalTaskSet, storageRoot = evalStorageRoot()): Promise<string> {
  if (!/^\d{8}T\d{6}Z-[a-f0-9]{8}$/u.test(runID)) throw new Error("Invalid collection run ID");
  const manifest = await loadCollection(manifestPath, taskSet);
  const selected = selectCollection(manifest, "development", taskSet);
  const evalRoot = await realpath(join(storageRoot, "agent-evals"));
  const runRoot = await containedPath(evalRoot, runID);
  const raw: unknown = JSON.parse(await readFile(await containedPath(runRoot, "run.json"), "utf8"));
  if (!Value.Check(runIdentitySchema, raw)) throw new Error("Run lacks a valid frozen L1 collection identity");
  if (raw.run_id !== runID || hashCanonicalJSON(raw.collection) !== hashCanonicalJSON(selected.identity)
    || raw.task_set_hash !== taskSetHash({ ...taskSet, tasks: selected.tasks })) {
    throw new Error("Run is not the complete frozen development collection");
  }
  // Reject mixed/legacy batches before opening their body-bearing trials or transcripts.
  await containedPath(runRoot, "summary.json");
  const lines = (await readFile(await containedPath(runRoot, "trials.jsonl"), "utf8")).trim().split("\n");
  const records: EvalTrialRecord[] = [];
  const paths: string[] = [];
  const seen = new Set<string>();
  const tasks = new Map(selected.tasks.map((task) => [task.id, task]));
  for (const line of lines) {
    const record: unknown = JSON.parse(line);
    if (!Value.Check(EvalTrialRecordSchema, record)) throw new Error("Invalid development trial");
    const task = tasks.get(record.task_id);
    const key = `${record.task_id}:${record.trial}`;
    if (!task || record.run_id !== runID || record.layer !== "l1" || record.trial > raw.trials || seen.has(key)
      || record.skill !== task.skill || record.suite !== task.suite
      || record.utterance !== task.utterances[(record.trial - 1) % task.utterances.length]
      || record.transcript_path !== `transcripts/${task.id}-${record.trial}.json`) {
      throw new Error("Development trial identity or transcript path mismatch");
    }
    seen.add(key);
    records.push(record);
    paths.push(await containedPath(runRoot, record.transcript_path));
  }
  if (records.length !== selected.tasks.length * raw.trials) throw new Error("Incomplete development batch");
  const trials = [];
  for (let index = 0; index < records.length; index += 1) {
    const record = records[index];
    const transcript = JSON.parse(await readFile(paths[index], "utf8")) as Record<string, unknown> | null;
    if (!transcript || transcript.schema_version !== 1 || transcript.run_id !== runID
      || transcript.task_id !== record.task_id || transcript.trial !== record.trial || transcript.utterance !== record.utterance) {
      throw new Error("Development transcript identity mismatch");
    }
    const task = tasks.get(record.task_id)!;
    trials.push({ task, world: taskSet.worlds.get(task.world), record, transcript: {
      output: transcript.output, tool_steps: transcript.tool_steps, stub_calls: transcript.stub_calls,
      error: transcript.error, terminal_status: transcript.terminal_status, events: transcript.events,
    } });
  }
  const output = {
    schema_version: 1, purpose: "development", run_id: runID, collection: selected.identity,
    task_set_hash: raw.task_set_hash, harness_hash: raw.harness_hash, skill_catalog_hash: raw.skill_catalog_hash,
    commit: raw.commit, worktree_dirty: raw.worktree_dirty, worktree_hash: raw.worktree_hash,
    model: raw.model, provider_kind: raw.provider_kind, reasoning_effort: raw.reasoning_effort,
    trials_per_task: raw.trials, trials,
  };
  await mkdir(join(evalRoot, "development-inputs"), { recursive: true, mode: 0o700 });
  const outputRoot = await containedPath(evalRoot, "development-inputs");
  const path = join(outputRoot, `${runID}-${manifest.hash}.json`);
  await writeFile(path, `${JSON.stringify(output, null, 2)}\n`, { flag: "wx", mode: 0o600 });
  return path;
}

async function containedPath(root: string, relative: string): Promise<string> {
  const path = await realpath(resolve(root, relative));
  if (path !== resolve(root, relative) || !path.startsWith(`${root}${sep}`)) throw new Error("Collection path escapes its owning directory or uses a symlink");
  return path;
}
