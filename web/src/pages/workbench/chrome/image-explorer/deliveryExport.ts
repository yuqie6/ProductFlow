import type { GalleryAsset } from "../../../../lib/types";

export type DeliveryExportEligibilityReason =
  | "empty"
  | "not_rendition"
  | "not_succeeded"
  | "missing_job_id"
  | "duplicate_job_id";

export interface DeliveryExportEligibility {
  eligible: boolean;
  renditionJobIds: string[];
  reason: DeliveryExportEligibilityReason | null;
}

/**
 * 交付包只能包含已持久化且成功的 rendition 结果。
 * 校验放在这里，避免 UI 静默丢掉非法选中项。
 */
export function evaluateDeliveryExportSelection(
  assets: readonly Pick<GalleryAsset, "rendition">[],
): DeliveryExportEligibility {
  if (assets.length === 0) {
    return { eligible: false, renditionJobIds: [], reason: "empty" };
  }

  const renditionJobIds: string[] = [];
  for (const asset of assets) {
    const rendition = asset.rendition;
    if (!rendition) {
      return { eligible: false, renditionJobIds: [], reason: "not_rendition" };
    }
    if (rendition.status !== "succeeded") {
      return { eligible: false, renditionJobIds: [], reason: "not_succeeded" };
    }
    if (typeof rendition.job_id !== "string" || !rendition.job_id.trim()) {
      return { eligible: false, renditionJobIds: [], reason: "missing_job_id" };
    }
    renditionJobIds.push(rendition.job_id);
  }

  if (new Set(renditionJobIds).size !== renditionJobIds.length) {
    return { eligible: false, renditionJobIds: [], reason: "duplicate_job_id" };
  }

  return { eligible: true, renditionJobIds, reason: null };
}
