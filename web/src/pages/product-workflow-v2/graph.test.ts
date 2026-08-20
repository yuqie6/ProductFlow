import { describe, expect, it } from "vitest";

import type {
  ProductWorkflowV2,
  WorkflowEdgeV2,
  WorkflowFolderV2,
  WorkflowNodeV2,
} from "../../lib/types";
import {
  buildLocalFolderGraph,
  buildAutoLayoutNodePositions,
  deriveFolderBounds,
  deriveFolderSummary,
  folderSyntheticNodeId,
  getV2ConnectionHandles,
  isV2WorkflowConnectionValid,
  isV2WorkflowLineageEdge,
  projectGlobalGraph,
  visibleRealNodeIds,
} from "./graph";

function makeNode(partial: Partial<WorkflowNodeV2> & Pick<WorkflowNodeV2, "id" | "node_type">): WorkflowNodeV2 {
  return {
    id: partial.id,
    workflow_id: "workflow-1",
    schema_version: 2,
    key: partial.key ?? partial.id,
    node_type: partial.node_type,
    title: partial.title ?? partial.id,
    position_x: partial.position_x ?? 0,
    position_y: partial.position_y ?? 0,
    folder_id: partial.folder_id ?? null,
    bound_image_asset_id: partial.bound_image_asset_id ?? null,
    current_prompt_artifact_version_id: null,
    config_json: partial.config_json ?? {},
    status: partial.status ?? "idle",
    output_json: partial.output_json ?? null,
    failure_reason: null,
    created_at: "2026-08-13T00:00:00Z",
    updated_at: "2026-08-13T00:00:00Z",
  };
}

function makeEdge(id: string, source: string, target: string): WorkflowEdgeV2 {
  return {
    id,
    workflow_id: "workflow-1",
    key: id,
    source_node_id: source,
    target_node_id: target,
    source_handle: "output",
    target_handle: "input",
    created_at: "2026-08-13T00:00:00Z",
  };
}

function makeWorkflow(): ProductWorkflowV2 {
  const folders: WorkflowFolderV2[] = [];
  const nodes: WorkflowNodeV2[] = [makeNode({ id: "product", node_type: "product_context", position_x: 0 })];
  const edges: WorkflowEdgeV2[] = [];
  for (let typeIndex = 0; typeIndex < 15; typeIndex += 1) {
    const folderId = `folder-${typeIndex}`;
    const promptId = `prompt-${typeIndex}`;
    folders.push({
      id: folderId,
      workflow_id: "workflow-1",
      key: folderId,
      title: `图片类型 ${typeIndex + 1}`,
      order: typeIndex,
      created_at: "2026-08-13T00:00:00Z",
      updated_at: "2026-08-13T00:00:00Z",
    });
    nodes.push(makeNode({
      id: promptId,
      node_type: "prompt_generation",
      folder_id: folderId,
      position_x: 420 + (typeIndex % 5) * 900,
      position_y: Math.floor(typeIndex / 5) * 600,
      status: typeIndex === 0 ? "running" : "succeeded",
    }));
    edges.push(makeEdge(`context-${typeIndex}`, "product", promptId));
    for (let imageIndex = 0; imageIndex < 2; imageIndex += 1) {
      const imageId = `image-${typeIndex}-${imageIndex}`;
      nodes.push(makeNode({
        id: imageId,
        node_type: "image_generation",
        folder_id: folderId,
        position_x: 760 + (typeIndex % 5) * 900,
        position_y: Math.floor(typeIndex / 5) * 600 + imageIndex * 220,
        status: "succeeded",
        bound_image_asset_id: `asset-${typeIndex}-${imageIndex}`,
      }));
      edges.push(makeEdge(`prompt-image-${typeIndex}-${imageIndex}`, promptId, imageId));
    }
  }
  return {
    id: "workflow-1",
    product_id: "product-1",
    title: "30 图工作流",
    active: true,
    schema_version: 2,
    revision: 1,
    edit_version: 0,
    source_draft_revision_id: "draft-revision-1",
    visual_system_version_id: "visual-version-1",
    materialization_id: "materialization-1",
    folders,
    nodes,
    edges,
    created_at: "2026-08-13T00:00:00Z",
    updated_at: "2026-08-13T00:00:00Z",
  };
}

describe("schema-v2 workflow graph projection", () => {
  it("projects global graph with real nodes and folder group frames", () => {
    const workflow = makeWorkflow();
    const projection = projectGlobalGraph(workflow);

    expect(projection.nodes).toHaveLength(61);
    expect(projection.nodes.filter((node) => node.kind === "folder")).toHaveLength(15);
    expect(projection.nodes.filter((node) => node.kind === "node")).toHaveLength(46);
    expect(projection.edges).toHaveLength(45);
    expect(projection.edges.every((edge) => !edge.projected && edge.count === 1)).toBe(true);
    expect(projection.edges[0].original_edge_ids[0]).toMatch(/^context-/);
  });

  it("returns real local nodes, edge ids, endpoints, and handles", () => {
    const workflow = makeWorkflow();
    const local = buildLocalFolderGraph(workflow, "folder-4");

    expect(local.nodes.map((node) => node.id).sort()).toEqual(["image-4-0", "image-4-1", "prompt-4"]);
    expect(local.edges.map((edge) => edge.id).sort()).toEqual([
      "prompt-image-4-0",
      "prompt-image-4-1",
    ]);
    expect(local.edges[0]).toMatchObject({
      source_node_id: "prompt-4",
      source_handle: "output",
      target_handle: "input",
    });
  });

  it("derives stable bounds and aggregate status without persisted folder geometry", () => {
    const workflow = makeWorkflow();
    const members = workflow.nodes.filter((node) => node.folder_id === "folder-0");
    const bounds = deriveFolderBounds(members);
    const summary = deriveFolderSummary(workflow, "folder-0");

    expect(bounds).toEqual({ x: 388, y: -32, width: 652, height: 520 });
    expect(summary).toMatchObject({
      member_count: 3,
      node_types: ["prompt_generation", "image_generation"],
      status: "running",
      preview_asset_ids: ["asset-0-0", "asset-0-1"],
      inbound_edge_count: 1,
      outbound_edge_count: 0,
    });
    expect(folderSyntheticNodeId("folder-0")).toBe("folder:folder-0");
  });

  it("does not report a folder as succeeded while a runnable member is still idle", () => {
    const workflow = makeWorkflow();
    const partialFolderNodes = workflow.nodes.filter((node) => node.folder_id === "folder-1");
    partialFolderNodes[0].status = "succeeded";
    partialFolderNodes[1].status = "succeeded";
    partialFolderNodes[2].status = "idle";

    expect(deriveFolderSummary(workflow, "folder-1").status).toBe("idle");
  });

  it("preserves an unknown provider effect in the folder summary", () => {
    const workflow = makeWorkflow();
    const runnable = workflow.nodes.filter((node) => node.folder_id === "folder-1");
    runnable[0].status = "unknown";
    runnable[1].status = "failed";
    runnable[2].status = "succeeded";

    expect(deriveFolderSummary(workflow, "folder-1").status).toBe("unknown");
  });

  it("matches selectable real nodes to the global or local projection", () => {
    const workflow = makeWorkflow();

    expect(visibleRealNodeIds(workflow, null)).toHaveLength(46);
    expect(visibleRealNodeIds(workflow, "folder-4").sort()).toEqual([
      "image-4-0",
      "image-4-1",
      "prompt-4",
    ]);
  });

  it("auto-layouts real nodes based on topological layers", () => {
    const workflow = makeWorkflow();

    const positions = buildAutoLayoutNodePositions(workflow, null);
    const positionById = new Map(positions.map((position) => [position.node_id, position]));

    expect(positions.length).toBe(workflow.nodes.length);
    expect(positionById.get("product")!.position_x % 24).toBe(0);
    expect(positionById.get("product")!.position_y % 24).toBe(0);
  });

  it("validates typed connections, duplicate pairs, synthetic folders, and cycles", () => {
    const workflow = makeWorkflow();
    const prompt = workflow.nodes.find((node) => node.id === "prompt-0")!;
    const firstImage = workflow.nodes.find((node) => node.id === "image-0-0")!;
    const secondImage = workflow.nodes.find((node) => node.id === "image-0-1")!;

    expect(getV2ConnectionHandles("product_context", "prompt_generation")).toEqual({
      source: "facts",
      target: "facts",
    });
    expect(getV2ConnectionHandles("image_generation", "reference_image")).toBeNull();
    expect(isV2WorkflowConnectionValid(workflow, firstImage.id, secondImage.id)).toBe(true);
    expect(isV2WorkflowConnectionValid(
      workflow,
      firstImage.id,
      secondImage.id,
      "image",
      "reference",
    )).toBe(true);
    expect(isV2WorkflowConnectionValid(
      workflow,
      firstImage.id,
      secondImage.id,
      "image",
      "prompt",
    )).toBe(false);
    expect(isV2WorkflowConnectionValid(workflow, prompt.id, firstImage.id)).toBe(false);
    expect(isV2WorkflowConnectionValid(workflow, firstImage.id, firstImage.id)).toBe(false);
    expect(isV2WorkflowConnectionValid(workflow, folderSyntheticNodeId("folder-0"), firstImage.id)).toBe(false);

    workflow.edges.push(makeEdge("image-chain", firstImage.id, secondImage.id));
    expect(isV2WorkflowConnectionValid(workflow, secondImage.id, firstImage.id)).toBe(false);
  });

  it("identifies only the required context and prompt lineage edges", () => {
    const workflow = makeWorkflow();
    const prompt = workflow.nodes.find((node) => node.id === "prompt-0")!;
    const image = workflow.nodes.find((node) => node.id === "image-0-0")!;
    prompt.config_json.prompt_plan_key = "plan-0";
    image.config_json.prompt_plan_key = "plan-0";

    expect(isV2WorkflowLineageEdge(workflow, {
      source_node_id: "product",
      target_node_id: prompt.id,
    })).toBe(true);
    expect(isV2WorkflowLineageEdge(workflow, {
      source_node_id: prompt.id,
      target_node_id: image.id,
    })).toBe(true);
    expect(isV2WorkflowLineageEdge(workflow, {
      source_node_id: image.id,
      target_node_id: "image-0-1",
    })).toBe(false);
  });
});
