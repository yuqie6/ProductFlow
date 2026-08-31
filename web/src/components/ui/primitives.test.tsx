import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { Button } from "./button";
import { Input } from "./field";
import { IconButton } from "./icon-button";
import { StatusBadge } from "./status-badge";

describe("workbench UI primitives", () => {
  it("keeps command buttons non-submitting by default", () => {
    const markup = renderToStaticMarkup(createElement(Button, null, "Save"));

    expect(markup).toContain('type="button"');
  });

  it("gives icon-only controls a programmatic name", () => {
    const markup = renderToStaticMarkup(createElement(
      IconButton,
      { label: "Delete", tooltip: false },
      createElement("span", { "aria-hidden": "true" }, "x"),
    ));

    expect(markup).toContain('aria-label="Delete"');
    expect(markup).toContain('class="sr-only"');
  });

  it("associates field labels and exposes validation errors", () => {
    const markup = renderToStaticMarkup(createElement(Input, {
      id: "product-name",
      label: "Product name",
      error: "Required",
    }));

    expect(markup).toContain('for="product-name"');
    expect(markup).toContain('id="product-name"');
    expect(markup).toContain('role="alert"');
    expect(markup).toContain("Required");
  });

  it("renders status text alongside its semantic tone", () => {
    const markup = renderToStaticMarkup(createElement(StatusBadge, {
      status: "failed",
      children: "Failed",
    }));

    expect(markup).toContain("text-state-error");
    expect(markup).toContain("Failed");
  });

  it("renders frozen tone for locked planned actions", () => {
    const markup = renderToStaticMarkup(createElement(StatusBadge, {
      status: "frozen",
      children: "Frozen",
    }));

    expect(markup).toContain("text-state-frozen");
    expect(markup).toContain("Frozen");
  });
});
