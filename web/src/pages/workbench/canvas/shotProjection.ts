/**
 * 一组图片节点就是一个计划镜头。
 *
 * 重跑更新当前资产；历史结果留在图库和运行记录。分组本身没有执行状态。
 */

import type {
  GraphNodeRun,
  GraphProjection,
  GraphRun,
  WorkflowNodeStatus,
} from "../../../lib/types";
import {
  graphNodeRunPreviewAssetId,
  LIVE_RUN_STATUSES,
} from "./graphRunDisplay";

export interface GraphShotProjection {
  groupId: string;
  title: string;
  imageNodeIds: string[];
  primaryImageNodeId: string;
  primaryImageAssetId: string | null;
  imageNodeCount: number;
  completedImageCount: number;
  latestNodeStatus: WorkflowNodeStatus;
  latestFailureReason: string | null;
  currentResultAssetIds: string[];
}

export function graphHasImageGenerationGroups(graph: GraphProjection): boolean {
  return graph.groups.some((group) => imageNodesForGroup(graph, group.id).length > 0);
}

export function projectGraphShots(
  graph: GraphProjection,
  runs: readonly GraphRun[],
): GraphShotProjection[] {
  const latestRuns = latestNodeRuns(runs);

  return graph.groups.flatMap((group) => {
    const imageNodes = imageNodesForGroup(graph, group.id);
    if (!imageNodes.length) return [];

    const nodeFacts = imageNodes.map((node) => {
      const latestRun = latestRuns.get(node.id) ?? null;
      const previewAssetId = node.preview_asset_id
        ?? (latestRun?.nodeRun.status === "succeeded"
          ? graphNodeRunPreviewAssetId(latestRun.nodeRun, graph)
          : null);
      return {
        node,
        status: latestRun?.nodeRun.status ?? (previewAssetId ? "succeeded" : "idle"),
        failureReason: latestRun?.nodeRun.status === "failed"
          ? latestRun.nodeRun.failure_reason ?? latestRun.runFailureReason
          : null,
        activityAt: latestRun?.activityAt ?? null,
        isLive: latestRun?.runIsLive ?? false,
        previewAssetId,
      };
    });
    const latest = nodeFacts.reduce((current, candidate) => {
      if (candidate.isLive !== current.isLive) return candidate.isLive ? candidate : current;
      const activity = compareActivity(candidate.activityAt, current.activityAt);
      if (activity !== 0) return activity > 0 ? candidate : current;
      if (Boolean(candidate.previewAssetId) !== Boolean(current.previewAssetId)) {
        return candidate.previewAssetId ? candidate : current;
      }
      return current;
    });
    const currentResultAssetIds = uniqueAssetIds(
      nodeFacts.flatMap((fact) => fact.previewAssetId ? [fact.previewAssetId] : []),
    );

    return [{
      groupId: group.id,
      title: group.title,
      imageNodeIds: imageNodes.map((node) => node.id),
      primaryImageNodeId: latest.node.id,
      primaryImageAssetId: latest.previewAssetId,
      imageNodeCount: imageNodes.length,
      completedImageCount: nodeFacts.filter((fact) => Boolean(fact.previewAssetId)).length,
      latestNodeStatus: latest.status,
      latestFailureReason: latest.failureReason,
      currentResultAssetIds,
    }];
  });
}

function imageNodesForGroup(graph: GraphProjection, groupId: string) {
  const group = graph.groups.find((item) => item.id === groupId);
  const memberIds = new Set(group?.member_ids ?? []);
  return graph.nodes.filter((node) => (
    node.node_type === "image_generation"
    && (node.group_id === groupId || memberIds.has(node.id))
  ));
}

interface LatestNodeRun {
  nodeRun: GraphNodeRun;
  activityAt: string;
  runStartedAt: string;
  runFailureReason: string | null;
  runId: string;
  runIsLive: boolean;
}

function latestNodeRuns(runs: readonly GraphRun[]): Map<string, LatestNodeRun> {
  const latest = new Map<string, LatestNodeRun>();
  for (const run of runs) {
    for (const nodeRun of run.node_runs) {
      if (!nodeRun.node_id) continue;
      const candidate: LatestNodeRun = {
        nodeRun,
        activityAt: nodeRun.finished_at ?? nodeRun.started_at ?? run.finished_at ?? run.started_at,
        runStartedAt: run.started_at,
        runFailureReason: run.failure_reason,
        runId: run.id,
        runIsLive: LIVE_RUN_STATUSES.has(run.status),
      };
      const current = latest.get(nodeRun.node_id);
      if (!current || compareNodeRuns(candidate, current) > 0) {
        latest.set(nodeRun.node_id, candidate);
      }
    }
  }
  return latest;
}

function compareNodeRuns(left: LatestNodeRun, right: LatestNodeRun): number {
  if (left.runIsLive !== right.runIsLive) return left.runIsLive ? 1 : -1;
  const activity = compareActivity(left.activityAt, right.activityAt);
  if (activity !== 0) return activity;
  const runStart = compareActivity(left.runStartedAt, right.runStartedAt);
  if (runStart !== 0) return runStart;
  return left.runId.localeCompare(right.runId);
}

function compareActivity(left: string | null, right: string | null): number {
  if (left === right) return 0;
  if (!left) return -1;
  if (!right) return 1;
  const leftTime = Date.parse(left);
  const rightTime = Date.parse(right);
  if (Number.isFinite(leftTime) && Number.isFinite(rightTime) && leftTime !== rightTime) {
    return leftTime - rightTime;
  }
  return left.localeCompare(right);
}

function uniqueAssetIds(assetIds: string[]): string[] {
  return [...new Set(assetIds)];
}
