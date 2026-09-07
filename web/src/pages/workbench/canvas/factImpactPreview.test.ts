import { describe, expect, it } from "vitest";
import {
  defaultSelectedImpactNodeIds,
  impactNodeLabel,
  shouldShowFactsImpactPreview,
  toggleImpactNodeSelection,
} from "./factImpactPreview";
import type { FactsImpactPreviewResponse } from "../../../lib/types";

const preview: FactsImpactPreviewResponse = {
  product_id: "p1",
  changed_fact_keys: ["capacity"],
  nodes: [
    {
      node_id: "spec",
      node_type: "image_prompt",
      title: "规格",
      image_type_key: "specifications",
      depends_on_changed_keys: ["capacity"],
      default_selected: true,
      has_artifact: true,
      reason: "依赖",
    },
    {
      node_id: "scene",
      node_type: "image_generation",
      title: "场景",
      image_type_key: "lifestyle",
      depends_on_changed_keys: [],
      default_selected: false,
      has_artifact: true,
      reason: "不更新",
    },
  ],
  default_update_node_ids: ["spec"],
  explanation: "说明",
  proposed_fact_count: 1,
};

describe("factImpactPreview", () => {
  it("defaults to nodes that use changed keys", () => {
    expect(defaultSelectedImpactNodeIds(preview)).toEqual(["spec"]);
    expect(shouldShowFactsImpactPreview(preview)).toBe(true);
  });

  it("toggles selection without forcing full graph", () => {
    expect(toggleImpactNodeSelection(["spec"], "scene", true).sort()).toEqual(["scene", "spec"]);
    expect(toggleImpactNodeSelection(["spec"], "spec", false)).toEqual([]);
  });

  it("labels image slots with type key", () => {
    expect(impactNodeLabel(preview.nodes[0])).toContain("specifications");
  });

  it("hides preview when no connected nodes", () => {
    expect(shouldShowFactsImpactPreview({ ...preview, nodes: [] })).toBe(false);
  });

  it("survives confirm-only impact preview with JSON-null slices", () => {
    // Go encodes empty ChangedFactKeys via append([]string(nil), …) as null.
    // Confirm「确认事实」then save often only flips status, so keys are unchanged.
    const wire = {
      ...preview,
      changed_fact_keys: null,
      nodes: null,
      default_update_node_ids: null,
    } as unknown as FactsImpactPreviewResponse;

    expect(() => shouldShowFactsImpactPreview(wire)).not.toThrow();
    expect(shouldShowFactsImpactPreview(wire)).toBe(false);
    expect(() => defaultSelectedImpactNodeIds(wire)).not.toThrow();
    expect(defaultSelectedImpactNodeIds(wire)).toEqual([]);
  });
});
