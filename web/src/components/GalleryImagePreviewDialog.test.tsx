import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { GalleryImagePreviewDialog } from "./GalleryImagePreviewDialog";

describe("GalleryImagePreviewDialog", () => {
  it("has one responsive grid owner so the desktop preview does not reserve an empty column", () => {
    const markup = renderToStaticMarkup(createElement(GalleryImagePreviewDialog, {
      ariaLabel: "Image preview",
      imageUrl: "/preview.png",
      imageAlt: "Preview",
      title: "Output",
      body: "Generated image",
      providerNotesTitle: "Provider",
      downloadUrl: "/download.png",
      downloadLabel: "Download",
      closeLabel: "Close",
      onClose: () => undefined,
    }));

    expect(markup.match(/lg:grid-cols-\[minmax\(0,1fr\)_minmax\(320px,380px\)\]/g)).toHaveLength(1);
    expect(markup).toContain("bg-media-backdrop");
    expect(markup).not.toContain("bg-surface-inverse");
  });
});
