/**
 * 局部编辑的蒙版几何。
 *
 * 坐标是源图像素，不是画布 CSS 像素；交付尺寸不能当编辑身份。
 */

import type { LocalImageEditOperation } from "../../../lib/types";

export type LocalEditOperation = LocalImageEditOperation;

export interface LocalEditPoint {
  x: number;
  y: number;
}

export interface LocalEditAffineTransform {
  a: number;
  b: number;
  c: number;
  d: number;
  e: number;
  f: number;
}

export interface LocalEditBrushBounds {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

export interface LocalEditMaskSummary {
  editPixelCount: number;
  protectedPixelCount: number;
  featheredPixelCount: number;
}

export interface LocalEditValidationInput {
  operation: string;
  instruction?: string | null;
  sourceText?: string | null;
  replacementText?: string | null;
  sourceReady: boolean;
  sourceWidth: number;
  sourceHeight: number;
  mask?: Uint8ClampedArray | null;
  maskSummary?: LocalEditMaskSummary | null;
}

export type LocalEditValidationIssue =
  | { code: "unsupported_operation" }
  | { code: "source_unavailable" }
  | { code: "invalid_source_dimensions" }
  | { code: "instruction_required" }
  | { code: "source_text_required" }
  | { code: "replacement_text_required" }
  | { code: "mask_required" }
  | { code: "mask_size_mismatch" }
  | { code: "mask_needs_edit_and_protection" };

const SUPPORTED_OPERATIONS: readonly LocalEditOperation[] = ["remove", "replace_text", "inpaint"];

export function isLocalEditOperation(value: string): value is LocalEditOperation {
  return SUPPORTED_OPERATIONS.includes(value as LocalEditOperation);
}

export function isFiniteAffineTransform(transform: LocalEditAffineTransform): boolean {
  return [transform.a, transform.b, transform.c, transform.d, transform.e, transform.f]
    .every((value) => Number.isFinite(value));
}

export function createContainTransform(
  sourceWidth: number,
  sourceHeight: number,
  displayWidth: number,
  displayHeight: number,
): LocalEditAffineTransform | null {
  if (![sourceWidth, sourceHeight, displayWidth, displayHeight].every((value) => Number.isFinite(value) && value > 0)) {
    return null;
  }
  const scale = Math.min(displayWidth / sourceWidth, displayHeight / sourceHeight);
  if (!Number.isFinite(scale) || scale <= 0) return null;
  return {
    a: scale,
    b: 0,
    c: 0,
    d: scale,
    e: (displayWidth - sourceWidth * scale) / 2,
    f: (displayHeight - sourceHeight * scale) / 2,
  };
}

export function invertAffineTransform(
  transform: LocalEditAffineTransform,
): LocalEditAffineTransform | null {
  if (!isFiniteAffineTransform(transform)) return null;
  const determinant = transform.a * transform.d - transform.b * transform.c;
  if (!Number.isFinite(determinant) || Math.abs(determinant) < 1e-12) return null;
  const inverse: LocalEditAffineTransform = {
    a: transform.d / determinant,
    b: -transform.b / determinant,
    c: -transform.c / determinant,
    d: transform.a / determinant,
    e: (transform.c * transform.f - transform.d * transform.e) / determinant,
    f: (transform.b * transform.e - transform.a * transform.f) / determinant,
  };
  return isFiniteAffineTransform(inverse) ? inverse : null;
}

export function applyAffineTransform(
  transform: LocalEditAffineTransform,
  point: LocalEditPoint,
): LocalEditPoint | null {
  if (!isFiniteAffineTransform(transform) || !Number.isFinite(point.x) || !Number.isFinite(point.y)) {
    return null;
  }
  const mapped = {
    x: transform.a * point.x + transform.c * point.y + transform.e,
    y: transform.b * point.x + transform.d * point.y + transform.f,
  };
  return Number.isFinite(mapped.x) && Number.isFinite(mapped.y) ? mapped : null;
}

export function mapDisplayPointToSource(
  point: LocalEditPoint,
  sourceToDisplay: LocalEditAffineTransform,
): LocalEditPoint | null {
  const displayToSource = invertAffineTransform(sourceToDisplay);
  return displayToSource ? applyAffineTransform(displayToSource, point) : null;
}

export function clampSourcePoint(
  point: LocalEditPoint,
  sourceWidth: number,
  sourceHeight: number,
): LocalEditPoint | null {
  if (!Number.isFinite(point.x) || !Number.isFinite(point.y) || sourceWidth < 1 || sourceHeight < 1) {
    return null;
  }
  return {
    x: Math.min(sourceWidth - 1, Math.max(0, point.x)),
    y: Math.min(sourceHeight - 1, Math.max(0, point.y)),
  };
}

export function brushBounds(
  center: LocalEditPoint,
  radius: number,
  sourceWidth: number,
  sourceHeight: number,
): LocalEditBrushBounds | null {
  if (
    !Number.isFinite(center.x)
    || !Number.isFinite(center.y)
    || !Number.isFinite(radius)
    || radius <= 0
    || !Number.isInteger(sourceWidth)
    || !Number.isInteger(sourceHeight)
    || sourceWidth < 1
    || sourceHeight < 1
  ) {
    return null;
  }
  return {
    left: Math.max(0, Math.floor(center.x - radius)),
    top: Math.max(0, Math.floor(center.y - radius)),
    right: Math.min(sourceWidth, Math.ceil(center.x + radius + 1)),
    bottom: Math.min(sourceHeight, Math.ceil(center.y + radius + 1)),
  };
}

export function mergeBrushBounds(
  first: LocalEditBrushBounds | null,
  second: LocalEditBrushBounds | null,
): LocalEditBrushBounds | null {
  if (!first) return second;
  if (!second) return first;
  return {
    left: Math.min(first.left, second.left),
    top: Math.min(first.top, second.top),
    right: Math.max(first.right, second.right),
    bottom: Math.max(first.bottom, second.bottom),
  };
}

export function brushSegmentBounds(
  from: LocalEditPoint,
  to: LocalEditPoint,
  radius: number,
  sourceWidth: number,
  sourceHeight: number,
): LocalEditBrushBounds | null {
  if (
    !Number.isFinite(from.x)
    || !Number.isFinite(from.y)
    || !Number.isFinite(to.x)
    || !Number.isFinite(to.y)
    || !Number.isFinite(radius)
    || radius <= 0
    || !Number.isInteger(sourceWidth)
    || !Number.isInteger(sourceHeight)
    || sourceWidth < 1
    || sourceHeight < 1
  ) {
    return null;
  }
  return {
    left: Math.max(0, Math.floor(Math.min(from.x, to.x) - radius)),
    top: Math.max(0, Math.floor(Math.min(from.y, to.y) - radius)),
    right: Math.min(sourceWidth, Math.ceil(Math.max(from.x, to.x) + radius + 1)),
    bottom: Math.min(sourceHeight, Math.ceil(Math.max(from.y, to.y) + radius + 1)),
  };
}

export function createProtectedMask(sourceWidth: number, sourceHeight: number): Uint8ClampedArray {
  if (!Number.isInteger(sourceWidth) || !Number.isInteger(sourceHeight) || sourceWidth < 1 || sourceHeight < 1) {
    throw new Error("source dimensions must be positive integers");
  }
  const mask = new Uint8ClampedArray(sourceWidth * sourceHeight);
  mask.fill(255);
  return mask;
}

function brushCoverage(distance: number, radius: number, hardness: number): number {
  if (distance >= radius) return 0;
  const normalizedHardness = Math.min(1, Math.max(0, hardness));
  const hardRadius = radius * normalizedHardness;
  if (distance <= hardRadius || distance === 0) return 1;
  const featherWidth = radius - hardRadius;
  return featherWidth > 0 ? (radius - distance) / featherWidth : 1;
}

export function paintMaskRaster(
  mask: Uint8ClampedArray,
  sourceWidth: number,
  sourceHeight: number,
  center: LocalEditPoint,
  radius: number,
  hardness: number,
  eraseSelection: boolean,
): Uint8ClampedArray {
  const bounds = brushBounds(center, radius, sourceWidth, sourceHeight);
  if (!bounds || mask.length !== sourceWidth * sourceHeight) return mask;
  for (let y = bounds.top; y < bounds.bottom; y += 1) {
    for (let x = bounds.left; x < bounds.right; x += 1) {
      const coverage = brushCoverage(Math.hypot(x - center.x, y - center.y), radius, hardness);
      if (coverage <= 0) continue;
      const index = y * sourceWidth + x;
      const paintedAlpha = Math.round(255 * coverage);
      mask[index] = eraseSelection
        ? Math.max(mask[index], paintedAlpha)
        : Math.min(mask[index], 255 - paintedAlpha);
    }
  }
  return mask;
}

export function paintMaskSegment(
  mask: Uint8ClampedArray,
  sourceWidth: number,
  sourceHeight: number,
  from: LocalEditPoint,
  to: LocalEditPoint,
  radius: number,
  hardness: number,
  eraseSelection: boolean,
): Uint8ClampedArray {
  if (!Number.isFinite(radius) || radius <= 0) return mask;
  const distance = Math.hypot(to.x - from.x, to.y - from.y);
  const step = Math.max(0.5, radius * 0.35);
  const count = Math.max(1, Math.ceil(distance / step));
  for (let index = 0; index <= count; index += 1) {
    const progress = index / count;
    paintMaskRaster(
      mask,
      sourceWidth,
      sourceHeight,
      {
        x: from.x + (to.x - from.x) * progress,
        y: from.y + (to.y - from.y) * progress,
      },
      radius,
      hardness,
      eraseSelection,
    );
  }
  return mask;
}

export function summarizeMaskAlpha(mask: Uint8ClampedArray): LocalEditMaskSummary {
  let editPixelCount = 0;
  let protectedPixelCount = 0;
  let featheredPixelCount = 0;
  for (const alpha of mask) {
    if (alpha === 0) editPixelCount += 1;
    else if (alpha === 255) protectedPixelCount += 1;
    else featheredPixelCount += 1;
  }
  return { editPixelCount, protectedPixelCount, featheredPixelCount };
}

export function validateLocalEditFields(
  input: LocalEditValidationInput,
): LocalEditValidationIssue | null {
  if (!isLocalEditOperation(input.operation)) return { code: "unsupported_operation" };
  if (!input.sourceReady) return { code: "source_unavailable" };
  if (
    !Number.isInteger(input.sourceWidth)
    || !Number.isInteger(input.sourceHeight)
    || input.sourceWidth < 1
    || input.sourceHeight < 1
  ) {
    return { code: "invalid_source_dimensions" };
  }
  if (input.operation === "replace_text") {
    if (!input.sourceText?.trim()) return { code: "source_text_required" };
    if (!input.replacementText?.trim()) return { code: "replacement_text_required" };
  } else if (!input.instruction?.trim()) {
    return { code: "instruction_required" };
  }
  if (!input.mask) {
    if (!input.maskSummary) return { code: "mask_required" };
    if (input.maskSummary.editPixelCount < 1 || input.maskSummary.protectedPixelCount < 1) {
      return { code: "mask_needs_edit_and_protection" };
    }
    return null;
  }
  if (input.mask.length !== input.sourceWidth * input.sourceHeight) return { code: "mask_size_mismatch" };
  const summary = input.maskSummary ?? summarizeMaskAlpha(input.mask);
  if (summary.editPixelCount < 1 || summary.protectedPixelCount < 1) {
    return { code: "mask_needs_edit_and_protection" };
  }
  return null;
}
