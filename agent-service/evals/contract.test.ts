import { describe, expect, it } from "vitest";

import { loadSkillCatalog } from "../src/skills.js";
import { assertTaskExpectations, formatEvalReport, paramsMatchSchema, runScriptedSkillEvals } from "./harness.js";
import { loadEvalTaskSet, validateEvalTask } from "./loader.js";
import { checkJSONSchema, loadGlobalDraftSchema } from "./json-schema.js";
import { collectCoverage } from "./report.js";
import { evalJSONSchemas } from "./schema.js";

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
});
