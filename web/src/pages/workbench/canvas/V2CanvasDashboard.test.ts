import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type {
  CanonicalProductDetail,
  ProductWorkflowV2,
  WorkflowDraftProductFact,
  WorkflowNodeV2,
} from "../../../lib/types";
import { V2CanvasDashboard } from "./V2CanvasDashboard";

const mockProduct: CanonicalProductDetail = {
  id: "prod-1",
  name: "高端羊绒大衣",
  category: "女装 / 外套",
  price: "2999",
  source_note: "秋冬新款",
  cover_image_asset_id: null,
  created_at: "2026-08-15T00:00:00Z",
  updated_at: "2026-08-15T00:00:00Z",
};

const makeNode = (
  id: string,
  type: WorkflowNodeV2["node_type"],
  title: string,
  status: WorkflowNodeV2["status"] = "succeeded",
  folderId: string | null = null,
): WorkflowNodeV2 => ({
  id,
  workflow_id: "wf-1",
  schema_version: 2,
  key: id,
  node_type: type,
  title,
  position_x: 0,
  position_y: 0,
  folder_id: folderId,
  bound_image_asset_id: null,
  current_prompt_artifact_version_id: null,
  config_json: {},
  status,
  output_json: null,
  failure_reason: null,
  created_at: "2026-08-15T00:00:00Z",
  updated_at: "2026-08-15T00:00:00Z",
});

const mockWorkflow: ProductWorkflowV2 = {
  id: "wf-1",
  product_id: "prod-1",
  title: "冬季上新主推工作流",
  active: true,
  schema_version: 2,
  revision: 3,
  edit_version: 7,
  source_draft_revision_id: "draft-rev-1",
  visual_system_version_id: "vs-1",
  materialization_id: "mat-1",
  folders: [
    {
      id: "folder-1",
      workflow_id: "wf-1",
      key: "folder-1",
      order: 0,
      title: "模特街拍场景组",
      created_at: "2026-08-15T00:00:00Z",
      updated_at: "2026-08-15T00:00:00Z",
    },
  ],
  nodes: [
    makeNode("node-fact", "product_context", "商品核心事实"),
    makeNode("node-ref", "reference_image", "模特姿态参考"),
    makeNode("node-prompt", "prompt_generation", "街拍光影 Prompt 生成器", "succeeded", "folder-1"),
    makeNode("node-img-1", "image_generation", "3:4 街拍全身图", "succeeded", "folder-1"),
    makeNode("node-img-2", "image_generation", "9:16 社媒海报", "running", "folder-1"),
  ],
  edges: [],
  created_at: "2026-08-15T00:00:00Z",
  updated_at: "2026-08-15T00:00:00Z",
};

const mockFacts: WorkflowDraftProductFact[] = [
  {
    key: "material",
    value: "100% 澳洲美利奴双面羊绒",
    source_type: "user",
    status: "confirmed",
    requires_confirmation: false,
    evidence_asset_ids: [],
    conflicts: [],
  },
  {
    key: "selling_point",
    value: "轻盈保暖，手工双面缝制，经典落肩廓形",
    source_type: "user",
    status: "confirmed",
    requires_confirmation: false,
    evidence_asset_ids: [],
    conflicts: [],
  },
];

describe("V2CanvasDashboard", () => {
  it("renders workflow metrics and status breakdown correctly", () => {
    const markup = renderToStaticMarkup(createElement(V2CanvasDashboard, {
      product: mockProduct,
      workflow: mockWorkflow,
      facts: mockFacts,
      onOpenAddPanel: () => undefined,
      onOpenLibraryPanel: () => undefined,
      onRunWorkflow: () => undefined,
    }));

    // Workflow header and badge
    expect(markup).toContain("冬季上新主推工作流");
    expect(markup).toContain("v3 · rev 7");

    // Metric cards
    expect(markup).toContain("事实");
    expect(markup).toContain('data-v2-canvas-dashboard-fact-count="2"');
    expect(markup).toContain("参考图");
    expect(markup).toContain("Prompt");
    expect(markup).toContain("生图通道");
    // Localized node/folder summary (not hardcoded Chinese)
    expect(markup).toContain("共 5 个节点 / 1 个文件夹");
    expect(markup).not.toContain("共 {nodes} 个节点");

    // Quick action buttons
    expect(markup).toContain("执行全部节点");
    expect(markup).toContain("添加新节点/模块");
    expect(markup).toContain("打开素材图库");

    // Product facts summary
    expect(markup).toContain("高端羊绒大衣");
    expect(markup).toContain("100% 澳洲美利奴双面羊绒");
    expect(markup).toContain("轻盈保暖，手工双面缝制，经典落肩廓形");
  });

  it("reports the real fact count instead of a hardcoded number", () => {
    const markup = renderToStaticMarkup(createElement(V2CanvasDashboard, {
      product: mockProduct,
      workflow: mockWorkflow,
      facts: [],
      onOpenAddPanel: () => undefined,
      onOpenLibraryPanel: () => undefined,
      onRunWorkflow: () => undefined,
    }));

    expect(markup).toContain('data-v2-canvas-dashboard-fact-count="0"');
    expect(markup).toContain("共 5 个节点 / 1 个文件夹");
  });
});
