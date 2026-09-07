/**
 * 成果视图交付采用：把当前节点产物写入新的不可变采用版本。
 * 文稿 O1–O7 候选采用不走此路径。
 */

import type {
  DeliveryAdoptionSlot,
  DeliveryAdoptionVersion,
  GraphProjection,
  WorkflowDeliverySpec,
} from "../../../lib/types";
import { parseWorkflowDeliverySpec } from "./deliveryRenditions";

export interface DeliveryAdoptionSlotDraft {
  slot_key: string;
  sort_order: number;
  image_type_key?: string | null;
  source_asset_id: string;
  source_node_id?: string | null;
  delivery_spec: WorkflowDeliverySpec;
  quality_status?: "pass" | "fail" | "unchecked";
}

/** 服务端硬闸对齐：显式不合格禁止采用；缺元数据仍可 unchecked，不得自称 pass。 */
export type AdoptionGateCode = "text_unqualified" | "route_unqualified";

export type AdoptionGateResult =
  | { ok: true; canMarkPass: boolean }
  | { ok: false; code: AdoptionGateCode };

export function evaluateAdoptionGate(
  payload: Record<string, unknown> | null | undefined,
): AdoptionGateResult {
  const textTrace = asRecord(payload?.text_trace);
  const produceRoute = asRecord(payload?.produce_route);

  if (textTrace && textTrace.text_qualified === false) {
    return { ok: false, code: "text_unqualified" };
  }
  if (produceRoute && produceRoute.route_qualified === false) {
    return { ok: false, code: "route_unqualified" };
  }

  const canMarkPass = Boolean(
    textTrace
    && textTrace.text_qualified === true
    && produceRoute
    && produceRoute.route_qualified === true,
  );
  return { ok: true, canMarkPass };
}

export function adoptionGateMessageKey(code: AdoptionGateCode):
  | "graph.results.adoptTextUnqualified"
  | "graph.results.adoptRouteUnqualified" {
  return code === "text_unqualified"
    ? "graph.results.adoptTextUnqualified"
    : "graph.results.adoptRouteUnqualified";
}

export function adoptedAssetBySlot(
  version: DeliveryAdoptionVersion | null | undefined,
): ReadonlyMap<string, string> {
  const map = new Map<string, string>();
  if (!version) return map;
  for (const slot of version.slots) {
    map.set(slot.slot_key, slot.source_asset_id);
  }
  return map;
}

export function buildAdoptionSlotsReplacingNode(input: {
  graph: GraphProjection;
  current: DeliveryAdoptionVersion | null;
  nodeId: string;
  sourceAssetId: string;
  qualityStatus?: "pass" | "unchecked";
  artifactPayload?: Record<string, unknown> | null;
}): DeliveryAdoptionSlotDraft[] | {
  error: "missing_delivery_spec" | "missing_asset" | AdoptionGateCode;
} {
  if (!input.sourceAssetId) return { error: "missing_asset" };
  const node = input.graph.nodes.find((item) => item.id === input.nodeId);
  if (!node || node.node_type !== "image_generation") {
    return { error: "missing_asset" };
  }
  const payload = input.artifactPayload
    ?? (node.current_artifact_payload as Record<string, unknown> | null | undefined);
  const gate = evaluateAdoptionGate(payload);
  if (!gate.ok) {
    return { error: gate.code };
  }
  const deliverySpec = parseWorkflowDeliverySpec(node.config?.delivery_spec);
  if (!deliverySpec) return { error: "missing_delivery_spec" };
  const imageTypeKey = typeof node.config?.image_type_key === "string"
    ? node.config.image_type_key
    : null;

  const retained = (input.current?.slots ?? [])
    .filter((slot) => slot.slot_key !== input.nodeId)
    .map((slot) => slotToDraft(slot));

  const requested = input.qualityStatus ?? "unchecked";
  const qualityStatus = requested === "pass" && gate.canMarkPass ? "pass" : "unchecked";

  const next: DeliveryAdoptionSlotDraft = {
    slot_key: input.nodeId,
    sort_order: 0,
    image_type_key: imageTypeKey,
    source_asset_id: input.sourceAssetId,
    source_node_id: input.nodeId,
    delivery_spec: deliverySpec,
    quality_status: qualityStatus,
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
    quality_status: slot.quality_status === "fail" ? "unchecked" : slot.quality_status,
  };
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}
