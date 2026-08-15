import { describe, expect, it } from "vitest";

import type { LegacyArchiveListItem } from "../../lib/types";
import {
  boundedJson,
  isLegacyArchiveKind,
  isLegacyArchiveMediaAvailable,
  legacyArchiveDetailPath,
  legacyArchiveExportFilename,
  payloadRecords,
} from "./model";

describe("legacy history model", () => {
  it("validates kinds and preserves list filters in detail URLs", () => {
    expect(isLegacyArchiveKind("workflow")).toBe(true);
    expect(isLegacyArchiveKind("unknown")).toBe(false);
    expect(
      legacyArchiveDetailPath(
        "canvas_agent_thread",
        "archive/1",
        new URLSearchParams({ product_id: "product 1", q: "橙蓝" }),
      ),
    ).toBe("/history/canvas_agent_thread/archive%2F1?product_id=product+1&q=%E6%A9%99%E8%93%9D");
  });

  it("exposes archive media actions only for verified canonical files", () => {
    expect(isLegacyArchiveMediaAvailable("verified")).toBe(true);
    expect(isLegacyArchiveMediaAvailable("legacy_pending")).toBe(false);
    expect(isLegacyArchiveMediaAvailable("missing")).toBe(false);
  });

  it("extracts object records and bounds large JSON previews", () => {
    expect(payloadRecords({ nodes: [{ id: "1" }, null, "invalid"] }, "nodes")).toEqual([{ id: "1" }]);
    expect(boundedJson({ prompt: "x".repeat(20) }, 12)).toBe('{\n  "prompt"\n...');
  });

  it("builds a filesystem-safe export filename", () => {
    const item = {
      kind: "workflow",
      id: "archive-1",
      title: "主图/场景图",
    } as LegacyArchiveListItem;
    expect(legacyArchiveExportFilename(item)).toBe("主图-场景图-workflow-archive-1.json");
  });
});
