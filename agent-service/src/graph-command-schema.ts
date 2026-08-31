/**
 * Graph Command 的 Pi tool JSON Schema。
 * op 表必须与 go/internal/graph GraphCommandOpNames / unmarshalOperation 对齐。
 */

import { Type } from "typebox";

export const GRAPH_COMMAND_OPS = [
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
] as const;

const ref = Type.String({ minLength: 1, maxLength: 80 });
const title = Type.String({ minLength: 1, maxLength: 255 });
const nodeType = Type.Union([
  Type.Literal("product_source"),
  Type.Literal("image_asset"),
  Type.Literal("creative_brief"),
  Type.Literal("visual_system"),
  Type.Literal("image_prompt"),
  Type.Literal("image_generation"),
]);

const graphCommandOperation = Type.Union([
  Type.Object(
    {
      op: Type.Literal("create_node"),
      client_ref: ref,
      node_type: nodeType,
      title,
      position_x: Type.Optional(Type.Integer()),
      position_y: Type.Optional(Type.Integer()),
      config: Type.Optional(Type.Object({}, { additionalProperties: true })),
      bound_asset_id: Type.Optional(ref),
      group_ref: Type.Optional(ref),
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("update_node_config"),
      node_ref: ref,
      config: Type.Object({}, { additionalProperties: true }),
      bound_asset_id: Type.Optional(Type.Union([ref, Type.Null()])),
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("rename_node"),
      node_ref: ref,
      title,
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("delete_node"),
      node_ref: ref,
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("connect_nodes"),
      client_ref: ref,
      source_ref: ref,
      target_ref: ref,
      order: Type.Optional(Type.Integer({ minimum: 0 })),
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("disconnect_edge"),
      edge_ref: ref,
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("move_nodes"),
      nodes: Type.Array(Type.Tuple([ref, Type.Integer(), Type.Integer()]), { minItems: 1 }),
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("create_group"),
      client_ref: ref,
      title,
      member_refs: Type.Optional(Type.Array(ref)),
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("move_nodes_to_group"),
      node_refs: Type.Array(ref, { minItems: 1 }),
      group_ref: Type.Union([ref, Type.Null()]),
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("rename_group"),
      group_ref: ref,
      title,
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("dissolve_group"),
      group_ref: ref,
    },
    { additionalProperties: false },
  ),
  Type.Object(
    {
      op: Type.Literal("reorder_edges"),
      node_ref: ref,
      role: Type.String({ minLength: 1, maxLength: 80 }),
      edge_refs: Type.Array(ref, { minItems: 1 }),
    },
    { additionalProperties: false },
  ),
]);

export const applyGraphChangeSetParameters = Type.Object(
  {
    base_graph_revision: Type.Integer({ minimum: 0 }),
    summary: Type.String({ minLength: 1, maxLength: 500 }),
    operations: Type.Array(graphCommandOperation, { minItems: 1, maxItems: 1 }),
  },
  { additionalProperties: false },
);

export const proposeGraphChangeSetParameters = Type.Object(
  {
    base_graph_revision: Type.Integer({ minimum: 0 }),
    summary: Type.String({ minLength: 1, maxLength: 500 }),
    operations: Type.Array(graphCommandOperation, { minItems: 1, maxItems: 128 }),
  },
  { additionalProperties: false },
);
