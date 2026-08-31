import { describe, expect, it } from "vitest";
import { loadSkillCatalog } from "../src/skills.js";
import {
  illegalLibraryOrganizationDraft,
  sampleLibraryMoveDraft,
  sampleLibraryOrganizationDraft,
  SKILL_EVAL_FIXTURES,
  validateSkillEvalFixture,
} from "./fixtures.js";
import { assertUtteranceAlignment, formatEvalReport, paramsMatchSchema, runScriptedSkillEvals } from "./harness.js";
import { checkJSONSchema, loadGlobalDraftSchema } from "./json-schema.js";

describe("ProductFlow scripted skill evals", () => {
  it("covers at least three fixtures per skill, includes a repair loop, and all pass", async () => {
    const counts = new Map<string, number>();
    for (const fixture of SKILL_EVAL_FIXTURES) {
      counts.set(fixture.skillName, (counts.get(fixture.skillName) ?? 0) + 1);
    }
    for (const [skillName, count] of counts) {
      expect(count, skillName).toBeGreaterThanOrEqual(3);
    }
    expect(SKILL_EVAL_FIXTURES.filter((fixture) => fixture.repair).length).toBeGreaterThanOrEqual(2);
    const report = await runScriptedSkillEvals();
    expect(report.ok, report.rows.filter((row) => !row.ok).map((row) => `${row.id}: ${row.error}`).join("; ")).toBe(true);
  });

  it("keeps fixtures aligned with the catalog, page_context, and progressive disclosure order", async () => {
    const catalog = await loadSkillCatalog();
    for (const fixture of SKILL_EVAL_FIXTURES) {
      expect(() => validateSkillEvalFixture(fixture, catalog)).not.toThrow();
      expect(catalog.names).toContain(fixture.skillName);
      expect(catalog.prompt).toContain(`<name>${fixture.skillName}</name>`);
      expect(fixture.scriptedCalls[0]?.name).toBe("load_productflow_skill");
      expect(fixture.pageContext.page_type.length).toBeGreaterThan(0);
    }
  });

  it("rejects fixtures whose domain tools are missing from guards_tools", async () => {
    const catalog = await loadSkillCatalog();
    const base = SKILL_EVAL_FIXTURES.find((item) => item.skillName === "product-intake");
    expect(base).toBeDefined();
    expect(() =>
      validateSkillEvalFixture(
        {
          ...base!,
          scriptedCalls: [
            { name: "load_productflow_skill", params: { skill_name: "product-intake" } },
            { name: "propose_global_draft", params: sampleLibraryOrganizationDraft() },
          ],
        },
        catalog,
      ),
    ).toThrow(/propose_global_draft is not in that skill's guards_tools/);
  });

  it("rejects an illegal payload and accepts a repaired legal payload within two attempts", () => {
    const repairs = SKILL_EVAL_FIXTURES.filter((item) => item.repair);
    expect(repairs.length).toBeGreaterThanOrEqual(2);
    for (const fixture of repairs) {
      const repair = fixture.repair!;
      expect(paramsMatchSchema(repair.toolName, repair.illegalParams), repair.toolName).toBe(false);
      expect(paramsMatchSchema(repair.toolName, repair.repairedParams), repair.toolName).toBe(true);
    }
  });

  it("rejects a scripted finalize whose selection does not match the utterance", () => {
    const fixture = SKILL_EVAL_FIXTURES.find((item) => item.userRequest.includes("主图2张"));
    expect(fixture).toBeDefined();
    expect(() =>
      assertUtteranceAlignment(fixture!, [
        { name: "load_productflow_skill", params: { skill_name: "product-intake" } },
        {
          name: "finalize_product_intake_v1",
          params: {
            selection: { schema_version: 1, image_types: [{ key: "hero", quantity: 1, order: 0 }] },
            reference_asset_ids: ["11111111-1111-4111-8111-111111111111"],
          },
        },
      ]),
    ).toThrow(/主图2张/);
  });

  it("validates propose_global_draft against the live global draft schema", () => {
    const schema = loadGlobalDraftSchema();
    expect(checkJSONSchema(schema, sampleLibraryOrganizationDraft())).toBe(true);
    expect(checkJSONSchema(schema, sampleLibraryMoveDraft())).toBe(true);
    expect(checkJSONSchema(schema, illegalLibraryOrganizationDraft())).toBe(false);
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
