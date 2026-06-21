import { DEFAULT_LOCALE, type Locale } from "./i18n";
import type { CanvasTemplateSummary, WorkflowNodeType } from "./types";

const CANVAS_TEMPLATE_SOURCE_LOCALE: Locale = "zh-CN";

interface BuiltInCanvasTemplateText {
  title: string;
  description: string;
  scenarioTitle: string;
  scenarioDescription: string;
  nodes: Record<string, string>;
  outputSlots: Record<string, string>;
  referenceInputHints?: Record<string, string>;
}

const DEFAULT_EXTERNAL_CONNECTION_LABELS: Record<string, string> = {
  "自动接商品": "Auto-connect product",
};

const BUILT_IN_TEMPLATE_TEXT: Record<string, BuiltInCanvasTemplateText> = {
  "ecommerce-main-image-v1": {
    title: "E-commerce main image",
    description: "Generate a product hero image with a clear subject, benefits, and composition.",
    scenarioTitle: "Main image",
    scenarioDescription: "For product listings and the first screen of the detail page.",
    nodes: {
      product: "Product info",
      copy: "Main-image benefits",
      image: "Generate main image",
      output: "Main image output",
      refine: "Refine main image",
      refined_output: "Refined output",
    },
    outputSlots: {
      output: "Main image output",
      refined_output: "Refined output",
    },
  },
  "ecommerce-taobao-main-image-v1": {
    title: "Taobao main image",
    description: "Generate a 1:1 product main image for Taobao search, recommendations, and detail entry.",
    scenarioTitle: "Taobao main image",
    scenarioDescription: "For Taobao listing traffic and the detail first screen, with a clear subject and benefits.",
    nodes: {
      product: "Product info",
      angle: "Search benefits",
      main: "Main image version",
      main_output: "Taobao main image",
      clean: "Clean version",
      clean_output: "Clean main image",
    },
    outputSlots: {
      main_output: "Taobao main image",
      clean_output: "Clean main image",
    },
  },
  "ecommerce-xiaohongshu-image-v1": {
    title: "Xiaohongshu image",
    description: "Generate a vertical lifestyle image for Xiaohongshu note covers and content seeding.",
    scenarioTitle: "Xiaohongshu",
    scenarioDescription: "For Xiaohongshu note covers, content seeding, and lifestyle display.",
    nodes: {
      style_reference: "Note style reference",
      product: "Product info",
      angle: "Cover angle",
      cover: "Vertical cover",
      cover_output: "Cover output",
      detail: "Content image",
      detail_output: "Content image output",
    },
    outputSlots: {
      cover_output: "Cover output",
      detail_output: "Content image output",
    },
    referenceInputHints: {
      style_reference: "Note style reference",
    },
  },
  "ecommerce-multi-angle-image-v1": {
    title: "Multi-angle images",
    description: "Generate front, side, and back or structure images for a complete detail gallery.",
    scenarioTitle: "Multi-angle",
    scenarioDescription: "For detail-page carousels that show appearance, structure, and back-side details.",
    nodes: {
      product: "Product info",
      angle_plan: "Angle plan",
      front_image: "Front angle",
      front_output: "Front output",
      side_image: "Side angle",
      side_output: "Side output",
      back_image: "Back / structure",
      back_output: "Structure output",
    },
    outputSlots: {
      front_output: "Front output",
      side_output: "Side output",
      back_output: "Structure output",
    },
  },
  "ecommerce-sku-variant-image-v1": {
    title: "SKU / variant images",
    description: "Generate differentiated visuals for color, specification, or bundle SKUs.",
    scenarioTitle: "SKU / variant",
    scenarioDescription: "For explaining specification differences in the product detail page.",
    nodes: {
      sku_reference: "SKU reference",
      product: "Product info",
      variant_copy: "Variant differences",
      single_variant: "Single SKU image",
      single_variant_output: "SKU image output",
      variant_grid: "Variant comparison",
      variant_grid_output: "Variant comparison output",
    },
    outputSlots: {
      single_variant_output: "SKU image output",
      variant_grid_output: "Variant comparison output",
    },
    referenceInputHints: {
      sku_reference: "SKU reference",
    },
  },
  "ecommerce-feature-infographic-v1": {
    title: "Feature infographic",
    description: "Turn core product functions into a structured infographic for detail-page persuasion.",
    scenarioTitle: "Feature highlights",
    scenarioDescription: "For detail-page feature explanation, function entry points, and conversion support.",
    nodes: {
      product: "Product info",
      feature_copy: "Benefit extraction",
      layout_copy: "Information hierarchy",
      infographic: "Feature infographic",
      infographic_output: "Feature graphic output",
    },
    outputSlots: {
      infographic_output: "Feature graphic output",
    },
  },
  "ecommerce-size-spec-image-v1": {
    title: "Size / spec image",
    description: "Generate size, capacity, material, and parameter graphics to reduce pre-purchase questions.",
    scenarioTitle: "Size / spec",
    scenarioDescription: "For explaining parameters, size, capacity, and specifications on the detail page.",
    nodes: {
      product: "Product info",
      spec_copy: "Spec organization",
      dimension_image: "Dimension annotation",
      dimension_output: "Dimension output",
      spec_table_image: "Parameter graphic",
      spec_table_output: "Parameter output",
    },
    outputSlots: {
      dimension_output: "Dimension output",
      spec_table_output: "Parameter output",
    },
  },
  "ecommerce-scale-reference-image-v1": {
    title: "Scale reference image",
    description: "Show real size through hand-held, worn, tabletop, or spatial references.",
    scenarioTitle: "Scale reference",
    scenarioDescription: "For explaining size, thickness, capacity, and real-life fit or placement.",
    nodes: {
      scale_reference: "Scale reference",
      product: "Product info",
      scale_copy: "Scale notes",
      handheld_image: "Hand-held / worn reference",
      handheld_output: "Scale image output",
      surface_image: "Tabletop / spatial reference",
      surface_output: "Spatial reference output",
    },
    outputSlots: {
      handheld_output: "Scale image output",
      surface_output: "Spatial reference output",
    },
    referenceInputHints: {
      scale_reference: "Scale reference",
    },
  },
  "ecommerce-package-checklist-image-v1": {
    title: "Package / checklist image",
    description: "Show packaging, accessories, gifts, and included items to reduce pre-sale questions.",
    scenarioTitle: "Package checklist",
    scenarioDescription: "For detail-page included items, accessory counts, and gift-box display.",
    nodes: {
      package_reference: "Packaging reference",
      product: "Product info",
      checklist_copy: "Checklist copy",
      flatlay_image: "Package flat lay",
      flatlay_output: "Checklist output",
      gift_image: "Gift-box / unboxing image",
      gift_output: "Unboxing output",
    },
    outputSlots: {
      flatlay_output: "Checklist output",
      gift_output: "Unboxing output",
    },
    referenceInputHints: {
      package_reference: "Packaging reference",
    },
  },
  "ecommerce-usage-steps-image-v1": {
    title: "Usage steps image",
    description: "Generate installation, unboxing, wearing, or cleaning step graphics.",
    scenarioTitle: "Usage steps",
    scenarioDescription: "For installation guides, tutorials, cleaning maintenance, and pre-support guidance.",
    nodes: {
      step_reference: "Step reference",
      product: "Product info",
      step_copy: "Step breakdown",
      step_image: "Step instruction graphic",
      step_output: "Step output",
      tip_copy: "Important notes",
      tip_image: "Important notes graphic",
      tip_output: "Important notes output",
    },
    outputSlots: {
      step_output: "Step output",
      tip_output: "Important notes output",
    },
    referenceInputHints: {
      step_reference: "Step reference",
    },
  },
  "ecommerce-comparison-image-v1": {
    title: "Comparison image",
    description: "Generate comparison graphics against older versions, regular versions, competitors, or bundles.",
    scenarioTitle: "Comparison",
    scenarioDescription: "For explaining upgrades, bundle differences, and purchase-decision dimensions.",
    nodes: {
      compare_reference: "Comparison reference",
      product: "Product info",
      comparison_copy: "Comparison dimensions",
      comparison_image: "Comparison graphic",
      comparison_output: "Comparison output",
      upgrade_image: "Upgrade highlights graphic",
      upgrade_output: "Upgrade highlights output",
    },
    outputSlots: {
      comparison_output: "Comparison output",
      upgrade_output: "Upgrade highlights output",
    },
    referenceInputHints: {
      compare_reference: "Comparison reference",
    },
  },
  "ecommerce-model-lifestyle-image-v1": {
    title: "Model / lifestyle image",
    description: "Generate product scene images with people, outfits, or lifestyle atmosphere.",
    scenarioTitle: "Model / lifestyle",
    scenarioDescription: "For apparel, beauty, home, and other categories that need usage context.",
    nodes: {
      style: "Pose / style reference",
      product: "Product info",
      copy: "Audience and scene",
      half_body: "Half-body / usage image",
      half_body_output: "Lifestyle image",
      detail_usage: "Usage detail image",
      detail_usage_output: "Usage detail output",
    },
    outputSlots: {
      half_body_output: "Lifestyle image",
      detail_usage_output: "Usage detail output",
    },
    referenceInputHints: {
      style: "Pose / style reference",
    },
  },
  "ecommerce-scene-image-v1": {
    title: "Scene image",
    description: "Place the product into an understandable usage space or business scene.",
    scenarioTitle: "Scene",
    scenarioDescription: "For explaining usage environment, styling, and spatial relationships.",
    nodes: {
      scene_reference: "Scene reference",
      product: "Product info",
      copy: "Scene notes",
      wide_scene: "Wide scene",
      scene_output: "Scene image output",
    },
    outputSlots: {
      scene_output: "Scene image output",
    },
    referenceInputHints: {
      scene_reference: "Scene reference",
    },
  },
  "ecommerce-detail-material-image-v1": {
    title: "Detail / material image",
    description: "Generate material, craft, local structure, or feature detail images.",
    scenarioTitle: "Detail / material",
    scenarioDescription: "For explaining material, craft, and key functions on the detail page.",
    nodes: {
      detail_reference: "Detail reference",
      product: "Product info",
      detail_copy: "Detail notes",
      macro_image: "Material close-up",
      macro_output: "Detail image output",
      structure_image: "Structure explanation graphic",
      structure_output: "Structure output",
    },
    outputSlots: {
      macro_output: "Detail image output",
      structure_output: "Structure output",
    },
    referenceInputHints: {
      detail_reference: "Detail reference",
    },
  },
  "ecommerce-campaign-promotion-image-v1": {
    title: "Campaign / promotion image",
    description: "Generate product images for campaign entry points, offer expression, and promotional atmosphere.",
    scenarioTitle: "Campaign / promotion",
    scenarioDescription: "For campaign pages, promotional placements, and in-site ad assets.",
    nodes: {
      campaign_style: "Campaign style reference",
      product: "Product info",
      offer_copy: "Offer information",
      visual_copy: "Visual hierarchy",
      banner: "Campaign banner",
      banner_output: "Campaign image output",
    },
    outputSlots: {
      banner_output: "Campaign image output",
    },
    referenceInputHints: {
      campaign_style: "Campaign style reference",
    },
  },
  "ecommerce-short-video-cover-v1": {
    title: "Short-video cover",
    description: "Generate vertical covers for short-video entry points, content feeds, and live previews.",
    scenarioTitle: "Short-video cover",
    scenarioDescription: "For in-site short videos, content feeds, live previews, and ad entry points.",
    nodes: {
      cover_style: "Cover style reference",
      product: "Product info",
      hook_copy: "Cover hook",
      frame_copy: "Frame rhythm",
      vertical_cover: "Vertical cover",
      vertical_cover_output: "Short-video cover output",
      closeup_cover: "Close-up cover",
      closeup_cover_output: "Close-up cover output",
    },
    outputSlots: {
      vertical_cover_output: "Short-video cover output",
      closeup_cover_output: "Close-up cover output",
    },
    referenceInputHints: {
      cover_style: "Cover style reference",
    },
  },
  "ecommerce-white-background-image-v1": {
    title: "White-background image",
    description: "Generate a white-background product image for marketplace rules, cutouts, or base displays.",
    scenarioTitle: "White background",
    scenarioDescription: "For platform base product images, spec graphics, and reusable assets.",
    nodes: {
      product_reference: "Subject reference",
      product: "Product info",
      clean_copy: "White-background requirements",
      white_image: "Standard white-background image",
      white_output: "White-background output",
      shadow_image: "Light-shadow display image",
      shadow_output: "Display output",
    },
    outputSlots: {
      white_output: "White-background output",
      shadow_output: "Display output",
    },
    referenceInputHints: {
      product_reference: "Subject reference",
    },
  },
};

const BUILT_IN_TEMPLATE_SOURCE_TEXT: Record<
  string,
  {
    nodes: Record<string, { nodeType: WorkflowNodeType; title: string }>;
    outputSlots: Record<string, string>;
    referenceInputHints?: Record<string, string>;
    defaultExternalConnections?: Record<string, string>;
  }
> = {
  "ecommerce-main-image-v1": {
    nodes: {
      product: { nodeType: "product_context", title: "商品资料" },
      copy: { nodeType: "copy_generation", title: "主图卖点" },
      image: { nodeType: "image_generation", title: "生成主图" },
      output: { nodeType: "reference_image", title: "主图输出" },
      refine: { nodeType: "image_generation", title: "细化主图" },
      refined_output: { nodeType: "reference_image", title: "细化输出" },
    },
    outputSlots: { output: "主图输出", refined_output: "细化输出" },
  },
  "ecommerce-taobao-main-image-v1": {
    nodes: {
      product: { nodeType: "product_context", title: "商品资料" },
      angle: { nodeType: "copy_generation", title: "搜索卖点" },
      main: { nodeType: "image_generation", title: "主图版本" },
      main_output: { nodeType: "reference_image", title: "淘宝主图" },
      clean: { nodeType: "image_generation", title: "干净版本" },
      clean_output: { nodeType: "reference_image", title: "干净主图" },
    },
    outputSlots: { main_output: "淘宝主图", clean_output: "干净主图" },
  },
  "ecommerce-xiaohongshu-image-v1": {
    nodes: {
      style_reference: { nodeType: "reference_image", title: "笔记风格参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      angle: { nodeType: "copy_generation", title: "封面角度" },
      cover: { nodeType: "image_generation", title: "竖版封面" },
      cover_output: { nodeType: "reference_image", title: "封面输出" },
      detail: { nodeType: "image_generation", title: "内容配图" },
      detail_output: { nodeType: "reference_image", title: "配图输出" },
    },
    outputSlots: { cover_output: "封面输出", detail_output: "配图输出" },
    referenceInputHints: { style_reference: "笔记风格参考" },
  },
  "ecommerce-multi-angle-image-v1": {
    nodes: {
      product: { nodeType: "product_context", title: "商品资料" },
      angle_plan: { nodeType: "copy_generation", title: "角度规划" },
      front_image: { nodeType: "image_generation", title: "正面角度" },
      front_output: { nodeType: "reference_image", title: "正面输出" },
      side_image: { nodeType: "image_generation", title: "侧面角度" },
      side_output: { nodeType: "reference_image", title: "侧面输出" },
      back_image: { nodeType: "image_generation", title: "背面/结构" },
      back_output: { nodeType: "reference_image", title: "结构输出" },
    },
    outputSlots: { front_output: "正面输出", side_output: "侧面输出", back_output: "结构输出" },
  },
  "ecommerce-sku-variant-image-v1": {
    nodes: {
      sku_reference: { nodeType: "reference_image", title: "SKU 参考图" },
      product: { nodeType: "product_context", title: "商品资料" },
      variant_copy: { nodeType: "copy_generation", title: "变体差异" },
      single_variant: { nodeType: "image_generation", title: "单 SKU 图" },
      single_variant_output: { nodeType: "reference_image", title: "SKU 图输出" },
      variant_grid: { nodeType: "image_generation", title: "变体对照" },
      variant_grid_output: { nodeType: "reference_image", title: "变体对照输出" },
    },
    outputSlots: { single_variant_output: "SKU 图输出", variant_grid_output: "变体对照输出" },
    referenceInputHints: { sku_reference: "SKU 参考图" },
  },
  "ecommerce-feature-infographic-v1": {
    nodes: {
      product: { nodeType: "product_context", title: "商品资料" },
      feature_copy: { nodeType: "copy_generation", title: "卖点提炼" },
      layout_copy: { nodeType: "copy_generation", title: "信息层级" },
      infographic: { nodeType: "image_generation", title: "卖点信息图" },
      infographic_output: { nodeType: "reference_image", title: "卖点图输出" },
    },
    outputSlots: { infographic_output: "卖点图输出" },
  },
  "ecommerce-size-spec-image-v1": {
    nodes: {
      product: { nodeType: "product_context", title: "商品资料" },
      spec_copy: { nodeType: "copy_generation", title: "规格整理" },
      dimension_image: { nodeType: "image_generation", title: "尺寸标注图" },
      dimension_output: { nodeType: "reference_image", title: "尺寸输出" },
      spec_table_image: { nodeType: "image_generation", title: "参数说明图" },
      spec_table_output: { nodeType: "reference_image", title: "参数输出" },
    },
    outputSlots: { dimension_output: "尺寸输出", spec_table_output: "参数输出" },
  },
  "ecommerce-scale-reference-image-v1": {
    nodes: {
      scale_reference: { nodeType: "reference_image", title: "参照物参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      scale_copy: { nodeType: "copy_generation", title: "尺度说明" },
      handheld_image: { nodeType: "image_generation", title: "手持/佩戴参照" },
      handheld_output: { nodeType: "reference_image", title: "尺度图输出" },
      surface_image: { nodeType: "image_generation", title: "桌面/空间参照" },
      surface_output: { nodeType: "reference_image", title: "空间参照输出" },
    },
    outputSlots: { handheld_output: "尺度图输出", surface_output: "空间参照输出" },
    referenceInputHints: { scale_reference: "参照物参考" },
  },
  "ecommerce-package-checklist-image-v1": {
    nodes: {
      package_reference: { nodeType: "reference_image", title: "包装参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      checklist_copy: { nodeType: "copy_generation", title: "清单文案" },
      flatlay_image: { nodeType: "image_generation", title: "包装平铺图" },
      flatlay_output: { nodeType: "reference_image", title: "清单输出" },
      gift_image: { nodeType: "image_generation", title: "礼盒/到手图" },
      gift_output: { nodeType: "reference_image", title: "到手输出" },
    },
    outputSlots: { flatlay_output: "清单输出", gift_output: "到手输出" },
    referenceInputHints: { package_reference: "包装参考" },
  },
  "ecommerce-usage-steps-image-v1": {
    nodes: {
      step_reference: { nodeType: "reference_image", title: "步骤参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      step_copy: { nodeType: "copy_generation", title: "步骤拆解" },
      step_image: { nodeType: "image_generation", title: "步骤说明图" },
      step_output: { nodeType: "reference_image", title: "步骤输出" },
      tip_copy: { nodeType: "copy_generation", title: "注意事项" },
      tip_image: { nodeType: "image_generation", title: "注意事项图" },
      tip_output: { nodeType: "reference_image", title: "注意事项输出" },
    },
    outputSlots: { step_output: "步骤输出", tip_output: "注意事项输出" },
    referenceInputHints: { step_reference: "步骤参考" },
  },
  "ecommerce-comparison-image-v1": {
    nodes: {
      compare_reference: { nodeType: "reference_image", title: "对比参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      comparison_copy: { nodeType: "copy_generation", title: "对比维度" },
      comparison_image: { nodeType: "image_generation", title: "对比说明图" },
      comparison_output: { nodeType: "reference_image", title: "对比输出" },
      upgrade_image: { nodeType: "image_generation", title: "升级点图" },
      upgrade_output: { nodeType: "reference_image", title: "升级点输出" },
    },
    outputSlots: { comparison_output: "对比输出", upgrade_output: "升级点输出" },
    referenceInputHints: { compare_reference: "对比参考" },
  },
  "ecommerce-model-lifestyle-image-v1": {
    nodes: {
      style: { nodeType: "reference_image", title: "姿态/风格参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      copy: { nodeType: "copy_generation", title: "人群与场景" },
      half_body: { nodeType: "image_generation", title: "半身/使用图" },
      half_body_output: { nodeType: "reference_image", title: "生活方式图" },
      detail_usage: { nodeType: "image_generation", title: "使用细节图" },
      detail_usage_output: { nodeType: "reference_image", title: "使用细节输出" },
    },
    outputSlots: { half_body_output: "生活方式图", detail_usage_output: "使用细节输出" },
    referenceInputHints: { style: "姿态/风格参考" },
  },
  "ecommerce-scene-image-v1": {
    nodes: {
      scene_reference: { nodeType: "reference_image", title: "场景参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      copy: { nodeType: "copy_generation", title: "场景说明" },
      wide_scene: { nodeType: "image_generation", title: "宽幅场景" },
      scene_output: { nodeType: "reference_image", title: "场景图输出" },
    },
    outputSlots: { scene_output: "场景图输出" },
    referenceInputHints: { scene_reference: "场景参考" },
  },
  "ecommerce-detail-material-image-v1": {
    nodes: {
      detail_reference: { nodeType: "reference_image", title: "细节参考图" },
      product: { nodeType: "product_context", title: "商品资料" },
      detail_copy: { nodeType: "copy_generation", title: "细节说明" },
      macro_image: { nodeType: "image_generation", title: "材质特写" },
      macro_output: { nodeType: "reference_image", title: "细节图输出" },
      structure_image: { nodeType: "image_generation", title: "结构说明图" },
      structure_output: { nodeType: "reference_image", title: "结构输出" },
    },
    outputSlots: { macro_output: "细节图输出", structure_output: "结构输出" },
    referenceInputHints: { detail_reference: "细节参考图" },
  },
  "ecommerce-campaign-promotion-image-v1": {
    nodes: {
      campaign_style: { nodeType: "reference_image", title: "活动风格参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      offer_copy: { nodeType: "copy_generation", title: "优惠信息" },
      visual_copy: { nodeType: "copy_generation", title: "视觉层级" },
      banner: { nodeType: "image_generation", title: "活动横图" },
      banner_output: { nodeType: "reference_image", title: "活动图输出" },
    },
    outputSlots: { banner_output: "活动图输出" },
    referenceInputHints: { campaign_style: "活动风格参考" },
  },
  "ecommerce-short-video-cover-v1": {
    nodes: {
      cover_style: { nodeType: "reference_image", title: "封面风格参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      hook_copy: { nodeType: "copy_generation", title: "封面钩子" },
      frame_copy: { nodeType: "copy_generation", title: "画面节奏" },
      vertical_cover: { nodeType: "image_generation", title: "竖版封面" },
      vertical_cover_output: { nodeType: "reference_image", title: "短视频封面输出" },
      closeup_cover: { nodeType: "image_generation", title: "特写封面" },
      closeup_cover_output: { nodeType: "reference_image", title: "特写封面输出" },
    },
    outputSlots: {
      vertical_cover_output: "短视频封面输出",
      closeup_cover_output: "特写封面输出",
    },
    referenceInputHints: { cover_style: "封面风格参考" },
  },
  "ecommerce-white-background-image-v1": {
    nodes: {
      product_reference: { nodeType: "reference_image", title: "主体参考图" },
      product: { nodeType: "product_context", title: "商品资料" },
      clean_copy: { nodeType: "copy_generation", title: "白底要求" },
      white_image: { nodeType: "image_generation", title: "标准白底图" },
      white_output: { nodeType: "reference_image", title: "白底图输出" },
      shadow_image: { nodeType: "image_generation", title: "轻阴影陈列图" },
      shadow_output: { nodeType: "reference_image", title: "陈列输出" },
    },
    outputSlots: { white_output: "白底图输出", shadow_output: "陈列输出" },
    referenceInputHints: { product_reference: "主体参考图" },
  },
};

const BUILT_IN_NODE_TITLE_BY_TYPE = new Map<WorkflowNodeType, Map<string, string>>();
const BUILT_IN_REFERENCE_LABELS = new Map<string, string>(Object.entries(DEFAULT_EXTERNAL_CONNECTION_LABELS));

function objectValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

for (const [templateKey, item] of Object.entries(BUILT_IN_TEMPLATE_TEXT)) {
  const sourceTemplate = BUILT_IN_TEMPLATE_SOURCE_TEXT[templateKey];
  for (const [nodeKey, title] of Object.entries(item.nodes)) {
    const sourceTitle = sourceTemplate.nodes[nodeKey];
    if (sourceTitle) {
      const titles = BUILT_IN_NODE_TITLE_BY_TYPE.get(sourceTitle.nodeType) ?? new Map<string, string>();
      titles.set(sourceTitle.title, title);
      titles.set(title, title);
      BUILT_IN_NODE_TITLE_BY_TYPE.set(sourceTitle.nodeType, titles);
    }
  }
  for (const [nodeKey, sourceLabel] of Object.entries(sourceTemplate.outputSlots)) {
    BUILT_IN_REFERENCE_LABELS.set(sourceLabel, item.outputSlots[nodeKey] ?? sourceLabel);
  }
  for (const [nodeKey, sourceLabel] of Object.entries(sourceTemplate.referenceInputHints ?? {})) {
    BUILT_IN_REFERENCE_LABELS.set(sourceLabel, item.referenceInputHints?.[nodeKey] ?? sourceLabel);
  }
  for (const [sourceLabel, localizedLabel] of Object.entries(sourceTemplate.defaultExternalConnections ?? {})) {
    BUILT_IN_REFERENCE_LABELS.set(sourceLabel, localizedLabel);
  }
  for (const localizedLabel of Object.values({
    ...item.outputSlots,
    ...item.referenceInputHints,
  })) {
    BUILT_IN_REFERENCE_LABELS.set(localizedLabel, localizedLabel);
  }
}

function shouldLocalizeTemplate(template: CanvasTemplateSummary, locale: Locale): boolean {
  return locale !== CANVAS_TEMPLATE_SOURCE_LOCALE && template.source === "builtin" && template.key in BUILT_IN_TEMPLATE_TEXT;
}

function localizedByKey(sourceValue: string, localizedValue: string | undefined, locale: Locale): string {
  return locale === CANVAS_TEMPLATE_SOURCE_LOCALE ? sourceValue : (localizedValue ?? sourceValue);
}

export function localizeCanvasTemplateSummary(
  template: CanvasTemplateSummary,
  locale: Locale = DEFAULT_LOCALE,
): CanvasTemplateSummary {
  if (!shouldLocalizeTemplate(template, locale)) {
    return template;
  }
  const localized = BUILT_IN_TEMPLATE_TEXT[template.key];
  return {
    ...template,
    title: localized.title,
    description: localized.description,
    scenario: {
      ...template.scenario,
      title: localized.scenarioTitle,
      description: localized.scenarioDescription,
    },
    preview_nodes: template.preview_nodes.map((node) => ({
      ...node,
      title: localizedByKey(node.title, localized.nodes[node.key], locale),
    })),
    output_slots: template.output_slots.map((slot) => ({
      ...slot,
      label: localizedByKey(slot.label, localized.outputSlots[slot.node_key], locale),
    })),
    reference_input_hints: template.reference_input_hints.map((hint) => ({
      ...hint,
      label: localizedByKey(hint.label, localized.referenceInputHints?.[hint.node_key], locale),
    })),
    default_external_connections: template.default_external_connections.map((connection) => ({
      ...connection,
      label: localizedByKey(
        connection.label,
        DEFAULT_EXTERNAL_CONNECTION_LABELS[connection.label],
        locale,
      ),
    })),
  };
}

export function localizeBuiltInTemplateNodeTitle(
  nodeType: WorkflowNodeType,
  title: string,
  locale: Locale = DEFAULT_LOCALE,
  configJson?: Record<string, unknown>,
): string | null {
  if (locale === CANVAS_TEMPLATE_SOURCE_LOCALE) {
    return null;
  }
  const templateMetadata = objectValue(configJson?._canvas_template);
  const templateKey = stringValue(templateMetadata?.template_key);
  const nodeKey = stringValue(templateMetadata?.node_key);
  const localizedByMetadata = templateKey && nodeKey ? BUILT_IN_TEMPLATE_TEXT[templateKey]?.nodes[nodeKey] : null;
  const sourceNode = templateKey && nodeKey ? BUILT_IN_TEMPLATE_SOURCE_TEXT[templateKey]?.nodes[nodeKey] : null;
  const trimmed = title.trim();
  if (localizedByMetadata && sourceNode?.nodeType === nodeType && (trimmed === sourceNode.title || trimmed === localizedByMetadata)) {
    return localizedByMetadata;
  }
  if (!trimmed) {
    return null;
  }
  return BUILT_IN_NODE_TITLE_BY_TYPE.get(nodeType)?.get(trimmed) ?? null;
}

export function localizeBuiltInTemplateLabel(label: string, locale: Locale = DEFAULT_LOCALE): string | null {
  if (locale === CANVAS_TEMPLATE_SOURCE_LOCALE) {
    return null;
  }
  const trimmed = label.trim();
  if (!trimmed) {
    return null;
  }
  return BUILT_IN_REFERENCE_LABELS.get(trimmed) ?? null;
}
