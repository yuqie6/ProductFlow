import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import controllerSourceText from "./ImageFidelityCheckController.tsx?raw";
import {
  emptyImageFidelityCheckDraft,
  imageFidelityCheckLabels,
  ImageFidelityCheckController,
} from "./ImageFidelityCheckController";

describe("ImageFidelityCheckController", () => {
  it("renders the initial state fail-closed until the current version is known", () => {
    const markup = renderToStaticMarkup(createElement(ImageFidelityCheckController, {
      productId: "product-1",
      assetId: "asset-1",
      locale: "zh-CN",
    }));

    expect(markup).toContain('data-image-fidelity-panel="true"');
    expect(markup).toContain('data-fidelity-loading="true"');
    expect(markup).toMatch(/data-fidelity-submit[^>]*disabled=""/);
  });

  it("keeps the four outcomes empty and maps every localized label", () => {
    expect(emptyImageFidelityCheckDraft()).toEqual({
      shape_fidelity: null,
      color_material_fidelity: null,
      logo_text_legibility: null,
      text_policy_compliance: null,
      notes: "",
    });

    const translate = ((key: string) => key) as Parameters<typeof imageFidelityCheckLabels>[0];
    const labels = imageFidelityCheckLabels(translate);
    expect(labels.outcomeLabels).toEqual({
      pass: "graph.fidelity.outcome.pass",
      fail: "graph.fidelity.outcome.fail",
      not_applicable: "graph.fidelity.outcome.notApplicable",
    });
    expect(labels.fieldLabels.text_policy_compliance).toBe("graph.fidelity.field.textPolicy");
  });

  it("preserves draft on conflict/reload and caches semantic retry identity", () => {
    expect(controllerSourceText).toContain("setDraft(emptyImageFidelityCheckDraft())");
    expect(controllerSourceText).toContain("setLatestVersionKnown(false)");
    expect(controllerSourceText).toContain("setConflict(null)");
    expect(controllerSourceText).toContain("idempotencyKeysRef.current.get(fingerprint)");
    expect(controllerSourceText).toContain("setDraft(emptyImageFidelityCheckDraft());");
    expect(controllerSourceText).toContain("saveFailure instanceof ApiError && saveFailure.status === 409");
    expect(controllerSourceText).toContain("setHistory(refreshedHistory)");
  });
});
