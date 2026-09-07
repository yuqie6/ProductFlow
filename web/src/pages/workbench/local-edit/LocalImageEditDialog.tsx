import {
  Check,
  Eraser,
  Image as ImageIcon,
  Library,
  Loader2,
  PencilLine,
  RotateCcw,
  X,
} from "lucide-react";
import { useCallback, useEffect, useId, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";

import { cn } from "../../../components/ui/cn";
import { CONTROL_CLASS } from "../../../components/ui/field";
import type { LocalImageEditTaskStatus } from "../../../lib/types";
import { Dialog, DialogContent, DialogTitle } from "../../../components/ui/dialog";
import { IconButton } from "../../../components/ui/icon-button";
import { humanizeTechnicalKey } from "../canvas/graphRunDisplay";
import {
  clampSourcePoint,
  createContainTransform,
  createProtectedMask,
  brushSegmentBounds,
  invertAffineTransform,
  mapDisplayPointToSource,
  mergeBrushBounds,
  paintMaskSegment,
  summarizeMaskAlpha,
  type LocalEditOperation,
  type LocalEditBrushBounds,
  type LocalEditPoint,
  type LocalEditMaskSummary,
  type LocalEditValidationIssue,
  validateLocalEditFields,
} from "./localEditGeometry";

export interface LocalImageEditSource {
  assetId?: string | null;
  url: string;
  sourceWidth: number;
  sourceHeight: number;
  alt?: string;
}

export interface LocalImageEditCapability {
  supported: boolean;
  message?: string | null;
  operations?: readonly LocalEditOperation[] | null;
}

export interface LocalImageEditLabels {
  title: string;
  editState: string;
  resultState: string;
  source: string;
  result: string;
  compare: string;
  operation: string;
  instruction: string;
  sourceText: string;
  replacementText: string;
  brushSize: string;
  hardness: string;
  eraseSelection: string;
  clear: string;
  submit: string;
  close: string;
  sourceLoading: string;
  sourceLoadFailed: string;
  actionFailed: string;
  capabilityUnavailable: string;
  targetNodeImpact: string;
  resultReady: string;
  keepInLibrary: string;
  adopt: string;
  revertAdoption: string;
  continueEdit: string;
  maskCanvasLabel: string;
  sourceAlt: string;
  resultAlt: string;
  statusLabels: Record<LocalImageEditTaskStatus, string>;
  cancelTask: string;
  retryTask: string;
  phase: string;
  provider: string;
  progressPhases: Record<string, string>;
  providers: Record<string, string>;
  operations: Partial<Record<LocalEditOperation, string>>;
  validationMessages: Partial<Record<LocalEditValidationIssue["code"], string>>;
}

export interface LocalImageEditSubmitPayload {
  sourceAssetId: string | null;
  operation: LocalEditOperation;
  instruction: string | null;
  sourceText: string | null;
  replacementText: string | null;
  mask: Blob;
  maskMimeType: "image/png";
  maskWidth: number;
  maskHeight: number;
  maskGeometry: {
    sourceWidth: number;
    sourceHeight: number;
    viewportWidth: number;
    viewportHeight: number;
    viewportToSource: [number, number, number, number, number, number];
    transformDirection: "viewport_to_source";
  };
}

export interface LocalImageEditDialogProps {
  open?: boolean;
  sourceAsset: LocalImageEditSource;
  resultUrl?: string | null;
  operation: LocalEditOperation;
  instruction?: string | null;
  sourceText?: string | null;
  replacementText?: string | null;
  labels?: Partial<LocalImageEditLabels> & {
    operations?: Partial<Record<LocalEditOperation, string>>;
    validationMessages?: Partial<Record<LocalEditValidationIssue["code"], string>>;
  };
  busy?: boolean;
  error?: string | null;
  capability?: LocalImageEditCapability;
  taskStatus?: LocalImageEditTaskStatus | null;
  progressPhase?: string | null;
  providerName?: string | null;
  failureReason?: string | null;
  isRetryable?: boolean;
  isCancelable?: boolean;
  targetNodeImpact?: string | null;
  adopted?: boolean;
  onSubmit: (payload: LocalImageEditSubmitPayload) => void | Promise<void>;
  onClose: () => void;
  onKeepInLibrary: () => void | Promise<void>;
  onAdopt?: () => void | Promise<void>;
  onRevert?: () => void | Promise<void>;
  onContinue: () => void | Promise<void>;
  onCancel?: () => void | Promise<void>;
  onRetry?: () => void | Promise<void>;
}

const DEFAULT_LABELS: LocalImageEditLabels = {
  title: "局部编辑",
  editState: "编辑",
  resultState: "编辑结果",
  source: "源图",
  result: "结果",
  compare: "对比",
  operation: "操作",
  instruction: "编辑要求",
  sourceText: "原文",
  replacementText: "替换为",
  brushSize: "笔刷大小",
  hardness: "硬度",
  eraseSelection: "擦除选区",
  clear: "清除选区",
  submit: "提交局部编辑",
  close: "关闭",
  sourceLoading: "正在加载源图",
  sourceLoadFailed: "源图加载失败，无法提交。",
  actionFailed: "操作失败，请重试。",
  capabilityUnavailable: "当前图片供应商不支持局部编辑。",
  targetNodeImpact: "目标节点影响",
  resultReady: "编辑结果已生成",
  keepInLibrary: "仅保留在媒体库",
  adopt: "采用为当前结果",
  revertAdoption: "撤销采用",
  continueEdit: "继续编辑",
  maskCanvasLabel: "局部编辑选区",
  sourceAlt: "源图",
  resultAlt: "编辑结果",
  statusLabels: {
    draft: "草稿",
    queued: "等待处理",
    running: "处理中",
    succeeded: "已完成",
    failed: "失败",
    cancelled: "已取消",
    unknown: "结果未知",
  },
  cancelTask: "取消编辑",
  retryTask: "重试编辑",
  phase: "阶段",
  provider: "供应商",
  progressPhases: {
    claimed: "已受理",
    prepared: "已准备",
    provider_call: "调用模型中",
    provider_result_received: "已收到结果",
    requeued_after_idle: "已重新排队",
  },
  providers: {
    mock: "Mock",
    openai_responses: "OpenAI Responses",
    openai_images: "OpenAI Images API",
    google_gemini_image: "Google Gemini Image",
  },
  operations: {
    remove: "移除",
    replace_text: "替换文字",
    inpaint: "局部重绘",
  },
  validationMessages: {
    unsupported_operation: "不支持的局部编辑操作。",
    source_unavailable: "源图尚未成功加载。",
    invalid_source_dimensions: "源图尺寸无效。",
    instruction_required: "请填写编辑要求。",
    source_text_required: "请填写原文。",
    replacement_text_required: "请填写替换文字。",
    mask_required: "请标记需要编辑的区域。",
    mask_size_mismatch: "选区尺寸与源图不一致。",
    mask_needs_edit_and_protection: "选区必须同时包含编辑区域和保护区域。",
  },
};

function mergeLabels(labels: LocalImageEditDialogProps["labels"]): LocalImageEditLabels {
  return {
    ...DEFAULT_LABELS,
    ...labels,
    operations: { ...DEFAULT_LABELS.operations, ...labels?.operations },
    validationMessages: { ...DEFAULT_LABELS.validationMessages, ...labels?.validationMessages },
    progressPhases: { ...DEFAULT_LABELS.progressPhases, ...labels?.progressPhases },
    providers: { ...DEFAULT_LABELS.providers, ...labels?.providers },
  };
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

function canvasToPng(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob);
      else reject(new Error("无法导出 PNG mask"));
    }, "image/png");
  });
}

function renderMaskCanvas(
  canvas: HTMLCanvasElement,
  mask: Uint8ClampedArray,
  sourceWidth: number,
  sourceHeight: number,
): void {
  canvas.width = sourceWidth;
  canvas.height = sourceHeight;
  const context = canvas.getContext("2d");
  if (!context) return;
  const pixels = context.createImageData(sourceWidth, sourceHeight);
  for (let index = 0; index < mask.length; index += 1) {
    const offset = index * 4;
    pixels.data[offset] = 255;
    pixels.data[offset + 1] = 255;
    pixels.data[offset + 2] = 255;
    pixels.data[offset + 3] = mask[index];
  }
  context.putImageData(pixels, 0, 0);
}

function clearMaskPreview(
  canvas: HTMLCanvasElement,
  sourceWidth: number,
  sourceHeight: number,
): void {
  canvas.width = sourceWidth;
  canvas.height = sourceHeight;
  const context = canvas.getContext("2d");
  context?.clearRect(0, 0, sourceWidth, sourceHeight);
}

function renderMaskPreviewRegion(
  canvas: HTMLCanvasElement,
  mask: Uint8ClampedArray,
  sourceWidth: number,
  bounds: LocalEditBrushBounds,
): void {
  const width = bounds.right - bounds.left;
  const height = bounds.bottom - bounds.top;
  if (
    width < 1
    || height < 1
    || bounds.left < 0
    || bounds.top < 0
    || bounds.right > sourceWidth
    || bounds.bottom > canvas.height
  ) {
    return;
  }
  const context = canvas.getContext("2d");
  if (!context) return;
  const pixels = context.createImageData(width, height);
  let offset = 0;
  for (let y = bounds.top; y < bounds.bottom; y += 1) {
    for (let x = bounds.left; x < bounds.right; x += 1) {
      const selectionOpacity = Math.round((255 - mask[y * sourceWidth + x]) * 0.48);
      pixels.data[offset] = 244;
      pixels.data[offset + 1] = 63;
      pixels.data[offset + 2] = 94;
      pixels.data[offset + 3] = selectionOpacity;
      offset += 4;
    }
  }
  context.putImageData(pixels, bounds.left, bounds.top);
}

function issueLabel(labels: LocalImageEditLabels, issue: LocalEditValidationIssue | null): string | null {
  return issue ? labels.validationMessages[issue.code] ?? null : null;
}

export function LocalImageEditDialog({
  open = true,
  sourceAsset,
  resultUrl = null,
  operation: initialOperation,
  instruction: initialInstruction = null,
  sourceText: initialSourceText = null,
  replacementText: initialReplacementText = null,
  labels: labelOverrides,
  busy = false,
  error = null,
  capability,
  taskStatus = null,
  progressPhase = null,
  providerName = null,
  failureReason = null,
  isRetryable = false,
  isCancelable = false,
  targetNodeImpact = null,
  adopted = false,
  onSubmit,
  onClose,
  onKeepInLibrary,
  onAdopt,
  onRevert,
  onContinue,
  onCancel,
  onRetry,
}: LocalImageEditDialogProps) {
  const labels = useMemo(() => mergeLabels(labelOverrides), [labelOverrides]);
  const titleId = useId();
  const maskCanvasRef = useRef<HTMLCanvasElement>(null);
  const maskPreviewCanvasRef = useRef<HTMLCanvasElement>(null);
  const maskRef = useRef<Uint8ClampedArray | null>(null);
  const pointerRef = useRef<LocalEditPoint | null>(null);
  const previewRafRef = useRef<number | null>(null);
  const dirtyBoundsRef = useRef<LocalEditBrushBounds | null>(null);
  const [sourceReady, setSourceReady] = useState(false);
  const [sourceError, setSourceError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [commandError, setCommandError] = useState<string | null>(null);
  const [maskSummary, setMaskSummary] = useState<LocalEditMaskSummary | null>(null);
  const [brushSize, setBrushSize] = useState(48);
  const [hardness, setHardness] = useState(0.72);
  const [eraseSelection, setEraseSelection] = useState(false);
  const [instruction, setInstruction] = useState(initialInstruction ?? "");
  const [sourceText, setSourceText] = useState(initialSourceText ?? "");
  const [replacementText, setReplacementText] = useState(initialReplacementText ?? "");
  const [selectedOperation, setSelectedOperation] = useState<LocalEditOperation>(initialOperation);
  const [resultView, setResultView] = useState<"source" | "result">("result");

  const sourceWidth = sourceAsset.sourceWidth;
  const sourceHeight = sourceAsset.sourceHeight;
  const validDimensions = Number.isInteger(sourceWidth)
    && Number.isInteger(sourceHeight)
    && sourceWidth > 0
    && sourceHeight > 0;
  const renderWidth = validDimensions ? sourceWidth : 1;
  const renderHeight = validDimensions ? sourceHeight : 1;
  const hasResult = Boolean(resultUrl);
  const capabilitySupported = capability?.supported !== false;
  const operationList = capability?.operations;
  const operationSupported = capabilitySupported
    && (operationList === undefined || operationList === null || operationList.includes(selectedOperation));
  const operationOptions: readonly LocalEditOperation[] = ["remove", "replace_text", "inpaint"];

  const cancelPreviewFrame = useCallback(() => {
    const frame = previewRafRef.current;
    if (frame !== null && typeof window !== "undefined") {
      window.cancelAnimationFrame(frame);
    }
    previewRafRef.current = null;
    dirtyBoundsRef.current = null;
  }, []);

  useEffect(() => {
    setSelectedOperation(initialOperation);
    setInstruction(initialInstruction ?? "");
    setSourceText(initialSourceText ?? "");
    setReplacementText(initialReplacementText ?? "");
  }, [initialInstruction, initialReplacementText, initialSourceText, initialOperation]);

  useEffect(() => {
    setResultView(resultUrl ? "result" : "source");
  }, [resultUrl]);

  useEffect(() => {
    setSourceReady(false);
    setSourceError(null);
    setSubmitError(null);
    cancelPreviewFrame();
    pointerRef.current = null;
    setMaskSummary(null);
    if (maskPreviewCanvasRef.current) {
      clearMaskPreview(maskPreviewCanvasRef.current, renderWidth, renderHeight);
    }
    if (!validDimensions || !sourceAsset.url.trim()) {
      maskRef.current = null;
      setSourceError(labels.sourceLoadFailed);
      return;
    }
    maskRef.current = createProtectedMask(sourceWidth, sourceHeight);
    const protectedSummary = {
      editPixelCount: 0,
      protectedPixelCount: sourceWidth * sourceHeight,
      featheredPixelCount: 0,
    };
    setMaskSummary(protectedSummary);
  }, [cancelPreviewFrame, labels.sourceLoadFailed, renderHeight, renderWidth, sourceAsset.assetId, sourceAsset.url, sourceHeight, sourceWidth, validDimensions]);

  useEffect(() => cancelPreviewFrame, [cancelPreviewFrame]);

  const markSourceReady = useCallback((image: HTMLImageElement) => {
    if (
      !image.naturalWidth
      || !image.naturalHeight
      || !validDimensions
      || image.naturalWidth !== sourceWidth
      || image.naturalHeight !== sourceHeight
    ) {
      setSourceError(labels.sourceLoadFailed);
      setSourceReady(false);
      return;
    }
    const decode = typeof image.decode === "function" ? image.decode() : Promise.resolve();
    void decode.then(() => {
      setSourceError(null);
      setSourceReady(true);
    }).catch(() => {
      setSourceReady(false);
      setSourceError(labels.sourceLoadFailed);
    });
  }, [labels.sourceLoadFailed, sourceHeight, sourceWidth, validDimensions]);

  const sourceLoadError = useCallback(() => {
    setSourceReady(false);
    setSourceError(labels.sourceLoadFailed);
  }, [labels.sourceLoadFailed]);

  const eventToSourcePoint = useCallback((event: ReactPointerEvent<HTMLCanvasElement>): LocalEditPoint | null => {
    if (!validDimensions) return null;
    const canvas = event.currentTarget;
    const rect = canvas.getBoundingClientRect();
    if (!rect.width || !rect.height) return null;
    const sourceToDisplay = createContainTransform(sourceWidth, sourceHeight, rect.width, rect.height);
    if (!sourceToDisplay) return null;
    const displayPoint = { x: event.clientX - rect.left, y: event.clientY - rect.top };
    const sourcePoint = mapDisplayPointToSource(displayPoint, sourceToDisplay);
    return sourcePoint ? clampSourcePoint(sourcePoint, sourceWidth, sourceHeight) : null;
  }, [sourceHeight, sourceWidth, validDimensions]);

  const flushMaskPreview = useCallback(() => {
    previewRafRef.current = null;
    const dirtyBounds = dirtyBoundsRef.current;
    dirtyBoundsRef.current = null;
    const mask = maskRef.current;
    if (!dirtyBounds || !mask || !sourceReady || !maskPreviewCanvasRef.current || !validDimensions) return;
    renderMaskPreviewRegion(maskPreviewCanvasRef.current, mask, sourceWidth, dirtyBounds);
  }, [sourceHeight, sourceWidth, sourceReady, validDimensions]);

  const scheduleMaskPreview = useCallback((dirtyBounds: LocalEditBrushBounds | null) => {
    if (!dirtyBounds) return;
    dirtyBoundsRef.current = mergeBrushBounds(dirtyBoundsRef.current, dirtyBounds);
    if (previewRafRef.current !== null) return;
    if (typeof window === "undefined" || typeof window.requestAnimationFrame !== "function") {
      flushMaskPreview();
      return;
    }
    previewRafRef.current = window.requestAnimationFrame(flushMaskPreview);
  }, [flushMaskPreview]);

  const refreshMaskSummary = useCallback((): LocalEditMaskSummary | null => {
    const mask = maskRef.current;
    const summary = mask ? summarizeMaskAlpha(mask) : null;
    setMaskSummary(summary);
    return summary;
  }, []);

  const paintTo = useCallback((from: LocalEditPoint, to: LocalEditPoint) => {
    const mask = maskRef.current;
    if (!mask || busy || !sourceReady) return;
    const dirtyBounds = brushSegmentBounds(from, to, brushSize, sourceWidth, sourceHeight);
    paintMaskSegment(
      mask,
      sourceWidth,
      sourceHeight,
      from,
      to,
      brushSize,
      hardness,
      eraseSelection,
    );
    scheduleMaskPreview(dirtyBounds);
  }, [brushSize, busy, eraseSelection, hardness, scheduleMaskPreview, sourceHeight, sourceReady, sourceWidth]);

  const handlePointerDown = useCallback((event: ReactPointerEvent<HTMLCanvasElement>) => {
    if (busy || !sourceReady) return;
    const point = eventToSourcePoint(event);
    if (!point) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    pointerRef.current = point;
    paintTo(point, point);
  }, [busy, eventToSourcePoint, paintTo, sourceReady]);

  const handlePointerMove = useCallback((event: ReactPointerEvent<HTMLCanvasElement>) => {
    const previous = pointerRef.current;
    if (!previous || busy || !sourceReady) return;
    const point = eventToSourcePoint(event);
    if (!point) return;
    paintTo(previous, point);
    pointerRef.current = point;
  }, [busy, eventToSourcePoint, paintTo, sourceReady]);

  const stopPointer = useCallback((event: ReactPointerEvent<HTMLCanvasElement>) => {
    const wasPainting = pointerRef.current !== null;
    pointerRef.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    if (wasPainting) refreshMaskSummary();
  }, [refreshMaskSummary]);

  const clearMask = useCallback(() => {
    if (!validDimensions || busy) return;
    cancelPreviewFrame();
    maskRef.current = createProtectedMask(sourceWidth, sourceHeight);
    const protectedSummary = {
      editPixelCount: 0,
      protectedPixelCount: sourceWidth * sourceHeight,
      featheredPixelCount: 0,
    };
    setMaskSummary(protectedSummary);
    if (maskPreviewCanvasRef.current) {
      clearMaskPreview(maskPreviewCanvasRef.current, sourceWidth, sourceHeight);
    }
  }, [busy, cancelPreviewFrame, sourceHeight, sourceWidth, validDimensions]);

  const validationIssue = useMemo(() => validateLocalEditFields({
    operation: selectedOperation,
    instruction,
    sourceText,
    replacementText,
    sourceReady,
    sourceWidth,
    sourceHeight,
    mask: null,
    maskSummary,
  }), [instruction, maskSummary, replacementText, selectedOperation, sourceHeight, sourceReady, sourceText, sourceWidth]);
  const validationText = issueLabel(labels, validationIssue);
  const capabilityText = capability?.supported === false
    ? capability.message || labels.capabilityUnavailable
    : null;
  const operationCapabilityText = !operationSupported
    ? capabilityText || labels.validationMessages.unsupported_operation || labels.capabilityUnavailable
    : null;
  const visibleError = sourceError || error || submitError || commandError;
  const canSubmit = !busy && !taskStatus && operationSupported && !sourceError && sourceReady && !validationIssue;

  const handleSubmit = useCallback(async () => {
    setSubmitError(null);
    const mask = maskRef.current;
    const currentSummary = mask ? summarizeMaskAlpha(mask) : null;
    setMaskSummary(currentSummary);
    const issue = validateLocalEditFields({
      operation: selectedOperation,
      instruction,
      sourceText,
      replacementText,
      sourceReady,
      sourceWidth,
      sourceHeight,
      mask,
      maskSummary: currentSummary,
    });
    if (!operationSupported) {
      setSubmitError(operationCapabilityText || labels.capabilityUnavailable);
      return;
    }
    if (issue) {
      setSubmitError(issueLabel(labels, issue) || labels.validationMessages.mask_required || "无法提交。");
      return;
    }
    if (!maskCanvasRef.current || !mask || !maskPreviewCanvasRef.current) {
      setSubmitError(labels.validationMessages.mask_required || "无法提交。");
      return;
    }
    const viewport = maskPreviewCanvasRef.current.getBoundingClientRect();
    const sourceToViewport = createContainTransform(sourceWidth, sourceHeight, viewport.width, viewport.height);
    const viewportToSource = sourceToViewport ? invertAffineTransform(sourceToViewport) : null;
    if (!viewportToSource) {
      setSubmitError(labels.sourceLoadFailed);
      return;
    }
    try {
      renderMaskCanvas(maskCanvasRef.current, mask, sourceWidth, sourceHeight);
      const maskBlob = await canvasToPng(maskCanvasRef.current);
      await onSubmit({
        sourceAssetId: sourceAsset.assetId ?? null,
        operation: selectedOperation,
        instruction: selectedOperation === "replace_text" ? null : instruction.trim(),
        sourceText: selectedOperation === "replace_text" ? sourceText.trim() : null,
        replacementText: selectedOperation === "replace_text" ? replacementText.trim() : null,
        mask: maskBlob,
        maskMimeType: "image/png",
        maskWidth: sourceWidth,
        maskHeight: sourceHeight,
        maskGeometry: {
          sourceWidth,
          sourceHeight,
          viewportWidth: viewport.width,
          viewportHeight: viewport.height,
          viewportToSource: [
            viewportToSource.a,
            viewportToSource.b,
            viewportToSource.c,
            viewportToSource.d,
            viewportToSource.e,
            viewportToSource.f,
          ],
          transformDirection: "viewport_to_source",
        },
      });
    } catch (submitFailure) {
      setSubmitError(errorMessage(submitFailure, labels.actionFailed));
    }
  }, [instruction, labels, onSubmit, operationCapabilityText, operationSupported, replacementText, selectedOperation, sourceAsset.assetId, sourceHeight, sourceReady, sourceText, sourceWidth]);

  const runCommand = useCallback(async (command: () => void | Promise<void>) => {
    setCommandError(null);
    try {
      await command();
    } catch (commandFailure) {
      setCommandError(errorMessage(commandFailure, labels.actionFailed));
    }
  }, [labels.actionFailed]);

  const handleResultViewKey = useCallback((view: "source" | "result") => {
    setResultView(view);
  }, []);

  if (!open) return null;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent
        hideClose
        labelledBy={titleId}
        className="flex max-h-[92dvh] w-full max-w-4xl flex-col overflow-hidden"
        bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden p-0"
      >
        <div data-local-edit-dialog className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <header className="flex shrink-0 items-center justify-between gap-3 border-b border-border-l1 px-4 py-3 sm:px-5">
            <div className="min-w-0">
              <DialogTitle id={titleId} className="truncate text-sm font-semibold">
                {labels.title}
              </DialogTitle>
              <p className="mt-0.5 text-xs text-text-muted">{hasResult ? labels.resultState : labels.editState}</p>
            </div>
            <IconButton
              label={labels.close}
              variant="ghost"
              size="md"
              onClick={onClose}
            >
              <X size={18} aria-hidden="true" />
            </IconButton>
          </header>

          <div className="min-h-0 overflow-y-auto px-4 py-4 sm:px-5">
            {visibleError ? (
              <div role="alert" data-local-edit-error className="mb-3 rounded-lg border border-state-error/30 bg-state-error/10 px-3 py-2 text-xs leading-5 text-state-error">
                {visibleError}
              </div>
            ) : null}
            {!sourceReady && !sourceError ? (
              <div role="status" data-local-edit-source-loading className="mb-3 flex items-center gap-2 text-xs text-text-muted">
                <ImageIcon size={15} aria-hidden="true" />
                {labels.sourceLoading}
              </div>
            ) : null}
            {targetNodeImpact ? (
              <div data-local-edit-target-impact className="mb-4 border-l-2 border-accent/60 pl-3 text-xs leading-5 text-text-secondary">
                <span className="font-semibold text-text-primary">{labels.targetNodeImpact}</span>
                <span className="ml-2">{targetNodeImpact}</span>
              </div>
            ) : null}
            {taskStatus ? (
              <TaskStatusState
                labels={labels}
                status={taskStatus}
                progressPhase={progressPhase}
                providerName={providerName}
                failureReason={failureReason}
                isRetryable={isRetryable}
                isCancelable={isCancelable}
                busy={busy}
                onCancel={onCancel ? () => void runCommand(onCancel) : undefined}
                onRetry={onRetry ? () => void runCommand(onRetry) : undefined}
              />
            ) : null}

            {hasResult && resultUrl ? (
              <ResultState
                labels={labels}
                sourceUrl={sourceAsset.url}
                resultUrl={resultUrl}
                resultView={resultView}
                onResultViewChange={handleResultViewKey}
                onSourceLoad={markSourceReady}
                onSourceError={sourceLoadError}
                busy={busy}
                onKeepInLibrary={() => void runCommand(onKeepInLibrary)}
                adopted={adopted}
                onAdopt={onAdopt && !adopted ? () => void runCommand(onAdopt) : undefined}
                onRevert={onRevert && adopted ? () => void runCommand(onRevert) : undefined}
                onContinue={() => void runCommand(onContinue)}
              />
            ) : taskStatus ? (
              <PersistedTaskRequest
                labels={labels}
                sourceUrl={sourceAsset.url}
                sourceAlt={sourceAsset.alt || labels.sourceAlt}
                operation={selectedOperation}
                instruction={instruction}
                sourceText={sourceText}
                replacementText={replacementText}
              />
            ) : (
              <fieldset disabled={busy} className="min-w-0 space-y-4 border-0 p-0">
                <legend className="sr-only">{labels.editState}</legend>
                <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs text-text-secondary">
                  <span className="font-semibold">{labels.operation}</span>
                  <div
                    role="group"
                    aria-label={labels.operation}
                    aria-invalid={!operationSupported}
                    data-local-edit-operation
                    data-selected-operation={selectedOperation}
                    className="inline-flex min-h-10 min-w-0 flex-wrap rounded-lg border border-border-l1 bg-surface-subtle p-0.5"
                  >
                    {operationOptions.map((candidate) => {
                      const candidateSupported = capabilitySupported
                        && (operationList === undefined || operationList === null || operationList.includes(candidate));
                      return (
                        <button
                          key={candidate}
                          type="button"
                          aria-pressed={selectedOperation === candidate}
                          data-local-edit-operation-option={candidate}
                          disabled={busy || !candidateSupported}
                          onClick={() => setSelectedOperation(candidate)}
                          className="min-h-9 min-w-20 rounded-md px-2.5 text-xs font-semibold text-text-secondary transition-colors aria-pressed:bg-surface-raised aria-pressed:text-text-primary disabled:cursor-not-allowed disabled:opacity-40"
                        >
                          {labels.operations[candidate] ?? candidate}
                        </button>
                      );
                    })}
                  </div>
                </div>
                {operationCapabilityText ? (
                  <p role="status" data-local-edit-operation-unavailable className="text-xs leading-5 text-state-error">
                    {operationCapabilityText}
                  </p>
                ) : null}

                {selectedOperation === "replace_text" ? (
                  <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                    <label className="min-w-0 space-y-1.5 text-xs font-medium">
                      <span>{labels.sourceText}</span>
                      <input
                        value={sourceText}
                        onChange={(event) => setSourceText(event.target.value)}
                        className={cn(CONTROL_CLASS, "h-11 min-w-0 px-3 text-sm font-normal")}
                      />
                    </label>
                    <label className="min-w-0 space-y-1.5 text-xs font-medium">
                      <span>{labels.replacementText}</span>
                      <input
                        value={replacementText}
                        onChange={(event) => setReplacementText(event.target.value)}
                        className={cn(CONTROL_CLASS, "h-11 min-w-0 px-3 text-sm font-normal")}
                      />
                    </label>
                  </div>
                ) : (
                  <label className="block space-y-1.5 text-xs font-medium">
                    <span>{labels.instruction}</span>
                    <textarea
                      value={instruction}
                      onChange={(event) => setInstruction(event.target.value)}
                      rows={3}
                      className={cn(CONTROL_CLASS, "min-h-20 resize-y px-3 py-2 text-sm font-normal leading-5")}
                    />
                  </label>
                )}

                <div className="flex min-w-0 flex-wrap items-end gap-3 border-y border-border-l1 py-3">
                  <label className="flex min-w-36 flex-1 flex-col gap-1.5 text-xs font-medium">
                    <span className="flex items-center justify-between gap-2">
                      <span>{labels.brushSize}</span>
                      <output data-local-edit-brush-size>{brushSize}</output>
                    </span>
                    <input
                      type="range"
                      className="accent-accent"
                      min="4"
                      max="256"
                      step="4"
                      value={brushSize}
                      onChange={(event) => setBrushSize(Number(event.target.value))}
                    />
                  </label>
                  <label className="flex min-w-36 flex-1 flex-col gap-1.5 text-xs font-medium">
                    <span className="flex items-center justify-between gap-2">
                      <span>{labels.hardness}</span>
                      <output data-local-edit-hardness>{Math.round(hardness * 100)}%</output>
                    </span>
                    <input
                      type="range"
                      className="accent-accent"
                      min="0"
                      max="1"
                      step="0.05"
                      value={hardness}
                      onChange={(event) => setHardness(Number(event.target.value))}
                    />
                  </label>
                  <label className="inline-flex min-h-10 shrink-0 items-center gap-2 text-xs font-medium">
                    <input
                      type="checkbox"
                      checked={eraseSelection}
                      onChange={(event) => setEraseSelection(event.target.checked)}
                      className="h-4 w-4 accent-accent"
                    />
                    <Eraser size={15} aria-hidden="true" />
                    <span>{labels.eraseSelection}</span>
                  </label>
                  <button
                    type="button"
                    onClick={clearMask}
                    className="inline-flex h-10 shrink-0 items-center gap-1.5 rounded-lg border border-border-l1 px-3 text-xs font-semibold text-text-secondary hover:bg-surface-subtle hover:text-text-primary"
                  >
                    <RotateCcw size={14} aria-hidden="true" />
                    <span>{labels.clear}</span>
                  </button>
                </div>

                <div
                  className="relative mx-auto min-w-0 overflow-hidden rounded-control border border-border-l1 bg-surface-subtle"
                  style={{
                    aspectRatio: `${renderWidth} / ${renderHeight}`,
                    width: `min(100%, ${48 * renderWidth / renderHeight}vh)`,
                  }}
                  data-local-edit-stage
                >
                  <img
                    src={sourceAsset.url || undefined}
                    alt={sourceAsset.alt || labels.sourceAlt}
                    crossOrigin="anonymous"
                    draggable={false}
                    onLoad={(event) => markSourceReady(event.currentTarget)}
                    onError={sourceLoadError}
                    className="absolute inset-0 h-full w-full select-none object-contain"
                  />
                  <canvas
                    ref={maskPreviewCanvasRef}
                    width={renderWidth}
                    height={renderHeight}
                    aria-label={labels.maskCanvasLabel}
                    aria-disabled={busy || !sourceReady}
                    data-local-edit-mask-canvas
                    onPointerDown={handlePointerDown}
                    onPointerMove={handlePointerMove}
                    onPointerUp={stopPointer}
                    onPointerCancel={stopPointer}
                    className="absolute inset-0 h-full w-full touch-none select-none"
                  />
                  <canvas ref={maskCanvasRef} width={renderWidth} height={renderHeight} aria-hidden="true" className="hidden" />
                </div>

                {validationText ? (
                  <p role="status" data-local-edit-validation className="text-xs leading-5 text-text-muted">{validationText}</p>
                ) : null}
                {maskSummary ? (
                  <span
                    data-local-edit-mask-summary
                    data-edit-pixels={maskSummary.editPixelCount}
                    data-protected-pixels={maskSummary.protectedPixelCount}
                    data-feathered-pixels={maskSummary.featheredPixelCount}
                    className="sr-only"
                  />
                ) : null}

                <div className="flex flex-wrap items-center justify-end gap-2 pt-1">
                  <button
                    type="button"
                    onClick={onClose}
                    className="inline-flex h-10 items-center rounded-lg px-3 text-xs font-semibold text-text-secondary hover:bg-surface-subtle"
                  >
                    {labels.close}
                  </button>
                  <button
                    type="button"
                    disabled={!canSubmit}
                    onClick={() => void handleSubmit()}
                    className="inline-flex h-10 min-w-32 items-center justify-center gap-1.5 rounded-lg bg-accent px-4 text-xs font-semibold text-accent-fg hover:bg-accent-strong disabled:cursor-not-allowed disabled:opacity-45"
                  >
                    <PencilLine size={14} aria-hidden="true" />
                    <span>{labels.submit}</span>
                  </button>
                </div>
              </fieldset>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function PersistedTaskRequest({
  labels,
  sourceUrl,
  sourceAlt,
  operation,
  instruction,
  sourceText,
  replacementText,
}: {
  labels: LocalImageEditLabels;
  sourceUrl: string;
  sourceAlt: string;
  operation: LocalEditOperation;
  instruction: string;
  sourceText: string;
  replacementText: string;
}) {
  const details = operation === "replace_text"
    ? [
      { label: labels.sourceText, value: sourceText },
      { label: labels.replacementText, value: replacementText },
    ]
    : [{ label: labels.instruction, value: instruction }];
  return (
    <section data-local-edit-persisted-request className="grid min-w-0 gap-4 border-t border-border-l1 pt-4 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,0.7fr)]">
      <div className="min-w-0 overflow-hidden rounded-lg border border-border-l1 bg-surface-subtle">
        <img src={sourceUrl} alt={sourceAlt} className="max-h-[42dvh] w-full object-contain" />
      </div>
      <dl className="min-w-0 space-y-3 text-xs">
        <div className="min-w-0">
          <dt className="font-semibold text-text-muted">{labels.operation}</dt>
          <dd className="mt-1 break-words text-text-primary">{labels.operations[operation]}</dd>
        </div>
        {details.map((detail) => detail.value.trim() ? (
          <div key={detail.label} className="min-w-0">
            <dt className="font-semibold text-text-muted">{detail.label}</dt>
            <dd className="mt-1 whitespace-pre-wrap break-words leading-5 text-text-primary">{detail.value}</dd>
          </div>
        ) : null)}
      </dl>
    </section>
  );
}

function ResultState({
  labels,
  sourceUrl,
  resultUrl,
  resultView,
  onResultViewChange,
  onSourceLoad,
  onSourceError,
  busy,
  adopted,
  onKeepInLibrary,
  onAdopt,
  onRevert,
  onContinue,
}: {
  labels: LocalImageEditLabels;
  sourceUrl: string;
  resultUrl: string;
  resultView: "source" | "result";
  onResultViewChange: (view: "source" | "result") => void;
  onSourceLoad: (image: HTMLImageElement) => void;
  onSourceError: () => void;
  busy: boolean;
  adopted: boolean;
  onKeepInLibrary: () => void;
  onAdopt?: () => void;
  onRevert?: () => void;
  onContinue: () => void;
}) {
  return (
    <section data-local-edit-result data-local-edit-adopted={adopted ? "true" : "false"} className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2 text-xs font-semibold text-text-primary">
          <Check size={15} className="text-state-success" aria-hidden="true" />
          <span>{labels.resultReady}</span>
        </div>
        <div role="tablist" aria-label={labels.resultState} data-local-edit-result-switch className="inline-flex min-h-10 rounded-lg border border-border-l1 bg-surface-subtle p-0.5">
          <button
            type="button"
            role="tab"
            aria-selected={resultView === "source"}
            data-local-edit-result-tab="source"
            onClick={() => onResultViewChange("source")}
            className="rounded-md px-3 text-xs font-semibold text-text-secondary aria-selected:bg-surface-raised aria-selected:text-text-primary"
          >
            {labels.source}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={resultView === "result"}
            data-local-edit-result-tab="result"
            onClick={() => onResultViewChange("result")}
            className="rounded-md px-3 text-xs font-semibold text-text-secondary aria-selected:bg-surface-raised aria-selected:text-text-primary"
          >
            {labels.result}
          </button>
        </div>
      </div>

      <div data-local-edit-result-preview className="overflow-hidden rounded-lg border border-border-l1 bg-surface-subtle">
        <img
          src={resultView === "source" ? sourceUrl : resultUrl}
          alt={resultView === "source" ? labels.sourceAlt : labels.resultAlt}
          crossOrigin={resultView === "source" ? "anonymous" : undefined}
          onLoad={resultView === "source" ? (event) => onSourceLoad(event.currentTarget) : undefined}
          onError={resultView === "source" ? onSourceError : undefined}
          className="max-h-[42dvh] w-full object-contain"
        />
      </div>

      <div data-local-edit-comparison className="grid min-w-0 gap-3 md:grid-cols-2">
        <h3 className="col-span-full text-xs font-semibold text-text-secondary">{labels.compare}</h3>
        <figure className="min-w-0 overflow-hidden rounded-lg border border-border-l1 bg-surface-subtle p-2">
          <figcaption className="mb-2 text-xs font-semibold text-text-secondary">{labels.source}</figcaption>
          <img
            src={sourceUrl}
            alt={labels.sourceAlt}
            crossOrigin="anonymous"
            onLoad={(event) => onSourceLoad(event.currentTarget)}
            onError={onSourceError}
            className="max-h-72 w-full object-contain"
          />
        </figure>
        <figure className="min-w-0 overflow-hidden rounded-lg border border-border-l1 bg-surface-subtle p-2">
          <figcaption className="mb-2 text-xs font-semibold text-text-secondary">{labels.result}</figcaption>
          <img src={resultUrl} alt={labels.resultAlt} className="max-h-72 w-full object-contain" />
        </figure>
      </div>

      <div className="flex flex-wrap items-center justify-end gap-2 border-t border-border-l1 pt-3">
        <button
          type="button"
          disabled={busy}
          onClick={onKeepInLibrary}
          className="inline-flex h-10 items-center gap-1.5 rounded-lg border border-border-l1 px-3 text-xs font-semibold text-text-secondary hover:bg-surface-subtle hover:text-text-primary disabled:cursor-not-allowed disabled:opacity-45"
        >
          <Library size={14} aria-hidden="true" />
          <span>{labels.keepInLibrary}</span>
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={onContinue}
          className="inline-flex h-10 items-center gap-1.5 rounded-lg border border-border-l1 px-3 text-xs font-semibold text-text-secondary hover:bg-surface-subtle hover:text-text-primary disabled:cursor-not-allowed disabled:opacity-45"
        >
          <PencilLine size={14} aria-hidden="true" />
          <span>{labels.continueEdit}</span>
        </button>
        {onRevert ? (
          <button
            type="button"
            disabled={busy}
            onClick={onRevert}
            className="inline-flex h-10 items-center gap-1.5 rounded-lg border border-border-l1 px-3 text-xs font-semibold text-text-secondary hover:bg-surface-subtle hover:text-text-primary disabled:cursor-not-allowed disabled:opacity-45"
          >
            <RotateCcw size={14} aria-hidden="true" />
            <span>{labels.revertAdoption}</span>
          </button>
        ) : onAdopt ? (
          <button
            type="button"
            disabled={busy}
            onClick={onAdopt}
            className="inline-flex h-10 items-center gap-1.5 rounded-lg bg-accent px-3 text-xs font-semibold text-accent-fg hover:bg-accent-strong disabled:cursor-not-allowed disabled:opacity-45"
          >
            <Check size={14} aria-hidden="true" />
            <span>{labels.adopt}</span>
          </button>
        ) : null}
      </div>
    </section>
  );
}

function TaskStatusState({
  labels,
  status,
  progressPhase,
  providerName,
  failureReason,
  isRetryable,
  isCancelable,
  busy,
  onCancel,
  onRetry,
}: {
  labels: LocalImageEditLabels;
  status: LocalImageEditTaskStatus;
  progressPhase: string | null;
  providerName: string | null;
  failureReason: string | null;
  isRetryable: boolean;
  isCancelable: boolean;
  busy: boolean;
  onCancel?: () => void;
  onRetry?: () => void;
}) {
  const active = status === "queued" || status === "running";
  const failed = status === "failed";
  const retryable = (status === "draft" || (failed && isRetryable)) && onRetry;
  return (
    <section
      data-local-edit-task-status={status}
      className="mb-4 rounded-lg border border-border-l1 bg-surface-subtle px-3 py-2.5"
    >
      <div className="flex min-w-0 items-start gap-2">
        {active ? <Loader2 size={15} className="mt-0.5 shrink-0 animate-spin text-accent" aria-hidden="true" /> : null}
        <div className="min-w-0 flex-1 text-xs leading-5">
          <p className="font-semibold text-text-primary">{labels.statusLabels[status]}</p>
          {progressPhase ? <p className="text-text-secondary">{labels.phase}: {labels.progressPhases[progressPhase] ?? humanizeTechnicalKey(progressPhase)}</p> : null}
          {providerName ? <p className="text-text-secondary">{labels.provider}: {labels.providers[providerName] ?? humanizeTechnicalKey(providerName)}</p> : null}
          {failureReason ? <p role="alert" className="mt-1 break-words text-state-error">{failureReason}</p> : null}
        </div>
        <div className="flex shrink-0 flex-wrap justify-end gap-2">
          {active && isCancelable && onCancel ? (
            <button
              type="button"
              disabled={busy}
              onClick={onCancel}
              className="inline-flex min-h-8 items-center gap-1.5 rounded-md border border-border-l1 px-2.5 text-[11px] font-semibold text-text-secondary hover:bg-surface-raised disabled:cursor-not-allowed disabled:opacity-45"
            >
              <X size={13} aria-hidden="true" />
              <span>{labels.cancelTask}</span>
            </button>
          ) : null}
          {retryable ? (
            <button
              type="button"
              disabled={busy}
              onClick={onRetry}
              className="inline-flex min-h-8 items-center gap-1.5 rounded-md border border-border-l1 px-2.5 text-[11px] font-semibold text-text-secondary hover:bg-surface-raised disabled:cursor-not-allowed disabled:opacity-45"
            >
              <RotateCcw size={13} aria-hidden="true" />
              <span>{labels.retryTask}</span>
            </button>
          ) : null}
        </div>
      </div>
    </section>
  );
}
