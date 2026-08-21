import { describe, expect, it } from "vitest";

import type { GraphNode } from "../../../lib/types";
import {
  graphBriefConfig,
  graphBriefDraft,
  graphImageGenerationConfig,
  graphImageGenerationDraft,
  graphPromptConfig,
  graphPromptDraft,
  graphProductSourceConfig,
  graphProductSourceDraft,
  productFactsDraft,
  productFactsPayload,
  validateProductFactsDraft,
  graphVisualConfig,
  graphVisualDraft,
} from "./graphNodeEditorDrafts";

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

describe("graph node inspector drafts", () => {
  it("reads creative brief lists and writes them back without dropping other keys", () => {
    const source = node({
      id: "brief",
      node_type: "creative_brief",
      title: "创作要求",
      config: { extra: "keep", design_goals: ["主图"], required_copy: ["标题"], prohibitions: ["变形"] },
    });
    const draft = graphBriefDraft(source);
    expect(draft.design_goals).toEqual(["主图"]);
    expect(graphBriefConfig(source, { ...draft, goal: "主图优先" }).extra).toBe("keep");
    expect(graphBriefConfig(source, { ...draft, goal: "主图优先" }).goal).toBe("主图优先");
  });

  it("fills image generation defaults instead of failing on empty spec", () => {
    const source = node({ id: "image", node_type: "image_generation", title: "主图 1" });
    const draft = graphImageGenerationDraft(source);
    expect(draft.generation.aspect_ratio).toBe("1:1");
    expect(graphImageGenerationConfig(source, draft).generation_spec).toMatchObject({
      resolution_tier: "high",
      text_policy: "none",
    });
  });

  it("stores prompt editor fields under config.prompt and keeps image_type_key", () => {
    const source = node({
      id: "prompt",
      node_type: "prompt_generation",
      title: "主图提示词",
      config: { image_type_key: "hero" },
    });
    const draft = graphPromptDraft(source);
    const config = graphPromptConfig(source, { ...draft, design_goal: "展示瓶身", headline: "夏日" });
    expect(config.image_type_key).toBe("hero");
    expect(config.prompt).toMatchObject({
      design_goal: "展示瓶身",
      text: { headline: "夏日", subtitle: null, body: null },
    });
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
    const config = graphPromptConfig(source, graphPromptDraft(source));
    expect(config.fact_keys).toBeUndefined();
    expect(config.prompt).toEqual(expect.not.objectContaining({
      images: expect.anything(),
      evidence_asset_ids: expect.anything(),
    }));
    expect((config.prompt as { images?: unknown }).images).toBeUndefined();
  });

  it("writes visual overlay fields without inventing a version id", () => {
    const source = node({ id: "visual", node_type: "visual_system", title: "视觉规范" });
    const config = graphVisualConfig(source, {
      ...graphVisualDraft(source),
      style: ["干净白底"],
      background: "#FFFFFF",
      prohibitions: ["变形"],
    });
    expect(config.visual_system_version_id).toBeNull();
    expect(config.visual_overlay).toEqual({
      style: ["干净白底"],
      prohibitions: ["变形"],
      colors: [{ role: "background", value: "#FFFFFF", label: "背景" }],
    });
  });

  it("keeps an unbound product source explicit and preserves a bound fact-set id", () => {
    const unbound = node({ id: "source", node_type: "product_source" });
    expect(graphProductSourceDraft(unbound)).toMatchObject({
      source_product_id: null,
      fact_set_version_id: null,
    });
    const bound = node({
      id: "source",
      node_type: "product_source",
      config: { source_product_id: "product-2", fact_set_version_id: "facts-2" },
    });
    const draft = graphProductSourceDraft(bound);
    expect(graphProductSourceConfig(bound, { ...draft, source_product_id: "product-3", fact_set_version_id: null })).toMatchObject({
      source_product_id: "product-3",
      fact_set_version_id: null,
    });
  });

  it("rejects empty and duplicate generic fact rows", () => {
    const product = {
      id: "product-1",
      name: "商品",
      category: null,
      price: null,
      source_note: null,
      cover_image_asset_id: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const draft = productFactsDraft(product, {
      id: "facts-1",
      version: 1,
      facts: [{ key: "material", value: "steel" }],
    });
    expect(validateProductFactsDraft(draft)).toBeNull();
    const original = { key: "material", value: "steel" };
    expect(validateProductFactsDraft({ ...draft, facts: [{ id: "a", key: "", value: "steel", original }] })).toBe("empty_key");
    expect(validateProductFactsDraft({ ...draft, facts: [
      { id: "a", key: "material", value: "steel", original },
      { id: "b", key: " MATERIAL ", value: "iron", original: { key: "material", value: "iron" } },
    ] })).toBe("duplicate_key");
    expect(validateProductFactsDraft({ ...draft, facts: [{ id: "a", key: "material", value: "", original }] })).toBe("empty_value");
  });

  it("preserves structured fact values and provenance when a row is unchanged", () => {
    const product = {
      id: "product-1",
      name: "商品",
      category: null,
      price: null,
      source_note: null,
      cover_image_asset_id: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const draft = productFactsDraft(product, {
      id: "facts-1",
      version: 1,
      facts: [{
        key: "dimensions",
        value: [10, 20],
        source_type: "image_observation",
        status: "confirmed",
        evidence_asset_ids: ["asset-1"],
      }],
    });
    expect(productFactsPayload(draft)).toEqual([{
      key: "dimensions",
      value: [10, 20],
      source_type: "image_observation",
      status: "confirmed",
      evidence_asset_ids: ["asset-1"],
    }]);
  });
});
