import { describe, expect, it } from "vitest";

import {
  buildCreateGenerationSpec,
  createOutputSummary,
  defaultCreateOutputDraft,
  isCreateOutputReady,
} from "./createIntake";

describe("create intake generation spec", () => {
  it("builds a required Chinese spec; aspect ratio is chosen per image type", () => {
    const spec = buildCreateGenerationSpec(defaultCreateOutputDraft());
    expect(spec).toMatchObject({
      text_policy: "required",
      text_language: "zh-CN",
      resolution_tier: "high",
    });
  });

  it("omits language when the image must stay text-free", () => {
    const spec = buildCreateGenerationSpec({
      textPolicy: "none",
      textLanguage: "zh-CN",
    });
    expect(spec).toMatchObject({
      text_policy: "none",
      text_language: null,
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
});
