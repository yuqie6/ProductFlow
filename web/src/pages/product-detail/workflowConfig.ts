import type { ProductDetail, WorkflowNode, WorkflowNodeType } from "../../lib/types";
import type { NodeConfigDraft } from "./types";
import { configString, outputStringArray, outputText } from "./utils";

export function draftFromNode(
  node: WorkflowNode | null,
  product?: ProductDetail | null,
): NodeConfigDraft {
  const copySetId = node?.output_json
    ? outputText(node.output_json, "copy_set_id")
    : null;
  const copySet = copySetId
    ? product?.copy_sets.find((item) => item.id === copySetId)
    : null;
  const outputSellingPoints = node
    ? outputStringArray(node, "selling_points")
    : [];
  return {
    title: node?.title ?? "",
    productName: configString(node, "name", product?.name ?? ""),
    category: configString(node, "category", product?.category ?? ""),
    price: configString(node, "price", product?.price ?? ""),
    sourceNote: configString(node, "source_note", product?.source_note ?? ""),
    instruction: configString(node, "instruction"),
    role: configString(node, "role", "reference"),
    label: configString(node, "label"),
    tone: configString(node, "tone", "转化清晰"),
    channel: configString(node, "channel", "商品主图"),
    size: configString(node, "size", "1024x1024"),
    copyTitle:
      copySet?.title ??
      (node?.output_json ? (outputText(node.output_json, "title") ?? "") : ""),
    copySellingPoints: (copySet?.selling_points ?? outputSellingPoints).join(
      "\n",
    ),
    copyPosterHeadline:
      copySet?.poster_headline ??
      (node?.output_json
        ? (outputText(node.output_json, "poster_headline") ?? "")
        : ""),
    copyCta:
      copySet?.cta ??
      (node?.output_json ? (outputText(node.output_json, "cta") ?? "") : ""),
  };
}

export function nodeConfigFromDraft(
  node: WorkflowNode,
  draft: NodeConfigDraft,
): Record<string, unknown> {
  const base = { ...node.config_json };
  if (node.node_type === "product_context") {
    return {
      ...base,
      name: draft.productName,
      category: draft.category,
      price: draft.price,
      source_note: draft.sourceNote,
    };
  }
  if (node.node_type === "reference_image") {
    return { ...base, role: draft.role, label: draft.label };
  }
  if (node.node_type === "copy_generation") {
    return {
      ...base,
      instruction: draft.instruction,
      tone: draft.tone,
      channel: draft.channel,
    };
  }
  if (node.node_type === "image_generation") {
    return {
      ...base,
      instruction: draft.instruction,
      size: draft.size,
    };
  }
  return base;
}

export function defaultConfigForType(type: WorkflowNodeType): Record<string, unknown> {
  if (type === "reference_image") {
    return { role: "reference", label: "参考图" };
  }
  if (type === "copy_generation") {
    return { instruction: "生成商品文案", tone: "清晰可信", channel: "商品图" };
  }
  if (type === "image_generation") {
    return {
      instruction: "生成商品图",
      size: "1024x1024",
    };
  }
  return {};
}

export function defaultTitleForType(type: WorkflowNodeType, index: number): string {
  return {
    product_context: `商品 ${index}`,
    reference_image: `参考图 ${index}`,
    copy_generation: `文案 ${index}`,
    image_generation: `生图 ${index}`,
  }[type];
}
