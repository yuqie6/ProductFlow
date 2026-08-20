import { describe, expect, it } from "vitest";

import type { GalleryAsset } from "../../../../lib/types";
import { toggleImageExplorerTargetAsset } from "./selectionTarget";

function asset(id: string): GalleryAsset {
  return { id } as GalleryAsset;
}

describe("image explorer attachment selection", () => {
  it("preserves selection order across independently loaded directories", () => {
    const first = toggleImageExplorerTargetAsset([], asset("folder-a"), 6);
    const second = toggleImageExplorerTargetAsset(first.assets, asset("folder-b"), 6);

    expect(second.assets.map((item) => item.id)).toEqual(["folder-a", "folder-b"]);
  });

  it("allows six assets, rejects the seventh, and supports deselection", () => {
    const six = Array.from({ length: 6 }, (_, index) => asset(`asset-${index + 1}`));
    const rejected = toggleImageExplorerTargetAsset(six, asset("asset-7"), 6);
    const removed = toggleImageExplorerTargetAsset(rejected.assets, asset("asset-3"), 6);

    expect(rejected.limitExceeded).toBe(true);
    expect(rejected.assets).toEqual(six);
    expect(removed.limitExceeded).toBe(false);
    expect(removed.assets.map((item) => item.id)).toEqual([
      "asset-1",
      "asset-2",
      "asset-4",
      "asset-5",
      "asset-6",
    ]);
  });
});
