/**
 * 成果视图：每个生图节点恰好一项；证据图单独可达。
 * 分组只组织展示，不合并多图，也不引入交付采用状态。
 */

import type {
  GraphNode,
  GraphNodeRunLike,
  GraphProjection,
  GraphRunLike,
  WorkflowNodeDisplayStatus,
} from "../../../lib/types";
import {
  graphNodeRunPreviewAssetId,
  LIVE_RUN_STATUSES,
} from "./graphRunDisplay";

export type GraphResultItemKind = "generation" | "evidence";

export type GraphResultSectionKind = "group" | "ungrouped" | "evidence";

export interface GraphResultItem {
  nodeId: string;
  kind: GraphResultItemKind;
  title: string;
  imageTypeKey: string | null;
  groupId: string | null;
  groupTitle: string | null;
  status: WorkflowNodeDisplayStatus;
  failureReason: string | null;
  /** 节点当前产物；不等于交付采用。 */
  currentAssetId: string | null;
  /** 最近运行失败/未知，但仍有可用当前图（旧结果）。 */
  showingStaleCurrent: boolean;
  runnable: boolean;
}

export interface GraphResultSection {
  key: string;
  kind: GraphResultSectionKind;
  groupId: string | null;
  title: string;
  items: GraphResultItem[];
}

const UNGROUPED_KEY = "ungrouped";
const EVIDENCE_KEY = "evidence";

export function graphHasResultItems(graph: GraphProjection): boolean {
  return graph.nodes.some((node) => isGenerationNode(node) || isEvidenceNode(node));
}

/**
 * 打开工作台时的条件默认：至少一枚生图/证据项已有当前图 → results；
 * 无产出或仅有空位节点 → flow。完成 ≠ 全局一律成果。
 */
export function graphHasUsableResultImages(graph: GraphProjection | null | undefined): boolean {
  if (!graph) return false;
  for (const node of graph.nodes) {
    if (isGenerationNode(node) && node.preview_asset_id) return true;
    if (isEvidenceNode(node) && (node.bound_asset_id ?? node.preview_asset_id)) return true;
  }
  return false;
}

export function defaultWorkbenchMainView(
  graph: GraphProjection | null | undefined,
): "flow" | "results" {
  return graphHasUsableResultImages(graph) ? "results" : "flow";
}

/**
 * 商品级显式偏好优先；无偏好或非法值时回退条件默认。
 * 打开时不得把条件默认写成偏好。
 */
export function resolveWorkbenchMainView(
  graph: GraphProjection | null | undefined,
  preferred: "flow" | "results" | null | undefined,
): "flow" | "results" {
  if (preferred === "flow" || preferred === "results") return preferred;
  return defaultWorkbenchMainView(graph);
}

export function projectGraphResults(
  graph: GraphProjection,
  runs: readonly GraphRunLike[],
): GraphResultSection[] {
  const latestRuns = latestNodeRuns(runs);
  const groupTitleById = new Map(graph.groups.map((group) => [group.id, group.title]));
  const generationItems: GraphResultItem[] = [];
  const evidenceItems: GraphResultItem[] = [];

  for (const node of graph.nodes) {
    if (isGenerationNode(node)) {
      generationItems.push(projectGenerationItem(graph, node, latestRuns.get(node.id) ?? null, groupTitleById));
      continue;
    }
    if (isEvidenceNode(node)) {
      evidenceItems.push(projectEvidenceItem(node, groupTitleById));
    }
  }

  const sections: GraphResultSection[] = [];
  const seenGroups = new Set<string>();

  for (const group of graph.groups) {
    const items = generationItems.filter((item) => item.groupId === group.id);
    if (!items.length) continue;
    seenGroups.add(group.id);
    sections.push({
      key: group.id,
      kind: "group",
      groupId: group.id,
      title: group.title,
      items: sortResultItems(items),
    });
  }

  const orphanGrouped = generationItems.filter((item) => (
    item.groupId != null && !seenGroups.has(item.groupId)
  ));
  for (const item of orphanGrouped) {
    const key = item.groupId!;
    if (seenGroups.has(key)) continue;
    seenGroups.add(key);
    sections.push({
      key,
      kind: "group",
      groupId: key,
      title: item.groupTitle ?? key,
      items: sortResultItems(generationItems.filter((candidate) => candidate.groupId === key)),
    });
  }

  const ungrouped = generationItems.filter((item) => item.groupId == null);
  if (ungrouped.length) {
    sections.push({
      key: UNGROUPED_KEY,
      kind: "ungrouped",
      groupId: null,
      title: "",
      items: sortResultItems(ungrouped),
    });
  }

  if (evidenceItems.length) {
    sections.push({
      key: EVIDENCE_KEY,
      kind: "evidence",
      groupId: null,
      title: "",
      items: sortResultItems(evidenceItems),
    });
  }

  return sections;
}

export function flattenGraphResultItems(sections: readonly GraphResultSection[]): GraphResultItem[] {
  return sections.flatMap((section) => section.items);
}

export function assertUniqueResultNodeIds(sections: readonly GraphResultSection[]): string[] {
  const seen = new Set<string>();
  const duplicates: string[] = [];
  for (const item of flattenGraphResultItems(sections)) {
    if (seen.has(item.nodeId)) duplicates.push(item.nodeId);
    else seen.add(item.nodeId);
  }
  return duplicates;
}

function projectGenerationItem(
  graph: GraphProjection,
  node: GraphNode,
  latest: LatestNodeRun | null,
  groupTitleById: Map<string, string>,
): GraphResultItem {
  const runPreviewAssetId = latest
    && (latest.nodeRun.status === "succeeded" || latest.nodeRun.status === "skipped")
    ? graphNodeRunPreviewAssetId(latest.nodeRun, graph)
    : null;
  const currentAssetId = node.preview_asset_id ?? runPreviewAssetId;
  const status: WorkflowNodeDisplayStatus = latest?.nodeRun.status
    ?? (currentAssetId ? "succeeded" : "idle");
  const failedOrUnknown = status === "failed" || status === "unknown";
  return {
    nodeId: node.id,
    kind: "generation",
    title: node.title,
    imageTypeKey: readStringConfig(node, "image_type_key"),
    groupId: node.group_id,
    groupTitle: node.group_id ? groupTitleById.get(node.group_id) ?? null : null,
    status,
    failureReason: latest?.nodeRun.status === "failed"
      ? latest.nodeRun.failure_reason ?? latest.runFailureReason
      : null,
    currentAssetId,
    showingStaleCurrent: Boolean(currentAssetId) && failedOrUnknown,
    runnable: true,
  };
}

function projectEvidenceItem(
  node: GraphNode,
  groupTitleById: Map<string, string>,
): GraphResultItem {
  const currentAssetId = node.bound_asset_id ?? node.preview_asset_id;
  return {
    nodeId: node.id,
    kind: "evidence",
    title: node.title,
    imageTypeKey: readStringConfig(node, "image_type_key"),
    groupId: node.group_id,
    groupTitle: node.group_id ? groupTitleById.get(node.group_id) ?? null : null,
    status: currentAssetId ? "succeeded" : "idle",
    failureReason: null,
    currentAssetId,
    showingStaleCurrent: false,
    runnable: false,
  };
}

function isGenerationNode(node: GraphNode): boolean {
  return node.node_type === "image_generation";
}

function isEvidenceNode(node: GraphNode): boolean {
  return node.node_type === "image_asset" && readStringConfig(node, "role") === "evidence";
}

function readStringConfig(node: GraphNode, key: string): string | null {
  const value = node.config?.[key];
  return typeof value === "string" && value.trim() ? value : null;
}

function sortResultItems(items: GraphResultItem[]): GraphResultItem[] {
  return [...items].sort((left, right) => left.title.localeCompare(right.title) || left.nodeId.localeCompare(right.nodeId));
}

interface LatestNodeRun {
  nodeRun: GraphNodeRunLike;
  activityAt: string;
  runStartedAt: string;
  runFailureReason: string | null;
  runId: string;
  runIsLive: boolean;
}

function latestNodeRuns(runs: readonly GraphRunLike[]): Map<string, LatestNodeRun> {
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
