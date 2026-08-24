import { createElement, type ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { DeliveryExportButton } from "./ProductImageExplorer";

describe("delivery export command", () => {
  it("renders an enabled command for a complete rendition selection", () => {
    const onExport = vi.fn();
    const markup = renderToStaticMarkup(
      createElement(DeliveryExportButton, {
        eligibility: { eligible: true, renditionJobIds: ["job-1"], reason: null },
        busy: false,
        exporting: false,
        label: "Export delivery package",
        exportingLabel: "Exporting delivery package",
        reasonLabel: null,
        onExport,
      }),
    );

    expect(markup).toContain('data-testid="delivery-export-button"');
    expect(markup).not.toMatch(/<button[^>]*\sdisabled(?:=|\s|>)/);
    expect(markup).toContain("Export delivery package");

    const element = DeliveryExportButton({
      eligibility: { eligible: true, renditionJobIds: ["job-1"], reason: null },
      busy: false,
      exporting: false,
      label: "Export delivery package",
      exportingLabel: "Exporting delivery package",
      reasonLabel: null,
      onExport,
    });
    const button = (element.props as {
      children: [ReactElement<{ onClick: () => void }>, ReactElement?];
    }).children[0];
    button.props.onClick();
    expect(onExport).toHaveBeenCalledWith(["job-1"]);
  });

  it("renders the localized reason and disabled state for an invalid selection", () => {
    const markup = renderToStaticMarkup(
      createElement(DeliveryExportButton, {
        eligibility: { eligible: false, renditionJobIds: [], reason: "duplicate_job_id" },
        busy: false,
        exporting: false,
        label: "Export delivery package",
        exportingLabel: "Exporting delivery package",
        reasonLabel: "The selection contains duplicate delivery jobs",
        onExport: vi.fn(),
      }),
    );

    expect(markup).toContain("disabled");
    expect(markup).toContain('data-testid="delivery-export-reason"');
    expect(markup).toContain("duplicate delivery jobs");
  });

  it("keeps a stable busy command while the export request is pending", () => {
    const markup = renderToStaticMarkup(
      createElement(DeliveryExportButton, {
        eligibility: { eligible: true, renditionJobIds: ["job-1"], reason: null },
        busy: true,
        exporting: true,
        label: "Export delivery package",
        exportingLabel: "Exporting delivery package",
        reasonLabel: null,
        onExport: vi.fn(),
      }),
    );

    expect(markup).toContain("disabled");
    expect(markup).toContain("Exporting delivery package");
    expect(markup).toContain("animate-spin");
  });
});
