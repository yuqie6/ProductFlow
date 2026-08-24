import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { LocalImageEditDialog, type LocalImageEditDialogProps } from "./LocalImageEditDialog";
import localImageEditDialogSource from "./LocalImageEditDialog.tsx?raw";

function renderDialog(overrides: Partial<LocalImageEditDialogProps> = {}): string {
  return renderToStaticMarkup(
    createElement(LocalImageEditDialog, {
      sourceAsset: {
        assetId: "asset-source",
        url: "/media/source.png",
        sourceWidth: 1200,
        sourceHeight: 800,
      },
      operation: "remove",
      instruction: "移除水印",
      onSubmit: vi.fn(),
      onClose: vi.fn(),
      onKeepInLibrary: vi.fn(),
      onAdopt: vi.fn(),
      onContinue: vi.fn(),
      ...overrides,
    }),
  );
}

describe("LocalImageEditDialog", () => {
  it("renders the stable edit surface and blocks submit before source/canvas readiness", () => {
    const markup = renderDialog();

    expect(markup).toContain('data-local-edit-dialog');
    expect(markup).toContain('data-local-edit-stage');
    expect(markup).toContain('data-local-edit-mask-canvas');
    expect(markup).toContain('data-local-edit-operation');
    expect(markup).toContain("笔刷大小");
    expect(markup).toContain("硬度");
    expect(markup).toContain("擦除选区");
    expect(markup).toContain("清除选区");
    expect(markup).toContain("提交局部编辑");
    expect(markup).toContain('disabled=""');
  });

  it("renders source/result comparison and all three explicit completion commands", () => {
    const markup = renderDialog({ resultUrl: "/media/result.png" });

    expect(markup).toContain('data-local-edit-result');
    expect(markup).toContain('data-local-edit-result-tab="source"');
    expect(markup).toContain('data-local-edit-result-tab="result"');
    expect(markup).toContain('data-local-edit-result-preview');
    expect(markup).toContain('data-local-edit-comparison');
    expect(markup).toContain("仅保留在媒体库");
    expect(markup).toContain("采用为当前结果");
    expect(markup).toContain("继续编辑");
  });

  it("uses the replace-text fields and preserves caller-provided labels", () => {
    const markup = renderDialog({
      operation: "replace_text",
      sourceText: "旧标题",
      replacementText: "新标题",
      labels: {
        sourceText: "当前文字",
        replacementText: "目标文字",
      },
    });

    expect(markup).toContain("当前文字");
    expect(markup).toContain("目标文字");
    expect(markup).toContain("旧标题");
    expect(markup).toContain("新标题");
  });

  it("exposes all operation choices and keeps the initial operation selected", () => {
    const markup = renderDialog({
      operation: "inpaint",
      instruction: "补齐背景",
    });

    expect(markup).toContain('data-local-edit-operation-option="remove"');
    expect(markup).toContain('data-local-edit-operation-option="replace_text"');
    expect(markup).toContain('data-local-edit-operation-option="inpaint"');
    expect(markup).toContain('data-selected-operation="inpaint"');
    expect(markup).toContain("局部重绘");
  });

  it("gates unsupported operations without silently replacing an unsupported initial value", () => {
    const markup = renderDialog({
      operation: "replace_text",
      capability: { supported: true, operations: ["remove", "inpaint"] },
      sourceText: "旧标题",
      replacementText: "新标题",
    });

    expect(markup).toContain('data-selected-operation="replace_text"');
    expect(markup).toMatch(/data-local-edit-operation-option="replace_text"[^>]*disabled=""/);
    expect(markup).not.toMatch(/data-local-edit-operation-option="remove"[^>]*disabled=""/);
    expect(markup).toContain('data-local-edit-operation-unavailable');
    expect(markup).toContain("不支持的局部编辑操作");
  });

  it("uses the internally selected operation for validation and submit fields", () => {
    const source = localImageEditDialogSource;
    expect(source).toContain("const [selectedOperation, setSelectedOperation] = useState");
    expect(source).toContain("onClick={() => setSelectedOperation(candidate)}");
    expect(source).toContain("operation: selectedOperation");
    expect(source).toContain('selectedOperation === "replace_text" ? null : instruction.trim()');
    expect(source).toContain('selectedOperation === "replace_text" ? sourceText.trim() : null');
    expect(source).toContain('selectedOperation === "replace_text" ? replacementText.trim() : null');
  });

  it("shows adoption reversal only when adopted and a revert handler is provided", () => {
    const adopted = renderDialog({
      resultUrl: "/media/result.png",
      adopted: true,
      onRevert: vi.fn(),
    });
    expect(adopted).toContain('data-local-edit-adopted="true"');
    expect(adopted).toContain("撤销采用");
    expect(adopted).not.toContain("采用为当前结果");

    const adoptedWithoutHandler = renderDialog({
      resultUrl: "/media/result.png",
      adopted: true,
      onRevert: undefined,
    });
    expect(adoptedWithoutHandler).not.toContain("撤销采用");
    expect(adoptedWithoutHandler).not.toContain("采用为当前结果");
  });

  it("shows durable task status, provider failure, and explicit retry controls", () => {
    const markup = renderDialog({
      taskStatus: "failed",
      providerName: "openai_images",
      progressPhase: "provider_call",
      failureReason: "供应商超时",
      isRetryable: true,
      onRetry: vi.fn(),
    });

    expect(markup).toContain('data-local-edit-task-status="failed"');
    expect(markup).toContain("供应商超时");
    expect(markup).toContain("openai_images");
    expect(markup).toContain("重试编辑");
  });

  it("shows the persisted request instead of a blank editable mask after task recovery", () => {
    const markup = renderDialog({
      taskStatus: "draft",
      operation: "replace_text",
      sourceText: "旧标题",
      replacementText: "新标题",
      onRetry: vi.fn(),
    });

    expect(markup).toContain('data-local-edit-persisted-request="true"');
    expect(markup).toContain("旧标题");
    expect(markup).toContain("新标题");
    expect(markup).toContain("重试编辑");
    expect(markup).not.toContain("data-local-edit-mask-canvas");
    expect(markup).not.toContain("提交局部编辑");
  });

  it("keeps pointer painting on dirty preview scheduling instead of full-mask work", () => {
    const source = localImageEditDialogSource;
    const paintStart = source.indexOf("const paintTo = useCallback");
    const pointerDownStart = source.indexOf("const handlePointerDown", paintStart);
    const paintBlock = source.slice(paintStart, pointerDownStart);
    expect(paintBlock).not.toContain("summarizeMaskAlpha");
    expect(paintBlock).not.toContain("setMaskRevision");
    expect(source).toContain("window.requestAnimationFrame(flushMaskPreview)");
    expect(source).toContain("context.putImageData(pixels, bounds.left, bounds.top)");

    const submitStart = source.indexOf("const handleSubmit = useCallback");
    const commandStart = source.indexOf("const runCommand", submitStart);
    const submitBlock = source.slice(submitStart, commandStart);
    expect(submitBlock).toContain("renderMaskCanvas(maskCanvasRef.current, mask, sourceWidth, sourceHeight)");
  });

  it("resets the internally selected operation when the requested task operation changes", () => {
    const source = localImageEditDialogSource;
    expect(source).toContain("setSelectedOperation(initialOperation)");
  });
});
