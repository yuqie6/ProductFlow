import { describe, expect, it } from "vitest";

import { zhCN, type TranslationKey } from "../../../lib/i18n";
import type { GraphCatalogConfigField } from "../../../lib/types";
import {
  documentCandidateFieldRows,
  formatDocumentCandidateValue,
} from "./documentCandidateView";

const t = (key: TranslationKey) => zhCN[key];

const compositionFields: GraphCatalogConfigField[] = [
  {
    key: "composition",
    value_kind: "object",
    required: false,
    control: "group",
    label_key: "workflowConfirmation.composition",
    fields: [
      { key: "viewpoint", value_kind: "string", required: false, control: "text", label_key: "agentWorkbench.nodeEditor.viewpoint" },
      { key: "product_share_percent", value_kind: "number", required: false, control: "number", label_key: "agentWorkbench.nodeEditor.productShare" },
      { key: "layout", value_kind: "string", required: false, control: "textarea", label_key: "agentWorkbench.nodeEditor.layout" },
      { key: "copy_regions", value_kind: "string_list", required: false, control: "string_list", label_key: "agentWorkbench.nodeEditor.copyRegions" },
    ],
  },
];

describe("documentCandidateFieldRows", () => {
  it("flattens nested composition objects into labeled leaf diffs", () => {
    const rows = documentCandidateFieldRows(
      {
        composition: {
          viewpoint: "正面平视",
          layout: "居中",
          product_share_percent: 65,
          copy_regions: [],
        },
      },
      {
        composition: {
          viewpoint: "正面平视",
          layout: "左侧主体，右侧留白",
          product_share_percent: 58,
          copy_regions: ["顶栏标题"],
        },
      },
      compositionFields,
    );
    expect(rows.map((row) => row.path)).toEqual([
      "composition.product_share_percent",
      "composition.layout",
      "composition.copy_regions",
    ]);
    expect(rows[1]?.labelKey).toBe("agentWorkbench.nodeEditor.layout");
    expect(rows.some((row) => row.path === "composition")).toBe(false);
  });

  it("translates field keys without catalog fields", () => {
    const rows = documentCandidateFieldRows(
      { design_goal: "保留材质" },
      { design_goal: "突出杯身纹理" },
    );
    expect(rows).toEqual([
      expect.objectContaining({
        path: "design_goal",
        labelKey: "workflowConfirmation.designGoal",
        current: "保留材质",
        proposed: "突出杯身纹理",
      }),
    ]);
  });

  it("treats missing and empty list values as equal", () => {
    const rows = documentCandidateFieldRows(
      { composition: { layout: "居中" } },
      { composition: { layout: "居中", copy_regions: [] } },
      compositionFields,
    );
    expect(rows).toEqual([]);
  });
});

describe("formatDocumentCandidateValue", () => {
  it("renders copy regions as lines instead of JSON", () => {
    const items = formatDocumentCandidateValue(
      ["顶栏标题", "底部卖点"],
      { key: "copy_regions", valueKind: "string_list", control: "string_list" },
      t,
    );
    expect(items.map((item) => item.text)).toEqual(["顶栏标题", "底部卖点"]);
    expect(items.some((item) => item.text.includes("[") || item.text.includes('"'))).toBe(false);
  });

  it("renders empty values as 未填写", () => {
    expect(formatDocumentCandidateValue(null, { key: "layout" }, t)).toEqual([
      { text: "未填写", empty: true },
    ]);
  });

  it("renders booleans and select choices in user language", () => {
    expect(formatDocumentCandidateValue(true, { key: "product_present", valueKind: "boolean" }, t)).toEqual([
      { text: "是" },
    ]);
    expect(formatDocumentCandidateValue(
      "none",
      { key: "picture_in_picture", choices: ["none", "allowed", "required"] },
      t,
    )).toEqual([{ text: "无" }]);
  });

  it("renders palette colors with swatches", () => {
    const items = formatDocumentCandidateValue(
      [{ role: "background", value: "#F5E6D3", label: "暖白釉" }],
      { key: "colors", control: "visual_background", valueKind: "object_list" },
      t,
    );
    expect(items).toEqual([{ text: "暖白釉 #F5E6D3", swatch: "#F5E6D3" }]);
  });
});
