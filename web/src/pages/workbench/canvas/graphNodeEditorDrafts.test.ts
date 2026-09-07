import { describe, expect, it } from "vitest";

import type { GraphNode } from "../../../lib/types";
import {
  confirmProductFactRow,
  graphProductSourceConfig,
  graphProductSourceDraft,
  productFactsDraft,
  productFactsPayload,
  validateProductFactsDraft,
} from "./graphNodeEditorDrafts";

function node(partial: Partial<GraphNode> & Pick<GraphNode, "id" | "node_type">): GraphNode {
  return {
    title: partial.title ?? partial.id,
    position_x: 0,
    position_y: 0,
    config: {},
    bound_asset_id: null,
    group_id: null,
    preview_asset_id: null,
    config_status: "incomplete",
    unused: false,
    incoming: [],
    outgoing: [],
    ...partial,
  };
}

describe("graph node inspector drafts", () => {
  it("keeps an unbound product source explicit and preserves a bound fact-set id", () => {
    const unbound = node({ id: "source", node_type: "product_source" });
    expect(graphProductSourceDraft(unbound)).toMatchObject({
      source_product_id: null,
      fact_set_version_id: null,
    });
    const bound = node({
      id: "source",
      node_type: "product_source",
      config: { source_product_id: "product-2", fact_set_version_id: "facts-2" },
    });
    const draft = graphProductSourceDraft(bound);
    expect(graphProductSourceConfig(bound, { ...draft, source_product_id: "product-3", fact_set_version_id: null })).toEqual({
      source_product_id: "product-3",
      fact_set_version_id: null,
    });
    const withExtra = node({
      id: "source",
      node_type: "product_source",
      config: { source_product_id: "product-2", fact_set_version_id: "facts-2", leftover: true },
    });
    expect(graphProductSourceConfig(withExtra, graphProductSourceDraft(withExtra))).toEqual({
      source_product_id: "product-2",
      fact_set_version_id: "facts-2",
    });
  });

  it("rejects empty and duplicate generic fact rows", () => {
    const product = {
      id: "product-1",
      name: "商品",
      category: null,
      price: null,
      source_note: null,
      cover_image_asset_id: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const draft = productFactsDraft(product, {
      id: "facts-1",
      version: 1,
      facts: [{ key: "material", value: "steel" }],
    });
    expect(validateProductFactsDraft(draft)).toBeNull();
    const original = { key: "material", value: "steel" };
    expect(validateProductFactsDraft({ ...draft, facts: [{ id: "a", key: "", value: "steel", original }] })).toBe("empty_key");
    expect(validateProductFactsDraft({ ...draft, facts: [
      { id: "a", key: "material", value: "steel", original },
      { id: "b", key: " MATERIAL ", value: "iron", original: { key: "material", value: "iron" } },
    ] })).toBe("duplicate_key");
    expect(validateProductFactsDraft({ ...draft, facts: [{ id: "a", key: "material", value: "", original }] })).toBe("empty_value");
  });

  it("preserves structured fact values and provenance when a row is unchanged", () => {
    const product = {
      id: "product-1",
      name: "商品",
      category: null,
      price: null,
      source_note: null,
      cover_image_asset_id: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const draft = productFactsDraft(product, {
      id: "facts-1",
      version: 1,
      facts: [{
        key: "dimensions",
        value: [10, 20],
        source_type: "image_observation",
        status: "observed",
        requires_confirmation: true,
        evidence_asset_ids: ["asset-1"],
      }],
    });
    expect(productFactsPayload(draft)).toEqual([{
      key: "dimensions",
      value: [10, 20],
      source_type: "image_observation",
      status: "observed",
      requires_confirmation: true,
      evidence_asset_ids: ["asset-1"],
      layer: "performance",
    }]);
  });

  it("confirms pending facts as user-owned performance rows", () => {
    const confirmed = confirmProductFactRow({
      id: "a",
      key: "material",
      value: "不锈钢",
      original: {
        key: "material",
        value: "不锈钢",
        source_type: "image_observation",
        status: "observed",
        requires_confirmation: true,
        layer: "performance",
      },
    });
    expect(confirmed.original).toMatchObject({
      source_type: "user",
      status: "confirmed",
      requires_confirmation: false,
      layer: "performance",
      conflicts: [],
    });
  });
});
