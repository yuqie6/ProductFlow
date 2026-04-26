import { describe, expect, it } from "vitest";

import type { ImageSessionAsset, ImageSessionRound } from "../../lib/types";
import { clampGenerationCount, groupImageSessionRounds, pruneSelectedReferenceIds } from "./branching";

const createdAt = "2026-04-27T00:00:00Z";

function asset(id: string): ImageSessionAsset {
  return {
    id,
    kind: "generated_image",
    original_filename: `${id}.png`,
    mime_type: "image/png",
    download_url: `/download/${id}`,
    preview_url: `/preview/${id}`,
    thumbnail_url: `/thumb/${id}`,
    created_at: createdAt,
  };
}

function round(overrides: Partial<ImageSessionRound>): ImageSessionRound {
  return {
    id: "round-1",
    prompt: "prompt",
    assistant_message: "done",
    size: "1024x1024",
    model_name: "mock",
    provider_name: "mock",
    prompt_version: "v1",
    provider_response_id: null,
    previous_response_id: null,
    image_generation_call_id: null,
    generation_group_id: null,
    candidate_index: 1,
    candidate_count: 1,
    base_asset_id: null,
    selected_reference_asset_ids: [],
    generated_asset: asset("asset-1"),
    created_at: createdAt,
    ...overrides,
  };
}

describe("image chat branching helpers", () => {
  it("groups multi-candidate rounds by generation group and sorts candidates", () => {
    const groups = groupImageSessionRounds([
      round({ id: "r1", generation_group_id: "g1", candidate_index: 2, generated_asset: asset("a2") }),
      round({ id: "r0", generation_group_id: "g1", candidate_index: 1, generated_asset: asset("a1") }),
      round({ id: "r2", generation_group_id: null, generated_asset: asset("a3") }),
    ]);

    expect(groups.map((group) => group.id)).toEqual(["g1", "r2"]);
    expect(groups[0].rounds.map((item) => item.generated_asset.id)).toEqual(["a1", "a2"]);
  });

  it("keeps generation count in the MVP range", () => {
    expect(clampGenerationCount(0)).toBe(1);
    expect(clampGenerationCount(3.6)).toBe(4);
    expect(clampGenerationCount(10)).toBe(4);
  });

  it("prunes deleted reference ids and removes duplicates", () => {
    expect(pruneSelectedReferenceIds(["ref-1", "ref-2", "ref-1", "gone"], ["ref-1", "ref-2"])).toEqual([
      "ref-1",
      "ref-2",
    ]);
    expect(pruneSelectedReferenceIds(["ref-1", "ref-2", "ref-3"], ["ref-1", "ref-2", "ref-3"], 2)).toEqual([
      "ref-1",
      "ref-2",
    ]);
  });
});
