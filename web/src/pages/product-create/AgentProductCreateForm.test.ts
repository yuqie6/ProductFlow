import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { AgentProductImageTypeKey, AgentProductWorkspaceOptions } from "../../lib/types";
import { AgentProductCreateForm } from "./AgentProductCreateForm";
import { defaultCreateOutputDraft } from "./createIntake";

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
  selections: Array<{ key: AgentProductImageTypeKey; quantity: number }> = [],
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
      onAspectRatioChange: () => undefined,
      onAddReferenceFiles: () => undefined,
      onRemoveReferenceFile: () => undefined,
      brief: "无线洗地机，面向都市白领",
      outputDraft: defaultCreateOutputDraft(),
      onBriefChange: () => undefined,
      onOutputChange: () => undefined,
      onRetryOptions: () => undefined,
      onSubmit: () => undefined,
    }),
  );
}

describe("AgentProductCreateForm", () => {
  it("renders product name, brief, image types, output settings, reference upload, and submit in one form", () => {
    const markup = renderForm();

    expect(markup.match(/<form/g)).toHaveLength(1);
    expect(markup).toContain('id="agent-product-name"');
    expect(markup).toContain('value="Sample product"');
    expect(markup).toContain('id="agent-product-brief"');
    expect(markup).toContain("无线洗地机，面向都市白领");
    expect(markup).toContain("商品说明");
    expect(markup).toContain("出图设定");
    expect(markup).toContain("要文案");
    expect(markup).toContain("可有文案");
    expect(markup).toContain("不要文案");
    expect(markup).toContain("先选择图片类型");
    expect(markup.match(/data-image-type=/g)).toHaveLength(15);
    expect(markup.match(/type="checkbox"/g)).toHaveLength(15);
    expect(markup).toContain("上传商品参考图");
    expect(markup).toContain('type="submit"');
    expect(markup).toContain("开始对话");
    expect(markup).not.toContain("checked=\"\"");
    expect(markup).not.toContain("商品主图");
    expect(markup).not.toContain("选择模板");
    expect(markup).toContain("已选 0 类，共 0 张");
    expect(markup).toContain('data-image-type-family="photography"');
    expect(markup).toContain('data-image-type-family="infographic"');
    expect(markup).toContain('data-image-type-family="evidence"');
    expect(markup).toContain("摄影镜头");
    expect(markup).toContain("信息图");
    expect(markup).toContain("证据图");
    expect(markup).toContain("绑定已有真图，不会生成");
    expect(markup).toContain("data-create-form-bottom-spacer");
    expect(markup).toContain("pb-56");
    expect(markup).toContain("h-28");
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

  it("locks evidence types to binding copy without a quantity stepper", () => {
    const markup = renderForm([{ key: "certification", quantity: 1 }]);
    expect(markup).toContain("绑定已有真图");
    expect(markup).toContain("已选 1 类，共 0 张");
    expect(markup.match(/type="number"/g)).toBeNull();
  });

  it("lets each selected image type pick its own aspect ratio", () => {
    const markup = renderForm([
      { key: "hero", quantity: 2 },
      { key: "detail", quantity: 1 },
    ]);

    expect(markup).toContain('data-image-type-aspect="hero"');
    expect(markup).toContain('data-image-type-aspect="detail"');
    expect(markup).toContain("3:4");
    expect(markup).toContain("1:1");
    expect(markup).not.toContain("先选择图片类型");
  });
});
