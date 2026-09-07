import { ImagePlus, Loader2, Send, Square, Trash2, Upload, X } from "lucide-react";
import type { ClipboardEvent, DragEvent, KeyboardEvent } from "react";
import { useEffect, useId, useRef, useState } from "react";

import { IconButton } from "../../../components/ui/icon-button";
import { Tooltip } from "../../../components/ui/tooltip";
import { api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { AgentAttachment, AgentQuestion, AgentQuestionAnswer } from "../../../lib/types";
import { AgentQuestionPrompt } from "./AgentQuestionPrompt";

export const AGENT_COMPOSER_MAX_ASSETS = 6;
const ACCEPT_IMAGE_TYPES = "image/jpeg,image/png,image/webp";
const ACCEPT_IMAGE_TYPE_SET = new Set(["image/jpeg", "image/png", "image/webp"]);
const ACCEPT_IMAGE_EXTENSION = /\.(png|jpe?g|webp)$/i;

export function classifyImageFiles(list: FileList | File[] | null | undefined): {
  accepted: File[];
  rejected: boolean;
} {
  const files = list ? Array.from(list) : [];
  const accepted = files.filter((file) => {
    if (ACCEPT_IMAGE_TYPE_SET.has(file.type)) return true;
    if (file.type) return false;
    return ACCEPT_IMAGE_EXTENSION.test(file.name);
  });
  return { accepted, rejected: files.length > 0 && accepted.length !== files.length };
}

interface AgentComposerProps {
  value: string;
  selectedAssets: readonly AgentAttachment[];
  isSubmitting: boolean;
  canSubmit: boolean;
  stopAvailable: boolean;
  isStopping: boolean;
  error: string | null;
  placeholder?: string;
  showAssetPicker?: boolean;
  assetPickerLabel?: string;
  selectedAssetsCountLabel?: string;
  onChange: (value: string) => void;
  onOpenAssets: () => void;
  onRemoveAsset: (assetId: string) => void;
  onPreviewAsset: (asset: AgentAttachment) => void;
  onSubmit: () => void;
  onStop: () => void;
  onUploadFiles?: (files: File[]) => void;
  isUploading?: boolean;
  question?: AgentQuestion | null;
  questionBusy?: boolean;
  questionError?: string | null;
  onAnswerQuestion?: (answer: AgentQuestionAnswer) => void;
}

export function AgentComposer({
  value,
  selectedAssets,
  isSubmitting,
  canSubmit,
  stopAvailable,
  isStopping,
  error,
  placeholder,
  showAssetPicker = true,
  assetPickerLabel,
  selectedAssetsCountLabel,
  onChange,
  onOpenAssets,
  onRemoveAsset,
  onPreviewAsset,
  onSubmit,
  onStop,
  onUploadFiles,
  isUploading = false,
  question = null,
  questionBusy = false,
  questionError = null,
  onAnswerQuestion,
}: AgentComposerProps) {
  const { t } = useI18n();
  const keyboardHintId = useId();
  const uploadInputId = useId();
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const [dragging, setDragging] = useState(false);
  const [uploadNotice, setUploadNotice] = useState<string | null>(null);
  const submitReady = canSubmit && !isSubmitting && !isUploading && Boolean(value.trim());

  useEffect(() => {
    const textarea = textareaRef.current;
    if (!textarea) {
      return;
    }
    textarea.style.height = "auto";
    textarea.style.height = `${Math.min(Math.max(textarea.scrollHeight, 42), 144)}px`;
    textarea.style.overflowY = textarea.scrollHeight > 144 ? "auto" : "hidden";
  }, [value]);

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing && submitReady) {
      event.preventDefault();
      onSubmit();
    }
  };

  const handleClearAllAssets = () => {
    for (const asset of selectedAssets) {
      onRemoveAsset(asset.id);
    }
  };

  const takeUploadedFiles = (list: FileList | File[] | null | undefined) => {
    if (!onUploadFiles || isSubmitting || isUploading) {
      return;
    }
    const { accepted, rejected } = classifyImageFiles(list);
    if (rejected) {
      setUploadNotice(t("agentWorkbench.uploadUnsupported"));
    }
    if (!accepted.length) {
      return;
    }
    const remaining = AGENT_COMPOSER_MAX_ASSETS - selectedAssets.length;
    if (remaining <= 0) {
      setUploadNotice(t("agentWorkbench.uploadLimit", { maximum: AGENT_COMPOSER_MAX_ASSETS }));
      return;
    }
    const limited = accepted.length > remaining;
    if (limited) {
      setUploadNotice(t("agentWorkbench.uploadLimit", { maximum: AGENT_COMPOSER_MAX_ASSETS }));
    } else if (!rejected) {
      setUploadNotice(null);
    }
    onUploadFiles(accepted.slice(0, remaining));
  };

  const handlePaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    if (!event.clipboardData.files.length) {
      return;
    }
    event.preventDefault();
    takeUploadedFiles(event.clipboardData.files);
  };

  const handleDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    setDragging(false);
    takeUploadedFiles(event.dataTransfer.files);
  };

  return (
    <div data-agent-composer className="shrink-0 border-t border-border-l1 bg-surface-panel px-3 py-3 sm:px-4 sm:py-4">
      <div className="mx-auto w-full max-w-[48rem]">
        {error || uploadNotice ? (
          <div role="alert" className="mb-2 rounded-lg border border-state-error/20 bg-state-error/10 px-3 py-2 text-xs leading-5 text-state-error">
            {error || uploadNotice}
          </div>
        ) : null}

        <div
          className={`overflow-hidden rounded-surface border bg-surface-raised shadow-elev-1 transition-[border-color,box-shadow] duration-fast focus-within:border-accent focus-within:ring-2 focus-within:ring-focus-ring motion-reduce:transition-none ${dragging ? "border-accent/80" : "border-border-l3"
            }`}
          onDragOver={(event) => {
            if (!onUploadFiles) {
              return;
            }
            event.preventDefault();
            setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={handleDrop}
        >
          {selectedAssets.length ? (
            <div className="border-b border-border-l1 px-3 pb-3 pt-3">
              <div className="mb-2 flex items-center justify-between gap-3 text-[11px] font-medium text-text-secondary">
                <span>{selectedAssetsCountLabel ?? t("agentWorkbench.composer.selectedAssetsCount", { count: selectedAssets.length })}</span>
                <IconButton
                  label={t("agentWorkbench.composer.clearAssets")}
                  size="toolbar"
                  disabled={isSubmitting}
                  onClick={handleClearAllAssets}
                >
                  <Trash2 size={13} />
                </IconButton>
              </div>
              <div className="flex gap-2 overflow-x-auto scrollbar-none [&::-webkit-scrollbar]:hidden">
                {selectedAssets.map((asset) => (
                  <div
                    key={asset.id}
                    className="group relative h-14 w-14 shrink-0 overflow-hidden rounded-xl border border-border-l2 bg-surface-subtle"
                  >
                    <button
                      type="button"
                      onClick={() => onPreviewAsset(asset)}
                      aria-label={t("agentWorkbench.previewAsset", { name: asset.display_name })}
                      className="h-full w-full focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                    >
                      <img
                        src={api.toApiUrl(asset.thumbnail_url)}
                        alt=""
                        className="h-full w-full object-cover"
                      />
                    </button>
                    <IconButton
                      label={t("agentWorkbench.removeAsset", { name: asset.display_name })}
                      size="sm"
                      className="absolute right-0.5 top-0.5 bg-surface-inverse/80 text-surface-raised opacity-100 hover:bg-state-error sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100"
                      onClick={() => onRemoveAsset(asset.id)}
                    >
                      <X size={11} />
                    </IconButton>
                  </div>
                ))}
              </div>
            </div>
          ) : null}

          {question && onAnswerQuestion ? (
            <AgentQuestionPrompt
              question={question}
              busy={questionBusy}
              error={questionError}
              onAnswer={onAnswerQuestion}
            />
          ) : (
            <textarea
              ref={textareaRef}
              value={value}
              maxLength={20_000}
              rows={1}
              onChange={(event) => onChange(event.target.value)}
              onKeyDown={handleKeyDown}
              onPaste={handlePaste}
              placeholder={placeholder ?? t("agentWorkbench.composerPlaceholder")}
              aria-label={t("agentWorkbench.composerLabel")}
              aria-describedby={keyboardHintId}
              className="block max-h-36 min-h-[42px] w-full resize-none border-0 bg-transparent px-4 pb-1 pt-3 text-[15px] leading-6 text-text-primary outline-none placeholder:text-text-muted"
            />
          )}

          <div className="flex min-h-12 items-center justify-between gap-3 px-2 pb-2 pt-1">
            <div className="flex min-w-0 items-center gap-1.5">
              {!question && onUploadFiles ? (
                <>
                  <input
                    id={uploadInputId}
                    type="file"
                    accept={ACCEPT_IMAGE_TYPES}
                    multiple
                    className="sr-only"
                    disabled={isSubmitting || isUploading || selectedAssets.length >= AGENT_COMPOSER_MAX_ASSETS}
                    onChange={(event) => {
                      takeUploadedFiles(event.target.files);
                      event.target.value = "";
                    }}
                  />
                  <Tooltip content={t("agentWorkbench.uploadAssets")}>
                    <label
                      htmlFor={uploadInputId}
                      aria-disabled={isSubmitting || isUploading || selectedAssets.length >= AGENT_COMPOSER_MAX_ASSETS}
                      className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-full transition-colors lg:h-9 lg:w-9 ${isSubmitting || isUploading || selectedAssets.length >= AGENT_COMPOSER_MAX_ASSETS
                          ? "pointer-events-none cursor-not-allowed opacity-40"
                          : "cursor-pointer text-text-muted hover:bg-surface-subtle hover:text-text-primary"
                        }`}
                    >
                      {isUploading ? <Loader2 size={16} className="animate-spin motion-reduce:animate-none" /> : <Upload size={16} />}
                      <span className="sr-only">{t("agentWorkbench.uploadAssets")}</span>
                    </label>
                  </Tooltip>
                </>
              ) : null}
              {!question && showAssetPicker ? (
                <IconButton
                  label={assetPickerLabel ?? t("agentWorkbench.selectAssets")}
                  size="toolbar"
                  className={selectedAssets.length > 0 ? "bg-accent/10 text-accent hover:bg-accent/20" : ""}
                  onClick={onOpenAssets}
                  disabled={isSubmitting}
                >
                  <ImagePlus size={17} />
                </IconButton>
              ) : null}
              {!question && value.length > 30 ? (
                <span className="truncate text-[10px] tabular-nums text-text-muted">{value.length} / 20000</span>
              ) : null}
            </div>
            <IconButton
              label={t(stopAvailable ? "agentWorkbench.cancelTurn" : "agentWorkbench.send")}
              size="toolbar"
              variant={stopAvailable ? "danger" : "primary"}
              className="rounded-full"
              onClick={stopAvailable ? onStop : onSubmit}
              disabled={question ? !stopAvailable || isStopping : stopAvailable ? isStopping : !submitReady}
              busy={stopAvailable && isStopping}
            >
              {stopAvailable ? <Square size={14} fill="currentColor" /> : <Send size={15} />}
            </IconButton>
          </div>
        </div>

        <span id={keyboardHintId} className="sr-only">{t("agentWorkbench.composer.keyboardHint")}</span>
      </div>
    </div>
  );
}
