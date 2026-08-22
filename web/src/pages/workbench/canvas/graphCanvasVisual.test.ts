import { describe, expect, it } from "vitest";

import { GRAPH_PORT_MAX_VISUAL_SCALE, graphEdgeEmphasis, graphPortVisualScale } from "./graphCanvasVisual";

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
});
