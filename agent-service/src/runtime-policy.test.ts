import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { loadRuntimePolicy, runtimePolicyPath } from "./runtime-policy.js";

describe("ProductFlow runtime policy", () => {
  it("loads the shared markdown beside the Go module", () => {
    const path = runtimePolicyPath();
    expect(path.endsWith(join("go", "prompts", "agent", "runtime-policy.md"))).toBe(true);
    const policy = loadRuntimePolicy();
    expect(policy).toContain("ProductFlow runtime policy:");
    expect(policy).toContain("business authority");
    expect(policy).toContain("Pi has no operating-system tools");
  });

  it("fails when the file is missing", () => {
    expect(() => loadRuntimePolicy("/tmp/productflow-missing-policy/runtime-policy.ts")).toThrow();
  });
});
