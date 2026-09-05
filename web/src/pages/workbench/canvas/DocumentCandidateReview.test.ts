import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { GraphDocumentCandidate } from "../../../lib/types";
import { DocumentCandidateReview } from "./DocumentCandidateReview";

const candidate: GraphDocumentCandidate = {
  artifact_id: "candidate-1",
  node_id: "prompt",
  document_action: "rewrite",
  status: "ready",
  base_document_hash: "a".repeat(64),
  input_digest: "b".repeat(64),
  current_document: { design_goal: "保留真实材质与产品结构" },
  candidate_document: { design_goal: "突出杯身纹理与便携卖点" },
  sections: [
    {
      key: "objective",
      changed: true,
      current: { design_goal: "保留真实材质与产品结构" },
      candidate: { design_goal: "突出杯身纹理与便携卖点" },
    },
    {
      key: "composition",
      changed: true,
      current: { composition: { layout: "居中", product_share_percent: 65, copy_regions: [] } },
      candidate: { composition: { layout: "左侧主体，右侧留白", product_share_percent: 58, copy_regions: ["顶栏标题"] } },
    },
  ],
  created_at: "2026-09-01T00:00:00.000Z",
};

describe("DocumentCandidateReview", () => {
  it("shows translated field labels instead of raw JSON", () => {
    const markup = renderToStaticMarkup(createElement(DocumentCandidateReview, {
      candidate,
      loading: false,
      error: null,
      fields: [],
      selectedKeys: ["objective", "composition"],
      busy: false,
      onToggle: () => undefined,
      onApplySelected: () => undefined,
      onApplyAll: () => undefined,
      onDiscard: () => undefined,
      onRetry: () => undefined,
    }));
    expect(markup).toContain("设计目标");
    expect(markup).toContain("布局");
    expect(markup).toContain("商品近似占比");
    expect(markup).toContain("文案区域");
    expect(markup).toContain("保留真实材质与产品结构");
    expect(markup).toContain("左侧主体，右侧留白");
    expect(markup).toContain("顶栏标题");
    expect(markup).toContain("未填写");
    expect(markup).not.toContain("design_goal:");
    expect(markup).not.toContain('"layout"');
    expect(markup).not.toContain('"copy_regions"');
    expect(markup).not.toContain("product_share_percent:");
    expect(markup).not.toContain('type="checkbox"');
    expect(markup).toContain('aria-pressed="true"');
    expect(markup).toContain("目标");
    expect(markup).toContain("构图");
  });
});
