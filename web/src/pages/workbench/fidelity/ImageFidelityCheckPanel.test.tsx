import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import {
  buildImageFidelityCheckSubmitPayload,
  ImageFidelityCheckPanel,
  type ImageFidelityCheckDraft,
  type ImageFidelityCheckPanelProps,
} from "./ImageFidelityCheckPanel";
import panelSourceText from "./ImageFidelityCheckPanel.tsx?raw";

const emptyDraft: ImageFidelityCheckDraft = {
  shape_fidelity: null,
  color_material_fidelity: null,
  logo_text_legibility: null,
  text_policy_compliance: null,
  notes: "",
};

const completeDraft: ImageFidelityCheckDraft = {
  shape_fidelity: "pass",
  color_material_fidelity: "fail",
  logo_text_legibility: "not_applicable",
  text_policy_compliance: "pass",
  notes: "人工复核记录",
};

function renderPanel(overrides: Partial<ImageFidelityCheckPanelProps> = {}): string {
  return renderToStaticMarkup(
    createElement(ImageFidelityCheckPanel, {
      value: emptyDraft,
      onChange: vi.fn(),
      latestVersion: 0,
      latestCheck: null,
      onSubmit: vi.fn(),
      onReload: vi.fn(),
      ...overrides,
    }),
  );
}

describe("ImageFidelityCheckPanel", () => {
  it("renders four independent selectors and enables submit only when all four are selected", () => {
    const incomplete = renderPanel();
    expect(incomplete).toContain('data-fidelity-field="shape_fidelity"');
    expect(incomplete).toContain('data-fidelity-field="color_material_fidelity"');
    expect(incomplete).toContain('data-fidelity-field="logo_text_legibility"');
    expect(incomplete).toContain('data-fidelity-field="text_policy_compliance"');
    expect(incomplete).toContain('data-fidelity-outcome="not_applicable"');
    expect(incomplete).toMatch(/data-fidelity-submit[^>]*disabled=""/);

    const complete = renderPanel({ value: completeDraft, latestVersion: 3 });
    expect((complete.match(/aria-pressed="true"/g) ?? [])).toHaveLength(4);
    expect(complete).toContain('data-fidelity-latest-version');
    expect(complete).not.toMatch(/data-fidelity-submit[^>]*disabled=""/);
  });

  it("keeps each outcome independent and includes expected version and n/a in the payload", () => {
    expect(buildImageFidelityCheckSubmitPayload(completeDraft, 7)).toEqual({
      expectedLatestVersion: 7,
      shape_fidelity: "pass",
      color_material_fidelity: "fail",
      logo_text_legibility: "not_applicable",
      text_policy_compliance: "pass",
      notes: "人工复核记录",
    });
    expect(buildImageFidelityCheckSubmitPayload(emptyDraft, 7)).toBeNull();
    expect(buildImageFidelityCheckSubmitPayload(completeDraft, -1)).toBeNull();
  });

  it("enforces the 4000 character notes boundary without generating an idempotency key", () => {
    const tooLong = { ...completeDraft, notes: "a".repeat(4001) };
    expect(buildImageFidelityCheckSubmitPayload(tooLong, 0)).toBeNull();

    const markup = renderPanel({ value: completeDraft });
    expect(markup).toContain('maxLength="4000"');
    expect(markup).not.toContain("idempotency_key");
  });

  it("shows latest, empty, loading, error, conflict, and reload states explicitly", () => {
    const latest = renderPanel({
      latestVersion: 2,
      latestCheck: {
        version: 2,
        shape_fidelity: "pass",
        color_material_fidelity: "fail",
        logo_text_legibility: "not_applicable",
        text_policy_compliance: "pass",
        notes: "保留细节",
        checked_by: "administrator",
        created_at: "2026-08-24T10:00:00Z",
      },
    });
    expect(latest).toContain('data-fidelity-latest="true"');
    expect(latest).toContain("administrator");
    expect(latest).toContain("保留细节");

    const empty = renderPanel();
    expect(empty).toContain('data-fidelity-empty="true"');
    expect(empty).toContain("尚无人工检查记录");

    const loading = renderPanel({ loading: true });
    expect(loading).toContain('data-fidelity-loading="true"');
    expect(loading).toMatch(/data-fidelity-submit[^>]*disabled=""/);

    const failure = renderPanel({ error: "网络失败", conflict: "版本已变化", reloading: true });
    expect(failure).toContain('data-fidelity-error="true"');
    expect(failure).toContain('data-fidelity-conflict="true"');
    expect(failure).toContain('data-fidelity-reloading="true"');
    expect(failure).toContain("网络失败");
    expect(failure).toContain("版本已变化");
    expect(failure).toContain("正在重新加载");
  });

  it("blocks submission when the latest persisted version is unknown", () => {
    const markup = renderPanel({
      value: completeDraft,
      latestVersion: 0,
      latestVersionKnown: false,
      error: "最新版本读取失败",
    });

    expect(markup).toContain("最新版本读取失败");
    expect(markup).toMatch(/data-fidelity-submit[^>]*disabled=""/);
  });

  it("keeps the draft controlled and contains no score or delete action", () => {
    expect(panelSourceText).not.toContain("useState");
    expect(panelSourceText).toContain("onChange(nextValue)");
    expect(panelSourceText).toContain("value={value.notes}");
    expect(panelSourceText).not.toContain("delete");
    expect(panelSourceText).not.toContain("score");
  });
});
