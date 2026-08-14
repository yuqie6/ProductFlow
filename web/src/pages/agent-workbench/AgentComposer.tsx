import { ImagePlus, Send, X } from "lucide-react";
import type { KeyboardEvent } from "react";

import { api } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import type { GalleryAsset } from "../../lib/types";

interface AgentComposerProps {
  value: string;
  selectedAssets: readonly GalleryAsset[];
  isSubmitting: boolean;
  canSubmit: boolean;
  error: string | null;
  onChange: (value: string) => void;
  onOpenAssets: () => void;
  onRemoveAsset: (assetId: string) => void;
  onPreviewAsset: (asset: GalleryAsset) => void;
  onSubmit: () => void;
}

export function AgentComposer({
  value,
  selectedAssets,
  isSubmitting,
  canSubmit,
  error,
  onChange,
  onOpenAssets,
  onRemoveAsset,
  onPreviewAsset,
  onSubmit,
}: AgentComposerProps) {
  const { t } = useI18n();
  const submitReady = canSubmit && !isSubmitting && Boolean(value.trim());
  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing && submitReady) {
      event.preventDefault();
      onSubmit();
    }
  };

  return (
    <div className="border-t border-zinc-200 bg-white px-3 py-3 dark:border-slate-800 dark:bg-[#0a0f16]">
      {selectedAssets.length ? (
        <div className="mb-2 flex gap-2 overflow-x-auto pb-1">
          {selectedAssets.map((asset) => (
            <div
              key={asset.id}
              className="group relative h-14 w-14 shrink-0 overflow-hidden rounded-md border border-zinc-200 bg-zinc-100 dark:border-slate-700 dark:bg-slate-900"
            >
              <button
                type="button"
                onClick={() => onPreviewAsset(asset)}
                aria-label={t("agentWorkbench.previewAsset", { name: asset.display_name })}
                className="h-full w-full"
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
                className="absolute right-0.5 top-0.5 flex h-6 w-6 items-center justify-center rounded-md bg-zinc-950/80 text-white opacity-100 backdrop-blur hover:bg-red-600 sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100"
              >
                <X size={13} />
              </button>
            </div>
          ))}
        </div>
      ) : null}

      {error ? (
        <div role="alert" className="mb-2 border-l-2 border-red-500 bg-red-50 px-3 py-2 text-xs leading-5 text-red-700 dark:bg-red-500/10 dark:text-red-200">
          {error}
        </div>
      ) : null}

      <div className="grid grid-cols-[44px_minmax(0,1fr)_44px] items-end gap-2 rounded-md border border-zinc-300 bg-white p-2 focus-within:border-blue-600 focus-within:ring-2 focus-within:ring-blue-100 dark:border-slate-700 dark:bg-[#0d131c] dark:focus-within:border-cyan-400 dark:focus-within:ring-cyan-400/15">
        <button
          type="button"
          onClick={onOpenAssets}
          disabled={isSubmitting}
          aria-label={t("agentWorkbench.selectAssets")}
          title={t("agentWorkbench.selectAssets")}
          className="flex h-11 w-11 items-center justify-center rounded-md text-zinc-500 hover:bg-zinc-100 hover:text-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 disabled:opacity-40 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-cyan-300"
        >
          <ImagePlus size={19} />
        </button>
        <textarea
          value={value}
          maxLength={20_000}
          rows={1}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={t("agentWorkbench.composerPlaceholder")}
          aria-label={t("agentWorkbench.composerLabel")}
          className="max-h-32 min-h-11 resize-none border-0 bg-transparent px-1 py-2.5 text-sm leading-6 text-zinc-950 outline-none placeholder:text-zinc-400 dark:text-white dark:placeholder:text-slate-500"
        />
        <button
          type="button"
          onClick={onSubmit}
          disabled={!submitReady}
          aria-label={t("agentWorkbench.send")}
          title={t("agentWorkbench.send")}
          className="flex h-11 w-11 items-center justify-center rounded-md bg-zinc-950 text-white hover:bg-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 disabled:cursor-not-allowed disabled:opacity-35 dark:bg-cyan-400 dark:text-[#071018] dark:hover:bg-cyan-300"
        >
          <Send size={18} />
        </button>
      </div>
    </div>
  );
}
