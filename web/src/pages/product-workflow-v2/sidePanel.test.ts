import { describe, expect, it } from "vitest";

import type { WorkflowNodeV2 } from "../../lib/types";
import { shouldOpenArtifactsForSelection } from "./sidePanel";

const imageNode = {
  id: "image-node",
  node_type: "image_generation",
} as WorkflowNodeV2;

describe("workflow side panel selection", () => {
  it("opens outputs when a single image node becomes selected", () => {
    expect(shouldOpenArtifactsForSelection([], [imageNode.id], [imageNode])).toBe(true);
  });

  it("keeps the user's active tab when React Flow repeats the same selection", () => {
    expect(
      shouldOpenArtifactsForSelection([imageNode.id], [imageNode.id], [imageNode]),
    ).toBe(false);
  });
});
