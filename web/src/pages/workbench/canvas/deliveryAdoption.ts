/**
 * 成果视图交付采用：把当前节点产物写入新的不可变采用版本。
 * 文稿 O1–O7 候选采用不走此路径。
 */

import type {
  DeliveryAdoptionSlotInput,
  DeliveryAdoptionSlot,
  DeliveryAdoptionQualityStatus,
  DeliveryAdoptionVersion,
  GraphProjection,
} from "../../../lib/types";
import { parseWorkflowDeliverySpec } from "./deliveryRenditions";

export type DeliveryAdoptionSlotDraft = Omit<DeliveryAdoptionSlotInput, "quality_status"> & {
  quality_status: DeliveryAdoptionQualityStatus;
};

export type AdoptionQualityIssueCode =
  | "text_unqualified"
  | "route_unqualified"
  | "quality_unchecked";

export interface AdoptionQualityAssessment {
  status: DeliveryAdoptionQualityStatus;
  issueCodes: readonly AdoptionQualityIssueCode[];
}

export type DeliveryAdoptionFreshnessIssueCode =
  | "different_graph"
  | "unlinked_node"
  | "deleted_node"
  | "missing_current_asset"
  | "asset_changed";

export interface DeliveryAdoptionFreshnessIssue {
  code: DeliveryAdoptionFreshnessIssueCode;
  nodeId: string | null;
}

export interface DeliveryAdoptionFreshnessAssessment {
  isStale: boolean;
  issues: readonly DeliveryAdoptionFreshnessIssue[];
}

/**
 * Reads the finite quality declarations already attached to an image artifact.
 * Server-side checks remain authoritative for the persisted adoption result.
 */
export function assessAdoptionQuality(
  payload: Record<string, unknown> | null | undefined,
): AdoptionQualityAssessment {
  const textTrace = asRecord(payload?.text_trace);
  const produceRoute = asRecord(payload?.produce_route);
  const issueCodes: AdoptionQualityIssueCode[] = [];

  if (textTrace?.text_qualified === false) issueCodes.push("text_unqualified");
  else if (textTrace?.text_qualified !== true) issueCodes.push("quality_unchecked");
  if (produceRoute?.route_qualified === false) issueCodes.push("route_unqualified");
  else if (produceRoute?.route_qualified !== true && !issueCodes.includes("quality_unchecked")) {
    issueCodes.push("quality_unchecked");
  }

  return {
    status: issueCodes.some((code) => code === "text_unqualified" || code === "route_unqualified")
      ? "fail"
      : issueCodes.length ? "unchecked" : "pass",
    issueCodes,
  };
}

export function adoptionQualityStatusMessageKey(status: DeliveryAdoptionQualityStatus) {
  const keys = {
    pass: "graph.results.qualityPass",
    fail: "graph.results.qualityFail",
    unchecked: "graph.results.qualityUnchecked",
  } as const;
  return keys[status];
}

export function adoptionQualityIssueMessageKey(code: AdoptionQualityIssueCode) {
  const keys = {
    text_unqualified: "graph.results.qualityTextFailed",
    route_unqualified: "graph.results.qualityRouteFailed",
    quality_unchecked: "graph.results.qualityUncheckedDetail",
  } as const;
  return keys[code];
}

export function deliveryAdoptionFreshnessIssueMessageKey(code: DeliveryAdoptionFreshnessIssueCode) {
  const keys = {
    different_graph: "graph.results.adoptionSnapshotDifferentGraph",
    deleted_node: "graph.results.adoptionSnapshotDeletedNode",
    unlinked_node: "graph.results.adoptionSnapshotUnlinkedNode",
    missing_current_asset: "graph.results.adoptionSnapshotMissingAsset",
    asset_changed: "graph.results.adoptionSnapshotAssetChanged",
  } as const;
  return keys[code];
}

/**
 * Compares an immutable delivery snapshot with the current result identities.
 * Graph revisions and layout changes are deliberately ignored.
 */
export function assessDeliveryAdoptionFreshness(input: {
  graph: GraphProjection;
  current: DeliveryAdoptionVersion | null | undefined;
  currentAssetByNodeId: ReadonlyMap<string, string | null>;
}): DeliveryAdoptionFreshnessAssessment {
  const version = input.current;
  if (!version) return { isStale: false, issues: [] };

  const issues: DeliveryAdoptionFreshnessIssue[] = [];
  const seen = new Set<string>();
  const addIssue = (code: DeliveryAdoptionFreshnessIssueCode, nodeId: string | null) => {
    const key = `${code}:${nodeId ?? "version"}`;
    if (seen.has(key)) return;
    seen.add(key);
    issues.push({ code, nodeId });
  };

  if (version.graph_id && version.graph_id !== input.graph.id) {
    addIssue("different_graph", null);
    return { isStale: true, issues };
  }

  const currentNodeIds = new Set(input.graph.nodes.map((node) => node.id));
  for (const slot of version.slots) {
    const nodeId = slot.source_node_id ?? slot.slot_key;
    if (!currentNodeIds.has(nodeId)) {
      addIssue(slot.source_node_id == null ? "unlinked_node" : "deleted_node", slot.source_node_id == null ? null : nodeId);
      continue;
    }

    const node = input.graph.nodes.find((candidate) => candidate.id === nodeId);
    const currentAssetId = input.currentAssetByNodeId.has(nodeId)
      ? input.currentAssetByNodeId.get(nodeId) ?? null
      : node?.preview_asset_id ?? null;
    if (!currentAssetId) {
      addIssue("missing_current_asset", nodeId);
      continue;
    }
    if (currentAssetId !== slot.source_asset_id) {
      addIssue("asset_changed", nodeId);
    }
  }

  return { isStale: issues.length > 0, issues };
}

export function adoptedSlotByNodeId(
  version: DeliveryAdoptionVersion | null | undefined,
): ReadonlyMap<string, DeliveryAdoptionSlot> {
  const map = new Map<string, DeliveryAdoptionSlot>();
  if (!version) return map;
  for (const slot of version.slots) {
    map.set(slot.source_node_id ?? slot.slot_key, slot);
  }
  return map;
}

export function buildAdoptionSlotsReplacingNode(input: {
  graph: GraphProjection;
  current: DeliveryAdoptionVersion | null;
  nodeId: string;
  sourceAssetId: string;
  qualityStatus?: DeliveryAdoptionQualityStatus;
  qualityDetail?: string | null;
  textOverflow?: boolean;
  artifactPayload?: Record<string, unknown> | null;
}): DeliveryAdoptionSlotDraft[] | {
  error: "missing_delivery_spec" | "missing_asset";
} {
  if (!input.sourceAssetId) return { error: "missing_asset" };
  const node = input.graph.nodes.find((item) => item.id === input.nodeId);
  if (!node || node.node_type !== "image_generation") {
    return { error: "missing_asset" };
  }
  const payload = input.artifactPayload
    ?? (node.current_artifact_payload as Record<string, unknown> | null | undefined);
  const quality = assessAdoptionQuality(payload);
  const deliverySpec = parseWorkflowDeliverySpec(node.config?.delivery_spec);
  if (!deliverySpec) return { error: "missing_delivery_spec" };
  const imageTypeKey = typeof node.config?.image_type_key === "string"
    ? node.config.image_type_key
    : null;

  const retained = (input.current?.slots ?? [])
    .filter((slot) => slot.slot_key !== input.nodeId && slot.source_node_id !== input.nodeId)
    .map((slot) => slotToDraft(slot));

  const requested = input.qualityStatus ?? quality.status;
  const qualityStatus = quality.status === "fail"
    ? "fail"
    : requested === "pass" && quality.status !== "pass"
      ? "unchecked"
      : requested;

  const next: DeliveryAdoptionSlotDraft = {
    slot_key: input.nodeId,
    sort_order: 0,
    image_type_key: imageTypeKey,
    source_asset_id: input.sourceAssetId,
    source_node_id: input.nodeId,
    delivery_spec: deliverySpec,
    quality_status: qualityStatus,
    quality_detail: input.qualityDetail ?? null,
    text_overflow: input.textOverflow ?? false,
  };
  const merged = [...retained, next]
    .sort((left, right) => left.slot_key.localeCompare(right.slot_key))
    .map((slot, index) => ({ ...slot, sort_order: index }));
  return merged;
}

function slotToDraft(slot: DeliveryAdoptionSlot): DeliveryAdoptionSlotDraft {
  return {
    slot_key: slot.slot_key,
    sort_order: slot.sort_order,
    image_type_key: slot.image_type_key,
    source_asset_id: slot.source_asset_id,
    source_node_id: slot.source_node_id,
    delivery_spec: slot.delivery_spec,
    quality_status: slot.quality_status,
    quality_detail: slot.quality_detail,
    text_overflow: slot.text_overflow,
  };
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}
