import { ImagePlus, Loader2, Send, Sparkles, Square, Trash2, X } from "lucide-react";
import type { KeyboardEvent } from "react";
import { useEffect, useId, useRef } from "react";

import { api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { AgentAttachment } from "../../../lib/types";

export const AGENT_COMPOSER_MAX_ASSETS = 6;

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
}

const QUICK_PROMPTS = [
  "agentWorkbench.composer.quickPrompt.composition",
  "agentWorkbench.composer.quickPrompt.lighting",
  "agentWorkbench.composer.quickPrompt.workflow",
] as const;

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
}: AgentComposerProps) {
  const { t } = useI18n();
  const keyboardHintId = useId();
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const submitReady = canSubmit && !isSubmitting && Boolean(value.trim());

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

  return (
    <div data-agent-composer className="shrink-0 border-t border-border-l1 bg-surface-base/95 px-3 py-3 backdrop-blur sm:px-4 sm:py-4">
      <div className="mx-auto w-full max-w-[48rem]">
        {!value.trim() && !selectedAssets.length && !stopAvailable ? (
          <div className="mb-2.5 flex items-center gap-2 overflow-x-auto pb-0.5 text-xs text-text-secondary scrollbar-none [&::-webkit-scrollbar]:hidden">
            <Sparkles size={13} className="shrink-0 text-accent" aria-hidden="true" />
            <div className="flex items-center gap-1.5">
              {QUICK_PROMPTS.map((promptKey) => {
                const label = t(promptKey);
                return (
                  <button
                    key={promptKey}
                    type="button"
                    onClick={() => onChange(label)}
                    disabled={isSubmitting}
                    className="shrink-0 rounded-full border border-border-l2 bg-surface-raised px-3 py-1.5 text-xs text-text-secondary shadow-sm transition-colors hover:border-accent/40 hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-40"
                  >
                    {label}
                  </button>
                );
              })}
            </div>
          </div>
        ) : null}

        {error ? (
          <div role="alert" className="mb-2 rounded-lg border border-state-error/20 bg-state-error/10 px-3 py-2 text-xs leading-5 text-state-error">
            {error}
          </div>
        ) : null}

        <div className="overflow-hidden rounded-[22px] border border-border-l3 bg-surface-raised shadow-[0_8px_24px_rgb(15_23_42_/_0.07)] transition-shadow focus-within:border-accent/70 focus-within:shadow-[0_8px_28px_rgb(99_102_241_/_0.14)] dark:shadow-[0_12px_30px_rgb(0_0_0_/_0.22)] dark:focus-within:shadow-[0_12px_34px_rgb(99_102_241_/_0.16)]">
          {selectedAssets.length ? (
            <div className="border-b border-border-l1 px-3 pb-3 pt-3">
              <div className="mb-2 flex items-center justify-between gap-3 text-[11px] font-medium text-text-secondary">
                <span>{selectedAssetsCountLabel ?? t("agentWorkbench.composer.selectedAssetsCount", { count: selectedAssets.length })}</span>
                <button
                  type="button"
                  onClick={handleClearAllAssets}
                  disabled={isSubmitting}
                  aria-label={t("agentWorkbench.composer.clearAssets")}
                  title={t("agentWorkbench.composer.clearAssets")}
                  className="inline-flex h-6 w-6 items-center justify-center rounded-md text-text-muted transition-colors hover:bg-state-error/10 hover:text-state-error focus:outline-none focus-visible:ring-2 focus-visible:ring-state-error disabled:opacity-40"
                >
                  <Trash2 size={13} />
                </button>
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
                    <button
                      type="button"
                      onClick={() => onRemoveAsset(asset.id)}
                      aria-label={t("agentWorkbench.removeAsset", { name: asset.display_name })}
                      title={t("agentWorkbench.removeAsset", { name: asset.display_name })}
                      className="absolute right-0.5 top-0.5 flex h-5 w-5 items-center justify-center rounded-full bg-slate-950/80 text-white opacity-100 backdrop-blur transition-opacity hover:bg-state-error sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100"
                    >
                      <X size={11} />
                    </button>
                  </div>
                ))}
              </div>
            </div>
          ) : null}

          <textarea
            ref={textareaRef}
            value={value}
            maxLength={20_000}
            rows={1}
            onChange={(event) => onChange(event.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={placeholder ?? t("agentWorkbench.composerPlaceholder")}
            aria-label={t("agentWorkbench.composerLabel")}
            aria-describedby={keyboardHintId}
            className="block max-h-36 min-h-[42px] w-full resize-none border-0 bg-transparent px-4 pb-1 pt-3 text-[15px] leading-6 text-text-primary outline-none placeholder:text-text-muted"
          />

          <div className="flex min-h-12 items-center justify-between gap-3 px-2 pb-2 pt-1">
            <div className="flex min-w-0 items-center gap-1.5">
              {showAssetPicker ? (
                <button
                  type="button"
                  onClick={onOpenAssets}
                  disabled={isSubmitting}
                  aria-label={assetPickerLabel ?? t("agentWorkbench.selectAssets")}
                  title={assetPickerLabel ?? t("agentWorkbench.selectAssets")}
                  className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-full transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-40 ${
                    selectedAssets.length > 0
                      ? "bg-accent/10 text-accent hover:bg-accent/20"
                      : "text-text-muted hover:bg-surface-subtle hover:text-text-primary"
                  }`}
                >
                  <ImagePlus size={17} />
                </button>
              ) : null}
              {value.length > 30 ? (
                <span className="truncate text-[10px] tabular-nums text-text-muted">{value.length} / 20000</span>
              ) : null}
            </div>
            <button
              type="button"
              onClick={stopAvailable ? onStop : onSubmit}
              disabled={stopAvailable ? isStopping : !submitReady}
              aria-label={t(stopAvailable ? "agentWorkbench.cancelTurn" : "agentWorkbench.send")}
              title={t(stopAvailable ? "agentWorkbench.cancelTurn" : "agentWorkbench.send")}
              className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-white shadow-sm transition-colors focus:outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40 ${
                stopAvailable
                  ? "bg-state-error hover:bg-state-error/90 focus-visible:ring-state-error"
                  : "bg-accent hover:bg-accent-strong focus-visible:ring-accent"
              }`}
            >
              {stopAvailable ? (
                isStopping ? <Loader2 size={15} className="animate-spin motion-reduce:animate-none" /> : <Square size={14} fill="currentColor" />
              ) : (
                <Send size={15} />
              )}
            </button>
          </div>
        </div>

        <span id={keyboardHintId} className="sr-only">{t("agentWorkbench.composer.keyboardHint")}</span>
      </div>
    </div>
  );
}
