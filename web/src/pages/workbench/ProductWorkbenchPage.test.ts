import { describe, expect, it } from "vitest";

import { shouldReadCurrentWorkflowGraph } from "./ProductWorkbenchPage";

describe("ProductWorkbenchPage graph bootstrap", () => {
  it("does not read current graph for the normal Agent bootstrap, including no live graph", () => {
    expect(shouldReadCurrentWorkflowGraph("product-1", null)).toBe(false);
    expect(shouldReadCurrentWorkflowGraph("product-1", { status: 404 })).toBe(false);
  });

  it("reads current graph only for the legacy no-Agent fallback", () => {
    expect(shouldReadCurrentWorkflowGraph("product-1", { status: 409 })).toBe(true);
    expect(shouldReadCurrentWorkflowGraph("", { status: 409 })).toBe(false);
  });
});
