import { describe, expect, it } from "vitest";
import { Type } from "typebox";

import {
  assertToolManifestCoverage,
  expectedToolNamesForScope,
  LIVE_GRAPH_TOOL_NAMES,
  TOOL_EFFECTS,
  TOOL_MANIFEST,
  canonicalJSON,
  resolvedToolContractVersion,
  TOOL_MANIFEST_VERSION,
  TOOL_PARAMETER_SCHEMAS,
  TOOL_STEP_KINDS,
  toolKind,
  toolParameters,
  toolRecoveryPolicy,
  validateToolResultMeta,
} from "./tool-manifest.js";

describe("ProductFlow tool manifest", () => {
  it("has unique names, bounded metadata, and a non-empty schema for every tool", () => {
    const names = TOOL_MANIFEST.map((entry) => entry.name);
    expect(new Set(names).size).toBe(names.length);
    expect(TOOL_MANIFEST.length).toBeGreaterThan(0);
    expect(TOOL_MANIFEST.every((entry) => entry.version >= 1)).toBe(true);
    expect(TOOL_MANIFEST.every((entry) => (TOOL_EFFECTS as readonly string[]).includes(entry.effect))).toBe(true);
    expect(TOOL_MANIFEST.every((entry) => entry.truncation_bytes > 0 && entry.input_schema.type === "object")).toBe(true);
    expect(new Set(TOOL_STEP_KINDS).size).toBe(TOOL_STEP_KINDS.length);
    expect(TOOL_MANIFEST_VERSION).toMatch(/^[a-f0-9]{64}$/u);
    expect(resolvedToolContractVersion({})).toMatch(/^[a-f0-9]{64}$/u);
    expect(resolvedToolContractVersion({})).not.toBe(TOOL_MANIFEST_VERSION);
    expect(resolvedToolContractVersion({ type: "object" })).not.toBe(resolvedToolContractVersion({}));
    expect(canonicalJSON({ z: 1, a: { b: 2, "<": "x&y" } })).toBe('{"a":{"<":"x&y","b":2},"z":1}');
    expect(Object.keys(TOOL_PARAMETER_SCHEMAS).sort()).toEqual([...names].sort());
    expect(() =>
      validateToolResultMeta("ask_user", { schema_version: 1, kind: "ask_question", unexpected: true }),
    ).toThrow("result metadata");
    expect(() =>
      validateToolResultMeta("ask_user", { schema_version: 1, kind: "ask_question", question_id: "question_1" }),
    ).not.toThrow();
  });

  it("declares list_product_image_assets_v2 with after, not cursor", () => {
    const schema = toolParameters("list_product_image_assets_v2");
    expect(JSON.stringify(schema)).toContain("\"after\"");
    expect(JSON.stringify(schema)).not.toContain("\"cursor\"");
    expect(JSON.stringify(schema)).toContain("\"directory_key\"");
  });

  it("keeps product and global workflow run requests as distinct schemas", () => {
    const product = JSON.stringify(toolParameters("request_workflow_run_v1"));
    const global = JSON.stringify(toolParameters("request_global_workflow_run_v1"));
    expect(product).not.toContain("\"product_id\"");
    expect(global).toContain("\"product_id\"");
    expect(global).toContain("\"workflow_id\"");
    expect(product).toContain("\"document_action\"");
    expect(product).toContain("\"rewrite\"");
    expect(product).toContain("\"force\"");
  });

  it("marks propose_global_draft as a contract draft schema", () => {
    const entry = TOOL_MANIFEST.find((item) => item.name === "propose_global_draft");
    expect(entry?.input_schema_source).toBe("contract:draft_schema");
    expect(() => toolParameters("propose_global_draft", Type.Object({ draft_kind: Type.Literal("library_organization") }))).not.toThrow();
  });

  it("covers registrations in both directions and rejects unknown names", () => {
    const registered = TOOL_MANIFEST.filter((entry) => entry.scope !== "synthetic").map((entry) => entry.name);
    expect(() => assertToolManifestCoverage(registered)).not.toThrow();
    expect(() => assertToolManifestCoverage([TOOL_MANIFEST[0].name])).toThrow("not registered");
    expect(() => assertToolManifestCoverage(["invented_tool"])).toThrow("missing from manifest");
    expect(() => toolKind("invented_tool")).toThrow("Unknown ProductFlow tool manifest entry");
  });

  it("omits live-graph tools from product scope until a live graph exists", () => {
    const withoutGraph = expectedToolNamesForScope("product_workflow", false);
    const withGraph = expectedToolNamesForScope("product_workflow", true);
    for (const name of LIVE_GRAPH_TOOL_NAMES) {
      expect(withoutGraph).not.toContain(name);
      expect(withGraph).toContain(name);
    }
    expect(withoutGraph).toContain("get_workflow_run_detail_v1");
    expect(withGraph).toContain("finalize_product_intake_v1");
    expect(expectedToolNamesForScope("global", false)).toContain("request_global_workflow_run_v1");
    expect(expectedToolNamesForScope("global", false)).not.toContain("request_workflow_run_v1");
  });

  it("puts recovery_policy on every manifest entry and matches the C-01 table", () => {
    const retry = new Set([
      "request_workflow_run_v1",
      "request_global_workflow_run_v1",
      "finalize_product_intake_v1",
      "create_product_workspace_v1",
      "apply_graph_change_set_v1",
      "propose_graph_change_set_v1",
      "discard_workflow_proposal_v1",
      "cancel_workflow_run_v1",
    ]);
    expect(TOOL_MANIFEST.map((entry) => entry.name).sort()).toEqual(Object.keys(TOOL_PARAMETER_SCHEMAS).sort());
    for (const entry of TOOL_MANIFEST) {
      const expected = retry.has(entry.name) ? "reconcile_then_retry" : "none";
      expect({ name: entry.name, recovery_policy: entry.recovery_policy }).toEqual({
        name: entry.name,
        recovery_policy: expected,
      });
      expect(toolRecoveryPolicy(entry.name)).toBe(expected);
    }
    expect(retry.size).toBe(8);
  });

  it("assigns distinct ui_kind values for intake, discard, cancel, and canvas focus", () => {
    const byName = new Map(TOOL_MANIFEST.map((entry) => [entry.name, entry]));
    expect(byName.get("finalize_product_intake_v1")?.ui_kind).toBe("expand_intake");
    expect(byName.get("discard_workflow_proposal_v1")?.ui_kind).toBe("discard_proposal");
    expect(byName.get("cancel_workflow_run_v1")?.ui_kind).toBe("cancel_run");
    expect(byName.get("focus_canvas_items_v1")?.effect).toBe("ui_effect");
    expect(TOOL_STEP_KINDS).toEqual(expect.arrayContaining([
      "expand_intake",
      "discard_proposal",
      "cancel_run",
      "focus_canvas",
    ]));
  });

  it("rejects undeclared result_meta fields", () => {
    expect(() =>
      validateToolResultMeta("ask_user", {
        schema_version: 1,
        kind: "ask_question",
        unexpected: true,
      }),
    ).toThrow("Invalid ProductFlow result metadata");
    expect(() =>
      validateToolResultMeta("ask_user", {
        schema_version: 1,
        kind: "ask_question",
        question_id: "question_1",
      }),
    ).not.toThrow();
    expect(() =>
      validateToolResultMeta("ask_user", {
        schema_version: 1,
        kind: "ask_question",
        run_id: "run_1",
      }),
    ).toThrow("Invalid ProductFlow result metadata");
    expect(() =>
      validateToolResultMeta("cancel_workflow_run_v1", {
        schema_version: 1,
        kind: "cancel_run",
        run_id: "run_1",
      }),
    ).not.toThrow();
  });
});
