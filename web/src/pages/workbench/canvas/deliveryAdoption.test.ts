import { describe, expect, it } from "vitest";

import type { DeliveryAdoptionVersion, GraphProjection } from "../../../lib/types";
import {
  adoptedAssetBySlot,
  adoptionGateMessageKey,
  buildAdoptionSlotsReplacingNode,
  evaluateAdoptionGate,
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
  it("maps adopted assets by slot key", () => {
    expect(adoptedAssetBySlot(current).get("node-hero")).toBe("old-asset");
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

  it("blocks adopt when text_qualified is false even if client wants pass", () => {
    expect(evaluateAdoptionGate({
      text_trace: { text_qualified: false },
      produce_route: { route_qualified: true },
    })).toEqual({ ok: false, code: "text_unqualified" });
    expect(buildAdoptionSlotsReplacingNode({
      graph,
      current: null,
      nodeId: "node-hero",
      sourceAssetId: "asset-1",
      qualityStatus: "pass",
      artifactPayload: {
        text_trace: { text_qualified: false },
        produce_route: { route_qualified: true },
      },
    })).toEqual({ error: "text_unqualified" });
    expect(adoptionGateMessageKey("text_unqualified")).toBe("graph.results.adoptTextUnqualified");
  });

  it("blocks adopt when route_qualified is false", () => {
    expect(evaluateAdoptionGate({
      text_trace: { text_qualified: true },
      produce_route: { route_qualified: false },
    })).toEqual({ ok: false, code: "route_unqualified" });
  });

  it("downgrades pass to unchecked when qualification metadata is missing", () => {
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
});
