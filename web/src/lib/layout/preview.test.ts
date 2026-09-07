import { describe, expect, it } from "vitest";
import {
  compareFrames,
  previewFrames,
  type LayoutDocument,
  type LayoutPlan,
} from "./preview";

/** 与 go/internal/layout cupFixture 同几何的固定夹具。 */
const cupDoc: LayoutDocument = {
  schema_version: 1,
  width: 400,
  height: 400,
  background: "#FFFFFF",
  safe_area: { top: 24, right: 24, bottom: 24, left: 24 },
  subject_asset_id: "asset-cup-body",
  layers: [
    {
      id: "cup",
      type: "image",
      x: 100,
      y: 60,
      width: 200,
      height: 280,
      z_index: 0,
      asset_id: "asset-cup-body",
    },
    {
      id: "bar",
      type: "shape",
      x: 40,
      y: 300,
      width: 320,
      height: 4,
      z_index: 1,
      shape: "rect",
      fill: "#333333",
    },
    {
      id: "capacity",
      type: "text",
      x: 40,
      y: 320,
      width: 320,
      height: 48,
      z_index: 1,
      text: "600ml",
      font_id: "liberation-sans",
      font_size: 28,
      color: "#111111",
      align: "center",
      valign: "middle",
    },
  ],
};

/** Go Compose 导出 Plan 的层框（同夹具；字形细节不在此比对）。 */
const exportPlan: LayoutPlan = {
  schema_version: 1,
  width: 400,
  height: 400,
  safe_area: { top: 24, right: 24, bottom: 24, left: 24 },
  layers: [
    {
      id: "cup",
      type: "image",
      x: 100,
      y: 60,
      width: 200,
      height: 280,
      z_index: 0,
      asset_id: "asset-cup-body",
    },
    {
      id: "bar",
      type: "shape",
      x: 40,
      y: 300,
      width: 320,
      height: 4,
      z_index: 1,
    },
    {
      id: "capacity",
      type: "text",
      x: 40,
      y: 320,
      width: 320,
      height: 48,
      z_index: 1,
    },
  ],
};

describe("layout preview frames", () => {
  it("matches export plan frames within 1px", () => {
    const preview = previewFrames(cupDoc);
    expect(compareFrames(preview, exportPlan, 1)).toBeNull();
  });

  it("keeps subject layer when only font_size changes", () => {
    const resized: LayoutDocument = {
      ...cupDoc,
      layers: cupDoc.layers.map((layer) =>
        layer.id === "capacity" ? { ...layer, font_size: 36 } : layer,
      ),
    };
    const before = previewFrames(cupDoc).find((f) => f.id === "cup");
    const after = previewFrames(resized).find((f) => f.id === "cup");
    expect(before).toEqual(after);
    expect(resized.subject_asset_id).toBe(cupDoc.subject_asset_id);
  });

  it("sorts by z_index then id", () => {
    const frames = previewFrames(cupDoc);
    expect(frames.map((f) => f.id)).toEqual(["cup", "bar", "capacity"]);
  });
});
