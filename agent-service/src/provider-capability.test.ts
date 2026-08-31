import { describe, expect, it } from "vitest";
import { ADAPTER_BACKGROUND_RESUMABLE, effectiveBackgroundResumable } from "./provider-capability.js";

describe("provider background capability", () => {
  it("keeps the Pi adapter fixed false even when the profile claims background", () => {
    expect(ADAPTER_BACKGROUND_RESUMABLE).toBe(false);
    expect(effectiveBackgroundResumable(true)).toBe(false);
    expect(effectiveBackgroundResumable(false)).toBe(false);
    expect(effectiveBackgroundResumable(undefined)).toBe(false);
  });
});
