import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { AgentProductImageTypeKey, AgentProductWorkspaceOptions } from "../../lib/types";
import { AgentProductCreateForm } from "./AgentProductCreateForm";

const imageTypeKeys: AgentProductImageTypeKey[] = [
  "hero",
  "selling_point",
  "scene",
  "detail",
  "sku",
  "dimensions",
  "specifications",
  "after_sales",
  "brand_story",
  "precautions",
  "certification",
  "faq",
  "factory",
  "packaging",
  "shipping",
];

const imageTypes: AgentProductWorkspaceOptions["image_types"] = imageTypeKeys.map((key, order) => ({
  key,
  order,
  title: key,
  description: `${key} description`,
}));

const options: AgentProductWorkspaceOptions = {
  schema_version: 1,
  image_types: imageTypes,
  limits: {
    min_image_types: 1,
    default_images_per_type: 2,
    min_images_per_type: 1,
    max_images_per_type: 6,
    max_total_images: 30,
    min_reference_images: 1,
    max_reference_images: 6,
    allowed_image_mime_types: ["image/png", "image/jpeg", "image/webp"],
  },
};

function renderForm(selections: Array<{ key: "hero" | "scene"; quantity: number }> = []): string {
  return renderToStaticMarkup(
    createElement(AgentProductCreateForm, {
      options,
      selections,
      referenceFiles: [],
      isOptionsLoading: false,
      isOptionsError: false,
      isSubmitting: false,
      error: "",
      onToggleImageType: () => undefined,
      onQuantityChange: () => undefined,
      onAddReferenceFiles: () => undefined,
      onRemoveReferenceFile: () => undefined,
      onRetryOptions: () => undefined,
      onSubmit: () => undefined,
    }),
  );
}

describe("AgentProductCreateForm", () => {
  it("renders all 15 backend options initially unselected without template or main-image semantics", () => {
    const markup = renderForm();

    expect(markup.match(/data-image-type=/g)).toHaveLength(15);
    expect(markup.match(/type="checkbox"/g)).toHaveLength(15);
    expect(markup).not.toContain("checked=\"\"");
    expect(markup).not.toContain("商品主图");
    expect(markup).not.toContain("选择模板");
    expect(markup).toContain("已选 0 类，共 0 张");
  });

  it("shows a stable quantity control only for selected types", () => {
    const markup = renderForm([{ key: "hero", quantity: 2 }]);

    expect(markup).toContain("已选 1 类，共 2 张");
    expect(markup).toContain("value=\"2\"");
    expect(markup.match(/type="number"/g)).toHaveLength(1);
  });
});
