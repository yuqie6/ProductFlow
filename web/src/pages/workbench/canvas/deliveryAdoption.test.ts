import { describe, expect, it } from "vitest";

import type { DeliveryAdoptionVersion, GraphProjection } from "../../../lib/types";
import {
  adoptedSlotByNodeId,
  adoptionQualityIssueMessageKey,
  buildAdoptionSlotsReplacingNode,
  assessAdoptionQuality,
  assessDeliveryAdoptionFreshness,
} from "./deliveryAdoption";

const graph = {
  id: "graph-1",
  revision: 3,
  nodes: [
    {
      id: "node-hero",
      node_type: "image_generation",
      title: "主图",
      current_artifact_payload: {
        text_trace: { text_qualified: true },
        produce_route: { route: "subject_preserve", route_qualified: true },
      },
      config: {
        image_type_key: "hero",
        delivery_spec: {
          width: 800,
          height: 800,
          format: "png",
          fit: "contain",
          max_byte_size: null,
          background_color: null,
          crop_anchor: null,
        },
      },
    },
    {
      id: "node-detail",
      node_type: "image_generation",
      title: "细节",
      config: { image_type_key: "detail" },
    },
  ],
} as unknown as GraphProjection;

const current: DeliveryAdoptionVersion = {
  id: "v1",
  product_id: "p1",
  version: 1,
  is_current: true,
  graph_id: "graph-1",
  graph_revision: 2,
  fact_set_version_id: null,
  visual_system_version_id: null,
  notes: null,
  created_at: "2026-09-07T00:00:00Z",
  slots: [
    {
      id: "s1",
      slot_key: "node-hero",
      sort_order: 0,
      image_type_key: "hero",
      source_asset_id: "old-asset",
      source_node_id: "node-hero",
      delivery_spec: {
        width: 800,
        height: 800,
        format: "png",
        fit: "contain",
      },
      delivery_spec_hash: "a".repeat(64),
      quality_status: "pass",
      quality_detail: null,
      text_overflow: false,
      qualified: true,
    },
  ],
};

describe("deliveryAdoption", () => {
  it("maps adopted assets by source node", () => {
    expect(adoptedSlotByNodeId(current).get("node-hero")).toEqual(current.slots[0]);
  });

  it("replaces one node slot and renumbers sort order without dropping siblings", () => {
    const built = buildAdoptionSlotsReplacingNode({
      graph,
      current,
      nodeId: "node-hero",
      sourceAssetId: "new-asset",
      qualityStatus: "pass",
    });
    expect(built).toEqual([
      expect.objectContaining({
        slot_key: "node-hero",
        source_asset_id: "new-asset",
        sort_order: 0,
        quality_status: "pass",
      }),
    ]);
  });

  it("requires delivery_spec on the generation node", () => {
    expect(buildAdoptionSlotsReplacingNode({
      graph,
      current: null,
      nodeId: "node-detail",
      sourceAssetId: "asset-1",
    })).toEqual({ error: "missing_delivery_spec" });
  });

  it("reports a failed text check while keeping adoption available", () => {
    expect(assessAdoptionQuality({
      text_trace: { text_qualified: false },
      produce_route: { route_qualified: true },
    })).toEqual({ status: "fail", issueCodes: ["text_unqualified"] });
    expect(adoptionQualityIssueMessageKey("text_unqualified")).toBe("graph.results.qualityTextFailed");
  });

  it("reports a failed route check while keeping adoption available", () => {
    expect(assessAdoptionQuality({
      text_trace: { text_qualified: true },
      produce_route: { route_qualified: false },
    })).toEqual({ status: "fail", issueCodes: ["route_unqualified"] });
  });

  it("reports missing qualification metadata as unchecked and does not claim pass", () => {
    expect(assessAdoptionQuality({})).toEqual({ status: "unchecked", issueCodes: ["quality_unchecked"] });
    const built = buildAdoptionSlotsReplacingNode({
      graph,
      current: null,
      nodeId: "node-hero",
      sourceAssetId: "asset-1",
      qualityStatus: "pass",
      artifactPayload: {},
    });
    expect(built).toEqual([
      expect.objectContaining({
        quality_status: "unchecked",
        source_asset_id: "asset-1",
      }),
    ]);
  });

  it("uses pass only when both finite checks are explicitly true", () => {
    const built = buildAdoptionSlotsReplacingNode({
      graph,
      current: null,
      nodeId: "node-hero",
      sourceAssetId: "asset-1",
    });
    expect(built).toEqual([
      expect.objectContaining({ quality_status: "pass" }),
    ]);
  });

  it("retains a sibling fail, detail, and overflow flag when replacing another slot", () => {
    const retained = {
      ...current,
      slots: [
        ...current.slots,
        {
          ...current.slots[0],
          id: "s2",
          slot_key: "node-other",
          source_node_id: "node-other",
          sort_order: 1,
          quality_status: "fail" as const,
          quality_detail: "图位文字溢出",
          text_overflow: true,
        },
      ],
    };
    const built = buildAdoptionSlotsReplacingNode({
      graph,
      current: retained,
      nodeId: "node-hero",
      sourceAssetId: "asset-new",
    });
    expect(built).toEqual(expect.arrayContaining([
      expect.objectContaining({
        slot_key: "node-other",
          source_node_id: "node-other",
        quality_status: "fail",
        quality_detail: "图位文字溢出",
        text_overflow: true,
      }),
    ]));
  });
});


describe("delivery snapshot identity", () => {
  const assess = (asset: string | null, version = current, projection = graph) => assessDeliveryAdoptionFreshness({
    graph: projection, current: version, currentAssetByNodeId: new Map([["node-hero", asset]]),
  });
  it("ignores revision and layout changes when the asset is unchanged", () => {
    expect(assess("old-asset", current, { ...graph, revision: 99, nodes: graph.nodes.map(node => ({ ...node, position_x: 900 })) })).toEqual({ isStale: false, issues: [] });
  });
  it("reports changed and missing current assets", () => {
    expect(assess("new-asset").issues).toEqual([{ code: "asset_changed", nodeId: "node-hero" }]);
    expect(assess(null).issues).toEqual([{ code: "missing_current_asset", nodeId: "node-hero" }]);
  });
  it("reports deleted source nodes and other graphs", () => {
    expect(assess("old-asset", current, { ...graph, nodes: [] }).issues).toEqual([{ code: "deleted_node", nodeId: "node-hero" }]);
    expect(assess("old-asset", { ...current, graph_id: "other" }).issues).toEqual([{ code: "different_graph", nodeId: null }]);
  });
  it("uses source node identity rather than slot label", () => {
    expect(assess("old-asset", { ...current, slots: [{ ...current.slots[0], slot_key: "hero" }] }).isStale).toBe(false);
  });
  it("clears the difference only after the replacement asset is adopted", () => {
    expect(assess("new-asset").isStale).toBe(true);
    expect(assess("new-asset", { ...current, slots: [{ ...current.slots[0], source_asset_id: "new-asset" }] }).isStale).toBe(false);
  });
});

it("replaces a named delivery slot for the same source node without retaining a stale duplicate", () => {
  const named = { ...current, slots: [{ ...current.slots[0], slot_key: "hero" }] };
  expect(adoptedSlotByNodeId(named).get("node-hero")).toEqual(named.slots[0]);
  const slots = buildAdoptionSlotsReplacingNode({ graph, current: named, nodeId: "node-hero", sourceAssetId: "new-asset" });
  expect(slots).toHaveLength(1);
  expect(slots).toEqual([expect.objectContaining({ source_asset_id: "new-asset" })]);
});

it("distinguishes an unlinked named slot from a deleted source node", () => {
  const assess = (slotKey: string) => assessDeliveryAdoptionFreshness({
    graph, current: { ...current, slots: [{ ...current.slots[0], source_node_id: null, slot_key: slotKey }] },
    currentAssetByNodeId: new Map([["node-hero", "old-asset"]]),
  });
  expect(assess("custom-hero").issues).toEqual([{ code: "unlinked_node", nodeId: null }]);
  expect(assess("node-hero").issues).toEqual([]);
});
