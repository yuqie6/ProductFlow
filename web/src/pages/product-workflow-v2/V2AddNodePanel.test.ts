import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { V2AddNodePanel } from "./V2AddNodePanel";

describe("V2AddNodePanel", () => {
  it("exposes the supported manual reference-node command", () => {
    const markup = renderToStaticMarkup(createElement(V2AddNodePanel, {
      busy: false,
      onCreateReference: () => undefined,
    }));

    expect(markup).toContain("新增参考图节点");
    expect(markup.match(/<button/g)).toHaveLength(1);
    expect(markup).not.toContain(' disabled=""');
  });

  it("disables the command while graph structure is locked", () => {
    const markup = renderToStaticMarkup(createElement(V2AddNodePanel, {
      busy: true,
      onCreateReference: () => undefined,
    }));

    expect(markup).toContain('disabled=""');
  });
});
