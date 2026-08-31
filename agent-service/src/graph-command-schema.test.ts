import { describe, expect, it } from "vitest";
import { Value } from "typebox/value";
import {
  GRAPH_COMMAND_OPS,
  applyGraphChangeSetParameters,
  proposeGraphChangeSetParameters,
} from "./graph-command-schema.js";

const createAndConnect = {
  base_graph_revision: 1,
  summary: "加节点并连线",
  operations: [
    {
      op: "create_node",
      client_ref: "n1",
      node_type: "image_prompt",
      title: "提示词",
    },
    {
      op: "connect_nodes",
      client_ref: "e1",
      source_ref: "src",
      target_ref: "n1",
    },
  ],
};

describe("Graph Command tool schema", () => {
  it("lists the closed Graph Command op table", () => {
    expect([...GRAPH_COMMAND_OPS]).toEqual([
      "create_node",
      "update_node_config",
      "rename_node",
      "delete_node",
      "connect_nodes",
      "disconnect_edge",
      "move_nodes",
      "create_group",
      "move_nodes_to_group",
      "rename_group",
      "dissolve_group",
      "reorder_edges",
    ]);
  });

  it("accepts create_node and connect_nodes on propose and rejects invented verbs", () => {
    expect(Value.Check(proposeGraphChangeSetParameters, createAndConnect)).toBe(true);
    expect(
      Value.Check(applyGraphChangeSetParameters, {
        ...createAndConnect,
        operations: [createAndConnect.operations[0]],
      }),
    ).toBe(true);
    expect(
      Value.Check(proposeGraphChangeSetParameters, {
        ...createAndConnect,
        operations: [{ op: "add_node", title: "猜的节点" }],
      }),
    ).toBe(false);
    expect(
      Value.Check(proposeGraphChangeSetParameters, {
        ...createAndConnect,
        operations: [{ op: "connect", source_ref: "a", target_ref: "b" }],
      }),
    ).toBe(false);
    expect(Value.Check(applyGraphChangeSetParameters, createAndConnect)).toBe(false);
  });

  it("does not advertise add_node in the JSON Schema", () => {
    const encoded = JSON.stringify(proposeGraphChangeSetParameters);
    expect(encoded).toContain("create_node");
    expect(encoded).toContain("connect_nodes");
    expect(encoded).not.toContain("add_node");
    expect(encoded).not.toMatch(/"connect"/);
  });
});
