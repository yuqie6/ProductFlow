/** Scripted L0 tool and skill contract evaluation. This never starts Pi. */

import { isDeepStrictEqual } from "node:util";

import { Value } from "typebox/value";

import { expectedToolNamesForScope, toolManifestEntry, type ToolName } from "../src/tool-manifest.js";
import { gradeOperations, gradeTools, gradeWrites } from "./graders/index.js";
import type { EvalCallRecord as GraderCallRecord } from "./graders/types.js";
import { loadSkillCatalog, type SkillCatalog } from "../src/skills.js";
import { loadEvalTaskSet, validateEvalTask } from "./loader.js";
import { checkJSONSchema, loadGlobalDraftSchema } from "./json-schema.js";
import type { EvalReferenceCall, EvalTask } from "./schema.js";

export type { EvalReferenceCall };

export interface EvalReportRow {
  id: string;
  skillName: string;
  ok: boolean;
  callCount: number;
  tokenCount: number | null;
  error?: string;
}

export interface EvalReport {
  ok: boolean;
  rows: EvalReportRow[];
}

export async function runScriptedSkillEvals(catalog?: SkillCatalog): Promise<EvalReport> {
  const loaded = catalog ?? await loadSkillCatalog();
  const { tasks, worlds } = await loadEvalTaskSet({ catalog: loaded });
  const rows: EvalReportRow[] = [];
  for (const task of tasks.filter((candidate) => candidate.layers.includes("l0"))) {
    const calls = [...task.reference.scripted_calls];
    try {
      validateEvalTask(task, worlds.get(task.world)!, loaded);
      assertRegistered(task, calls);
      assertSchemas(calls);
      assertTaskExpectations(task, calls);
      assertRepair(task);
      rows.push({ id: task.id, skillName: task.skill, ok: true, callCount: calls.length, tokenCount: 0 });
    } catch (error) {
      rows.push({
        id: task.id,
        skillName: task.skill,
        ok: false,
        callCount: calls.length,
        tokenCount: 0,
        error: error instanceof Error ? error.message : String(error),
      });
    }
  }
  return { ok: rows.every((row) => row.ok), rows };
}

function assertRegistered(task: EvalTask, calls: EvalReferenceCall[]): void {
  const expected = new Set(expectedToolNamesForScope(task.scope, true));
  expected.add("load_productflow_skill");
  expected.add("ask_user");
  for (const call of calls) {
    if (!expected.has(call.name as ToolName) && !expected.has(call.name)) {
      throw new Error(`Tool ${call.name} is not registered for ${task.scope}`);
    }
  }
}

function assertSchemas(calls: EvalReferenceCall[]): void {
  for (const call of calls) {
    if (!paramsMatchSchema(call.name, call.params)) {
      throw new Error(`Params for ${call.name} do not match the manifest schema`);
    }
  }
}

export function paramsMatchSchema(name: string, params: unknown): boolean {
  const entry = toolManifestEntry(name);
  if (!entry) throw new Error(`Unknown tool ${name}`);
  if ("input_schema_source" in entry && entry.input_schema_source === "contract:draft_schema") {
    return checkJSONSchema(loadGlobalDraftSchema(), params);
  }
  return Value.Check(entry.input_schema, params);
}

export function assertTaskExpectations(task: EvalTask, calls: EvalReferenceCall[]): void {
  const recorded: GraderCallRecord[] = calls.map((call) => ({ ...call, ts: "reference" }));
  const checks = [
    ["expect.tools", gradeTools(task.expect.tools, recorded)],
    ["expect.ops", gradeOperations(task.expect.ops, recorded)],
    ["expect.writes", gradeWrites(task.expect.writes, recorded)],
  ] as const;
  const errors = checks.flatMap(([name, grade]) => grade.errors.map((error) => `${name}: ${error}`));
  if (errors.length > 0) throw new Error(errors.join("; "));
}

function assertRepair(task: EvalTask): void {
  const repair = task.reference.repair;
  if (!repair) return;
  if (!toolManifestEntry(repair.tool_name)) throw new Error(`Unknown repair tool ${repair.tool_name}`);
  if (paramsMatchSchema(repair.tool_name, repair.illegal_params)) {
    throw new Error(`Illegal params for ${repair.tool_name} were accepted on attempt 1`);
  }
  if (!paramsMatchSchema(repair.tool_name, repair.repaired_params)) {
    throw new Error(`Repaired params for ${repair.tool_name} failed schema on attempt 2`);
  }
  const gold = task.reference.scripted_calls.find((call) => call.name === repair.tool_name);
  if (!gold) throw new Error(`repair tool ${repair.tool_name} is missing from reference.scripted_calls`);
  if (!isDeepStrictEqual(gold.params, repair.repaired_params)) {
    throw new Error(`reference.repair.repaired_params must equal the scripted gold call for ${repair.tool_name}`);
  }
  const isolatedTask = {
    ...task,
    expect: {
      ...task.expect,
      tools: { required: [repair.tool_name], forbidden: task.expect.tools.forbidden },
      ops: { required: [], forbidden: task.expect.ops.forbidden },
      writes: task.expect.writes.filter((write) => write.tool === repair.tool_name),
    },
  };
  assertTaskExpectations(isolatedTask, [{ name: repair.tool_name, params: repair.repaired_params }]);
}

export function formatEvalReport(report: EvalReport): string {
  const passed = report.rows.filter((row) => row.ok).length;
  const calls = report.rows.reduce((sum, row) => sum + row.callCount, 0);
  const tokens = report.rows.reduce((sum, row) => sum + (row.tokenCount ?? 0), 0);
  const unavailableUsage = report.rows.filter((row) => row.tokenCount === null).length;
  const lines = [
    "skill eval report",
    `ok=${report.ok} scenarios=${report.rows.length} passed=${passed} success_rate=${passed}/${report.rows.length} calls=${calls} tokens=${tokens} usage_unavailable=${unavailableUsage}`,
  ];
  for (const row of report.rows) {
    lines.push(
      `${row.ok ? "pass" : "fail"} ${row.id} calls=${row.callCount} tokens=${row.tokenCount ?? "unavailable"}${row.error ? ` error=${row.error}` : ""}`,
    );
  }
  return lines.join("\n");
}
