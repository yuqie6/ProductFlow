import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  buildRunReport,
  collectCoverage,
  diffReports,
  formatCoverageReport,
  formatReportDiff,
  formatRunReport,
  loadRunReport,
  passAtK,
  readTrialRecords,
  saturationWarnings,
  wilson95,
  type TrialRecord,
} from "./report.js";

const roots: string[] = [];

afterEach(async () => {
  await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true })));
});

describe("agent eval report", () => {
  it.each(["status", "terminal"] as const)("rejects unknown %s even without an unknown tool outcome", (field) => {
    const record = { ...trial("run-a", "l1", "unresolved", "graph-editing", "regression", 1, true), [field]: "unknown" };
    const report = buildRunReport("run-a", [record], 1);
    expect(report).toMatchObject({ measurementEligible: false, unobservableTrials: 1 });
    expect(report.overall.trialCount).toBe(1);
    expect(report.regressionGate.passed).toBeNull();
    expect(() => diffReports(report, report)).toThrow("cannot compare unobservable");
  });
  it("keeps unobservable trials visible and refuses capability comparison", () => {
    const record = { ...trial("run-a", "l1", "blocked", "media-library-organization", "regression", 1, false), status: "unobservable", errors: ["missing production revision"] };
    const report = buildRunReport("run-a", [record], 1);
    expect(report).toMatchObject({ measurementEligible: false, unobservableTrials: 1 });
    expect(report.overall.trialCount).toBe(1);
    expect(report.regressionGate.passed).toBeNull();
    expect(formatRunReport(report)).toContain("diagnostic, not capability scores");
    expect(() => diffReports(report, report)).toThrow("cannot compare unobservable");
  });
  it("computes task-mean pass^1, unbiased pass^k, groups, and a trial Wilson interval", () => {
    const records = [
      trial("run-a", "l1", "task-a", "product-intake", "regression", 1, true),
      trial("run-a", "l1", "task-a", "product-intake", "regression", 2, true),
      trial("run-a", "l1", "task-a", "product-intake", "regression", 3, true),
      trial("run-a", "l1", "task-b", "graph-edit", "capability", 1, true),
      trial("run-a", "l1", "task-b", "graph-edit", "capability", 2, true),
      trial("run-a", "l1", "task-b", "graph-edit", "capability", 3, false, null),
    ];
    const report = buildRunReport("run-a", records, 3);

    expect(report.tasks.map((task) => [task.taskId, task.passAt1, task.passAtK])).toEqual([
      ["task-a", 1, 1],
      ["task-b", 2 / 3, 0],
    ]);
    expect(report.overall.passAt1).toBeCloseTo(5 / 6);
    expect(report.overall.passAtK).toBeCloseTo(1 / 2);
    expect(report.overall.trialSuccessProportion).toBeCloseTo(5 / 6);
    expect(report.overall.trialSuccessWilson95).toEqual(wilson95(5, 6));
    expect(report.overall.tokenCount).toBe(50);
    expect(report.overall.tokenCountUnavailable).toBe(1);
    expect(report.bySkill).toHaveLength(2);
    expect(report.bySuite).toHaveLength(2);
    expect(report.regressionGate).toMatchObject({ applicable: true, passed: true, passAt1: 1, passAt3: 1 });

    const markdown = formatRunReport(report);
    expect(markdown).toContain("pass^1=0.8333 pass^3=0.5000");
    expect(markdown).toContain("It is not a confidence interval for the task-mean pass^3");
  });

  it("marks pass^k unavailable when any task has fewer than k trials", () => {
    expect(passAtK(2, 2, 3)).toBeNull();
    const report = buildRunReport(
      "run-short",
      [trial("run-short", "l1", "short", "product-intake", "regression", 1, true)],
      3,
    );
    expect(report.tasks[0].passAtK).toBeNull();
    expect(report.overall.passAtK).toBeNull();
    expect(report.regressionGate).toMatchObject({ applicable: false, passed: null });
    expect(formatRunReport(report)).toContain("fewer than 3 trials");

    const same = buildRunReport(
      "run-short-next",
      [trial("run-short-next", "l1", "short", "product-intake", "regression", 1, true)],
      3,
    );
    expect(diffReports(report, same).tasks[0].change).toBe("unchanged");
  });

  it("loads STORAGE_ROOT runs and rejects malformed or mismatched JSONL", async () => {
    const root = await tempRoot();
    const runDir = join(root, "agent-evals", "run-disk");
    await mkdir(runDir, { recursive: true });
    await writeFile(join(runDir, "run.json"), JSON.stringify({ run_id: "run-disk", k: 1 }));
    await writeFile(join(runDir, "trials.jsonl"), `${JSON.stringify(trial("run-disk", "l1", "task", "skill", "regression", 1, true))}\n`);

    const loaded = await loadRunReport("run-disk", { storageRoot: root });
    expect(loaded.passK).toBe(1);
    expect(loaded.overall.passAtK).toBe(1);

    await writeFile(join(runDir, "trials.jsonl"), `${JSON.stringify(trial("other", "l1", "task", "skill", "regression", 1, true))}\n`);
    await expect(loadRunReport("run-disk", { storageRoot: root })).rejects.toThrow(/run_id mismatch/);
    await writeFile(join(runDir, "trials.jsonl"), '{"passed":true}\nnot-json\n');
    await expect(readTrialRecords(join(runDir, "trials.jsonl"))).rejects.toThrow(/does not match EvalTrialRecord|invalid trials JSONL/);
  });

  it("rejects trial JSONL that fails EvalTrialRecordSchema even when required-looking fields are present", async () => {
    const root = await tempRoot();
    const path = join(root, "trials.jsonl");
    const extra = { ...trial("run-schema", "l1", "task", "skill", "regression", 1, true), extra: true };
    await writeFile(path, `${JSON.stringify(extra)}\n`);
    await expect(readTrialRecords(path)).rejects.toThrow(/does not match EvalTrialRecord schema/);

    const badSuite = { ...trial("run-schema", "l1", "task", "skill", "regression", 1, true), suite: "not-a-suite" };
    await writeFile(path, `${JSON.stringify(badSuite)}\n`);
    await expect(readTrialRecords(path)).rejects.toThrow(/does not match EvalTrialRecord schema/);
  });

  it("diffs the task mean and reports added, removed, and changed tasks", () => {
    const baseline = buildRunReport(
      "baseline",
      [
        trial("baseline", "l1", "same", "skill", "regression", 1, false),
        trial("baseline", "l1", "removed", "skill", "regression", 1, true),
      ],
      1,
    );
    const candidate = buildRunReport(
      "candidate",
      [
        trial("candidate", "l1", "same", "skill", "regression", 1, true),
        trial("candidate", "l1", "added", "skill", "regression", 1, true),
      ],
      1,
    );
    const diff = diffReports(baseline, candidate);
    expect(diff.deltaPassAt1).toBe(0.5);
    expect(diff.tasks.map((task) => [task.taskId, task.change])).toEqual([
      ["added", "added"],
      ["removed", "removed"],
      ["same", "changed"],
    ]);
    expect(formatReportDiff(diff)).toContain("delta_pass^1=+0.5000");
  });

  it("measures only required tool/op expectations against the live closed sets", async () => {
    const root = await tempRoot();
    await mkdir(join(root, "nested"));
    await writeFile(
      join(root, "nested", "coverage-a.json"),
      JSON.stringify({
        id: "coverage-a",
        expect: {
          tools: { required: ["load_productflow_skill"], forbidden: ["ask_user"] },
          ops: { required: ["rename_node"], forbidden: ["create_node"] },
        },
      }),
    );
    await writeFile(
      join(root, "coverage-b.json"),
      JSON.stringify({
        id: "coverage-b",
        expect: { tools: { required: ["unknown_tool"] }, ops: { required: ["unknown_op"] } },
      }),
    );
    const coverage = await collectCoverage(root);
    expect(coverage.complete).toBe(false);
    expect(coverage.tools.covered).toContainEqual({ name: "load_productflow_skill", taskCount: 1 });
    expect(coverage.tools.missing).toContain("ask_user");
    expect(coverage.tools.unknown).toEqual(["unknown_tool"]);
    expect(coverage.ops.covered).toContainEqual({ name: "rename_node", taskCount: 1 });
    expect(coverage.ops.missing).toContain("create_node");
    expect(coverage.ops.unknown).toEqual(["unknown_op"]);
    expect(formatCoverageReport(coverage)).toContain("complete=false");
  });

  it("flags a suite that stayed at 100% for four consecutive reports", () => {
    const full = {
      bySuite: [{ name: "regression", passAt1: 1, taskCount: 10 } as never],
    };
    expect(saturationWarnings([full, full, full, full])).toEqual([
      "regression passed 100% in the last 4 reports; add capability tasks",
    ]);
  });
});

function trial(
  runId: string,
  layer: TrialRecord["layer"],
  taskId: string,
  skill: string,
  suite: TrialRecord["suite"],
  trialNumber: number,
  passed: boolean,
  tokenCount: number | null = 10,
): TrialRecord {
  return {
    schema_version: 1,
    run_id: runId,
    layer,
    task_id: taskId,
    skill,
    suite,
    trial: trialNumber,
    utterance: "测试",
    started_at: "2026-09-04T00:00:00.000Z",
    duration_ms: 100,
    status: passed ? "succeeded" : "failed",
    passed,
    errors: passed ? [] : ["failed"],
    terminal: passed ? "succeeded" : "failed",
    tool_calls: [],
    token_count: tokenCount,
  };
}

async function tempRoot(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "productflow-eval-report-"));
  roots.push(root);
  return root;
}
