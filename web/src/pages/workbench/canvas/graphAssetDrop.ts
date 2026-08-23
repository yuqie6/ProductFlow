import type { GraphChangeSet, GraphNode, GraphNodeCatalog, GraphProjection } from "../../../lib/types";
import { graphNodeHasInput, isGraphConnectionValid } from "./graphCatalog";
import { GRAPH_DUPLICATE_OFFSET, graphChangeSetClientRef, snapGraphCoordinate } from "./graphLayout";

export interface GraphAssetDropInput {
  assetIds: string[];
  position: { x: number; y: number };
  nodeId: string | null;
  groupId?: string | null;
}

export type GraphAssetDropPlan =
  | { kind: "apply"; summary: string; operations: GraphChangeSet["operations"] }
  | {
    kind: "choose_reuse";
    assetId: string;
    targetNodeId: string;
    existing: GraphNode[];
    position: { x: number; y: number };
  }
  | { kind: "rejected"; reasonKey: "graph.drop.incompatible" | "graph.drop.catalogMissing" }
  | { kind: "ignored" };

export function boundImageAssetNodes(graph: GraphProjection, assetId: string): GraphNode[] {
  return graph.nodes.filter((node) => node.node_type === "image_asset" && node.bound_asset_id === assetId);
}

export function resolveGraphAssetDrop(
  graph: GraphProjection,
  input: GraphAssetDropInput,
  catalog: GraphNodeCatalog | null | undefined,
): GraphAssetDropPlan {
  const assetIds = uniqueAssetIds(input.assetIds);
  if (!assetIds.length) return { kind: "ignored" };
  const target = input.nodeId ? graph.nodes.find((node) => node.id === input.nodeId) ?? null : null;
  if (target?.node_type === "image_asset") {
    const assetId = assetIds[0];
    if (target.bound_asset_id === assetId) return { kind: "ignored" };
    return {
      kind: "apply",
      summary: "绑定素材",
      operations: [{
        op: "update_node_config",
        node_ref: target.id,
        config: target.config,
        bound_asset_id: assetId,
      }],
    };
  }
  if (target && !catalog) {
    return { kind: "rejected", reasonKey: "graph.drop.catalogMissing" };
  }
  if (target && graphNodeHasInput(target.node_type, catalog)) {
    if (assetIds.length === 1) {
      const existing = boundImageAssetNodes(graph, assetIds[0]).filter((node) => (
        isGraphConnectionValid(graph, node.id, target.id, catalog)
      ));
      if (existing.length) {
        return {
          kind: "choose_reuse",
          assetId: assetIds[0],
          targetNodeId: target.id,
          existing,
          position: input.position,
        };
      }
    }
    return {
      kind: "apply",
      summary: "添加参考图",
      operations: buildCreateAndConnectOperations(assetIds, target.id, input.position, undefined, input.groupId),
    };
  }
  if (target) return { kind: "rejected", reasonKey: "graph.drop.incompatible" };
  return {
    kind: "apply",
    summary: "添加图片素材",
    operations: buildCreateBoundImageAssetOperations(assetIds, input.position, undefined, input.groupId),
  };
}

function createNodeGroupFields(groupId?: string | null): { group_ref: string } | Record<string, never> {
  return groupId ? { group_ref: groupId } : {};
}

export function buildCreateBoundImageAssetOperations(
  assetIds: string[],
  position: { x: number; y: number },
  titleForIndex: (index: number) => string = (index) => `素材 ${index + 1}`,
  groupId?: string | null,
): GraphChangeSet["operations"] {
  return uniqueAssetIds(assetIds).map((assetId, index) => ({
    op: "create_node",
    client_ref: graphChangeSetClientRef("node"),
    node_type: "image_asset",
    title: titleForIndex(index),
    position_x: snapGraphCoordinate(position.x + index * GRAPH_DUPLICATE_OFFSET),
    position_y: snapGraphCoordinate(position.y + index * GRAPH_DUPLICATE_OFFSET),
    config: {},
    bound_asset_id: assetId,
    ...createNodeGroupFields(groupId),
  }));
}

export function buildCreateAndConnectOperations(
  assetIds: string[],
  targetNodeId: string,
  position: { x: number; y: number },
  titleForIndex: (index: number) => string = (index) => `素材 ${index + 1}`,
  groupId?: string | null,
): GraphChangeSet["operations"] {
  const operations: GraphChangeSet["operations"] = [];
  for (const [index, assetId] of uniqueAssetIds(assetIds).entries()) {
    const nodeRef = graphChangeSetClientRef("node");
    operations.push({
      op: "create_node",
      client_ref: nodeRef,
      node_type: "image_asset",
      title: titleForIndex(index),
      position_x: snapGraphCoordinate(position.x + index * GRAPH_DUPLICATE_OFFSET),
      position_y: snapGraphCoordinate(position.y + index * GRAPH_DUPLICATE_OFFSET),
      config: {},
      bound_asset_id: assetId,
      ...createNodeGroupFields(groupId),
    });
    operations.push({
      op: "connect_nodes",
      client_ref: graphChangeSetClientRef("edge"),
      source_ref: nodeRef,
      target_ref: targetNodeId,
    });
  }
  return operations;
}

export function buildReuseConnectOperations(
  sourceNodeId: string,
  targetNodeId: string,
): GraphChangeSet["operations"] {
  return [{
    op: "connect_nodes",
    client_ref: graphChangeSetClientRef("edge"),
    source_ref: sourceNodeId,
    target_ref: targetNodeId,
  }];
}

function uniqueAssetIds(assetIds: string[]): string[] {
  return [...new Set(assetIds.map((item) => item.trim()).filter(Boolean))];
}
