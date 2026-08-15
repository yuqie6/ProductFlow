import { describe, expect, it } from "vitest";

import { compactImageToolOptions, pruneSelectedReferenceIds } from "./imageToolOptions";

describe("image tool option helpers", () => {
  it("compacts optional provider fields before shared generation submits", () => {
    expect(
      compactImageToolOptions({
        model: "  gpt-image-2 ",
        quality: "high",
        output_format: null,
        output_compression: 101,
        background: "transparent",
        moderation: null,
        action: "generate",
        input_fidelity: "high",
        partial_images: 4,
      }),
    ).toEqual({
      model: "gpt-image-2",
      quality: "high",
      output_compression: 100,
      action: "generate",
      input_fidelity: "high",
      partial_images: 3,
    });
    expect(compactImageToolOptions({ background: "transparent" }, ["background"] as const)).toEqual({
      background: "transparent",
    });
    expect(compactImageToolOptions({})).toBeUndefined();
  });

  it("prunes deleted reference ids and removes duplicates for shared generation controls", () => {
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
