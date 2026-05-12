import { describe, expect, it } from "vitest";

import type { TranslationKey, TranslationParams } from "../../lib/i18n";
import type { WorkflowNode } from "../../lib/types";
import {
  connectionDescription,
  defaultTitleForNodeType,
  referenceSlotLabel,
  workflowNodeDisplayLabel,
  workflowNodeDisplayTitle,
} from "./nodeDisplay";

const baseNode: WorkflowNode = {
  id: "node-1",
  workflow_id: "workflow-1",
  node_type: "reference_image",
  title: "参考图 1",
  position_x: 0,
  position_y: 0,
  config_json: {},
  status: "idle",
  output_json: null,
  failure_reason: null,
  last_run_at: null,
  created_at: "2026-05-10T00:00:00Z",
  updated_at: "2026-05-10T00:00:00Z",
};

function stubT(values: Partial<Record<TranslationKey, string>>) {
  return (key: TranslationKey, params?: TranslationParams): string => {
    const value = values[key] ?? key;
    return value.replace(/\{(\w+)\}/g, (_match, paramKey: string) => String(params?.[paramKey] ?? `{${paramKey}}`));
  };
}

function stubEnT(values: Partial<Record<TranslationKey, string>>) {
  const t = stubT(values) as ReturnType<typeof stubT> & { locale?: "en-US" };
  t.locale = "en-US";
  return t;
}

describe("node display helpers", () => {
  it("uses business-facing labels for internal node types", () => {
    expect(workflowNodeDisplayLabel({ ...baseNode, node_type: "product_context" })).toBe("商品资料");
    expect(workflowNodeDisplayLabel(baseNode)).toBe("承载图片节点");
    expect(workflowNodeDisplayLabel({ ...baseNode, node_type: "copy_generation" })).toBe("文案生成节点");
    expect(workflowNodeDisplayLabel({ ...baseNode, node_type: "image_generation" })).toBe("生图触发器节点");
    expect(workflowNodeDisplayLabel({ ...baseNode, title: "Image node 1" }, stubT({
      "detail.node.referenceImage": "Image carrier node",
      "detail.node.legacyReference": "Reference",
    }))).toBe("Image carrier node");
  });

  it("derives reference slot labels from explicit labels and merchant roles", () => {
    expect(referenceSlotLabel({ ...baseNode, config_json: { label: "活动图" } })).toBe("活动图");
    expect(referenceSlotLabel({ ...baseNode, config_json: { role: "model_image" } })).toBe("模特图");
    expect(referenceSlotLabel({ ...baseNode, config_json: { role: "scene_image" } })).toBe("场景图");
    expect(referenceSlotLabel(baseNode)).toBe("承载图片节点");
    expect(referenceSlotLabel({ ...baseNode, config_json: { role: "model_image" } }, stubT({
      "detail.referenceRole.modelImage": "Model image",
    }))).toBe("Model image");
  });

  it("hides legacy default titles but preserves user-authored titles", () => {
    expect(workflowNodeDisplayTitle({ ...baseNode, title: "参考图 2" })).toBe("承载图片节点");
    expect(workflowNodeDisplayTitle({ ...baseNode, title: "参考图 2" }, stubT({
      "detail.node.referenceImage": "Image carrier node",
      "detail.node.legacyReference": "Reference",
    }))).toBe("Image carrier node");
    expect(workflowNodeDisplayTitle({ ...baseNode, title: "Image node 2" })).toBe("承载图片节点");
    expect(workflowNodeDisplayTitle({ ...baseNode, title: "详情图 A" })).toBe("详情图 A");
    expect(defaultTitleForNodeType("reference_image", 2)).toBe("承载图片节点 2");
    expect(defaultTitleForNodeType("image_generation", 2)).toBe("生图触发器节点 2");
    expect(defaultTitleForNodeType("image_generation", 2, stubT({
      "detail.node.imageGeneration": "Image trigger node",
    }))).toBe("Image trigger node 2");
  });

  it("localizes persisted built-in template node titles without translating user titles", () => {
    const t = stubEnT({
      "detail.node.referenceImage": "Image carrier node",
      "detail.node.copyGeneration": "Copy generation node",
      "detail.node.legacyReference": "Reference",
      "detail.node.legacyCopy": "Copy",
    });

    expect(workflowNodeDisplayTitle({ ...baseNode, node_type: "copy_generation", title: "主图卖点" }, t)).toBe(
      "Main-image benefits",
    );
    expect(workflowNodeDisplayTitle({ ...baseNode, title: "主图输出" }, t)).toBe("Main image output");
    expect(workflowNodeDisplayTitle({ ...baseNode, title: "我的输出" }, t)).toBe("我的输出");
    expect(referenceSlotLabel({ ...baseNode, config_json: { label: "主图输出" } }, t)).toBe("Main image output");
    expect(referenceSlotLabel({ ...baseNode, config_json: { label: "我的输出" } }, t)).toBe("我的输出");
  });

  it("explains confusing connection semantics", () => {
    const copyNode: WorkflowNode = {
      ...baseNode,
      id: "copy-node",
      node_type: "copy_generation",
      title: "文案 1",
    };
    const imageNode: WorkflowNode = {
      ...baseNode,
      id: "image-node",
      node_type: "image_generation",
      title: "生图触发器节点 1",
    };

    expect(connectionDescription(baseNode, { ...baseNode, title: "参考图 2" })).toContain(
      "承载图片节点不能互连",
    );
    expect(connectionDescription(baseNode, copyNode)).toBe("承载图片节点作为文案生成节点参考。");
    expect(connectionDescription(imageNode, baseNode)).toBe("生图触发器节点写入承载图片节点。");
    expect(connectionDescription(
      { ...baseNode, title: "Image node 1" },
      { ...copyNode, title: "Product copy 1" },
      stubT({
        "detail.node.referenceImage": "Image carrier node",
        "detail.node.copyGeneration": "Copy generation node",
        "detail.node.legacyReference": "Reference",
        "detail.node.legacyCopy": "Copy",
        "detail.connection.referenceToCopy": "{source} references {target}.",
      }),
    )).toBe("Image carrier node references Copy generation node.");
  });
});
