/**
 * 为一个可生成图种构造 Graph Command 操作。
 *
 * 证据类在别处是未绑定的 `image_asset` 占位。可生成类是一组、一个提示词节点和 N 个图节点。
 * 第一张图用 `to_node` 跑，让上游内容节点只执行一次。
 */

import { defaultAspectRatioForType, imageTypeFamily, isGeneratingImageType } from "../../../lib/imageTypeFamilies";
import type { AgentProductImageTypeKey, GraphChangeSet, GraphNode, GraphProjection, GraphRunScope } from "../../../lib/types";
import { defaultGraphNodeConfig, graphChangeSetClientRef, snapGraphCoordinate } from "./graphLayout";
import { LIVE_RUN_STATUSES } from "./graphRunDisplay";

export interface CreateShotInput {
  imageTypeKey: AgentProductImageTypeKey;
  title: string;
  position: { x: number; y: number };
  graph: GraphProjection;
  selectedNodeIds: readonly string[];
}

export function shotGenerationSpec(imageTypeKey: AgentProductImageTypeKey): Record<string, unknown> {
  const infographic = imageTypeFamily(imageTypeKey) === "infographic";
  return {
    aspect_ratio: defaultAspectRatioForType(imageTypeKey),
    resolution_tier: "high",
    quality_intent: "high",
    reference_fidelity: "high",
    background_intent: "auto",
    text_policy: infographic ? "required" : "none",
    ...(infographic ? { text_language: "zh-CN" } : {}),
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
      node_type: "prompt_generation",
      title: `${input.title}提示词`,
      position_x: x,
      position_y: y,
      group_ref: groupRef,
      config: {
        image_type_key: input.imageTypeKey,
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
        ...defaultGraphNodeConfig("image_generation"),
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

/** 组内第一张图用 `to_node` 跑，让上游提示词/brief 只执行一次。 */
export function shotRunRequests(
  graph: GraphProjection,
  groupId: string,
): Array<{ scope: GraphRunScope; node_id: string }> {
  const images = graph.nodes
    .filter((node) => node.group_id === groupId && node.node_type === "image_generation")
    .sort((left, right) => left.position_y - right.position_y || left.position_x - right.position_x || left.id.localeCompare(right.id));
  return images.map((node, index) => ({
    scope: index === 0 ? "to_node" : "node",
    node_id: node.id,
  }));
}

export function graphRunIsActive(status: string): boolean {
  return LIVE_RUN_STATUSES.has(status);
}

export async function waitUntilGraphRunNotRunning<TRun extends { id: string; status: string }>(
  run: TRun,
  fetchRun: (runId: string) => Promise<TRun>,
  sleep: (ms: number) => Promise<void> = (ms) => new Promise((resolve) => {
    setTimeout(resolve, ms);
  }),
  intervalMs = 400,
): Promise<TRun> {
  let current = run;
  while (graphRunIsActive(current.status)) {
    await sleep(intervalMs);
    current = await fetchRun(current.id);
  }
  return current;
}

export async function sequenceShotRuns<TRun extends { id: string; status: string }>(
  requests: Array<{ scope: GraphRunScope; node_id: string }>,
  submit: (input: { scope: GraphRunScope; node_id: string }) => Promise<TRun>,
  waitUntilNotRunning: (run: TRun) => Promise<TRun>,
): Promise<void> {
  for (const request of requests) {
    const run = await submit(request);
    const settled = await waitUntilNotRunning(run);
    if (settled.status !== "succeeded") return;
  }
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
