import { describe, expect, it } from "vitest";

import { parseWorkflowGenerationSpec } from "./generationSpec";

const validSpec = {
  aspect_ratio: "4:5",
  resolution_tier: "high",
  quality_intent: "high",
  reference_fidelity: "high",
  background_intent: "auto",
};

describe("workflow generation spec", () => {
  it("parses the typed intent and normalizes an optional language", () => {
    expect(parseWorkflowGenerationSpec({ ...validSpec, aspect_ratio: " 4:5 " })).toEqual({
      ...validSpec,
      aspect_ratio: "4:5",
    });
  });

  it("enforces ratio, enum, and text-language cross-field contracts", () => {
    expect(parseWorkflowGenerationSpec({ ...validSpec, aspect_ratio: "4/5" })).toBeNull();
    expect(parseWorkflowGenerationSpec({ ...validSpec, resolution_tier: "8k" })).toBeNull();
    expect(parseWorkflowGenerationSpec({ ...validSpec, text_policy: "required", text_language: null })).toBeNull();
    expect(parseWorkflowGenerationSpec({ ...validSpec, text_policy: "none", text_language: "zh-CN" })).toBeNull();
    expect(parseWorkflowGenerationSpec({ ...validSpec, text_language: " " })).toBeNull();
  });
});
