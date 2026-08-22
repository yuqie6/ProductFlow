import { describe, expect, it } from "vitest";

import { aspectRatioFrameSize, formatAspectRatio, parseAspectRatio } from "./ImageRatioFrame";

describe("image aspect ratio helpers", () => {
  it("parses provider-neutral ratios and rejects malformed values", () => {
    expect(parseAspectRatio("16:9")).toEqual({ width: 16, height: 9 });
    expect(parseAspectRatio(" 4:5 ")).toEqual({ width: 4, height: 5 });
    expect(parseAspectRatio("0:1")).toBeNull();
    expect(parseAspectRatio("1000:1")).toBeNull();
    expect(parseAspectRatio("16/9")).toBeNull();
  });

  it("formats custom ratio inputs without silently coercing invalid values", () => {
    expect(formatAspectRatio("21", "9")).toBe("21:9");
    expect(formatAspectRatio("001", "9")).toBe("1:9");
    expect(formatAspectRatio("", "9")).toBeNull();
    expect(formatAspectRatio("0", "9")).toBeNull();
    expect(formatAspectRatio("1000", "9")).toBeNull();
  });

  it("keeps portrait, square, and landscape frames within stable bounds", () => {
    expect(aspectRatioFrameSize("1:1")).toEqual({ width: 40, height: 40 });
    expect(aspectRatioFrameSize("16:9")).toEqual({ width: 40, height: 23 });
    expect(aspectRatioFrameSize("9:16")).toEqual({ width: 23, height: 40 });
    expect(aspectRatioFrameSize("999:1")).toEqual({ width: 40, height: 18 });
    expect(aspectRatioFrameSize("1:1", "sm")).toEqual({ width: 20, height: 20 });
    expect(aspectRatioFrameSize("9:16", "sm")).toEqual({ width: 11, height: 20 });
  });
});
