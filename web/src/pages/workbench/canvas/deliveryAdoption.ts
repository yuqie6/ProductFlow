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
}): DeliveryAdoptionSlotDraft[] | { error: "missing_delivery_spec" | "missing_asset" } {
  if (!input.sourceAssetId) return { error: "missing_asset" };
  const node = input.graph.nodes.find((item) => item.id === input.nodeId);
  if (!node || node.node_type !== "image_generation") {
    return { error: "missing_asset" };
  }
  const deliverySpec = parseWorkflowDeliverySpec(node.config?.delivery_spec);
  if (!deliverySpec) return { error: "missing_delivery_spec" };
  const imageTypeKey = typeof node.config?.image_type_key === "string"
    ? node.config.image_type_key
    : null;

  const retained = (input.current?.slots ?? [])
    .filter((slot) => slot.slot_key !== input.nodeId)
    .map((slot) => slotToDraft(slot));

  const next: DeliveryAdoptionSlotDraft = {
    slot_key: input.nodeId,
    sort_order: 0,
    image_type_key: imageTypeKey,
    source_asset_id: input.sourceAssetId,
    source_node_id: input.nodeId,
    delivery_spec: deliverySpec,
    quality_status: input.qualityStatus ?? "unchecked",
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
