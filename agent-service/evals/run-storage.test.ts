import { access, mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { Value } from "typebox/value";
import { describe, expect, it } from "vitest";

import { EvalRunStorage, type EvalRunMetadata } from "./run-storage.js";
import { EvalTrialRecordSchema, type EvalTrialRecord } from "./schema.js";

describe("EvalRunStorage", () => {
  it("serializes concurrent trial writes and keeps transcripts outside JSONL", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-eval-storage-"));
    try {
      const storage = new EvalRunStorage("run-1", root);
      await storage.init(sampleMetadata());
      await Promise.all([
        storage.appendTrial(sampleTrial("a", true)),
        storage.appendTrial(sampleTrial("b", false)),
      ]);
      const transcript = await storage.writeTranscript("task/unsafe", 1, { output: "ok" });
      await storage.finish({ passed: 1, total: 2 }, { successful: false, publishLatest: false });

      const lines = (await readFile(join(storage.runDir, "trials.jsonl"), "utf8")).trim().split("\n");
      expect(lines.map((line) => JSON.parse(line).task_id)).toEqual(["a", "b"]);
      expect(transcript).toBe("transcripts/task-unsafe-1.json");
      expect(JSON.parse(await readFile(join(storage.runDir, "summary.json"), "utf8"))).toEqual({ passed: 1, total: 2 });
      await expect(pathExists(join(root, "agent-evals", "latest.json"))).resolves.toBe(false);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("rejects trial records that fail EvalTrialRecordSchema", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-eval-storage-"));
    try {
      const storage = new EvalRunStorage("run-invalid", root);
      await storage.init(sampleMetadata({ run_id: "run-invalid" }));
      await expect(storage.appendTrial({ passed: true } as EvalTrialRecord)).rejects.toThrow(
        /cannot persist an invalid Agent eval TrialRecord/,
      );
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("atomically writes latest.json only for a successful finalizer with publishLatest", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-eval-storage-"));
    try {
      const storage = new EvalRunStorage("run-ok", root);
      await storage.init(sampleMetadata({ run_id: "run-ok" }));
      await storage.appendTrial(sampleTrial("a", true, "run-ok"));
      await storage.finish({ passed: 1, total: 1 }, { successful: true, publishLatest: true });

      const latestPath = join(root, "agent-evals", "latest.json");
      expect(JSON.parse(await readFile(latestPath, "utf8")).run_id).toBe("run-ok");
      const leftovers = (await readdir(join(root, "agent-evals"))).filter((name) => name.startsWith("latest.json.tmp"));
      expect(leftovers).toEqual([]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("does not write or replace latest.json for an unsuccessful run and throws when publishLatest is true", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-eval-storage-"));
    try {
      const storage = new EvalRunStorage("run-fail", root);
      await storage.init(sampleMetadata({ run_id: "run-fail" }));
      await storage.appendTrial(sampleTrial("a", false, "run-fail"));
      const latestPath = join(root, "agent-evals", "latest.json");
      await writeFile(latestPath, `${JSON.stringify({ schema_version: 1, run_id: "older" }, null, 2)}\n`, "utf8");

      await expect(
        storage.finish({ passed: 0, total: 1 }, { successful: false, publishLatest: true }),
      ).rejects.toThrow(/cannot publish latest.json for an unsuccessful Agent eval run/);

      expect(JSON.parse(await readFile(join(storage.runDir, "summary.json"), "utf8"))).toEqual({ passed: 0, total: 1 });
      expect((await readFile(join(storage.runDir, "trials.jsonl"), "utf8")).trim()).not.toBe("");
      expect(JSON.parse(await readFile(latestPath, "utf8")).run_id).toBe("older");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("does not write latest.json when successful but publishLatest is false", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-eval-storage-"));
    try {
      const storage = new EvalRunStorage("run-quiet", root);
      await storage.init(sampleMetadata({ run_id: "run-quiet" }));
      await storage.appendTrial(sampleTrial("a", true, "run-quiet"));
      await storage.finish({ passed: 1, total: 1 }, { successful: true, publishLatest: false });

      expect(JSON.parse(await readFile(join(storage.runDir, "summary.json"), "utf8"))).toEqual({ passed: 1, total: 1 });
      await expect(pathExists(join(root, "agent-evals", "latest.json"))).resolves.toBe(false);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});

function sampleMetadata(overrides: Partial<EvalRunMetadata> = {}): EvalRunMetadata {
  return {
    schema_version: 1,
    run_id: "run-1",
    created_at: "2026-09-04T00:00:00.000Z",
    commit: "abc",
    worktree_dirty: false,
    worktree_hash: "hash",
    skill_catalog_hash: "skills",
    task_set_hash: "tasks",
    model: "model",
    provider_kind: "openai",
    reasoning_effort: null,
    trials: 2,
    concurrency: 2,
    production_max_concurrent_turns: 3,
    layers: ["l1"],
    ...overrides,
  };
}

function sampleTrial(taskID: string, passed: boolean, runID = "run-1"): EvalTrialRecord {
  const record: EvalTrialRecord = {
    schema_version: 1,
    run_id: runID,
    layer: "l1",
    task_id: taskID,
    skill: "graph-editing",
    suite: "regression",
    trial: 1,
    utterance: "测试",
    started_at: "2026-09-04T00:00:00.000Z",
    duration_ms: 10,
    status: passed ? "succeeded" : "failed",
    passed,
    errors: passed ? [] : ["failed"],
    terminal: passed ? "succeeded" : "failed",
    tool_calls: [],
    token_count: 1,
  };
  if (!Value.Check(EvalTrialRecordSchema, record)) {
    throw new Error("test fixture is not a valid EvalTrialRecord");
  }
  return record;
}

async function pathExists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}
