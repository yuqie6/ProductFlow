import { describe, expect, it } from "vitest";

import type { GalleryAsset } from "../../../../lib/types";
import { evaluateDeliveryExportSelection } from "./deliveryExport";

type SelectedAsset = Pick<GalleryAsset, "rendition">;

function asset(
  jobId: string,
  status: NonNullable<GalleryAsset["rendition"]>["status"] = "succeeded",
): SelectedAsset {
  return {
    rendition: {
      job_id: jobId,
      source_asset_id: "source-1",
      delivery_spec: { width: 1200, height: 1600, format: "png", fit: "contain" },
      status,
    },
  };
}

describe("delivery export selection", () => {
  it("returns successful rendition jobs in selection order without mutating input", () => {
    const selected = [asset("job-2"), asset("job-1")];
    const snapshot = structuredClone(selected);

    expect(evaluateDeliveryExportSelection(selected)).toEqual({
      eligible: true,
      renditionJobIds: ["job-2", "job-1"],
      reason: null,
    });
    expect(selected).toEqual(snapshot);
  });

  it("rejects an empty selection", () => {
    expect(evaluateDeliveryExportSelection([])).toMatchObject({ eligible: false, reason: "empty" });
  });

  it("rejects original assets instead of silently dropping them", () => {
    expect(evaluateDeliveryExportSelection([{ rendition: null }, asset("job-1")])).toMatchObject({
      eligible: false,
      renditionJobIds: [],
      reason: "not_rendition",
    });
  });

  it("requires every rendition job to have succeeded", () => {
    expect(evaluateDeliveryExportSelection([asset("job-1", "running")])).toMatchObject({
      eligible: false,
      renditionJobIds: [],
      reason: "not_succeeded",
    });
  });

  it("rejects missing and duplicate job identities", () => {
    expect(evaluateDeliveryExportSelection([asset(" ")])).toMatchObject({
      eligible: false,
      renditionJobIds: [],
      reason: "missing_job_id",
    });
    expect(evaluateDeliveryExportSelection([asset("job-1"), asset("job-1")])).toMatchObject({
      eligible: false,
      renditionJobIds: [],
      reason: "duplicate_job_id",
    });
  });
});
