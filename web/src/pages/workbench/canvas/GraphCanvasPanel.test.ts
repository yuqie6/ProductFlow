import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { zhCN } from "../../../lib/i18n";
import type { GraphNodeCatalog, GraphProjection } from "../../../lib/types";
import { GraphCanvasNotice, graphHistoryShortcutAction } from "./GraphCanvasPanel";
import graphCanvasPanelSource from "./GraphCanvasPanel.tsx?raw";
import { graphConnectionInvalidReason } from "./graphCatalog";

describe("graphHistoryShortcutAction", () => {
  it("maps undo and redo to dedicated actions", () => {
    expect(graphHistoryShortcutAction("undo")).toBe("undo");
    expect(graphHistoryShortcutAction("redo")).toBe("redo");
    expect(graphHistoryShortcutAction("delete")).toBeNull();
  });
});

describe("GraphCanvasNotice", () => {
  it("renders the illegal-connect reason from the catalog helper", () => {
    const catalog: GraphNodeCatalog = {
      version: 1,
      nodes: [
        { node_type: "product_source", output_data_type: "product_facts", kind: "source", accepts: [] },
        {
          node_type: "image_generation",
          output_data_type: "image_asset",
          kind: "effect",
          accepts: [{ data_type: "prompt", role: "prompt", max_count: 1, required_to_run: true }],
        },
      ],
    };
    const graph = {
      nodes: [
        {
          id: "source",
          node_type: "product_source",
          incoming: [],
          outgoing: [],
        },
        {
          id: "image",
          node_type: "image_generation",
          incoming: [],
          outgoing: [],
        },
      ],
      edges: [],
    } as unknown as GraphProjection;
    const reason = graphConnectionInvalidReason(graph, "source", "image", catalog);
    expect(reason).toBe("graph.connect.incompatible");
    const markup = renderToStaticMarkup(createElement(GraphCanvasNotice, {
      notice: zhCN[reason!],
    }));
    expect(markup).toContain("data-graph-canvas-notice");
    expect(markup).toContain("这两种节点不能相连");
    expect(renderToStaticMarkup(createElement(GraphCanvasNotice, { notice: null }))).toBe("");
  });
});

describe("commitNode baseline", () => {
  it("sends the inspector draft revision and does not replay document saves on 409", () => {
    expect(graphCanvasPanelSource).toContain("baseGraphRevision: input.baseGraphRevision");
    expect(graphCanvasPanelSource).toContain("return await applyMutation.mutateAsync(changeSet);");
    expect(graphCanvasPanelSource).toMatch(/commitNode[\s\S]{0,1600}graph\.canvas\.revisionConflict/);
    expect(graphCanvasPanelSource).not.toMatch(/commitNode[\s\S]{0,1200}base_graph_revision:\s*graphRef\.current\.revision/);
    expect(graphCanvasPanelSource).not.toMatch(/commitNode[\s\S]{0,1200}executeApply\(/);
  });
});
