import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { WorkflowDraft, WorkflowDraftPayloadV1 } from "../../lib/types";
import { WorkflowDraftConfirmation } from "./WorkflowDraftConfirmation";
import { deriveWorkflowDraftReview } from "./workflowDraftConfirmation";

const generationSpec = {
  aspect_ratio: "1:1",
  resolution_tier: "high",
  quality_intent: "high",
  reference_fidelity: "high",
  background_intent: "opaque",
  text_policy: "required",
  text_language: "zh-CN",
} as const;

function payload(): WorkflowDraftPayloadV1 {
  return {
    schema_version: 1,
    title: "工业收纳商品工作流",
    facts: [{
      key: "price",
      value: "299 元",
      source_type: "user",
      status: "user_declared",
      requires_confirmation: true,
      evidence_asset_ids: ["asset-a"],
      conflicts: [],
    }],
    required_fact_keys: ["price"],
    missing_fact_keys: [],
    reference_bindings: [
      { key: "reference-a", asset_id: "asset-a", role: "product", label: "商品正面" },
      { key: "reference-c", asset_id: "asset-c", role: "detail", label: "结构细节" },
    ],
    visual_system: {
      mode: "draft",
      payload: {
        name: "工业极简视觉体系",
        style: ["专业工业风格", "现代极简主义"],
        colors: [{ role: "primary", value: "#FF6B00", label: "工业警示橙" }],
        typography: {
          title_font: "思源黑体 Bold",
          body_font: "思源黑体 Regular",
          scale: { headline: 3, subtitle: 1.8, body: 1 },
        },
        spacing: { min_edge_whitespace_percent: 30, principles: ["保持边缘留白"] },
        decorations: { elements: ["细线网格"], icon_style: "工程图细线" },
        photography: {
          lighting: "硬调侧逆光",
          depth_of_field: "中等景深",
          camera_parameters: ["f/11", "ISO 100", "85mm"],
        },
        quality: {
          resolution: "4K",
          commercial_grade: "商业广告级",
          realism: "照片级",
          minimum_quality: "high",
        },
        product_fidelity: {
          preserve_shape: true,
          preserve_proportions: true,
          preserve_materials: true,
          requirements: ["像素级还原组合结构"],
        },
        locked_fields: ["style", "colors", "product_fidelity"],
        variants: [{ key: "clean", title: "洁净室", guidance: ["浅灰背景"] }],
        prohibitions: ["不得改变商品结构"],
        reference_assets: [{ asset_id: "asset-a", role: "product", label: "商品正面" }],
      },
      source_markdown: "视觉体系来源文本",
    },
    visual_exceptions: [],
    prompt_plans: [{
      key: "hero-prompt",
      image_type_key: "hero",
      title: "首屏海报提示词",
      payload: {
        schema_version: 1,
        shared_rules: ["保持商品结构"],
        design_goal: "全景展示收纳套装",
        product_fidelity: {
          complex_structure: true,
          product_present: true,
          picture_in_picture: "none",
          requirements: ["严格依原图比例"],
        },
        creative_boundary: ["浅灰工业台面"],
        composition: {
          viewpoint: "俯视45度侧切视角",
          product_share_percent: 75,
          layout: "V 字型环绕排布",
          copy_regions: ["顶部中心", "右侧"],
        },
        content: {
          focus: ["套装完整性"],
          selling_points: ["一套搞定"],
          background: "浅灰色干净背景",
          decorations: ["深蓝色促销角标"],
        },
        text: { headline: "硬质刀具 分类收纳", subtitle: "一站式解决车间杂乱", body: null },
        atmosphere: { keywords: ["专业", "秩序"], lighting: "左上侧向硬光" },
        visual_variant_key: "clean",
        fact_keys: ["price"],
        evidence_asset_ids: ["asset-a"],
        images: [{
          image_plan_key: "hero-1",
          instruction: "主标题靠上",
          viewpoint: "俯视45度",
          composition_adjustments: [],
          lighting: "侧向硬光",
        }],
      },
    }],
    image_types: [
      {
        key: "detail",
        title: "细节展示图",
        order: 0,
        quantity: 1,
        prompt_plan_key: "hero-prompt",
        images: [{ key: "detail-1", order: 0, generation_spec: generationSpec }],
      },
      {
        key: "hero",
        title: "首屏海报图",
        order: 1,
        quantity: 3,
        prompt_plan_key: "hero-prompt",
        images: [0, 1, 2].map((order) => ({
          key: `hero-${order + 1}`,
          order,
          variation_instruction: order ? `候选 ${order + 1}` : null,
          generation_spec: generationSpec,
          delivery_spec: {
            width: 2000,
            height: 2000,
            format: "webp",
            max_byte_size: 2 * 1024 * 1024,
            fit: "contain",
            background_color: "#FFFFFF",
          },
        })),
      },
    ],
    folders: [{
      key: "hero-folder",
      title: "首屏海报图",
      order: 0,
      position_x: 320,
      position_y: 0,
      width: 900,
      height: 600,
    }],
    nodes: [
      { key: "context", title: "商品资料", node_type: "product_context", position_x: 0, position_y: 0 },
      { key: "reference-a", title: "商品正面", node_type: "reference_image", reference_key: "reference-a", position_x: 300, position_y: 0 },
      { key: "prompt", title: "首屏提示词", node_type: "prompt_generation", prompt_plan_key: "hero-prompt", folder_key: "hero-folder", position_x: 600, position_y: 0 },
      { key: "image", title: "首图 1", node_type: "image_generation", image_plan_key: "hero-1", folder_key: "hero-folder", position_x: 900, position_y: 0 },
    ],
    edges: [{ key: "context-prompt", source_node_key: "context", target_node_key: "prompt" }],
    confirmation_summary: "将创建工业风商品图片工作流，请确认结构和文案。",
  };
}

function draft(): WorkflowDraft {
  const currentPayload = payload();
  return {
    id: "draft-1",
    product_id: "product-1",
    status: "awaiting_confirmation",
    current_revision_id: "revision-3",
    current_version: 3,
    current_revision: {
      id: "revision-3",
      draft_id: "draft-1",
      version: 3,
      schema_version: 1,
      payload: currentPayload,
      payload_hash: "hash",
      source_turn_id: "turn-1",
      source_artifact_step_id: "step-1",
      confirmed_at: null,
      fact_set_version_id: null,
      visual_system_version_id: null,
      created_at: "2026-08-14T00:00:00Z",
    },
    revisions: [],
    intake: {
      schema_version: 1,
      image_types: [
        { key: "hero", quantity: 2, order: 0 },
        { key: "scene", quantity: 2, order: 1 },
      ],
      reference_asset_ids: ["asset-a", "asset-b"],
    },
    final_workflow_id: null,
    recipe_seed: null,
    legacy_archive_seed: null,
    limits: {
      min_image_types: 1,
      min_images_per_type: 1,
      max_images_per_type: 6,
      max_total_images: 30,
      max_reference_assets: 6,
    },
    created_at: "2026-08-14T00:00:00Z",
    updated_at: "2026-08-14T00:00:00Z",
  };
}

describe("WorkflowDraftConfirmation", () => {
  it("derives explicit Agent changes from intake without reading assistant prose", () => {
    const source = draft();
    const review = deriveWorkflowDraftReview(source.intake, source.current_revision!.payload);

    expect(review.imageTypes.map((item) => [item.key, item.changes])).toEqual([
      ["detail", ["added"]],
      ["hero", ["quantity", "order"]],
      ["scene", ["removed"]],
    ]);
    expect(review.initialImageCount).toBe(4);
    expect(review.currentImageCount).toBe(4);
    expect(review.addedReferenceAssetIds).toEqual(["asset-c"]);
    expect(review.removedReferenceAssetIds).toEqual(["asset-b"]);
    expect(review.textLanguages).toEqual(["zh-CN"]);
  });

  it("keeps user decisions in the default layer and technical contracts in collapsed advanced details", () => {
    const markup = renderToStaticMarkup(createElement(WorkflowDraftConfirmation, {
      draft: draft(),
      busy: false,
      error: null,
      onConfirm: () => undefined,
      onClose: () => undefined,
    }));
    const primaryStart = markup.indexOf("data-workflow-confirmation-primary");
    const advancedStart = markup.indexOf("data-workflow-confirmation-advanced");
    const advancedTagStart = markup.lastIndexOf("<details", advancedStart);
    const defaultText = visibleText(markup.slice(0, advancedTagStart));
    const primaryText = visibleText(markup.slice(primaryStart, advancedTagStart));
    const advancedText = visibleText(markup.slice(advancedTagStart));

    expect(primaryStart).toBeGreaterThan(-1);
    expect(advancedStart).toBeGreaterThan(primaryStart);
    expect(markup).toContain("草案 v3");
    expect(primaryText).toContain("Agent 新增");
    expect(primaryText).toContain("Agent 移除");
    expect(primaryText).toContain("价格");
    expect(primaryText).toContain("299 元");
    expect(primaryText).toContain("工业极简视觉体系");
    expect(primaryText).toContain("#FF6B00");
    expect(primaryText).toContain("硬质刀具 分类收纳");
    expect(primaryText).toContain("zh-CN");
    expect(primaryText).toContain("商品正面");
    expect(defaultText).toContain("继续修改");
    expect(defaultText).toContain("确认并创建工作流");
    expect(markup).toContain("flex-col gap-4 sm:flex-row");
    expect(markup).toContain("grid-cols-[auto_minmax(0,1fr)]");
    expect(primaryText).not.toContain("asset-a");
    expect(primaryText).not.toContain("hero-prompt");
    expect(primaryText).not.toContain("prompt_generation");
    expect(primaryText).not.toContain("context -> prompt");
    expect(primaryText).not.toContain("节点");

    expect(markup).toContain("<details data-workflow-confirmation-advanced=\"true\" class=\"group");
    expect(advancedText).toContain("高级详情");
    expect(advancedText).toContain("asset-a");
    expect(advancedText).toContain("hero-prompt");
    expect(advancedText).toContain("prompt_generation");
    expect(advancedText).toContain("context -> prompt");
    expect(advancedText).toContain("2000x2000");
  });
});

function visibleText(markup: string): string {
  return markup
    .replace(/<[^>]*>/g, " ")
    .replace(/&gt;/g, ">")
    .replace(/&lt;/g, "<")
    .replace(/&amp;/g, "&")
    .replace(/\s+/g, " ")
    .trim();
}
