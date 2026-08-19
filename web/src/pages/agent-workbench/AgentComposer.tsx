import { ImagePlus, Loader2, Send, Sparkles, Square, Trash2, X } from "lucide-react";
import type { KeyboardEvent } from "react";

import { api } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import type { GalleryAsset } from "../../lib/types";

interface AgentComposerProps {
  value: string;
  selectedAssets: readonly GalleryAsset[];
  isSubmitting: boolean;
  canSubmit: boolean;
  stopAvailable: boolean;
  isStopping: boolean;
  error: string | null;
  onChange: (value: string) => void;
  onOpenAssets: () => void;
  onRemoveAsset: (assetId: string) => void;
  onPreviewAsset: (asset: GalleryAsset) => void;
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
  onChange,
  onOpenAssets,
  onRemoveAsset,
  onPreviewAsset,
  onSubmit,
  onStop,
}: AgentComposerProps) {
  const { t } = useI18n();
  const submitReady = canSubmit && !isSubmitting && Boolean(value.trim());

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing && submitReady) {
      event.preventDefault();
      onSubmit();
    }
  };

  const handleQuickPromptClick = (promptText: string) => {
    onChange(promptText);
  };

  const handleClearAllAssets = () => {
    for (const asset of selectedAssets) {
      onRemoveAsset(asset.id);
    }
  };

  return (
    <div className="border-t border-border-l1 bg-surface-raised px-3 py-3">
      {/* 快捷建议胶囊：仅在尚未输入且未选图时展示 */}
      {!value.trim() && !selectedAssets.length && !stopAvailable ? (
        <div className="mb-2.5 flex items-center gap-1.5 overflow-x-auto pb-0.5 text-xs text-text-secondary">
          <Sparkles size={13} className="shrink-0 text-accent" aria-hidden="true" />
          <div className="flex items-center gap-1.5">
            {QUICK_PROMPTS.map((promptKey) => {
              const label = t(promptKey);
              return (
                <button
                  key={promptKey}
                  type="button"
                  onClick={() => handleQuickPromptClick(label)}
                  disabled={isSubmitting}
                  className="shrink-0 rounded-full border border-border-l2 bg-surface px-2.5 py-1 text-xs text-text-secondary transition-colors hover:border-accent/40 hover:bg-surface-raised hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-40"
                >
                  {label}
                </button>
              );
            })}
          </div>
        </div>
      ) : null}

      {/* 已选参考图托盘 */}
      {selectedAssets.length ? (
        <div className="mb-2.5 rounded-lg border border-border-l2 bg-surface/50 p-2">
          <div className="mb-1.5 flex items-center justify-between text-[11px] font-medium text-text-secondary">
            <span>{t("agentWorkbench.composer.selectedAssetsCount", { count: selectedAssets.length })}</span>
            <button
              type="button"
              onClick={handleClearAllAssets}
              disabled={isSubmitting}
              className="inline-flex items-center gap-1 text-text-tertiary transition-colors hover:text-state-error focus:outline-none disabled:opacity-40"
            >
              <Trash2 size={11} />
              <span>{t("agentWorkbench.composer.clearAssets")}</span>
            </button>
          </div>
          <div className="flex gap-2 overflow-x-auto pb-0.5">
            {selectedAssets.map((asset) => (
              <div
                key={asset.id}
                className="group relative h-14 w-14 shrink-0 overflow-hidden rounded-md border border-border-l2 bg-surface"
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
                  className="absolute right-0.5 top-0.5 flex h-5 w-5 items-center justify-center rounded bg-zinc-950/80 text-white opacity-100 backdrop-blur transition-opacity hover:bg-state-error sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100"
                >
                  <X size={12} />
                </button>
              </div>
            ))}
          </div>
        </div>
      ) : null}

      {error ? (
        <div role="alert" className="mb-2 border-l-2 border-state-error bg-state-error/10 px-3 py-2 text-xs leading-5 text-state-error">
          {error}
        </div>
      ) : null}

      {/* 主输入区域 */}
      <div className="grid grid-cols-[40px_minmax(0,1fr)_40px] items-end gap-2 rounded-xl border border-border-l3 bg-surface p-1.5 transition-all focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/20">
        <button
          type="button"
          onClick={onOpenAssets}
          disabled={isSubmitting}
          aria-label={t("agentWorkbench.selectAssets")}
          title={t("agentWorkbench.selectAssets")}
          className={`flex h-10 w-10 items-center justify-center rounded-lg transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-40 ${
            selectedAssets.length > 0
              ? "bg-accent/10 text-accent hover:bg-accent/20"
              : "text-text-tertiary hover:bg-surface-raised hover:text-text-primary"
          }`}
        >
          <ImagePlus size={18} />
        </button>
        <textarea
          value={value}
          maxLength={20_000}
          rows={1}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={t("agentWorkbench.composerPlaceholder")}
          aria-label={t("agentWorkbench.composerLabel")}
          className="max-h-36 min-h-10 resize-none border-0 bg-transparent px-1.5 py-2 text-sm leading-6 text-text-primary outline-none placeholder:text-text-tertiary"
        />
        <button
          type="button"
          onClick={stopAvailable ? onStop : onSubmit}
          disabled={stopAvailable ? isStopping : !submitReady}
          aria-label={t(stopAvailable ? "agentWorkbench.cancelTurn" : "agentWorkbench.send")}
          title={t(stopAvailable ? "agentWorkbench.cancelTurn" : "agentWorkbench.send")}
          className={`flex h-10 w-10 items-center justify-center rounded-lg text-white shadow-sm transition-all focus:outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40 ${
            stopAvailable
              ? "bg-state-error hover:bg-state-error/90 focus-visible:ring-state-error"
              : "bg-accent hover:bg-accent-strong focus-visible:ring-accent"
          }`}
        >
          {stopAvailable ? (
            isStopping ? <Loader2 size={16} className="animate-spin motion-reduce:animate-none" /> : <Square size={15} fill="currentColor" />
          ) : (
            <Send size={16} />
          )}
        </button>
      </div>

      {/* 底部键盘提示与状态 */}
      <div className="mt-1.5 flex items-center justify-between px-1 text-[11px] text-text-tertiary">
        <span>{t("agentWorkbench.composer.keyboardHint")}</span>
        {value.length > 30 ? (
          <span className="tabular-nums">
            {value.length} / 20000
          </span>
        ) : null}
      </div>
    </div>
  );
}
