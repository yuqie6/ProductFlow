import { describe, expect, it } from "vitest";

import { GRAPH_PORT_MAX_VISUAL_SCALE, graphEdgeDeleteClassName, graphEdgeEmphasis, graphPortVisualScale } from "./graphCanvasVisual";

describe("graphCanvasVisual", () => {
  it("caps port scale at low zoom", () => {
    expect(graphPortVisualScale(0.15)).toBe(GRAPH_PORT_MAX_VISUAL_SCALE);
    expect(GRAPH_PORT_MAX_VISUAL_SCALE).toBe(1.4);
  });

  it("marks unselected edges as receded", () => {
    expect(graphEdgeEmphasis({
      edgeSelected: false,
      sourceSelected: false,
      targetSelected: false,
    })).toBe("receded");
  });

  it("keeps selected-edge delete visible without hover", () => {
    expect(graphEdgeDeleteClassName(true, false)).toContain("opacity-100");
    expect(graphEdgeDeleteClassName(true, false)).toContain("!pointer-events-auto");
    expect(graphEdgeDeleteClassName(true, false)).not.toContain("opacity-0");
    expect(graphEdgeDeleteClassName(false, false)).toContain("opacity-0");
    expect(graphEdgeDeleteClassName(false, false)).toContain("!pointer-events-none");
  });
});
