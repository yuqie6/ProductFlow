import * as fs from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { loadEvalTaskSet, type EvalTaskSet } from "./loader.js";
import { exportDevelopment, freezeCollection, loadCollection, persistCollection, selectCollection, taskSetHash, type CollectionPlan } from "./collections.js";
import type { EvalTrialRecord } from "./schema.js";
import { runLiveEvals } from "./live-runner.js";
import { EvalRunStorage } from "./run-storage.js";
import { runCLI } from "./cli.js";

vi.mock("node:fs/promises", async (original) => ({ ...await original<typeof import("node:fs/promises")>() }));

let taskSet: EvalTaskSet;
const roots: string[] = [];
const runID = "20260905T000000Z-1234abcd";
const plan: CollectionPlan = { schema_version: 1, groups: [
  { scene_id: "dev-scene", source_ids: ["source-dev"], purpose: "development", exposed: true, evidence: "fixture exposed", task_ids: ["dev-task"] },
  { scene_id: "hidden-scene", source_ids: ["source-hidden"], purpose: "regression", exposed: false, evidence: "synthetic only", task_ids: ["hidden-task"] },
] };

beforeAll(async () => {
  const loaded = await loadEvalTaskSet();
  const task = loaded.tasks[0];
  taskSet = { worlds: loaded.worlds, tasks: [
    { ...structuredClone(task), id: "dev-task", origin: "fixture:dev", layers: ["l1"] },
    { ...structuredClone(task), id: "hidden-task", origin: "fixture:hidden", layers: ["l1"], utterances: ["HIDDEN-TEXT"] },
  ] };
});
afterEach(async () => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  for (const root of roots.splice(0)) await fs.rm(root, { recursive: true, force: true });
});

async function fixture() {
  const root = await fs.mkdtemp(join(tmpdir(), "productflow-collection-"));
  roots.push(root);
  const manifest = freezeCollection(plan, taskSet);
  const path = await persistCollection(manifest, root);
  const selected = selectCollection(manifest, "development", taskSet);
  const task = selected.tasks[0];
  const metadata = {
    schema_version: 1, run_id: runID, task_set_hash: taskSetHash({ ...taskSet, tasks: selected.tasks }),
    harness_hash: "a".repeat(64), skill_catalog_hash: "b".repeat(64), model: "fixture", provider_kind: "fixture",
    commit: "c".repeat(40), worktree_dirty: false, worktree_hash: "d".repeat(64), reasoning_effort: null,
    trials: 1, layers: ["l1"], collection: selected.identity,
  };
  const record: EvalTrialRecord = {
    schema_version: 1, run_id: runID, task_id: task.id, skill: task.skill, suite: task.suite,
    trial: 1, layer: "l1", utterance: task.utterances[0], started_at: "2026-09-05T00:00:00Z",
    duration_ms: 1, status: "failed", passed: false, errors: ["fixture failure"], terminal: "failed",
    tool_calls: [], token_count: 1, transcript_path: `transcripts/${task.id}-1.json`,
  };
  const runRoot = join(root, "agent-evals", runID);
  await fs.mkdir(join(runRoot, "transcripts"), { recursive: true });
  await fs.writeFile(join(runRoot, "run.json"), JSON.stringify(metadata));
  await fs.writeFile(join(runRoot, "summary.json"), JSON.stringify({ fixture: true }));
  await fs.writeFile(join(runRoot, "trials.jsonl"), JSON.stringify(record) + "\n");
  await fs.writeFile(join(runRoot, record.transcript_path!), JSON.stringify({
    schema_version: 1, run_id: runID, task_id: task.id, trial: 1, utterance: task.utterances[0],
    output: "development output", thinking: "PRIVATE REASONING", stub_calls: [], events: [], tool_steps: [],
  }));
  return { root, path, manifest, metadata, record, runRoot };
}

describe("frozen evaluation collections", () => {
  it("canonicalizes assignment order and rejects scene/source/origin leakage", () => {
    const frozen = freezeCollection(plan, taskSet);
    expect(freezeCollection({ ...plan, groups: [...plan.groups].reverse() }, taskSet)).toEqual(frozen);
    for (const field of ["scene_id", "source_ids"] as const) {
      const bad = structuredClone(plan);
      if (field === "scene_id") bad.groups[1].scene_id = bad.groups[0].scene_id;
      else bad.groups[1].source_ids = bad.groups[0].source_ids;
      expect(() => freezeCollection(bad, taskSet)).toThrow();
    }
    const sameOrigin = structuredClone(taskSet);
    sameOrigin.tasks[1].origin = sameOrigin.tasks[0].origin;
    expect(() => freezeCollection(plan, sameOrigin)).toThrow(/origin spans/);
  });

  it("rejects exposed hidden groups, incomplete coverage and unknown/duplicate tasks", () => {
    const exposed = structuredClone(plan);
    exposed.groups[1].exposed = true;
    expect(() => freezeCollection(exposed, taskSet)).toThrow(/Exposed/);
    expect(() => freezeCollection({ ...plan, groups: [plan.groups[0]] }, taskSet)).toThrow(/complete/);
    for (const id of ["dev-task", "unknown-task"]) {
      const bad = structuredClone(plan);
      bad.groups[1].task_ids = [id];
      expect(() => freezeCollection(bad, taskSet)).toThrow(/Unknown or duplicate/);
    }
    expect(() => freezeCollection({ ...plan, unexpected: true }, taskSet)).toThrow(/Invalid/);
  });

  it("invalidates a manifest when task, world or manifest contents change", async () => {
    const f = await fixture();
    expect(await loadCollection(f.path, taskSet)).toEqual(f.manifest);
    const changed = structuredClone(taskSet);
    changed.tasks[0].utterances.push("changed wording");
    await expect(loadCollection(f.path, changed)).rejects.toThrow(/changed/);
    const changedWorld = structuredClone(taskSet);
    changedWorld.worlds.values().next().value!.live_graph.revision += 1;
    await expect(loadCollection(f.path, changedWorld)).rejects.toThrow(/changed/);
    await fs.writeFile(f.path, JSON.stringify({ ...f.manifest, hash: "0".repeat(64) }));
    await expect(loadCollection(f.path, taskSet)).rejects.toThrow(/changed/);
  });

  it("does not equate an empty independent set with acceptance evidence", () => {
    const manifest = freezeCollection(plan, taskSet);
    expect(() => selectCollection(manifest, "acceptance", taskSet)).toThrow(/no L1 tasks/);
    expect(() => selectCollection(manifest, "held_in", taskSet)).toThrow(/Unknown/);
  });

  it("freezes the current public library through the CLI as exposed development material", async () => {
    const all = await loadEvalTaskSet();
    const root = await fs.mkdtemp(join(tmpdir(), "productflow-collection-cli-"));
    roots.push(root);
    const path = join(root, "plan.json");
    await fs.writeFile(path, JSON.stringify({ schema_version: 1, groups: [{
      scene_id: "public-library", source_ids: ["public-repository"], purpose: "development",
      exposed: true, evidence: "CLI fixture; not independent acceptance", task_ids: all.tasks.map((task) => task.id),
    }] }));
    let stdout = "";
    let stderr = "";
    expect(await runCLI(["freeze-collection", path, "--storage-root", root], {
      stdout: (text) => { stdout += text; }, stderr: (text) => { stderr += text; },
    })).toBe(0);
    expect(stderr).toBe("");
    const report = JSON.parse(stdout);
    expect(report.counts).toEqual({ development: all.tasks.length, regression: 0, acceptance: 0 });
    expect((await loadCollection(report.path, all)).hash).toBe(report.hash);
  });

  it("exports complete development failures without hidden data or reasoning", async () => {
    const f = await fixture();
    const output = await exportDevelopment(runID, f.path, taskSet, f.root);
    const text = await fs.readFile(output, "utf8");
    const packet = JSON.parse(text);
    expect(packet.trials).toHaveLength(1);
    expect(packet.trials[0].record).toMatchObject({ passed: false, errors: ["fixture failure"] });
    expect(packet.trials[0].transcript.output).toBe("development output");
    expect(text).not.toContain("HIDDEN-TEXT");
    expect(text).not.toContain("hidden-task");
    expect(text).not.toContain("PRIVATE REASONING");
    expect((await fs.stat(output)).mode & 0o777).toBe(0o600);
    await expect(exportDevelopment(runID, f.path, taskSet, f.root)).rejects.toMatchObject({ code: "EEXIST" });
  });

  it.each(["legacy", "mixed", "wrong_hash", "wrong_run", "wrong_task_hash"])("rejects %s metadata before reading trial bodies", async (mode) => {
    const f = await fixture();
    const metadata: Record<string, unknown> = structuredClone(f.metadata);
    if (mode === "legacy") delete metadata.collection;
    if (mode === "mixed") f.metadata.collection.task_ids.push("hidden-task");
    if (mode === "mixed") metadata.collection = f.metadata.collection;
    if (mode === "wrong_hash") metadata.collection = { ...f.metadata.collection, manifest_hash: "0".repeat(64) };
    if (mode === "wrong_run") metadata.run_id = "different";
    if (mode === "wrong_task_hash") metadata.task_set_hash = "0".repeat(64);
    await fs.writeFile(join(f.runRoot, "run.json"), JSON.stringify(metadata));
    const reads = vi.spyOn(fs, "readFile");
    await expect(exportDevelopment(runID, f.path, taskSet, f.root)).rejects.toThrow();
    expect(reads.mock.calls.some(([path]) => String(path).endsWith("trials.jsonl"))).toBe(false);
    expect(reads.mock.calls.some(([path]) => String(path).includes("/transcripts/"))).toBe(false);
  });

  it.each(["unobservable_status", "unknown_status", "unknown_terminal", "unknown_write", "unknown_read"])("rejects %s before opening transcripts or exporting a packet", async (mode) => {
    const f = await fixture();
    const record = { ...f.record, passed: true, errors: [] };
    if (mode === "unobservable_status") record.status = "unobservable";
    else if (mode === "unknown_status") record.status = "unknown";
    else if (mode === "unknown_terminal") record.terminal = "unknown";
    else record.tool_calls = [{
      name: mode === "unknown_write" ? "apply_graph_change_set_v1" : "get_node_detail_v1",
      params: {}, ts: record.started_at, outcome: "unknown",
    }];
    await fs.writeFile(join(f.runRoot, "trials.jsonl"), JSON.stringify(record) + "\n");
    // A stale summary cannot overrule the raw observation evidence.
    await fs.writeFile(join(f.runRoot, "summary.json"), JSON.stringify({ metrics: { measurementEligible: true } }));
    const reads = vi.spyOn(fs, "readFile");
    await expect(exportDevelopment(runID, f.path, taskSet, f.root)).rejects.toThrow(/Unobservable development batch/);
    expect(reads.mock.calls.some(([path]) => String(path).includes("/transcripts/"))).toBe(false);
    await expect(fs.stat(join(f.root, "agent-evals", "development-inputs"))).rejects.toMatchObject({ code: "ENOENT" });
  });

  it.each(["succeeded", "failed"] as const)("exports observed %s tool results even when the capability trial fails", async (outcome) => {
    const f = await fixture();
    f.record.tool_calls = [{ name: "apply_graph_change_set_v1", params: {}, ts: f.record.started_at, outcome }];
    await fs.writeFile(join(f.runRoot, "trials.jsonl"), JSON.stringify(f.record) + "\n");
    const packet = JSON.parse(await fs.readFile(await exportDevelopment(runID, f.path, taskSet, f.root), "utf8"));
    expect(packet.trials[0].record).toMatchObject({ passed: false, tool_calls: [{ outcome }] });
  });

  it.each(["unknown", "duplicate", "missing", "wrong_run", "traversal"])("rejects %s trials before opening any transcript", async (mode) => {
    const f = await fixture();
    const record = { ...f.record };
    if (mode === "unknown") record.task_id = "hidden-task";
    if (mode === "wrong_run") record.run_id = "other-run";
    if (mode === "traversal") record.transcript_path = "../secret.json";
    const records = mode === "duplicate" ? [record, record] : mode === "missing" ? [] : [record];
    await fs.writeFile(join(f.runRoot, "trials.jsonl"), records.map((row) => JSON.stringify(row)).join("\n"));
    const reads = vi.spyOn(fs, "readFile");
    await expect(exportDevelopment(runID, f.path, taskSet, f.root)).rejects.toThrow();
    expect(reads.mock.calls.some(([path]) => String(path).includes("/transcripts/"))).toBe(false);
  });

  it("rejects symlink escapes and mismatched transcript identity", async () => {
    const f = await fixture();
    const path = join(f.runRoot, f.record.transcript_path!);
    await fs.writeFile(path, JSON.stringify({ schema_version: 1, task_id: "hidden-task" }));
    await expect(exportDevelopment(runID, f.path, taskSet, f.root)).rejects.toThrow(/transcript identity/);
    await fs.unlink(path);
    await fs.symlink(f.path, path);
    await expect(exportDevelopment(runID, f.path, taskSet, f.root)).rejects.toThrow(/symlink/);
    await expect(exportDevelopment("../outside", f.path, taskSet, f.root)).rejects.toThrow(/run ID/);
  });

  it("requires finalization before reading trial bodies", async () => {
    const f = await fixture();
    await fs.unlink(join(f.runRoot, "summary.json"));
    const reads = vi.spyOn(fs, "readFile");
    await expect(exportDevelopment(runID, f.path, taskSet, f.root)).rejects.toThrow();
    expect(reads.mock.calls.some(([path]) => String(path).endsWith("trials.jsonl"))).toBe(false);
  });

  it("binds the actual L1 runner to one complete purpose before model execution", async () => {
    const all = await loadEvalTaskSet();
    const root = await fs.mkdtemp(join(tmpdir(), "productflow-collection-runner-"));
    roots.push(root);
    const manifest = freezeCollection({ schema_version: 1, groups: [{
      scene_id: "exposed-library", source_ids: ["public-repository"], purpose: "development",
      exposed: true, evidence: "test boundary only", task_ids: all.tasks.map((task) => task.id),
    }] }, all);
    const path = await persistCollection(manifest, root);
    vi.stubEnv("PRODUCTFLOW_RUN_AGENT_EVALS", "1");
    vi.stubEnv("AGENT_PROVIDER_API_KEY", "not-used");
    vi.stubEnv("PRODUCTFLOW_AGENT_EVAL_FILTER", "");
    vi.stubEnv("PRODUCTFLOW_AGENT_EVAL_SUITE", "");
    const stop = new Error("metadata boundary");
    const init = vi.spyOn(EvalRunStorage.prototype, "init").mockRejectedValue(stop);
    await expect(runLiveEvals({ collectionPath: path, collectionPurpose: "development" })).rejects.toBe(stop);
    expect(init).toHaveBeenCalledWith(expect.objectContaining({
      collection: selectCollection(manifest, "development", all).identity,
    }));
    init.mockClear();
    await expect(runLiveEvals({ collectionPath: path, collectionPurpose: "development", filter: "dev" })).rejects.toThrow(/unfiltered/);
    await expect(runLiveEvals({ collectionPath: path })).rejects.toThrow(/both required/);
    expect(init).not.toHaveBeenCalled();
  });
});
