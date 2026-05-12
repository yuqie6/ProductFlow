import {
  localizeBuiltInTemplateLabel,
  localizeBuiltInTemplateNodeTitle,
} from "../../lib/canvasTemplateLocalization";
import { LOCALES, translate, type Locale, type TranslationKey, type TranslationParams } from "../../lib/i18n";
import type { WorkflowNode, WorkflowNodeType } from "../../lib/types";

type TranslateFunction = (key: TranslationKey, params?: TranslationParams) => string;
type LocaleAwareTranslateFunction = TranslateFunction & { locale?: Locale };

const defaultT: TranslateFunction = (key, params) => translate("zh-CN", key, params);

const BASE_NODE_LABEL_KEYS: Record<WorkflowNodeType, TranslationKey> = {
  product_context: "detail.node.productContext",
  reference_image: "detail.node.referenceImage",
  copy_generation: "detail.node.copyGeneration",
  image_generation: "detail.node.imageGeneration",
};

const LEGACY_TITLE_PREFIX_KEYS: Record<WorkflowNodeType, TranslationKey> = {
  product_context: "detail.node.legacyProduct",
  reference_image: "detail.node.legacyReference",
  copy_generation: "detail.node.legacyCopy",
  image_generation: "detail.node.legacyImage",
};

const EXTRA_LEGACY_TITLE_PREFIXES: Partial<Record<WorkflowNodeType, string[]>> = {
  reference_image: ["图片节点", "图片输入", "Image node", "Image input"],
  copy_generation: ["商品文案", "文案生成", "Product copy", "Copy generation"],
  image_generation: ["生成图片", "图片生成", "Generate image", "Image generation"],
};

const REFERENCE_ROLE_LABEL_KEYS: Record<string, TranslationKey> = {
  reference: "detail.referenceRole.reference",
  style: "detail.referenceRole.style",
  product_angle: "detail.referenceRole.productAngle",
  main_image: "detail.referenceRole.mainImage",
  sku_image: "detail.referenceRole.skuImage",
  model_image: "detail.referenceRole.modelImage",
  scene_image: "detail.referenceRole.sceneImage",
  detail_image: "detail.referenceRole.detailImage",
  campaign_image: "detail.referenceRole.campaignImage",
  background: "detail.referenceRole.background",
};

function stringConfig(node: Pick<WorkflowNode, "config_json">, key: string): string {
  const value = node.config_json[key];
  return typeof value === "string" ? value.trim() : "";
}

function defaultTitlePrefixes(type: WorkflowNodeType, t: TranslateFunction): string[] {
  const localizedPrefixes = [t(LEGACY_TITLE_PREFIX_KEYS[type]), t(BASE_NODE_LABEL_KEYS[type])];
  const knownLocalePrefixes = LOCALES.flatMap((locale) => [
    translate(locale, LEGACY_TITLE_PREFIX_KEYS[type]),
    translate(locale, BASE_NODE_LABEL_KEYS[type]),
  ]);
  return Array.from(new Set([...localizedPrefixes, ...knownLocalePrefixes, ...(EXTRA_LEGACY_TITLE_PREFIXES[type] ?? [])]));
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function legacyDefaultTitle(type: WorkflowNodeType, title: string, t: TranslateFunction = defaultT): boolean {
  const trimmed = title.trim();
  return defaultTitlePrefixes(type, t).some(
    (prefix) => trimmed === prefix || new RegExp(`^${escapeRegExp(prefix)}\\s+\\d+$`).test(trimmed),
  );
}

export function workflowNodeTypeLabel(type: WorkflowNodeType): string {
  return defaultT(BASE_NODE_LABEL_KEYS[type]);
}

export function localizedWorkflowNodeTypeLabel(type: WorkflowNodeType, t: TranslateFunction = defaultT): string {
  return t(BASE_NODE_LABEL_KEYS[type]);
}

export function referenceSlotLabel(node: Pick<WorkflowNode, "config_json" | "title" | "node_type">, t: TranslateFunction = defaultT): string {
  const explicitLabel = stringConfig(node, "label");
  if (explicitLabel) {
    return localizeBuiltInTemplateLabel(explicitLabel, (t as LocaleAwareTranslateFunction).locale) ?? explicitLabel;
  }
  const roleLabelKey = REFERENCE_ROLE_LABEL_KEYS[stringConfig(node, "role")];
  if (roleLabelKey) {
    return t(roleLabelKey);
  }
  const title = node.title.trim();
  if (title && !legacyDefaultTitle(node.node_type, title, t)) {
    return title;
  }
  return t(BASE_NODE_LABEL_KEYS.reference_image);
}

export function workflowNodeDisplayLabel(node: Pick<WorkflowNode, "node_type" | "config_json" | "title">, t: TranslateFunction = defaultT): string {
  if (node.node_type === "reference_image") {
    return referenceSlotLabel(node, t);
  }
  return t(BASE_NODE_LABEL_KEYS[node.node_type]);
}

export function workflowNodeDisplayTitle(node: Pick<WorkflowNode, "node_type" | "config_json" | "title">, t: TranslateFunction = defaultT): string {
  const title = node.title.trim();
  const builtInTemplateTitle = localizeBuiltInTemplateNodeTitle(
    node.node_type,
    title,
    (t as LocaleAwareTranslateFunction).locale,
    node.config_json,
  );
  if (builtInTemplateTitle) {
    return builtInTemplateTitle;
  }
  if (title && !legacyDefaultTitle(node.node_type, title, t)) {
    return title;
  }
  return workflowNodeDisplayLabel(node, t);
}

export function defaultTitleForNodeType(type: WorkflowNodeType, index: number, t: TranslateFunction = defaultT): string {
  return `${t(BASE_NODE_LABEL_KEYS[type])} ${index}`;
}

export function connectionDescription(
  source: Pick<WorkflowNode, "node_type" | "config_json" | "title">,
  target: Pick<WorkflowNode, "node_type" | "config_json" | "title">,
  t: TranslateFunction = defaultT,
): string {
  if (source.node_type === "reference_image" && target.node_type === "reference_image") {
    return t("detail.connection.referenceToReference");
  }
  const sourceTitle = workflowNodeDisplayTitle(source, t);
  const targetTitle = workflowNodeDisplayTitle(target, t);
  if (source.node_type === "reference_image" && target.node_type === "copy_generation") {
    return t("detail.connection.referenceToCopy", { source: sourceTitle, target: targetTitle });
  }
  if (source.node_type === "image_generation" && target.node_type === "reference_image") {
    return t("detail.connection.imageToReference", { source: sourceTitle, target: targetTitle });
  }
  if (target.node_type === "image_generation") {
    return t("detail.connection.toImage", { source: sourceTitle, target: targetTitle });
  }
  return t("detail.connection.default", { source: sourceTitle, target: targetTitle });
}
