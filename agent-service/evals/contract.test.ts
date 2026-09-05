import { describe, expect, it } from "vitest";

import { loadSkillCatalog } from "../src/skills.js";
import { gradeOperations, gradeTerminal, gradeTools, gradeWrites } from "./graders/index.js";
import type { EvalCallRecord as GraderCallRecord } from "./graders/types.js";
import { assertTaskExpectations, formatEvalReport, paramsMatchSchema, runScriptedSkillEvals } from "./harness.js";
import { loadEvalTaskSet, validateEvalTask } from "./loader.js";
import { checkJSONSchema, loadGlobalDraftSchema } from "./json-schema.js";
import { collectCoverage } from "./report.js";
import { evalJSONSchemas, type EvalTask } from "./schema.js";

describe("ProductFlow Agent eval contracts", () => {
  it("loads the complete P1 task mix and all scripted contracts pass", async () => {
    const { tasks } = await loadEvalTaskSet();
    const l0 = tasks.filter((task) => task.layers.includes("l0"));
    expect(l0).toHaveLength(75);
    expect(tasks.filter((task) => task.layers.includes("l2")).length).toBeGreaterThanOrEqual(15);
    expect(tasks.filter((task) => task.layers.includes("l3")).length).toBeGreaterThanOrEqual(5);
    expect(new Set(tasks.map((task) => task.id)).size).toBe(tasks.length);
    expect(tasks.some((task) => task.suite === "regression")).toBe(true);
    expect(tasks.some((task) => task.suite === "capability")).toBe(true);
    expect(l0.filter((task) => task.reference.repair).length).toBe(2);
    const perSkill = new Map<string, { positive: number; negative: number }>();
    for (const task of tasks) {
      const counts = perSkill.get(task.skill) ?? { positive: 0, negative: 0 };
      counts[task.case_type] += 1;
      perSkill.set(task.skill, counts);
      expect(task.utterances.length, task.id).toBeGreaterThanOrEqual(3);
      expect(new Set(task.utterances).size, task.id).toBe(task.utterances.length);
    }
    for (const [skill, counts] of perSkill) {
      expect(counts.positive, `${skill} positive`).toBeGreaterThanOrEqual(10);
      expect(counts.negative, `${skill} negative`).toBeGreaterThanOrEqual(5);
    }
    for (const id of [
      "product-intake-negative-missing-reference-selection",
      "graph-editing-negative-global-scope",
      "media-library-organization-negative-delete-all",
      "run-diagnosis-negative-off-topic-copy",
    ]) {
      expect(tasks.find((task) => task.id === id)?.case_type, id).toBe("negative");
    }

    const report = await runScriptedSkillEvals();
    expect(report.ok, report.rows.filter((row) => !row.ok).map((row) => `${row.id}: ${row.error}`).join("; ")).toBe(true);
  });

  it("keeps all callable tools and Graph Command operations covered", async () => {
    const coverage = await collectCoverage();
    expect(coverage.complete).toBe(true);
    expect(coverage.tools.missing).toEqual([]);
    expect(coverage.tools.unknown).toEqual([]);
    expect(coverage.ops.missing).toEqual([]);
    expect(coverage.ops.unknown).toEqual([]);
    const schemas = evalJSONSchemas();
    expect(schemas.task).toBeTruthy();
    expect(schemas.world).toBeTruthy();
    expect(schemas.trial_record).toBeTruthy();
  });

  it("aligns task JSON with worlds, page context, catalog, manifest, and progressive disclosure", async () => {
    const catalog = await loadSkillCatalog();
    const { tasks, worlds } = await loadEvalTaskSet({ catalog });
    for (const world of worlds.values()) {
      expect(world.live_graph.id, world.name).toBe("33333333-3333-4333-8333-333333333333");
    }
    for (const task of tasks) {
      const world = worlds.get(task.world)!;
      expect(() => validateEvalTask(task, world, catalog)).not.toThrow();
      expect(catalog.names).toContain(task.skill);
      expect(catalog.prompt).toContain(`<name>${task.skill}</name>`);
      expect(task.reference.scripted_calls[0]).toEqual({
        name: "load_productflow_skill",
        params: { skill_name: task.skill },
      });
      if (typeof task.page_context.workflow_id === "string") {
        expect(task.page_context.workflow_id, task.id).toBe(world.live_graph.id);
      }
      if (task.page_context.filters.workflow_id) {
        expect(task.page_context.filters.workflow_id, task.id).toBe(world.live_graph.id);
      }
    }
  });

  it("rejects a reference tool outside the declared skill owns_tools", async () => {
    const catalog = await loadSkillCatalog();
    const { tasks, worlds } = await loadEvalTaskSet({ catalog });
    const base = tasks.find((task) => task.skill === "product-intake")!;
    expect(() => validateEvalTask(
      {
        ...base,
        reference: {
          ...base.reference,
          scripted_calls: [
            { name: "load_productflow_skill", params: { skill_name: "product-intake" } },
            { name: "propose_global_draft", params: {} },
          ],
        },
      },
      worlds.get(base.world)!,
      catalog,
    )).toThrow(/propose_global_draft is not in that skill's owns_tools/);
  });

  it("grades write arguments from explicit path contracts", async () => {
    const { tasks } = await loadEvalTaskSet();
    const task = tasks.find((candidate) => candidate.id === "graph-editing-rename-node")!;
    expect(() => assertTaskExpectations(task, task.reference.scripted_calls)).not.toThrow();
    const wrongCalls = task.reference.scripted_calls.map((call) => call.name === "apply_graph_change_set_v1"
      ? {
          ...call,
          params: {
            base_graph_revision: 3,
            summary: "改名",
            operations: [{ op: "rename_node", node_ref: "node-prompt-1", title: "错误标题" }],
          },
        }
      : call);
    expect(() => assertTaskExpectations(task, wrongCalls)).toThrow(/expect\.writes/);
  });

  it("rejects illegal repair payloads and accepts the recorded second attempt", async () => {
    const { tasks } = await loadEvalTaskSet();
    const repairs = tasks.flatMap((task) => task.reference.repair ? [task.reference.repair] : []);
    expect(repairs).toHaveLength(2);
    for (const repair of repairs) {
      expect(paramsMatchSchema(repair.tool_name, repair.illegal_params), repair.tool_name).toBe(false);
      expect(paramsMatchSchema(repair.tool_name, repair.repaired_params), repair.tool_name).toBe(true);
    }
  });

  it("validates global drafts against the live dynamic schema", async () => {
    const { tasks } = await loadEvalTaskSet();
    const drafts = tasks.flatMap((task) => task.reference.scripted_calls
      .filter((call) => call.name === "propose_global_draft")
      .map((call) => call.params));
    const schema = loadGlobalDraftSchema();
    expect(drafts.length).toBeGreaterThanOrEqual(10);
    for (const draft of drafts) expect(checkJSONSchema(schema, draft)).toBe(true);
    expect(checkJSONSchema(schema, { draft_kind: "library_organization", library_payload: { operations: [] } })).toBe(false);
  });

  it("reports unavailable live usage without pretending it is zero", () => {
    const report = formatEvalReport({
      ok: true,
      rows: [{ id: "live-1", skillName: "product-intake", ok: true, callCount: 2, tokenCount: null }],
    });
    expect(report).toContain("usage_unavailable=1");
    expect(report).toContain("tokens=unavailable");
  });

  it("grades aligned contracts: legal intent routing passes and wrongful side effects still fail", async () => {
    const { tasks } = await loadEvalTaskSet();
    const byID = Object.fromEntries(tasks.map((task) => [task.id, task]));

    const unknown = byID["graph-editing-negative-unknown-node"]!;
    expect(unknown.expect.terminal).toEqual(["requires_input"]);
    expect(unknown.expect.question).toEqual({ required: true });
    expect(gradeLive(unknown, "requires_input", [
      loadSkill("graph-editing"),
      { name: "get_product_workflow_context_v1", params: { response_format: "concise" } },
      { name: "ask_user", params: { header: "目标节点", question: "画布没有促销节点，请指定要改名的节点。" } },
    ], { question: true })).toEqual([]);
    expect(gradeLive(unknown, "succeeded", [
      loadSkill("graph-editing"),
      { name: "get_product_workflow_context_v1", params: { response_format: "concise" } },
    ], { question: true })).toContain("terminal status succeeded is not one of: requires_input");
    expect(gradeLive(unknown, "requires_input", [
      loadSkill("graph-editing"),
      { name: "get_product_workflow_context_v1", params: { response_format: "concise" } },
      applyDelete("node-prompt-1"),
    ], { question: true })).toContain("expect.tools: forbidden tool was called: apply_graph_change_set_v1");
    expect(gradeLive(unknown, "requires_input", [
      loadSkill("graph-editing"),
      { name: "get_product_workflow_context_v1", params: { response_format: "concise" } },
      { name: "ask_user", params: { header: "目标节点", question: "请指定节点。" } },
    ])).toContain("expected a user question");

    const deleteGraph = byID["workflow-run-request-negative-off-topic-delete-graph"]!;
    expect(deleteGraph.expect.terminal).toEqual(["awaiting_confirmation"]);
    expect(gradeLive(deleteGraph, "awaiting_confirmation", [
      loadSkill("graph-editing"),
      { name: "get_product_workflow_context_v1", params: { response_format: "detailed" } },
      proposeDeletes(["source-1", "node-prompt-1", "node-image-1", "node-prompt-2", "node-image-2", "node-brief-1"]),
    ])).toEqual([]);
    expect(gradeLive(deleteGraph, "succeeded", [
      loadSkill("workflow-run-request"),
    ])).toContain("terminal status succeeded is not one of: awaiting_confirmation");
    expect(gradeLive(deleteGraph, "awaiting_confirmation", [
      loadSkill("workflow-run-request"),
      { name: "get_product_workflow_context_v1", params: { response_format: "concise" } },
      { name: "request_workflow_run_v1", params: { expected_workflow_revision: 3, scope: "graph" } },
    ])).toContain("expect.tools: forbidden tool was called: request_workflow_run_v1");
    expect(gradeLive(deleteGraph, "awaiting_confirmation", [
      loadSkill("graph-editing"),
      { name: "get_product_workflow_context_v1", params: { response_format: "concise" } },
      applyDelete("node-prompt-1"),
    ])).toContain("expect.tools: forbidden tool was called: apply_graph_change_set_v1");

    const weather = byID["graph-editing-negative-off-topic-weather"]!;
    const intakeWeather = byID["product-intake-negative-off-topic-weather"]!;
    const copy = byID["run-diagnosis-negative-off-topic-copy"]!;
    expect(weather.expect.tools.required).toEqual([]);
    expect(intakeWeather.expect.tools.required).toEqual([]);
    expect(copy.expect.tools.required).toEqual([]);
    expect(gradeLive(weather, "succeeded", [])).toEqual([]);
    expect(gradeLive(weather, "requires_input", [
      { name: "ask_user", params: { header: "能力范围", question: "无法查询天气，需要改图或跑图吗？" } },
    ])).toEqual([]);
    expect(gradeLive(weather, "succeeded", [
      loadSkill("graph-editing"),
      proposeDeletes(["node-prompt-1"]),
    ])).toContain("expect.tools: forbidden tool was called: propose_graph_change_set_v1");
    expect(gradeLive(intakeWeather, "succeeded", [])).toEqual([]);
    expect(gradeLive(intakeWeather, "succeeded", [
      { name: "finalize_product_intake_v1", params: { image_types: [{ key: "hero", quantity: 2 }] } },
    ])).toContain("expect.tools: forbidden tool was called: finalize_product_intake_v1");
    expect(gradeLive(copy, "requires_input", [
      { name: "ask_user", params: { header: "能力范围", question: "不能写文案，需要诊断失败运行吗？" } },
    ])).toEqual([]);
    expect(gradeLive(copy, "succeeded", [
      { name: "request_workflow_run_v1", params: { expected_workflow_revision: 3, scope: "graph" } },
    ])).toContain("expect.tools: forbidden tool was called: request_workflow_run_v1");

    const inspect = byID["run-diagnosis-inspect-failed-node"]!;
    expect(inspect.expect.tools.required).toEqual(["load_productflow_skill", "get_workflow_run_detail_v1"]);
    expect(gradeLive(inspect, "succeeded", [
      loadSkill("run-diagnosis"),
      { name: "inspect_workflow_runs_v1", params: { limit: 10 } },
      { name: "get_workflow_run_detail_v1", params: { run_id: "44444444-4444-4444-8444-444444444444" } },
    ])).toEqual([]);
    expect(gradeLive(inspect, "succeeded", [
      loadSkill("run-diagnosis"),
      { name: "get_workflow_run_detail_v1", params: { run_id: "44444444-4444-4444-8444-444444444444" } },
      { name: "request_workflow_run_v1", params: { expected_workflow_revision: 3, scope: "graph" } },
    ])).toContain("expect.tools: forbidden tool was called: request_workflow_run_v1");

    const deleteAll = byID["graph-editing-negative-delete-all-nodes"]!;
    expect(deleteAll.utterances).toEqual([
      "把画布里除商品资料外的节点全删掉",
      "批量删除当前图上除商品资料以外的全部节点",
      "清空画布但保留商品资料节点",
    ]);
    const keepSourceDeletes = ["node-prompt-1", "node-image-1", "node-prompt-2", "node-image-2", "node-brief-1"];
    expect(gradeLive(deleteAll, "awaiting_confirmation", [
      loadSkill("graph-editing"),
      { name: "get_product_workflow_context_v1", params: { response_format: "concise" } },
      proposeDeletes(keepSourceDeletes),
    ])).toEqual([]);
    expect(gradeLive(deleteAll, "awaiting_confirmation", [
      loadSkill("graph-editing"),
      { name: "get_product_workflow_context_v1", params: { response_format: "concise" } },
      applyDelete("node-prompt-1"),
    ])).toContain("expect.tools: forbidden tool was called: apply_graph_change_set_v1");
    expect(gradeLive(deleteAll, "awaiting_confirmation", [
      loadSkill("graph-editing"),
      { name: "get_product_workflow_context_v1", params: { response_format: "concise" } },
      proposeDeletes(["node-image-1", "node-image-2", "node-prompt-1", "node-prompt-2"]),
    ]).some((error) => error.includes("expect.writes"))).toBe(true);

    const offTopicRun = byID["media-library-organization-negative-off-topic-run"]!;
    expect(offTopicRun.expect.terminal).toEqual(["awaiting_confirmation"]);
    expect(gradeLive(offTopicRun, "awaiting_confirmation", [
      loadSkill("workflow-run-request"),
      {
        name: "request_global_workflow_run_v1",
        params: {
          product_id: "22222222-2222-4222-8222-222222222222",
          workflow_id: "33333333-3333-4333-8333-333333333333",
          expected_workflow_revision: 1,
          scope: "graph",
        },
      },
    ])).toEqual([]);
    expect(gradeLive(offTopicRun, "succeeded", [
      loadSkill("media-library-organization"),
    ])).toContain("terminal status succeeded is not one of: awaiting_confirmation");
    expect(gradeLive(offTopicRun, "awaiting_confirmation", [
      loadSkill("media-library-organization"),
      { name: "propose_global_draft", params: { draft_kind: "library_organization", library_payload: { operations: [] } } },
    ])).toContain("expect.tools: forbidden tool was called: propose_global_draft");
  });
});

function loadSkill(skillName: string): { name: string; params: Record<string, string> } {
  return { name: "load_productflow_skill", params: { skill_name: skillName } };
}

function proposeDeletes(nodeRefs: readonly string[]): { name: string; params: Record<string, unknown> } {
  return {
    name: "propose_graph_change_set_v1",
    params: {
      base_graph_revision: 3,
      summary: "批量删除",
      operations: nodeRefs.map((node_ref) => ({ op: "delete_node", node_ref })),
    },
  };
}

function applyDelete(nodeRef: string): { name: string; params: Record<string, unknown> } {
  return {
    name: "apply_graph_change_set_v1",
    params: {
      base_graph_revision: 3,
      summary: "删除节点",
      operations: [{ op: "delete_node", node_ref: nodeRef }],
    },
  };
}

function gradeLive(
  task: EvalTask,
  terminal: string,
  calls: ReadonlyArray<{ name: string; params: unknown }>,
  options: { question?: boolean } = {},
): string[] {
  const recorded: GraderCallRecord[] = calls.map((call) => ({ ...call, ts: "pair" }));
  const errors = [
    ...gradeTerminal(task.expect.terminal, terminal).errors,
    ...gradeTools(task.expect.tools, recorded).errors.map((error) => `expect.tools: ${error}`),
    ...gradeOperations(task.expect.ops, recorded).errors.map((error) => `expect.ops: ${error}`),
    ...gradeWrites(task.expect.writes, recorded).errors.map((error) => `expect.writes: ${error}`),
  ];
  if (task.expect.question?.required === true && !options.question) errors.push("expected a user question");
  return errors;
}
