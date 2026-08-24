/**
 * 把 Draft 载荷相对不可变 intake 做确认卡 diff。
 *
 * Agent 可以提议改计划；已确认的用户 intake（图种和参考资产 id）是基线，不能悄悄覆盖。
 */

import type {
  AgentProductImageTypeKey,
  WorkflowDraftNodePlan,
  WorkflowDraftPayloadV1,
  WorkflowIntakeV1,
} from "../../../lib/types";

export type WorkflowImageTypeChange = "added" | "removed" | "quantity" | "order";

export interface WorkflowImageTypeReviewRow {
  key: string;
  title: string;
  initialQuantity: number | null;
  currentQuantity: number | null;
  initialOrder: number | null;
  currentOrder: number | null;
  changes: WorkflowImageTypeChange[];
}

export interface WorkflowDraftReviewSummary {
  imageTypes: WorkflowImageTypeReviewRow[];
  initialImageCount: number;
  currentImageCount: number;
  plannedImageCount: number;
  addedReferenceAssetIds: string[];
  removedReferenceAssetIds: string[];
  textLanguages: string[];
  nodeTypeCounts: Record<WorkflowDraftNodePlan["node_type"], number>;
}

export function deriveWorkflowDraftReview(
  intake: WorkflowIntakeV1 | null,
  payload: WorkflowDraftPayloadV1,
): WorkflowDraftReviewSummary {
  const initialTypes = new Map((intake?.image_types ?? []).map((item) => [item.key, item]));
  const currentTypes = new Map(payload.image_types.map((item) => [item.key, item]));
  const imageTypes: WorkflowImageTypeReviewRow[] = payload.image_types
    .slice()
    .sort((left, right) => left.order - right.order || left.key.localeCompare(right.key))
    .map((current) => {
      const initial = initialTypes.get(current.key as AgentProductImageTypeKey);
      const changes: WorkflowImageTypeChange[] = [];
      if (!initial) changes.push("added");
      if (initial && initial.quantity !== current.quantity) changes.push("quantity");
      if (initial && initial.order !== current.order) changes.push("order");
      return {
        key: current.key,
        title: current.title,
        initialQuantity: initial?.quantity ?? null,
        currentQuantity: current.quantity,
        initialOrder: initial?.order ?? null,
        currentOrder: current.order,
        changes,
      };
    });
  (intake?.image_types ?? [])
    .filter((initial) => !currentTypes.has(initial.key))
    .sort((left, right) => left.order - right.order || left.key.localeCompare(right.key))
    .forEach((initial) => imageTypes.push({
      key: initial.key,
      title: initial.key,
      initialQuantity: initial.quantity,
      currentQuantity: null,
      initialOrder: initial.order,
      currentOrder: null,
      changes: ["removed"],
    }));

  const intakeReferences = new Set(intake?.reference_asset_ids ?? []);
  const currentReferences = new Set(payload.reference_bindings.map((reference) => reference.asset_id));
  const nodeTypeCounts: WorkflowDraftReviewSummary["nodeTypeCounts"] = {
    product_context: 0,
    reference_image: 0,
    prompt_generation: 0,
    image_generation: 0,
  };
  payload.nodes.forEach((node) => {
    nodeTypeCounts[node.node_type] += 1;
  });

  return {
    imageTypes,
    initialImageCount: (intake?.image_types ?? []).reduce((sum, item) => sum + item.quantity, 0),
    currentImageCount: payload.image_types.reduce((sum, item) => sum + item.quantity, 0),
    plannedImageCount: payload.image_types.reduce((sum, item) => sum + item.images.length, 0),
    addedReferenceAssetIds: [...currentReferences].filter((assetId) => !intakeReferences.has(assetId)),
    removedReferenceAssetIds: [...intakeReferences].filter((assetId) => !currentReferences.has(assetId)),
    textLanguages: Array.from(new Set(
      payload.image_types.flatMap((imageType) =>
        imageType.images.flatMap((image) => image.generation_spec.text_language ?? []),
      ),
    )),
    nodeTypeCounts,
  };
}

export function isAgentProductImageTypeKey(value: string): value is AgentProductImageTypeKey {
  return [
    "hero",
    "selling_point",
    "scene",
    "detail",
    "sku",
    "dimensions",
    "specifications",
    "after_sales",
    "brand_story",
    "precautions",
    "certification",
    "faq",
    "factory",
    "packaging",
    "shipping",
  ].includes(value as AgentProductImageTypeKey);
}

export function formatWorkflowDraftValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === null) return "null";
  return JSON.stringify(value);
}
