import { describe, expect, it } from "vitest";

import type { WorkflowNodeV2 } from "../../lib/types";
import {
  formatFactValue,
  humanizeFactKey,
  imageEditorDraft,
  normalizeImageDraft,
  normalizeReferenceDraft,
  referenceEditorDraft,
  validateImageDraft,
  validateReferenceDraft,
} from "./nodeEditorDrafts";

function node(overrides: Partial<WorkflowNodeV2>): WorkflowNodeV2 {
  return {
    id: "node-1",
    workflow_id: "workflow-1",
    schema_version: 2,
    key: "node_1",
    node_type: "reference_image",
    title: "Reference",
    position_x: 0,
    position_y: 0,
    folder_id: null,
    bound_image_asset_id: null,
    current_prompt_artifact_version_id: null,
    config_json: {},
    status: "idle",
    output_json: null,
    failure_reason: null,
    created_at: "2026-08-15T00:00:00Z",
    updated_at: "2026-08-15T00:00:00Z",
    ...overrides,
  };
}

describe("v2 node editor drafts", () => {
  it("extracts and normalizes editable reference metadata", () => {
    const draft = referenceEditorDraft(node({
      title: " Product reference ",
      config_json: { role: " subject ", label: " Front view " },
    }));

    expect(normalizeReferenceDraft(draft)).toEqual({
      title: "Product reference",
      role: "subject",
      label: "Front view",
    });
    expect(validateReferenceDraft(normalizeReferenceDraft(draft), "invalid")).toBeNull();
    expect(validateReferenceDraft({ ...draft, role: " " }, "invalid")).toBe("invalid");
  });

  it("rejects an invalid persisted image contract instead of presenting unsafe controls", () => {
    const invalid = node({
      node_type: "image_generation",
      config_json: {
        generation_spec: {
          aspect_ratio: "square",
          resolution_tier: "high",
          quality_intent: "high",
          reference_fidelity: "high",
          background_intent: "auto",
          text_policy: "none",
          text_language: null,
        },
      },
    });

    expect(imageEditorDraft(invalid)).toBeNull();
  });

  it("normalizes a valid image draft and validates delivery cross-fields", () => {
    const draft = imageEditorDraft(node({
      node_type: "image_generation",
      title: " Hero image ",
      config_json: {
        variation_instruction: " Brighter side light ",
        generation_spec: {
          aspect_ratio: "16:9",
          resolution_tier: "ultra",
          quality_intent: "high",
          reference_fidelity: "high",
          background_intent: "opaque",
          text_policy: "required",
          text_language: " zh-CN ",
        },
        delivery_spec: {
          width: 1920,
          height: 1080,
          format: "webp",
          fit: "cover",
          crop_anchor: "center",
        },
      },
    }));

    expect(draft).not.toBeNull();
    const normalized = normalizeImageDraft(draft!);
    expect(normalized.title).toBe("Hero image");
    expect(normalized.variation).toBe("Brighter side light");
    expect(normalized.generation.text_language).toBe("zh-CN");
    expect(normalized.delivery).toMatchObject({ max_byte_size: null, background_color: null });
    expect(validateImageDraft(normalized, "invalid")).toBeNull();
    expect(validateImageDraft({
      ...normalized,
      delivery: { ...normalized.delivery!, fit: "contain", crop_anchor: "center" },
    }, "invalid")).toBe("invalid");
  });

  it("formats confirmed facts as readable labels rather than raw JSON", () => {
    expect(humanizeFactKey("product.material_name")).toBe("Product Material Name");
    expect(formatFactValue(
      { material_name: "Steel", washable: true, sizes: ["S", "M"] },
      "Not set",
      { true: "Yes", false: "No" },
    )).toBe("Material Name: Steel; Washable: Yes; Sizes: S · M");
  });
});
