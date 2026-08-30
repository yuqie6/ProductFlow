import { describe, expect, it } from "vitest";

import type { GraphCatalogConfigField, GraphNode } from "../../../lib/types";
import {
  catalogConfigForSave,
  catalogNodeDraft,
  patchCatalogValue,
  readVisualBackground,
  validateCatalogDraft,
  writeVisualBackground,
} from "./catalogConfig";

function node(partial: Partial<GraphNode> & Pick<GraphNode, "id" | "node_type">): GraphNode {
  return {
    title: partial.title ?? partial.id,
    position_x: 0,
    position_y: 0,
    config: {},
    bound_asset_id: null,
    group_id: null,
    preview_asset_id: null,
    config_status: "incomplete",
    unused: false,
    incoming: [],
    outgoing: [],
    ...partial,
  };
}

const briefFields: GraphCatalogConfigField[] = [
  { key: "title", value_kind: "string", required: false, control: "hidden" },
  { key: "goal", value_kind: "string", required: false, control: "textarea", label_key: "workflowConfirmation.designGoal" },
  { key: "design_goals", value_kind: "string_list", required: false, control: "string_list", label_key: "graph.inspector.designGoals" },
  { key: "required_copy", value_kind: "string_list", required: false, control: "string_list", label_key: "graph.inspector.requiredCopy" },
  { key: "prohibitions", value_kind: "string_list", required: false, control: "string_list" },
];

const visualFields: GraphCatalogConfigField[] = [
  { key: "visual_system_version_id", value_kind: "string_or_null", required: false, control: "hidden" },
  {
    key: "visual_overlay",
    value_kind: "object_or_null",
    required: false,
    control: "group",
    fields: [
      { key: "style", value_kind: "string_list", required: false, control: "string_list" },
      { key: "colors", value_kind: "object_list", required: false, control: "visual_background" },
      { key: "prohibitions", value_kind: "string_list", required: false, control: "string_list" },
    ],
  },
];

const promptFields: GraphCatalogConfigField[] = [
  { key: "image_type_key", value_kind: "string", required: false, control: "hidden" },
  {
    key: "prompt",
    value_kind: "object",
    required: false,
    control: "group",
    fields: [
      { key: "design_goal", value_kind: "string", required: false, control: "textarea" },
      { key: "shared_rules", value_kind: "string_list", required: false, control: "string_list" },
      {
        key: "text",
        value_kind: "object",
        required: false,
        control: "group",
        fields: [
          { key: "headline", value_kind: "string_or_null", required: false, control: "text" },
          { key: "subtitle", value_kind: "string_or_null", required: false, control: "text" },
          { key: "body", value_kind: "string_or_null", required: false, control: "textarea" },
        ],
      },
    ],
  },
];

const imageFields: GraphCatalogConfigField[] = [
  { key: "image_type_key", value_kind: "string", required: false, control: "hidden" },
  { key: "variation_instruction", value_kind: "string_or_null", required: false, control: "textarea" },
  {
    key: "generation_spec",
    value_kind: "object",
    required: false,
    control: "group",
    default: {
      aspect_ratio: "1:1",
      resolution_tier: "high",
      quality_intent: "high",
      reference_fidelity: "high",
      background_intent: "auto",
      text_policy: "none",
      text_language: null,
    },
    fields: [
      { key: "aspect_ratio", value_kind: "string", required: false, control: "aspect_ratio", panel: "basic", default: "1:1" },
      { key: "resolution_tier", value_kind: "string", required: false, control: "select", default: "high" },
      { key: "quality_intent", value_kind: "string", required: false, control: "select", default: "high" },
      { key: "reference_fidelity", value_kind: "string", required: false, control: "select", default: "high" },
      { key: "background_intent", value_kind: "string", required: false, control: "select", default: "auto" },
      { key: "text_policy", value_kind: "string", required: false, control: "select", default: "none" },
      { key: "text_language", value_kind: "string_or_null", required: false, control: "text" },
    ],
  },
  {
    key: "delivery_spec",
    value_kind: "object_or_null",
    required: false,
    control: "optional_object",
    panel: "advanced",
    default: { width: 1200, height: 1200, format: "png" },
    fields: [
      { key: "width", value_kind: "number", required: false, control: "number", default: 1200 },
      { key: "height", value_kind: "number", required: false, control: "number", default: 1200 },
      { key: "format", value_kind: "string", required: false, control: "select", default: "png" },
    ],
  },
];

describe("catalog config drafts", () => {
  it("drops unregistered keys and writes brief lists from catalog fields", () => {
    const source = node({
      id: "brief",
      node_type: "creative_brief",
      title: "创作要求",
      config: { extra: "keep", design_goals: ["主图"], required_copy: ["标题"], prohibitions: ["变形"] },
    });
    const draft = catalogNodeDraft(source, briefFields);
    const saved = catalogConfigForSave(briefFields, { ...draft.config, goal: "主图优先" });
    expect(saved.extra).toBeUndefined();
    expect(saved.goal).toBe("主图优先");
    expect(saved.design_goals).toEqual(["主图"]);
  });

  it("only serializes fields declared by the catalog", () => {
    const fields: GraphCatalogConfigField[] = [
      { key: "goal", value_kind: "string", required: false, control: "textarea" },
    ];
    const saved = catalogConfigForSave(fields, {
      document_origin: "generated",
      goal: "手填目标",
    });
    expect(saved).toEqual({ goal: "手填目标" });
  });

  it("fills image generation defaults from catalog instead of a private draft type", () => {
    const source = node({ id: "image", node_type: "image_generation", title: "主图 1" });
    const draft = catalogNodeDraft(source, imageFields);
    const saved = catalogConfigForSave(imageFields, draft.config);
    expect(saved.generation_spec).toMatchObject({
      aspect_ratio: "1:1",
      resolution_tier: "high",
      text_policy: "none",
    });
    expect(draft.config.delivery_spec).toBeUndefined();
    expect(saved.delivery_spec).toBeUndefined();
  });

  it("does not persist catalog delivery_spec default when the node omitted the key", () => {
    const source = node({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
      config: { image_type_key: "hero", generation_spec: { aspect_ratio: "1:1" } },
    });
    const draft = catalogNodeDraft(source, imageFields);
    const saved = catalogConfigForSave(imageFields, {
      ...draft.config,
      variation_instruction: "换角度",
    });
    expect(saved.delivery_spec).toBeUndefined();
    expect(saved.variation_instruction).toBe("换角度");
  });

  it("stores prompt fields under config.prompt and keeps image_type_key", () => {
    const source = node({
      id: "prompt",
      node_type: "prompt_generation",
      title: "主图提示词",
      config: { image_type_key: "hero" },
    });
    const draft = catalogNodeDraft(source, promptFields);
    const next = patchCatalogValue(promptFields, draft.config, ["prompt", "design_goal"], "展示瓶身");
    const withHeadline = patchCatalogValue(promptFields, next, ["prompt", "text", "headline"], "夏日");
    const saved = catalogConfigForSave(promptFields, withHeadline);
    expect(saved.image_type_key).toBe("hero");
    expect(saved.prompt).toEqual({
      design_goal: "展示瓶身",
      text: { headline: "夏日" },
    });
  });

  it("keeps an object without a default sparse until a child is edited", () => {
    const source = node({
      id: "prompt",
      node_type: "prompt_generation",
      config: { image_type_key: "hero" },
    });
    const draft = catalogNodeDraft(source, promptFields);
    expect(draft.config).toEqual({ image_type_key: "hero" });

    const next = patchCatalogValue(promptFields, draft.config, ["prompt", "text", "headline"], "  夏日  ");
    expect(next.prompt).toEqual({ text: { headline: "  夏日  " } });
    expect(catalogConfigForSave(promptFields, next).prompt).toEqual({ text: { headline: "夏日" } });
  });

  it("trims strings and keeps string-list draft line breaks until save", () => {
    const draft = patchCatalogValue(
      promptFields,
      { image_type_key: "  hero  ", prompt: { design_goal: "  目标  " } },
      ["prompt", "shared_rules"],
      " 第一行 \n\n 第二行 \n",
    );
    expect((draft.prompt as Record<string, unknown>).shared_rules).toBe(" 第一行 \n\n 第二行 \n");
    expect(catalogConfigForSave(promptFields, draft)).toMatchObject({
      image_type_key: "hero",
      prompt: { design_goal: "目标", shared_rules: ["第一行", "第二行"] },
    });
  });

  it("preserves explicit false and zero while dropping unregistered nested keys", () => {
    const fields: GraphCatalogConfigField[] = [
      { key: "enabled", value_kind: "boolean", required: false, control: "checkbox" },
      { key: "count", value_kind: "number", required: false, control: "number" },
      {
        key: "prompt",
        value_kind: "object",
        required: false,
        control: "group",
        fields: [{ key: "design_goal", value_kind: "string", required: false, control: "textarea" }],
      },
    ];
    const saved = catalogConfigForSave(fields, {
      enabled: false,
      count: 0,
      prompt: { design_goal: "  ", extra: false },
    });
    expect(saved).toEqual({ enabled: false, count: 0 });
  });

  it("caps visual background hex by the value field, not the color list length", () => {
    const fields: GraphCatalogConfigField[] = [{
      key: "colors",
      value_kind: "object_list",
      required: false,
      control: "visual_background",
      max_length: 32,
      fields: [
        { key: "role", value_kind: "string", required: false, control: "text" },
        { key: "value", value_kind: "string", required: false, control: "text", max_length: 7 },
      ],
    }];
    expect(validateCatalogDraft({
      title: "色",
      config: { colors: [{ role: "background", value: "#FFFFFFF" }] },
    }, fields, "配置无效")).toBe("配置无效");
    expect(validateCatalogDraft({
      title: "色",
      config: { colors: [{ role: "background", value: "#FFFFFF" }] },
    }, fields, "配置无效")).toBeNull();
  });

  it("blocks invalid number drafts instead of falling back to the previous value", () => {
    const fields: GraphCatalogConfigField[] = [{
      key: "settings",
      value_kind: "object",
      required: false,
      control: "group",
      fields: [{
        key: "count",
        value_kind: "number",
        required: false,
        control: "number",
        min_value: 1,
        max_value: 3,
      }],
    }];
    const draft = { title: "设置", config: { settings: { count: "9" } } };
    expect(catalogConfigForSave(fields, draft.config)).toEqual({ settings: { count: "9" } });
    expect(validateCatalogDraft(draft, fields, "配置无效")).toBe("配置无效");
  });

  it("validates nested types, choices, ranges, and max lengths", () => {
    const fields: GraphCatalogConfigField[] = [{
      key: "settings",
      value_kind: "object",
      required: false,
      control: "group",
      fields: [
        { key: "mode", value_kind: "string", required: false, control: "select", choices: ["safe"] },
        { key: "count", value_kind: "number", required: false, control: "number", min_value: 1, max_value: 3 },
        { key: "label", value_kind: "string", required: false, control: "text", max_length: 3 },
      ],
    }];
    const message = "配置无效";
    expect(validateCatalogDraft({ title: "设置", config: { settings: { mode: "fast" } } }, fields, message)).toBe(message);
    expect(validateCatalogDraft({ title: "设置", config: { settings: { count: 0 } } }, fields, message)).toBe(message);
    expect(validateCatalogDraft({ title: "设置", config: { settings: { label: "超过长度" } } }, fields, message)).toBe(message);
    expect(validateCatalogDraft({ title: "设置", config: { settings: { mode: 1 } } }, fields, message)).toBe(message);
  });

  it("strips topology keys from prompt config writes", () => {
    const source = node({
      id: "prompt",
      node_type: "prompt_generation",
      title: "主图提示词",
      config: {
        image_type_key: "hero",
        fact_keys: ["product_name"],
        prompt: {
          design_goal: "旧目标",
          images: [{ image_plan_key: "hero-1" }],
          evidence_asset_ids: ["asset-1"],
        },
      },
    });
    const saved = catalogConfigForSave(promptFields, catalogNodeDraft(source, promptFields).config);
    expect(saved.fact_keys).toBeUndefined();
    expect((saved.prompt as { images?: unknown }).images).toBeUndefined();
  });

  it("writes visual overlay colors from the visual_background control", () => {
    const source = node({ id: "visual", node_type: "visual_system", title: "视觉规范" });
    const draft = catalogNodeDraft(source, visualFields);
    const withStyle = patchCatalogValue(visualFields, draft.config, ["visual_overlay", "style"], ["干净白底"]);
    const withBackground = patchCatalogValue(
      visualFields,
      withStyle,
      ["visual_overlay", "colors"],
      [{ role: "background", value: "#FFFFFF", label: "背景" }],
    );
    const withProhibitions = patchCatalogValue(visualFields, withBackground, ["visual_overlay", "prohibitions"], ["变形"]);
    const saved = catalogConfigForSave(visualFields, withProhibitions);
    expect(saved.visual_system_version_id).toBeNull();
    expect(saved.visual_overlay).toEqual({
      style: ["干净白底"],
      prohibitions: ["变形"],
      colors: [{ role: "background", value: "#FFFFFF", label: "背景" }],
    });
    expect(readVisualBackground((saved.visual_overlay as { colors: unknown }).colors)).toBe("#FFFFFF");
  });

  it("preserves non-background overlay colors when editing the background", () => {
    const existing = [
      { role: "primary", value: "#112233", label: "主色" },
      { role: "background", value: "#FFFFFF", label: "背景" },
    ];
    expect(writeVisualBackground("#F8F8F8", existing)).toEqual([
      { role: "primary", value: "#112233", label: "主色" },
      { role: "background", value: "#F8F8F8", label: "背景" },
    ]);
    expect(catalogConfigForSave(visualFields, {
      visual_overlay: { colors: writeVisualBackground("#F8F8F8", existing) },
    })).toEqual({
      visual_overlay: {
        colors: existing.map((color) => color.role === "background" ? { ...color, value: "#F8F8F8" } : color),
      },
    });
  });
});
