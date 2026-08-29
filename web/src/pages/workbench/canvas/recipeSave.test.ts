import { describe, expect, it } from "vitest";

import type { GraphProjection } from "../../../lib/types";
import { resolveRecipeSaveRequest, uniqueSelectedGroupId } from "./recipeSave";

function graph(): GraphProjection {
  return {
    id: "g1",
    product_id: "p1",
    title: "夏季主图",
    schema_version: 3,
    revision: 4,
    last_operation_group_id: null,
    can_undo: true,
    can_redo: false,
    nodes: [
      {
        id: "prompt",
        node_type: "prompt_generation",
        title: "主图提示词",
        position_x: 0,
        position_y: 0,
        config: {},
        bound_asset_id: null,
        group_id: "group-1",
        preview_asset_id: null,
        config_status: "ready",
        unused: false,
        incoming: [],
        outgoing: [],
      },
      {
        id: "image",
        node_type: "image_generation",
        title: "主图 1",
        position_x: 240,
        position_y: 0,
        config: {},
        bound_asset_id: null,
        group_id: "group-1",
        preview_asset_id: null,
        config_status: "ready",
        unused: false,
        incoming: [],
        outgoing: [],
      },
      {
        id: "brief",
        node_type: "creative_brief",
        title: "创作要求",
        position_x: 0,
        position_y: 200,
        config: {},
        bound_asset_id: null,
        group_id: null,
        preview_asset_id: null,
        config_status: "ready",
        unused: false,
        incoming: [],
        outgoing: [],
      },
    ],
    edges: [],
    groups: [{ id: "group-1", title: "主图组", member_ids: ["prompt", "image"] }],
  };
}

describe("resolveRecipeSaveRequest", () => {
  it("saves the full graph without product identity fields", () => {
    expect(resolveRecipeSaveRequest(graph(), [], null, "workflow")).toEqual({
      source_type: "workflow",
      expected_graph_revision: 4,
    });
  });

  it("prefers the entered group, then a unique selected group", () => {
    const live = graph();
    expect(resolveRecipeSaveRequest(live, [], "group-1", "group")).toEqual({
      source_type: "group",
      group_id: "group-1",
      expected_graph_revision: 4,
    });
    expect(uniqueSelectedGroupId(live, ["prompt", "image"])).toBe("group-1");
    expect(resolveRecipeSaveRequest(live, ["prompt", "image"], null, "group")?.group_id).toBe("group-1");
    expect(resolveRecipeSaveRequest(live, ["brief"], null, "group")).toBeNull();
  });

  it("saves an explicit or selected node set", () => {
    const live = graph();
    expect(resolveRecipeSaveRequest(live, ["brief"], null, "selection")).toEqual({
      source_type: "selection",
      node_ids: ["brief"],
      expected_graph_revision: 4,
    });
    expect(resolveRecipeSaveRequest(live, [], null, "selection", ["prompt"])).toEqual({
      source_type: "selection",
      node_ids: ["prompt"],
      expected_graph_revision: 4,
    });
    expect(resolveRecipeSaveRequest(live, [], null, "selection")).toBeNull();
  });
});
