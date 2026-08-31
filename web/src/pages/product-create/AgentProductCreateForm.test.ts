import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type {
  AgentProductImageTypeKey,
  AgentProductWorkspaceOptions,
  DeliveryPresetCatalog,
} from "../../lib/types";
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

const deliveryPresetCatalog: DeliveryPresetCatalog = {
  supports_custom: true,
  items: [{
    key: "jd_hero",
    title: "京东主图",
    aspect_ratio: "1:1",
    applicable_image_type: "hero",
    reviewed_at: "2026-08-24",
    source: "official catalog",
    disclaimer: "Default only",
    delivery_spec: {
      width: 1200,
      height: 1200,
      format: "png",
      max_byte_size: null,
      fit: "contain",
      background_color: null,
      crop_anchor: null,
    },
  }],
};

function renderForm(
  selections: Array<{ key: AgentProductImageTypeKey; quantity: number }> = [],
  input: {
    productName?: string;
    isProductNameReadOnly?: boolean;
    isSubmitting?: boolean;
    editingLocked?: boolean;
    primaryActionLabel?: string;
    deliveryPresetKey?: string | null;
    deliveryPresetCatalog?: DeliveryPresetCatalog | null;
    isDeliveryPresetLoading?: boolean;
    isDeliveryPresetError?: boolean;
    referenceFiles?: File[];
    onDirectCreate?: () => void;
  } = {},
): string {
  return renderToStaticMarkup(
    createElement(AgentProductCreateForm, {
      productName: input.productName ?? "Sample product",
      isProductNameReadOnly: input.isProductNameReadOnly ?? false,
      options,
      selections,
      referenceFiles: input.referenceFiles ?? [],
      deliveryPresetCatalog: input.deliveryPresetCatalog ?? null,
      deliveryPresetKey: input.deliveryPresetKey ?? null,
      isDeliveryPresetLoading: input.isDeliveryPresetLoading ?? false,
      isDeliveryPresetError: input.isDeliveryPresetError ?? false,
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
      onDeliveryPresetChange: () => undefined,
      onRetryDeliveryPresets: () => undefined,
      brief: "无线洗地机，面向都市白领",
      outputDraft: defaultCreateOutputDraft(),
      onBriefChange: () => undefined,
      onOutputChange: () => undefined,
      onRetryOptions: () => undefined,
      onApplyRecommendedSet: () => undefined,
      onSubmit: () => undefined,
      onDirectCreate: input.onDirectCreate ?? (() => undefined),
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
    expect(markup).toContain("只建画布");
    expect(markup).toContain("开始对话：只带商品资料，镜头在对话里补");
    expect(markup).toContain('data-create-outcome="conversation"');
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
    expect(markup).toContain('data-agent-apply-recommended-set="true"');
    expect(markup).toContain("应用推荐套图");
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

  it("disables the recommended set command when editing is locked or all recommendations are selected", () => {
    const lockedMarkup = renderForm([{ key: "hero", quantity: 1 }], { editingLocked: true });
    const submittingMarkup = renderForm([{ key: "hero", quantity: 1 }], { isSubmitting: true });
    const completeMarkup = renderForm([
      { key: "hero", quantity: 2 },
      { key: "selling_point", quantity: 4 },
      { key: "specifications", quantity: 1 },
      { key: "sku", quantity: 1 },
      { key: "scene", quantity: 1 },
      { key: "detail", quantity: 1 },
    ]);

    const lockedButton = lockedMarkup.match(/<button[^>]+data-agent-apply-recommended-set[^>]*>/)?.[0] ?? "";
    const submittingButton = submittingMarkup.match(/<button[^>]+data-agent-apply-recommended-set[^>]*>/)?.[0] ?? "";
    const completeButton = completeMarkup.match(/<button[^>]+data-agent-apply-recommended-set[^>]*>/)?.[0] ?? "";
    expect(lockedButton).toContain('disabled=""');
    expect(submittingButton).toContain('disabled=""');
    expect(completeButton).toContain('disabled=""');
  });

  it("keeps platform governance metadata out of the merchant form", () => {
    const markup = renderForm([], {
      deliveryPresetCatalog,
      deliveryPresetKey: "jd_hero",
    });

    expect(markup).toContain('data-delivery-preset-control="true"');
    expect(markup).toContain('id="agent-create-delivery-preset"');
    expect(markup).toContain("京东主图");
    expect(markup).not.toContain("2026-08-24");
    expect(markup).not.toContain("official catalog");
    expect(markup).not.toContain("Default only");
  });

  it("warns when image types are selected without a reference", () => {
    const markup = renderForm([{ key: "hero", quantity: 1 }]);
    expect(markup).toContain('data-create-outcome="need-plan"');
    expect(markup).toContain("图种或参考图还没齐，补齐后才会带上完整画布");
    expect(markup).toContain("当前没有详情转化图");
  });

  it("keeps canvas-only disabled until image types and a reference are present", () => {
    const incomplete = renderForm();
    const complete = renderForm([{ key: "hero", quantity: 1 }], {
      referenceFiles: [new File(["x"], "ref.png", { type: "image/png" })],
    });
    const incompleteDirect = incomplete.match(/<button[^>]+data-create-direct[^>]*>/)?.[0] ?? "";
    const completeDirect = complete.match(/<button[^>]+data-create-direct[^>]*>/)?.[0] ?? "";

    expect(incomplete).toContain('data-create-outcome="conversation"');
    expect(incompleteDirect).toContain('disabled=""');
    expect(incompleteDirect).toContain("先选图种并上传参考图");
    expect(complete).toContain('data-create-outcome="full-canvas"');
    expect(complete).toContain("开始对话：带完整画布，并打开对话");
    expect(complete).toContain("只建画布：带完整画布，不打开对话");
    expect(completeDirect).not.toMatch(/\sdisabled(?:=|\s|>)/);
  });

  it("keeps submit available when the optional platform catalog fails", () => {
    const markup = renderForm([], { isDeliveryPresetError: true });
    const submitTag = markup.match(/<button type="submit"[^>]*>/)?.[0] ?? "";

    expect(markup).toContain("平台规格加载失败");
    expect(submitTag).not.toMatch(/\sdisabled(?:=|\s|>)/);
  });
});
