import { describe, expect, it } from "vitest";

import {
  DEFAULT_IMAGE_SIZE_OPTIONS,
  buildImageSizeOptions,
  labelForImageSize,
  normalizeImageSizeValue,
  parseImageSizeValue,
  resolveImageSize,
} from "./imageSizes";

describe("image size helpers", () => {
  it("provides built-in ratio/tier presets without runtime config", () => {
    expect(DEFAULT_IMAGE_SIZE_OPTIONS.map((option) => option.value)).toEqual([
      "1024x1024",
      "1024x1536",
      "1536x1024",
      "2048x2048",
      "2048x3072",
      "3072x2048",
      "3840x3840",
      "2160x3840",
      "3840x2160",
    ]);
    expect(DEFAULT_IMAGE_SIZE_OPTIONS[3]).toMatchObject({ label: "方图 · 2K", aspect: "1:1" });
    expect(DEFAULT_IMAGE_SIZE_OPTIONS.at(-1)?.description).toBe("16:9 · 3840×2160");
  });

  it("normalizes and parses custom size strings", () => {
    expect(normalizeImageSizeValue("3840X2160")).toBe("3840x2160");
    expect(parseImageSizeValue("1280x720")).toEqual({ width: 1280, height: 720 });
    expect(labelForImageSize("1280x720")).toBe("自定义 · 1280×720");
    expect(normalizeImageSizeValue("0x720")).toBeNull();
    expect(normalizeImageSizeValue("1024 * 1024")).toBeNull();
  });

  it("calibrates oversized dimensions to project safety bounds", () => {
    expect(resolveImageSize(5000, 2500)).toEqual({
      width: 3840,
      height: 1920,
      value: "3840x1920",
      calibrated: true,
    });
    expect(resolveImageSize(4000, 4000)).toEqual({
      width: 3840,
      height: 3840,
      value: "3840x3840",
      calibrated: true,
    });
    expect(resolveImageSize(100, 0)).toBeNull();
  });

  it("filters built-in presets by runtime max dimension", () => {
    expect(buildImageSizeOptions(2048).map((option) => option.value)).toEqual([
      "1024x1024",
      "1024x1536",
      "1536x1024",
      "2048x2048",
    ]);
    expect(resolveImageSize(3072, 2048, 2048)).toEqual({
      width: 2048,
      height: 1365,
      value: "2048x1365",
      calibrated: true,
    });
    expect(normalizeImageSizeValue("3840X2160", 2048)).toBe("2048x1152");
  });
});
