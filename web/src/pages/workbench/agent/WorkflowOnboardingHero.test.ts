import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { WorkflowOnboardingHero } from "./WorkflowOnboardingHero";

describe("WorkflowOnboardingHero", () => {
  it("renders the 3-track onboarding pathways and product header", () => {
    const markup = renderToStaticMarkup(createElement(WorkflowOnboardingHero, {
      productName: "真丝法式印花连衣裙",
      onOpenAgent: () => undefined,
      onOpenRecipes: () => undefined,
      onOpenAddPanel: () => undefined,
    }));

    // 商品徽章与标题
    expect(markup).toContain("真丝法式印花连衣裙");
    expect(markup).toContain("初始化商品生图工作流");

    // 路径 1：Agent 智能规划
    expect(markup).toContain("Agent 智能批量规划");
    expect(markup).toContain("打开对话");

    // 路径 2：行业预设
    expect(markup).toContain("套用工作流预设");
    expect(markup).toContain("选择预设模版");

    // 路径 3：从空白手动建图
    expect(markup).toContain("自由空白建图");
    expect(markup).toContain("打开添加面板");
  });
});
