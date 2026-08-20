import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { V2AddNodePanel } from "./V2AddNodePanel";

describe("V2AddNodePanel", () => {
  it("exposes the supported manual reference-node and folder commands with recipes entrance", () => {
    const markup = renderToStaticMarkup(createElement(V2AddNodePanel, {
      busy: false,
      onCreateReference: () => undefined,
      onCreateFolder: () => undefined,
      onOpenRecipesTab: () => undefined,
    }));

    expect(markup).toContain("参考图节点");
    expect(markup).toContain("新建分类文件夹");
    expect(markup).not.toContain("电商场景预设套件");
    expect(markup).not.toContain(' disabled=""');
  });

  it("disables the command while graph structure is locked", () => {
    const markup = renderToStaticMarkup(createElement(V2AddNodePanel, {
      busy: true,
      onCreateReference: () => undefined,
      onCreateFolder: () => undefined,
    }));

    expect(markup).toContain('disabled=""');
  });
});


