/**
 * 脚本化工具/技能评测。不启动 Pi。
 * fixture.scriptedCalls 是与用户话语对齐的脚本化模型应答；harness 校验
 * 作用域、TypeBox / draft schema、guards、禁令、话语约束，以及两次内修复。
 */

import { Value } from "typebox/value";

import { expectedToolNamesForScope, toolManifestEntry, type ToolName } from "../src/tool-manifest.js";
import { loadSkillCatalog, type SkillCatalog } from "../src/skills.js";
import {
  SKILL_EVAL_FIXTURES,
  validateSkillEvalFixture,
  type EvalCall,
  type SkillEvalFixture,
} from "./fixtures.js";
import { checkJSONSchema, loadGlobalDraftSchema } from "./json-schema.js";

export type { EvalCall };

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
  const rows: EvalReportRow[] = [];
  for (const [index, fixture] of SKILL_EVAL_FIXTURES.entries()) {
    const id = `${fixture.skillName}-${index + 1}`;
    try {
      validateSkillEvalFixture(fixture, loaded);
      const calls = [...fixture.scriptedCalls];
      assertRegistered(fixture, calls);
      assertSchemas(calls);
      assertForbidden(fixture, calls);
      assertUtteranceAlignment(fixture, calls);
      assertRepair(fixture);
      rows.push({ id, skillName: fixture.skillName, ok: true, callCount: calls.length, tokenCount: 0 });
    } catch (error) {
      rows.push({
        id,
        skillName: fixture.skillName,
        ok: false,
        callCount: fixture.scriptedCalls.length,
        tokenCount: 0,
        error: error instanceof Error ? error.message : String(error),
      });
    }
  }
  return { ok: rows.every((row) => row.ok), rows };
}

function assertRegistered(fixture: SkillEvalFixture, calls: EvalCall[]): void {
  const expected = new Set(expectedToolNamesForScope(fixture.contractScope, true));
  expected.add("load_productflow_skill");
  expected.add("ask_user");
  for (const call of calls) {
    if (call.name === "load_productflow_skill") continue;
    if (!expected.has(call.name as ToolName) && !expected.has(call.name)) {
      throw new Error(`Tool ${call.name} is not registered for ${fixture.contractScope}`);
    }
  }
}

function assertSchemas(calls: EvalCall[]): void {
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

function assertForbidden(fixture: SkillEvalFixture, calls: EvalCall[]): void {
  const names = new Set(calls.map((call) => call.name));
  for (const toolName of fixture.neverTools ?? []) {
    if (names.has(toolName)) throw new Error(`Forbidden tool was selected: ${toolName}`);
  }
  const ops = graphOps(calls);
  for (const op of fixture.neverOps ?? []) {
    if (ops.includes(op)) throw new Error(`Forbidden Graph Command op was used: ${op}`);
  }
}

function graphOps(calls: EvalCall[]): string[] {
  return calls.flatMap((call) => {
    if (!call.params || typeof call.params !== "object") return [];
    const operations = (call.params as { operations?: Array<{ op?: string }> }).operations;
    if (!Array.isArray(operations)) return [];
    return operations.map((operation) => operation.op).filter((op): op is string => typeof op === "string");
  });
}

export function assertUtteranceAlignment(fixture: SkillEvalFixture, calls: EvalCall[]): void {
  const request = fixture.userRequest;
  if (/主图\s*2/.test(request) && request.includes("细节")) {
    const types = intakeTypes(calls);
    const askedWithoutFinalize = calls.some((call) => call.name === "ask_user") && types.length === 0;
    if (!askedWithoutFinalize) {
      if (!types.some((row) => row.key === "hero" && row.quantity === 2)) {
        throw new Error("utterance asked for 主图2张 but finalize selection does not match");
      }
      if (!types.some((row) => row.key === "detail" && row.quantity === 2)) {
        throw new Error("utterance asked for 细节图2张 but finalize selection does not match");
      }
    }
  }
  if (request.includes("展开模板")) {
    const types = intakeTypes(calls);
    const worldTypes = (fixture.world.intake?.image_types ?? []) as Array<{ key: string; quantity: number }>;
    if (worldTypes.length === 0 || types.length !== worldTypes.length) {
      throw new Error("expand-template fixture must finalize the existing intake selection");
    }
    for (const row of worldTypes) {
      if (!types.some((item) => item.key === row.key && item.quantity === row.quantity)) {
        throw new Error(`expand-template selection missing ${row.key}:${row.quantity}`);
      }
    }
  }
  if (request.includes("改名为新标题") || request.includes("改个名字")) {
    const ops = graphOps(calls.filter((call) => call.name === "apply_graph_change_set_v1"));
    if (ops.length !== 1 || ops[0] !== "rename_node") {
      throw new Error("rename utterance must apply exactly one rename_node");
    }
  }
  if (request.includes("场景镜头")) {
    const ops = graphOps(calls.filter((call) => call.name === "propose_graph_change_set_v1"));
    for (const required of ["create_group", "create_node", "connect_nodes"]) {
      if (!ops.includes(required)) throw new Error(`add-shot utterance must propose ${required}`);
    }
  }
  if (request.includes("季节文件夹")) {
    const draft = calls.find((call) => call.name === "propose_global_draft");
    const operations = draftLibraryOps(draft?.params);
    if (!operations.some((op) => op === "move" || op === "archive")) {
      throw new Error("seasonal-folder utterance must propose move or archive");
    }
  }
  if (request.includes("重试")) {
    const run = calls.find((call) => call.name === "request_workflow_run_v1");
    const source = run && typeof run.params === "object" && run.params
      ? (run.params as { source_run_id?: unknown }).source_run_id
      : undefined;
    if (typeof source !== "string" || source.length === 0) {
      throw new Error("retry utterance must set source_run_id");
    }
  }
  for (const call of calls) {
    if (call.name === "apply_graph_change_set_v1" || call.name === "propose_graph_change_set_v1") {
      const revision = call.params && typeof call.params === "object"
        ? (call.params as { base_graph_revision?: unknown }).base_graph_revision
        : undefined;
      if (revision !== fixture.world.liveGraph.revision) {
        throw new Error(`${call.name} must use world liveGraph.revision`);
      }
    }
  }
}

function intakeTypes(calls: EvalCall[]): Array<{ key: string; quantity: number }> {
  const finalize = calls.find((call) => call.name === "finalize_product_intake_v1");
  if (!finalize || !finalize.params || typeof finalize.params !== "object") return [];
  const selection = (finalize.params as { selection?: { image_types?: unknown } }).selection;
  const rows = selection?.image_types;
  if (!Array.isArray(rows)) return [];
  return rows.flatMap((row) => {
    if (!row || typeof row !== "object") return [];
    const key = (row as { key?: unknown }).key;
    const quantity = (row as { quantity?: unknown }).quantity;
    if (typeof key !== "string" || typeof quantity !== "number") return [];
    return [{ key, quantity }];
  });
}

function draftLibraryOps(params: unknown): string[] {
  if (!params || typeof params !== "object") return [];
  const payload = (params as { library_payload?: { operations?: Array<{ operation?: string }> } }).library_payload;
  if (!payload || !Array.isArray(payload.operations)) return [];
  return payload.operations.map((row) => row.operation).filter((op): op is string => typeof op === "string");
}

function assertRepair(fixture: SkillEvalFixture): void {
  const repair = fixture.repair;
  if (!repair) return;
  if (!toolManifestEntry(repair.toolName)) throw new Error(`Unknown repair tool ${repair.toolName}`);
  if (paramsMatchSchema(repair.toolName, repair.illegalParams)) {
    throw new Error(`Illegal params for ${repair.toolName} were accepted on attempt 1`);
  }
  if (!paramsMatchSchema(repair.toolName, repair.repairedParams)) {
    throw new Error(`Repaired params for ${repair.toolName} failed schema on attempt 2`);
  }
  const gold = fixture.scriptedCalls.find((call) => call.name === repair.toolName);
  if (!gold) throw new Error(`repair tool ${repair.toolName} is missing from scriptedCalls`);
  if (JSON.stringify(gold.params) !== JSON.stringify(repair.repairedParams)) {
    throw new Error(`repair.repairedParams must equal the scripted gold call for ${repair.toolName}`);
  }
  assertForbidden(fixture, [{ name: repair.toolName, params: repair.repairedParams }]);
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
