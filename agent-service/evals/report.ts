import { readFile, readdir } from "node:fs/promises";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { Value } from "typebox/value";

import { GRAPH_COMMAND_OPS } from "../src/graph-command-schema.js";
import { TOOL_MANIFEST } from "../src/tool-manifest.js";
import { evalStorageRoot } from "./run-storage.js";
import { EvalTrialRecordSchema, type EvalTrialRecord } from "./schema.js";

const WILSON_Z_95 = 1.959963984540054;
const DEFAULT_PASS_K = 3;

export type TrialRecord = EvalTrialRecord;

export function isUnobservableTrial(record: Pick<TrialRecord, "status" | "terminal" | "tool_calls">): boolean {
  return record.status === "unobservable" || record.status === "unknown" || record.terminal === "unknown"
    || record.tool_calls.some((call) => call.outcome === "unknown");
}

export interface WilsonInterval {
  low: number;
  high: number;
}

export interface TaskMetric {
  key: string;
  layer: string;
  taskId: string;
  skill: string;
  suite: string;
  successes: number;
  trials: number;
  passAt1: number;
  passAtK: number | null;
  tokenCount: number;
  tokenCountUnavailable: number;
  durationMs: number;
}

export interface MetricSummary {
  taskCount: number;
  trialCount: number;
  successes: number;
  passAt1: number;
  passAtK: number | null;
  passAtKUnavailableTasks: number;
  trialSuccessProportion: number;
  trialSuccessWilson95: WilsonInterval;
  tokenCount: number;
  tokenCountUnavailable: number;
  durationMs: number;
}

export interface GroupMetric extends MetricSummary {
  name: string;
}

export interface RunReport {
  measurementEligible: boolean;
  unobservableTrials: number;
  runId: string;
  passK: number;
  tasks: TaskMetric[];
  overall: MetricSummary;
  bySkill: GroupMetric[];
  bySuite: GroupMetric[];
  regressionGate: RegressionGate;
}

export interface RegressionGate {
  applicable: boolean;
  passed: boolean | null;
  reason: string;
  passAt1: number | null;
  passAt3: number | null;
  thresholds: { passAt1: 0.95; passAt3: 0.9 };
}

export interface TaskDiff {
  key: string;
  layer: string;
  taskId: string;
  skill: string;
  suite: string;
  change: "added" | "removed" | "changed" | "unchanged";
  baselinePassAt1: number | null;
  candidatePassAt1: number | null;
  deltaPassAt1: number | null;
  baselinePassAtK: number | null;
  candidatePassAtK: number | null;
  deltaPassAtK: number | null;
}

export interface ReportDiff {
  baselineRunId: string;
  candidateRunId: string;
  passK: number;
  deltaPassAt1: number;
  deltaPassAtK: number | null;
  tasks: TaskDiff[];
}

export interface CoverageItem {
  name: string;
  taskCount: number;
}

export interface CoverageDimension {
  expected: string[];
  covered: CoverageItem[];
  missing: string[];
  unknown: string[];
  ratio: number;
}

export interface CoverageReport {
  taskCount: number;
  tools: CoverageDimension;
  ops: CoverageDimension;
  complete: boolean;
}

interface RunMetadata {
  run_id?: unknown;
  k?: unknown;
  trials?: unknown;
}

interface CoverageTask {
  id: string;
  expect?: {
    tools?: { required?: string[] };
    ops?: { required?: string[] };
  };
}

export async function loadRunReport(
  runId: string,
  options: { storageRoot?: string; passK?: number } = {},
): Promise<RunReport> {
  const root = options.storageRoot ? resolve(options.storageRoot) : evalStorageRoot();
  assertRunId(runId);
  const runDir = join(root, "agent-evals", runId);
  const records = await readTrialRecords(join(runDir, "trials.jsonl"), runId);
  const metadata = await readRunMetadata(join(runDir, "run.json"));
  if (typeof metadata?.run_id === "string" && metadata.run_id !== runId) {
    throw new Error(`run metadata id mismatch: expected ${runId}, got ${metadata.run_id}`);
  }
  const passK = options.passK ?? metadataPassK(metadata) ?? DEFAULT_PASS_K;
  return buildRunReport(runId, records, passK);
}

export async function readTrialRecords(path: string, expectedRunId?: string): Promise<TrialRecord[]> {
  let raw: string;
  try {
    raw = await readFile(path, "utf8");
  } catch (error) {
    throw new Error(`cannot read eval trials ${path}: ${errorMessage(error)}`);
  }
  const records: TrialRecord[] = [];
  for (const [index, line] of raw.split(/\r?\n/u).entries()) {
    if (!line.trim()) continue;
    let parsed: unknown;
    try {
      parsed = JSON.parse(line);
    } catch (error) {
      throw new Error(`invalid trials JSONL at ${path}:${index + 1}: ${errorMessage(error)}`);
    }
    const record = parseTrialRecord(parsed, `${path}:${index + 1}`);
    if (expectedRunId !== undefined && record.run_id !== expectedRunId) {
      throw new Error(
        `trial run_id mismatch at ${path}:${index + 1}: expected ${expectedRunId}, got ${record.run_id}`,
      );
    }
    records.push(record);
  }
  if (records.length === 0) throw new Error(`eval trials file is empty: ${path}`);
  return records;
}

export function buildRunReport(runId: string, records: readonly TrialRecord[], passK = DEFAULT_PASS_K): RunReport {
  assertPositiveInteger(passK, "pass k");
  if (records.length === 0) throw new Error("cannot report an empty trial set");
  const buckets = new Map<string, TrialRecord[]>();
  const seenTrials = new Set<string>();
  for (const record of records) {
    if (record.run_id !== runId) {
      throw new Error(`trial ${record.layer}/${record.task_id} belongs to run ${record.run_id}, expected ${runId}`);
    }
    const key = taskKey(record.layer, record.task_id);
    const uniqueTrial = `${key}\u0000${record.trial}`;
    if (seenTrials.has(uniqueTrial)) {
      throw new Error(`duplicate trial number ${record.trial} for ${record.layer}/${record.task_id}`);
    }
    seenTrials.add(uniqueTrial);
    const rows = buckets.get(key) ?? [];
    if (rows.length > 0 && (rows[0].skill !== record.skill || rows[0].suite !== record.suite)) {
      throw new Error(`task metadata changed across trials for ${record.layer}/${record.task_id}`);
    }
    rows.push(record);
    buckets.set(key, rows);
  }

  const tasks = [...buckets.entries()]
    .map(([key, rows]) => taskMetric(key, rows, passK))
    .sort(compareTasks);
  const bySuite = groupMetrics(tasks, (task) => task.suite);
  const unobservableTrials = records.filter(isUnobservableTrial).length;
  const gate = regressionGate(bySuite, passK);
  return {
    measurementEligible: unobservableTrials === 0,
    unobservableTrials,
    runId,
    passK,
    tasks,
    overall: summarize(tasks),
    bySkill: groupMetrics(tasks, (task) => task.skill),
    bySuite,
    regressionGate: unobservableTrials ? { ...gate, passed: null, reason: `${unobservableTrials} unobservable trial(s); raw counts are diagnostic only` } : gate,
  };
}

export function passAtK(successes: number, trials: number, k: number): number | null {
  assertNonNegativeInteger(successes, "successes");
  assertNonNegativeInteger(trials, "trials");
  assertPositiveInteger(k, "pass k");
  if (successes > trials) throw new Error(`successes ${successes} cannot exceed trials ${trials}`);
  if (trials < k) return null;
  if (successes < k) return 0;
  let ratio = 1;
  for (let index = 0; index < k; index += 1) {
    ratio *= (successes - index) / (trials - index);
  }
  return ratio;
}

export function wilson95(successes: number, trials: number): WilsonInterval {
  assertNonNegativeInteger(successes, "successes");
  assertPositiveInteger(trials, "trials");
  if (successes > trials) throw new Error(`successes ${successes} cannot exceed trials ${trials}`);
  const proportion = successes / trials;
  const zSquared = WILSON_Z_95 ** 2;
  const denominator = 1 + zSquared / trials;
  const center = (proportion + zSquared / (2 * trials)) / denominator;
  const margin =
    (WILSON_Z_95 * Math.sqrt((proportion * (1 - proportion)) / trials + zSquared / (4 * trials ** 2))) /
    denominator;
  return { low: Math.max(0, center - margin), high: Math.min(1, center + margin) };
}

export function diffReports(baseline: RunReport, candidate: RunReport): ReportDiff {
  if (!baseline.measurementEligible || !candidate.measurementEligible) throw new Error("cannot compare unobservable eval runs as capability measurements");
  if (baseline.passK !== candidate.passK) {
    throw new Error(`cannot diff pass^${baseline.passK} against pass^${candidate.passK}`);
  }
  const baselineTasks = new Map(baseline.tasks.map((task) => [task.key, task]));
  const candidateTasks = new Map(candidate.tasks.map((task) => [task.key, task]));
  const keys = [...new Set([...baselineTasks.keys(), ...candidateTasks.keys()])].sort();
  const tasks = keys.map((key): TaskDiff => {
    const before = baselineTasks.get(key);
    const after = candidateTasks.get(key);
    const anchor = after ?? before!;
    if (before && after && (before.skill !== after.skill || before.suite !== after.suite)) {
      throw new Error(`task metadata changed between runs for ${after.layer}/${after.taskId}`);
    }
    const deltaPassAt1 = before && after ? after.passAt1 - before.passAt1 : null;
    const deltaPassAtK = before && after && before.passAtK !== null && after.passAtK !== null
      ? after.passAtK - before.passAtK
      : null;
    let change: TaskDiff["change"];
    if (!before) change = "added";
    else if (!after) change = "removed";
    else if (deltaPassAt1 === 0 && before.passAtK === after.passAtK) change = "unchanged";
    else change = "changed";
    return {
      key,
      layer: anchor.layer,
      taskId: anchor.taskId,
      skill: anchor.skill,
      suite: anchor.suite,
      change,
      baselinePassAt1: before?.passAt1 ?? null,
      candidatePassAt1: after?.passAt1 ?? null,
      deltaPassAt1,
      baselinePassAtK: before?.passAtK ?? null,
      candidatePassAtK: after?.passAtK ?? null,
      deltaPassAtK,
    };
  });
  return {
    baselineRunId: baseline.runId,
    candidateRunId: candidate.runId,
    passK: candidate.passK,
    deltaPassAt1: candidate.overall.passAt1 - baseline.overall.passAt1,
    deltaPassAtK:
      candidate.overall.passAtK !== null && baseline.overall.passAtK !== null
        ? candidate.overall.passAtK - baseline.overall.passAtK
        : null,
    tasks,
  };
}

export async function collectCoverage(tasksRoot = resolveFromModule("tasks")): Promise<CoverageReport> {
  const files = await listJSONFiles(tasksRoot);
  if (files.length === 0) throw new Error(`no eval task JSON files found under ${tasksRoot}`);
  const tasks: CoverageTask[] = [];
  const taskIDs = new Set<string>();
  for (const file of files) {
    let parsed: unknown;
    try {
      parsed = JSON.parse(await readFile(file, "utf8"));
    } catch (error) {
      throw new Error(`invalid eval task JSON ${file}: ${errorMessage(error)}`);
    }
    const task = parseCoverageTask(parsed, file);
    if (taskIDs.has(task.id)) throw new Error(`duplicate eval task id in coverage input: ${task.id}`);
    taskIDs.add(task.id);
    tasks.push(task);
  }

  const toolCounts = countRequired(tasks, (task) => task.expect?.tools?.required ?? []);
  const opCounts = countRequired(tasks, (task) => task.expect?.ops?.required ?? []);
  const expectedTools = TOOL_MANIFEST.filter((entry) => entry.scope !== "synthetic").map((entry) => entry.name).sort();
  const expectedOps = [...GRAPH_COMMAND_OPS].sort();
  const tools = coverageDimension(expectedTools, toolCounts);
  const ops = coverageDimension(expectedOps, opCounts);
  return {
    taskCount: tasks.length,
    tools,
    ops,
    complete: tools.missing.length === 0 && tools.unknown.length === 0 && ops.missing.length === 0 && ops.unknown.length === 0,
  };
}

export function formatRunReport(report: RunReport): string {
  const overall = report.overall;
  const lines = [
    `# Agent Eval Report: ${report.runId}`,
    `measurement_eligible=${report.measurementEligible} unobservable_trials=${report.unobservableTrials}${report.measurementEligible ? "" : "; all counts below are diagnostic, not capability scores"}`,
    "",
    `run_id=${report.runId} tasks=${overall.taskCount} trials=${overall.trialCount} pass^1=${formatRatio(overall.passAt1)} pass^${report.passK}=${formatNullableRatio(overall.passAtK)} trial_success=${formatRatio(overall.trialSuccessProportion)} wilson95_trial_success=[${formatRatio(overall.trialSuccessWilson95.low)},${formatRatio(overall.trialSuccessWilson95.high)}] tokens=${overall.tokenCount} token_usage_unavailable=${overall.tokenCountUnavailable} duration_ms=${overall.durationMs}`,
    `regression_gate=${report.regressionGate.passed === null ? "unavailable" : report.regressionGate.passed ? "pass" : "fail"} pass^1_threshold=0.9500 pass^3_threshold=0.9000 reason=${report.regressionGate.reason}`,
    "",
    `Wilson 95% interval is calculated from ${overall.successes}/${overall.trialCount} trial outcomes. It is not a confidence interval for the task-mean pass^${report.passK}.`,
    "",
    `## By Skill`,
    "",
    metricTable(report.bySkill, report.passK),
    "",
    `## By Suite`,
    "",
    metricTable(report.bySuite, report.passK),
    "",
    "## Tasks",
    "",
    `| Layer | Task | Skill | Suite | c/n | pass^1 | pass^${report.passK} | Tokens | Duration ms |`,
    "| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: |",
    ...report.tasks.map(
      (task) =>
        `| ${escapeCell(task.layer)} | ${escapeCell(task.taskId)} | ${escapeCell(task.skill)} | ${escapeCell(task.suite)} | ${task.successes}/${task.trials} | ${formatRatio(task.passAt1)} | ${formatNullableRatio(task.passAtK)} | ${task.tokenCount}${task.tokenCountUnavailable > 0 ? ` (${task.tokenCountUnavailable} unavailable)` : ""} | ${task.durationMs} |`,
    ),
  ];
  if (overall.passAtK === null) lines.splice(4, 0, `pass^${report.passK} is unavailable because ${overall.passAtKUnavailableTasks} task(s) have fewer than ${report.passK} trials.`, "");
  return lines.join("\n");
}

function regressionGate(groups: readonly GroupMetric[], passK: number): RegressionGate {
  const regression = groups.find((group) => group.name === "regression");
  const thresholds = { passAt1: 0.95 as const, passAt3: 0.9 as const };
  if (!regression) {
    return { applicable: false, passed: null, reason: "no regression tasks", passAt1: null, passAt3: null, thresholds };
  }
  if (passK !== 3 || regression.passAtK === null) {
    return {
      applicable: false,
      passed: null,
      reason: "requires at least three trials per regression task",
      passAt1: regression.passAt1,
      passAt3: null,
      thresholds,
    };
  }
  return {
    applicable: true,
    passed: regression.passAt1 >= thresholds.passAt1 && regression.passAtK >= thresholds.passAt3,
    reason: "evaluated",
    passAt1: regression.passAt1,
    passAt3: regression.passAtK,
    thresholds,
  };
}

export function formatReportDiff(diff: ReportDiff): string {
  return [
    `# Agent Eval Diff: ${diff.baselineRunId} -> ${diff.candidateRunId}`,
    "",
    `baseline=${diff.baselineRunId} candidate=${diff.candidateRunId} delta_pass^1=${formatSignedRatio(diff.deltaPassAt1)} delta_pass^${diff.passK}=${formatNullableSignedRatio(diff.deltaPassAtK)}`,
    "",
    `| Change | Layer | Task | Skill | Suite | pass^1 before | pass^1 after | Delta | pass^${diff.passK} before | pass^${diff.passK} after | Delta |`,
    "| --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |",
    ...diff.tasks.map(
      (task) =>
        `| ${task.change} | ${escapeCell(task.layer)} | ${escapeCell(task.taskId)} | ${escapeCell(task.skill)} | ${escapeCell(task.suite)} | ${formatNullableRatio(task.baselinePassAt1)} | ${formatNullableRatio(task.candidatePassAt1)} | ${formatNullableSignedRatio(task.deltaPassAt1)} | ${formatNullableRatio(task.baselinePassAtK)} | ${formatNullableRatio(task.candidatePassAtK)} | ${formatNullableSignedRatio(task.deltaPassAtK)} |`,
    ),
  ].join("\n");
}

export function formatCoverageReport(report: CoverageReport): string {
  const lines = [
    "# Agent Eval Coverage",
    "",
    `tasks=${report.taskCount} tools=${report.tools.covered.length}/${report.tools.expected.length} ops=${report.ops.covered.length}/${report.ops.expected.length} complete=${report.complete}`,
    "",
    coverageSection("Tools", report.tools),
    "",
    coverageSection("Graph Command Ops", report.ops),
  ];
  return lines.join("\n");
}

export async function loadEvalHistory(
  options: { storageRoot?: string; limit?: number } = {},
): Promise<Array<Pick<RunReport, "runId" | "bySuite" | "overall">>> {
  const root = options.storageRoot ? resolve(options.storageRoot) : evalStorageRoot();
  const path = join(root, "agent-evals", "history.jsonl");
  let raw: string;
  try {
    raw = await readFile(path, "utf8");
  } catch (error) {
    if (isNodeError(error) && error.code === "ENOENT") return [];
    throw error;
  }
  const lines = raw.split(/\r?\n/u).filter((line) => line.trim());
  const limited = options.limit ? lines.slice(-options.limit) : lines;
  const reports: Array<Pick<RunReport, "runId" | "bySuite" | "overall">> = [];
  for (const line of limited) {
    const parsed = JSON.parse(line) as { run_id?: string };
    if (typeof parsed.run_id !== "string") continue;
    try {
      reports.push(await loadRunReport(parsed.run_id, { storageRoot: options.storageRoot }));
    } catch {
      // history may mention a deleted run; skip rather than fail report
    }
  }
  return reports;
}

export function saturationWarnings(history: ReadonlyArray<Pick<RunReport, "bySuite">>): string[] {
  if (history.length < 4) return [];
  const recent = history.slice(-4);
  const names = new Set(recent.flatMap((report) => report.bySuite.map((suite) => suite.name)));
  const warnings: string[] = [];
  for (const name of [...names].sort()) {
    const series = recent.map((report) => report.bySuite.find((suite) => suite.name === name));
    if (series.some((suite) => !suite || suite.passAt1 < 1 || suite.taskCount === 0)) continue;
    warnings.push(`${name} passed 100% in the last 4 reports; add capability tasks`);
  }
  return warnings;
}

function taskMetric(key: string, rows: readonly TrialRecord[], k: number): TaskMetric {
  const first = rows[0];
  const successes = rows.filter((row) => row.passed).length;
  const tokenRows = rows.filter((row) => row.token_count !== null);
  return {
    key,
    layer: first.layer,
    taskId: first.task_id,
    skill: first.skill,
    suite: first.suite,
    successes,
    trials: rows.length,
    passAt1: successes / rows.length,
    passAtK: passAtK(successes, rows.length, k),
    tokenCount: tokenRows.reduce((total, row) => total + (row.token_count ?? 0), 0),
    tokenCountUnavailable: rows.length - tokenRows.length,
    durationMs: rows.reduce((total, row) => total + row.duration_ms, 0),
  };
}

function summarize(tasks: readonly TaskMetric[]): MetricSummary {
  if (tasks.length === 0) throw new Error("cannot summarize an empty task set");
  const trialCount = tasks.reduce((total, task) => total + task.trials, 0);
  const successes = tasks.reduce((total, task) => total + task.successes, 0);
  const unavailable = tasks.filter((task) => task.passAtK === null).length;
  return {
    taskCount: tasks.length,
    trialCount,
    successes,
    passAt1: mean(tasks.map((task) => task.passAt1)),
    passAtK: unavailable === 0 ? mean(tasks.map((task) => task.passAtK as number)) : null,
    passAtKUnavailableTasks: unavailable,
    trialSuccessProportion: successes / trialCount,
    trialSuccessWilson95: wilson95(successes, trialCount),
    tokenCount: tasks.reduce((total, task) => total + task.tokenCount, 0),
    tokenCountUnavailable: tasks.reduce((total, task) => total + task.tokenCountUnavailable, 0),
    durationMs: tasks.reduce((total, task) => total + task.durationMs, 0),
  };
}

function groupMetrics(tasks: readonly TaskMetric[], key: (task: TaskMetric) => string): GroupMetric[] {
  const groups = new Map<string, TaskMetric[]>();
  for (const task of tasks) groups.set(key(task), [...(groups.get(key(task)) ?? []), task]);
  return [...groups.entries()]
    .map(([name, rows]) => ({ name, ...summarize(rows) }))
    .sort((left, right) => left.name.localeCompare(right.name));
}

function metricTable(groups: readonly GroupMetric[], k: number): string {
  return [
    `| Name | Tasks | Trials | pass^1 | pass^${k} | Trial success | Wilson 95% (trial success) |`,
    "| --- | ---: | ---: | ---: | ---: | ---: | ---: |",
    ...groups.map(
      (group) =>
        `| ${escapeCell(group.name)} | ${group.taskCount} | ${group.trialCount} | ${formatRatio(group.passAt1)} | ${formatNullableRatio(group.passAtK)} | ${formatRatio(group.trialSuccessProportion)} | [${formatRatio(group.trialSuccessWilson95.low)}, ${formatRatio(group.trialSuccessWilson95.high)}] |`,
    ),
  ].join("\n");
}

function taskKey(layer: string, taskId: string): string {
  return `${layer}\u0000${taskId}`;
}

function compareTasks(left: TaskMetric, right: TaskMetric): number {
  return left.layer.localeCompare(right.layer) || left.taskId.localeCompare(right.taskId);
}

function mean(values: readonly number[]): number {
  return values.reduce((total, value) => total + value, 0) / values.length;
}

function parseTrialRecord(value: unknown, source: string): TrialRecord {
  if (Value.Check(EvalTrialRecordSchema, value)) return value;
  const details = Value.Errors(EvalTrialRecordSchema, value)
    .map((error) => `${error.instancePath || "/"}: ${error.message}`)
    .join("; ");
  throw new Error(
    `trial at ${source} does not match EvalTrialRecord schema${details ? `: ${details}` : ""}`,
  );
}

async function readRunMetadata(path: string): Promise<RunMetadata | null> {
  try {
    const parsed: unknown = JSON.parse(await readFile(path, "utf8"));
    if (!isRecord(parsed)) throw new Error("root must be an object");
    return parsed;
  } catch (error) {
    if (isNodeError(error) && error.code === "ENOENT") return null;
    throw new Error(`invalid eval run metadata ${path}: ${errorMessage(error)}`);
  }
}

function metadataPassK(metadata: RunMetadata | null): number | null {
  if (!metadata) return null;
  const value = metadata.k ?? metadata.trials;
  if (value === undefined) return null;
  assertPositiveInteger(value, "run metadata k");
  return value as number;
}

function assertRunId(runId: string): void {
  if (!runId || runId === "." || runId === ".." || runId.includes("/") || runId.includes("\\")) {
    throw new Error(`invalid eval run id: ${runId || "(empty)"}`);
  }
}

async function listJSONFiles(root: string): Promise<string[]> {
  let entries;
  try {
    entries = await readdir(root, { withFileTypes: true });
  } catch (error) {
    throw new Error(`cannot read eval tasks directory ${root}: ${errorMessage(error)}`);
  }
  const files: string[] = [];
  for (const entry of entries.sort((left, right) => left.name.localeCompare(right.name))) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) files.push(...await listJSONFiles(path));
    else if (entry.isFile() && entry.name.endsWith(".json")) files.push(path);
  }
  return files;
}

function parseCoverageTask(value: unknown, source: string): CoverageTask {
  if (!isRecord(value) || typeof value.id !== "string" || value.id.length === 0) {
    throw new Error(`eval task at ${source} has invalid id`);
  }
  const expect = value.expect;
  if (expect !== undefined && !isRecord(expect)) throw new Error(`eval task ${value.id} has invalid expect`);
  const tools = isRecord(expect) ? expect.tools : undefined;
  const ops = isRecord(expect) ? expect.ops : undefined;
  if (tools !== undefined && !isRecord(tools)) throw new Error(`eval task ${value.id} has invalid expect.tools`);
  if (ops !== undefined && !isRecord(ops)) throw new Error(`eval task ${value.id} has invalid expect.ops`);
  const requiredTools = isRecord(tools) ? parseStringList(tools.required, `task ${value.id} expect.tools.required`) : [];
  const requiredOps = isRecord(ops) ? parseStringList(ops.required, `task ${value.id} expect.ops.required`) : [];
  return { id: value.id, expect: { tools: { required: requiredTools }, ops: { required: requiredOps } } };
}

function parseStringList(value: unknown, source: string): string[] {
  if (value === undefined) return [];
  if (!Array.isArray(value) || value.some((item) => typeof item !== "string" || item.length === 0)) {
    throw new Error(`${source} must be an array of non-empty strings`);
  }
  return value as string[];
}

function countRequired(tasks: readonly CoverageTask[], read: (task: CoverageTask) => readonly string[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const task of tasks) {
    for (const name of new Set(read(task))) counts.set(name, (counts.get(name) ?? 0) + 1);
  }
  return counts;
}

function coverageDimension(expected: readonly string[], counts: ReadonlyMap<string, number>): CoverageDimension {
  const expectedSet = new Set(expected);
  const covered = expected
    .filter((name) => counts.has(name))
    .map((name) => ({ name, taskCount: counts.get(name) ?? 0 }));
  return {
    expected: [...expected],
    covered,
    missing: expected.filter((name) => !counts.has(name)),
    unknown: [...counts.keys()].filter((name) => !expectedSet.has(name)).sort(),
    ratio: expected.length === 0 ? 1 : covered.length / expected.length,
  };
}

function coverageSection(title: string, dimension: CoverageDimension): string {
  return [
    `## ${title}`,
    "",
    `coverage=${dimension.covered.length}/${dimension.expected.length} (${formatRatio(dimension.ratio)})`,
    `missing=${dimension.missing.length > 0 ? dimension.missing.join(",") : "(none)"}`,
    `unknown=${dimension.unknown.length > 0 ? dimension.unknown.join(",") : "(none)"}`,
  ].join("\n");
}

function resolveFromModule(relative: string): string {
  return resolve(fileURLToPath(new URL(relative, import.meta.url)));
}

function formatRatio(value: number): string {
  return value.toFixed(4);
}

function formatNullableRatio(value: number | null): string {
  return value === null ? "unavailable" : formatRatio(value);
}

function formatSignedRatio(value: number): string {
  return `${value >= 0 ? "+" : ""}${value.toFixed(4)}`;
}

function formatNullableSignedRatio(value: number | null): string {
  return value === null ? "unavailable" : formatSignedRatio(value);
}

function escapeCell(value: string): string {
  return value.replaceAll("|", "\\|").replaceAll("\n", " ");
}

function assertPositiveInteger(value: unknown, name: string): asserts value is number {
  if (!Number.isInteger(value) || (value as number) < 1) throw new Error(`${name} must be a positive integer`);
}

function assertNonNegativeInteger(value: unknown, name: string): asserts value is number {
  if (!Number.isInteger(value) || (value as number) < 0) throw new Error(`${name} must be a non-negative integer`);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isNodeError(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && "code" in error;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
