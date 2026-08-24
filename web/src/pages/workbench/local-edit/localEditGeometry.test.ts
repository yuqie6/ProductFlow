import { describe, expect, it } from "vitest";

import {
  applyAffineTransform,
  brushBounds,
  brushSegmentBounds,
  createContainTransform,
  createProtectedMask,
  invertAffineTransform,
  mapDisplayPointToSource,
  mergeBrushBounds,
  paintMaskRaster,
  paintMaskSegment,
  summarizeMaskAlpha,
  validateLocalEditFields,
} from "./localEditGeometry";

function validInput(overrides: Partial<Parameters<typeof validateLocalEditFields>[0]> = {}) {
  const mask = createProtectedMask(12, 12);
  paintMaskRaster(mask, 12, 12, { x: 3, y: 3 }, 2, 1, false);
  return {
    operation: "remove",
    instruction: "移除指定元素",
    sourceText: null,
    replacementText: null,
    sourceReady: true,
    sourceWidth: 12,
    sourceHeight: 12,
    mask,
    ...overrides,
  };
}

describe("local edit geometry", () => {
  it("maps display points back through the inverse contain transform", () => {
    const transform = createContainTransform(1000, 500, 400, 300);
    expect(transform).toEqual({ a: 0.4, b: 0, c: 0, d: 0.4, e: 0, f: 50 });
    expect(mapDisplayPointToSource({ x: 200, y: 150 }, transform!)).toEqual({
      x: expect.closeTo(500),
      y: expect.closeTo(250),
    });
    expect(applyAffineTransform(transform!, { x: 500, y: 250 })).toEqual({
      x: expect.closeTo(200),
      y: expect.closeTo(150),
    });
  });

  it("keeps mapping correct at a different scale and rejects points outside the source after clamping", () => {
    const transform = createContainTransform(400, 800, 320, 400);
    expect(transform).toEqual({ a: 0.5, b: 0, c: 0, d: 0.5, e: 60, f: 0 });
    expect(mapDisplayPointToSource({ x: 160, y: 200 }, transform!)).toEqual({ x: 200, y: 400 });
    expect(brushBounds({ x: 0.5, y: 0.5 }, 20, 16, 10)).toEqual({
      left: 0,
      top: 0,
      right: 16,
      bottom: 10,
    });
  });

  it("clips continuous brush dirty bounds to the source and merges adjacent frame work", () => {
    expect(brushSegmentBounds({ x: -3, y: 2 }, { x: 8, y: 14 }, 4, 10, 12)).toEqual({
      left: 0,
      top: 0,
      right: 10,
      bottom: 12,
    });
    expect(mergeBrushBounds(
      { left: 1, top: 2, right: 5, bottom: 8 },
      { left: 4, top: 6, right: 10, bottom: 12 },
    )).toEqual({ left: 1, top: 2, right: 10, bottom: 12 });
    expect(mergeBrushBounds(null, { left: 0, top: 0, right: 1, bottom: 1 })).toEqual({
      left: 0,
      top: 0,
      right: 1,
      bottom: 1,
    });
  });

  it("rejects non-finite and non-invertible affine transforms", () => {
    expect(invertAffineTransform({ a: 1, b: 0, c: 0, d: 0, e: 0, f: 0 })).toBeNull();
    expect(invertAffineTransform({ a: Number.POSITIVE_INFINITY, b: 0, c: 0, d: 1, e: 0, f: 0 })).toBeNull();
    expect(applyAffineTransform({ a: 1, b: 0, c: 0, d: 1, e: 0, f: 0 }, { x: Number.NaN, y: 1 })).toBeNull();
    expect(createContainTransform(0, 100, 300, 300)).toBeNull();
  });

  it("rasterizes transparent edit pixels, protected pixels, and feathered alpha", () => {
    const mask = createProtectedMask(12, 12);
    paintMaskRaster(mask, 12, 12, { x: 6, y: 6 }, 3, 0.4, false);
    const summary = summarizeMaskAlpha(mask);
    expect(summary.editPixelCount).toBeGreaterThan(0);
    expect(summary.protectedPixelCount).toBeGreaterThan(0);
    expect(summary.featheredPixelCount).toBeGreaterThan(0);

    const erased = new Uint8ClampedArray(mask);
    paintMaskRaster(erased, 12, 12, { x: 6, y: 6 }, 1.5, 1, true);
    expect(erased[6 * 12 + 6]).toBe(255);
    expect(summarizeMaskAlpha(erased).editPixelCount).toBeLessThan(summary.editPixelCount);
  });

  it("covers a continuous segment between pointer samples", () => {
    const mask = createProtectedMask(32, 8);
    paintMaskSegment(mask, 32, 8, { x: 2, y: 4 }, { x: 29, y: 4 }, 1.1, 1, false);
    for (let x = 2; x <= 29; x += 1) {
      expect(mask[4 * 32 + x]).toBe(0);
    }
  });

  it("rejects empty and all-edit masks before submit", () => {
    const empty = createProtectedMask(4, 4);
    expect(validateLocalEditFields(validInput({ mask: empty, sourceWidth: 4, sourceHeight: 4 }))).toEqual({
      code: "mask_needs_edit_and_protection",
    });
    const allEdit = new Uint8ClampedArray(16);
    expect(validateLocalEditFields(validInput({ sourceWidth: 4, sourceHeight: 4, mask: allEdit }))).toEqual({
      code: "mask_needs_edit_and_protection",
    });
  });

  it("enforces operation-specific fields", () => {
    expect(validateLocalEditFields(validInput({ instruction: "" }))).toEqual({ code: "instruction_required" });
    expect(validateLocalEditFields(validInput({ operation: "inpaint", instruction: "" }))).toEqual({
      code: "instruction_required",
    });
    expect(validateLocalEditFields(validInput({
      operation: "replace_text",
      instruction: null,
      sourceText: "旧文字",
      replacementText: "新文字",
    }))).toBeNull();
    expect(validateLocalEditFields(validInput({
      operation: "replace_text",
      sourceText: "",
      replacementText: "新文字",
    }))).toEqual({ code: "source_text_required" });
    expect(validateLocalEditFields(validInput({
      operation: "replace_text",
      sourceText: "旧文字",
      replacementText: "",
    }))).toEqual({ code: "replacement_text_required" });
    expect(validateLocalEditFields(validInput({ operation: "crop" }))).toEqual({ code: "unsupported_operation" });
    expect(validateLocalEditFields(validInput({ sourceReady: false }))).toEqual({ code: "source_unavailable" });
  });
});
