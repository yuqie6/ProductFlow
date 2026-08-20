import { ReactFlowProvider } from "@xyflow/react";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { WorkflowCanvasMobileModeTabs, WorkflowCanvasNodePort } from "./WorkflowCanvasChrome";

describe("shared workflow canvas chrome", () => {
  it("renders the stable browse, edit, and select modes with one active choice", () => {
    const markup = renderToStaticMarkup(createElement(WorkflowCanvasMobileModeTabs, {
      value: "edit",
      items: [
        { key: "browse", label: "浏览", description: "浏览模式", icon: "B" },
        { key: "edit", label: "编辑", description: "编辑模式", icon: "E" },
        { key: "select", label: "选择", description: "选择模式", icon: "S" },
      ],
      onChange: () => undefined,
    }));

    expect(markup.match(/<button/g)).toHaveLength(3);
    expect(markup.match(/aria-pressed="true"/g)).toHaveLength(1);
    expect(markup).toContain("浏览");
    expect(markup).toContain("编辑");
    expect(markup).toContain("选择");
  });

  it("disables both connection directions when a presentation port is read-only", () => {
    const markup = renderToStaticMarkup(
      createElement(
        ReactFlowProvider,
        null,
        createElement(WorkflowCanvasNodePort, {
          type: "target",
          top: "50%",
          label: "只读输入",
          connectable: false,
        }),
      ),
    );

    expect(markup).not.toMatch(/\bconnectable(start|end)?\b/);
  });
});
