import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { ApiError } from "../../lib/api";
import { GraphAgentPanel } from "./GraphWorkbenchPage";

describe("GraphAgentPanel", () => {
  it("renders the Agent chrome instead of an empty sidebar slot", () => {
    const markup = renderToStaticMarkup(createElement(GraphAgentPanel));

    expect(markup).toContain("data-graph-agent-panel");
    expect(markup).toContain("工作流 Agent");
    expect(markup).toContain("还没有挂上这个商品的 Agent 对话。");
  });

  it("surfaces a recoverable ensure failure", () => {
    const markup = renderToStaticMarkup(createElement(GraphAgentPanel, {
      error: new ApiError(409, "当前商品还没有可执行的工作流"),
      onRetry: () => undefined,
    }));

    expect(markup).toContain("当前商品还没有可执行的工作流");
    expect(markup).toContain("重试连接 Agent");
  });
});
