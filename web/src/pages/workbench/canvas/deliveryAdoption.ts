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

export function adoptedSlotBySlot(
  version: DeliveryAdoptionVersion | null | undefined,
): ReadonlyMap<string, DeliveryAdoptionSlot> {
  const map = new Map<string, DeliveryAdoptionSlot>();
  if (!version) return map;
  for (const slot of version.slots) {
    map.set(slot.slot_key, slot);
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
    .filter((slot) => slot.slot_key !== input.nodeId)
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
