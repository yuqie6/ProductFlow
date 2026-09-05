/**
 * 为一个可生成图种构造 Graph Command 操作。
 *
 * 证据类在别处是未绑定的 `image_asset` 占位。可生成类是一组、一个提示词节点和 N 个图节点。
 * 镜头运行提交一次 `scope=selection`，组内生图节点并行（受全局并发上限）。
 */

import { defaultAspectRatioForType, imageTypeFamily, isGeneratingImageType } from "../../../lib/imageTypeFamilies";
import type { AgentProductImageTypeKey, GraphChangeSet, GraphNode, GraphProjection, GraphRunSubmitInput } from "../../../lib/types";
import { graphChangeSetClientRef, snapGraphCoordinate } from "./graphLayout";

export interface CreateShotInput {
  imageTypeKey: AgentProductImageTypeKey;
  title: string;
  position: { x: number; y: number };
  graph: GraphProjection;
  selectedNodeIds: readonly string[];
}

export function shotGenerationSpec(imageTypeKey: AgentProductImageTypeKey): Record<string, unknown> {
  return {
    aspect_ratio: defaultAspectRatioForType(imageTypeKey),
  };
}

/** 一个可生成图种：分组 + 提示词 + 第一张图节点，接到共享的 source/brief/visual。 */
export function buildCreateShotOperations(input: CreateShotInput): GraphChangeSet["operations"] {
  if (!isGeneratingImageType(input.imageTypeKey)) return [];
  const x = snapGraphCoordinate(input.position.x);
  const y = snapGraphCoordinate(input.position.y);
  const groupRef = graphChangeSetClientRef("shot");
  const promptRef = graphChangeSetClientRef("prompt");
  const imageRef = graphChangeSetClientRef("image");
  const operations: GraphChangeSet["operations"] = [
    { op: "create_group", client_ref: groupRef, title: input.title, member_refs: [] },
    {
      op: "create_node",
      client_ref: promptRef,
      node_type: "image_prompt",
      title: `${input.title}提示词`,
      position_x: x,
      position_y: y,
      group_ref: groupRef,
      config: {
        image_type_key: input.imageTypeKey,
        text_settings: { policy: imageTypeFamily(input.imageTypeKey) === "infographic" ? "required" : "none", language: imageTypeFamily(input.imageTypeKey) === "infographic" ? "zh-CN" : null },
        prompt: { design_goal: input.title },
      },
    },
    {
      op: "create_node",
      client_ref: imageRef,
      node_type: "image_generation",
      title: `${input.title} 1`,
      position_x: x + 340,
      position_y: y,
      group_ref: groupRef,
      config: {
        image_type_key: input.imageTypeKey,
        generation_spec: shotGenerationSpec(input.imageTypeKey),
      },
    },
    {
      op: "connect_nodes",
      client_ref: graphChangeSetClientRef("edge"),
      source_ref: promptRef,
      target_ref: imageRef,
      order: 0,
    },
  ];
  const shared = firstNodeOfType(input.graph, "product_source");
  const visual = firstNodeOfType(input.graph, "visual_system");
  const brief = firstNodeOfType(input.graph, "creative_brief");
  if (shared) {
    operations.push(connect(shared.id, promptRef, 0));
  }
  if (brief) {
    operations.push(connect(brief.id, promptRef, 0));
  }
  if (visual) {
    operations.push(connect(visual.id, promptRef, 0));
    operations.push(connect(visual.id, imageRef, 0));
  }
  const identityRefs = selectedIdentityAssets(input.graph, input.selectedNodeIds);
  identityRefs.forEach((node, order) => {
    operations.push(connect(node.id, promptRef, order));
    operations.push(connect(node.id, imageRef, order));
  });
  return operations;
}

/** 镜头内生图节点一次 `scope=selection` 运行，使用当前提示词文稿。 */
export function shotRunRequest(
  graph: GraphProjection,
  groupId: string,
): GraphRunSubmitInput | null {
  const images = graph.nodes
    .filter((node) => node.group_id === groupId && node.node_type === "image_generation")
    .sort((left, right) => left.position_y - right.position_y || left.position_x - right.position_x || left.id.localeCompare(right.id));
  if (!images.length) return null;
  return {
    scope: "selection",
    node_ids: images.map((node) => node.id),
  };
}

function firstNodeOfType(graph: GraphProjection, nodeType: GraphNode["node_type"]): GraphNode | undefined {
  return graph.nodes.find((node) => node.node_type === nodeType);
}

function selectedIdentityAssets(graph: GraphProjection, selectedNodeIds: readonly string[]): GraphNode[] {
  const selected = new Set(selectedNodeIds);
  return graph.nodes.filter((node) => (
    selected.has(node.id)
    && node.node_type === "image_asset"
    && node.bound_asset_id
    && node.config.role !== "evidence"
  ));
}

function connect(sourceRef: string, targetRef: string, order: number): GraphChangeSet["operations"][number] {
  return {
    op: "connect_nodes",
    client_ref: graphChangeSetClientRef("edge"),
    source_ref: sourceRef,
    target_ref: targetRef,
    order,
  };
}
