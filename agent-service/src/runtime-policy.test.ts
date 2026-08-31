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
    expect(policy).toContain("text enclosed in quotation marks as the user's literal value");
    expect(policy).toContain("Never end a Turn with a question only in assistant prose");
  });

  it("uses the bundled policy even when the source path is unavailable", () => {
    expect(loadRuntimePolicy("/tmp/productflow-missing-policy/runtime-policy.ts")).toContain("business authority");
  });
});
