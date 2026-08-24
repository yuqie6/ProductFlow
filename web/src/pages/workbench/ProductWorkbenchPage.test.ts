import { describe, expect, it } from "vitest";

import { shouldReadCurrentWorkflowGraph } from "./ProductWorkbenchPage";

describe("ProductWorkbenchPage graph bootstrap", () => {
  it("reads the current graph for every product, whether Agent workbench exists or not", () => {
    expect(shouldReadCurrentWorkflowGraph("product-1", null)).toBe(true);
    expect(shouldReadCurrentWorkflowGraph("product-1", { status: 404 })).toBe(true);
    expect(shouldReadCurrentWorkflowGraph("product-1", { status: 409 })).toBe(true);
    expect(shouldReadCurrentWorkflowGraph("", { status: 409 })).toBe(false);
  });
});
