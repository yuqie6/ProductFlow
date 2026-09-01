import { describe, expect, it } from "vitest";

import { shouldReadCurrentWorkflowGraph } from "./ProductWorkbenchPage";

describe("ProductWorkbenchPage graph bootstrap", () => {
  it("eventually reads the current graph for products without an Agent graph", () => {
    expect(shouldReadCurrentWorkflowGraph("product-1", null)).toBe(true);
    expect(shouldReadCurrentWorkflowGraph("product-1", { status: 404 })).toBe(true);
    expect(shouldReadCurrentWorkflowGraph("product-1", { status: 409 })).toBe(true);
    expect(shouldReadCurrentWorkflowGraph("", { status: 409 })).toBe(false);
  });

  it("waits for Agent bootstrap before reading graph and reuses a bootstrapped graph", () => {
    expect(shouldReadCurrentWorkflowGraph("product-1", undefined, true, false)).toBe(false);
    expect(shouldReadCurrentWorkflowGraph("product-1", null, false, true)).toBe(false);
    expect(shouldReadCurrentWorkflowGraph("product-1", null, false, false)).toBe(true);
  });
});
