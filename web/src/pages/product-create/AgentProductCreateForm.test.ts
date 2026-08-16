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

function renderForm(
  selections: Array<{ key: "hero" | "scene"; quantity: number }> = [],
  input: {
    productName?: string;
    isProductNameReadOnly?: boolean;
    isSubmitting?: boolean;
    editingLocked?: boolean;
    primaryActionLabel?: string;
  } = {},
): string {
  return renderToStaticMarkup(
    createElement(AgentProductCreateForm, {
      productName: input.productName ?? "Sample product",
      isProductNameReadOnly: input.isProductNameReadOnly ?? false,
      options,
      selections,
      referenceFiles: [],
      isOptionsLoading: false,
      isOptionsError: false,
      isSubmitting: input.isSubmitting ?? false,
      editingLocked: input.editingLocked ?? false,
      primaryActionLabel: input.primaryActionLabel,
      error: "",
      onProductNameChange: () => undefined,
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
  it("renders product name, all image types, reference upload, and submit in one form", () => {
    const markup = renderForm();

    expect(markup.match(/<form/g)).toHaveLength(1);
    expect(markup).toContain('id="agent-product-name"');
    expect(markup).toContain('value="Sample product"');
    expect(markup.match(/data-image-type=/g)).toHaveLength(15);
    expect(markup.match(/type="checkbox"/g)).toHaveLength(15);
    expect(markup).toContain("上传商品参考图");
    expect(markup).toContain('type="submit"');
    expect(markup).toContain("创建并进入 Agent");
    expect(markup).not.toContain("checked=\"\"");
    expect(markup).not.toContain("商品主图");
    expect(markup).not.toContain("选择模板");
    expect(markup).toContain("已选 0 类，共 0 张");
  });

  it("makes the product name read-only after a workspace persists and disables it while submitting", () => {
    const persistedMarkup = renderForm([], {
      productName: "Persisted product",
      isProductNameReadOnly: true,
    });
    const submittingMarkup = renderForm([], { isSubmitting: true });

    expect(persistedMarkup).toContain('readOnly=""');
    expect(persistedMarkup).toContain('value="Persisted product"');
    expect(submittingMarkup).toContain('id="agent-product-name"');
    expect(submittingMarkup).toContain('disabled=""');
  });

  it("locks editing but keeps the status recheck action enabled", () => {
    const markup = renderForm([{ key: "hero", quantity: 2 }], {
      editingLocked: true,
      primaryActionLabel: "重新检查状态",
    });

    expect(markup).toContain("重新检查状态");
    expect(markup).toMatch(/<input[^>]+id="agent-product-name"[^>]+disabled=""/);
    const submitTag = markup.match(/<button type="submit"[^>]*>/)?.[0] ?? "";
    expect(submitTag).not.toMatch(/\sdisabled(?:=|\s|>)/);
  });

  it("shows a stable quantity control only for selected types", () => {
    const markup = renderForm([{ key: "hero", quantity: 2 }]);

    expect(markup).toContain("已选 1 类，共 2 张");
    expect(markup).toContain("value=\"2\"");
    expect(markup.match(/type="number"/g)).toHaveLength(1);
  });
});
