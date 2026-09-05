import { describe, expect, it } from "vitest";

import {
  buildCreateGenerationSpec,
  buildCreateTextSettings,
  createOutputSummary,
  defaultCreateOutputDraft,
  isCreateCanvasReady,
  isCreateOutputReady,
  resolveCreateSubmitAction,
} from "./createIntake";

describe("create intake generation spec", () => {
  it("keeps text settings outside generation settings", () => {
    const spec = buildCreateGenerationSpec(defaultCreateOutputDraft());
    expect(spec).toMatchObject({
      resolution_tier: "high",
    });
    expect(spec).not.toHaveProperty("text_policy");
    expect(buildCreateTextSettings(defaultCreateOutputDraft())).toEqual({ policy: "none", language: null });
  });

  it("clears effective language when text is disabled", () => {
    const spec = buildCreateTextSettings({
      textPolicy: "none",
      textLanguage: "zh-CN",
    });
    expect(spec).toMatchObject({
      policy: "none",
      language: null,
    });
    expect(createOutputSummary({
      textPolicy: "none",
      textLanguage: "zh-CN",
    }).textLanguage).toBeNull();
  });

  it("rejects required copy without a language", () => {
    expect(
      buildCreateGenerationSpec({
        textPolicy: "required",
        textLanguage: "  ",
      }),
    ).toBeNull();
    expect(
      isCreateOutputReady({
        textPolicy: "required",
        textLanguage: "  ",
      }),
    ).toBe(false);
  });

  it("requires brief, image types, and a reference before the full canvas path is ready", () => {
    const limits = {
      min_image_types: 1,
      default_images_per_type: 2,
      min_images_per_type: 1,
      max_images_per_type: 6,
      max_total_images: 30,
      min_reference_images: 1,
      max_reference_images: 6,
      allowed_image_mime_types: ["image/png" as const],
    };
    const base = {
      name: "马克杯",
      brief: "暖白釉",
      selections: [{ key: "hero" as const, quantity: 1 }],
      limits,
      outputDraft: defaultCreateOutputDraft(),
    };
    expect(isCreateCanvasReady({ ...base, referenceImageCount: 0 })).toBe(false);
    expect(isCreateCanvasReady({ ...base, brief: "", referenceImageCount: 1 })).toBe(false);
    expect(isCreateCanvasReady({ ...base, selections: [], referenceImageCount: 1 })).toBe(false);
    expect(isCreateCanvasReady({ ...base, referenceImageCount: 1 })).toBe(true);
  });

  it("starts a conversation from a name, and refuses to drop a partial image plan", () => {
    const limits = {
      min_image_types: 1,
      default_images_per_type: 2,
      min_images_per_type: 1,
      max_images_per_type: 6,
      max_total_images: 30,
      min_reference_images: 1,
      max_reference_images: 6,
      allowed_image_mime_types: ["image/png" as const],
    };
    const outputDraft = defaultCreateOutputDraft();
    expect(resolveCreateSubmitAction({
      name: "马克杯",
      brief: "暖白釉",
      selections: [],
      referenceImageCount: 0,
      limits,
      outputDraft,
    })).toBe("conversation");
    expect(resolveCreateSubmitAction({
      name: "马克杯",
      brief: "暖白釉",
      selections: [{ key: "hero", quantity: 1 }],
      referenceImageCount: 0,
      limits,
      outputDraft,
    })).toBe("blocked");
    expect(resolveCreateSubmitAction({
      name: "马克杯",
      brief: "暖白釉",
      selections: [{ key: "hero", quantity: 1 }],
      referenceImageCount: 1,
      limits,
      outputDraft,
    })).toBe("full-canvas");
  });
});
