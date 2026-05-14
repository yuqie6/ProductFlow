import type { CSSProperties, PointerEvent as ReactPointerEvent, WheelEvent as ReactWheelEvent } from "react";

import type { PromptPreview } from "../../components/PromptPreviewDialog";
import { clampPanelSize, wheelDeltaToPixels } from "./resizableLayout";
import type { ImageHistoryBranch } from "./branching";
import type { ImageChatTranslate } from "./display";
import { HistoryBranchStrip } from "./HistoryBranchStrip";

function handleHistoryWheelScroll(event: ReactWheelEvent<HTMLDivElement>) {
  if (event.ctrlKey) {
    return;
  }
  const container = event.currentTarget;
  const maxScrollLeft = container.scrollWidth - container.clientWidth;
  if (maxScrollLeft <= 1) {
    return;
  }
  const absDeltaX = Math.abs(event.deltaX);
  const absDeltaY = Math.abs(event.deltaY);
  if (absDeltaY === 0 || absDeltaX >= absDeltaY) {
    return;
  }
  const delta = wheelDeltaToPixels(event.deltaY, event.deltaMode, container.clientWidth);
  const nextScrollLeft = clampPanelSize(container.scrollLeft + delta, 0, maxScrollLeft);
  if (nextScrollLeft === container.scrollLeft) {
    return;
  }
  event.preventDefault();
  container.scrollLeft = nextScrollLeft;
}

interface ImageChatHistoryPanelProps {
  historyBranches: ImageHistoryBranch[];
  selectedGeneratedAssetId: string | null;
  selectedTaskPlaceholderId: string | null;
  branchBaseAssetId: string | null;
  branchBaseSelected: boolean;
  style: CSSProperties;
  onResizeStart: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onSelectRound: (assetId: string) => void;
  onSelectPlaceholder: (placeholderId: string) => void;
  onPreviewPrompt: (preview: PromptPreview) => void;
  t: ImageChatTranslate;
}

export function ImageChatHistoryPanel({
  historyBranches,
  selectedGeneratedAssetId,
  selectedTaskPlaceholderId,
  branchBaseAssetId,
  branchBaseSelected,
  style,
  onResizeStart,
  onSelectRound,
  onSelectPlaceholder,
  onPreviewPrompt,
  t,
}: ImageChatHistoryPanelProps) {
  return (
    <div
      className="relative flex h-[9.5rem] shrink-0 flex-col border-t border-slate-200 bg-white/95 px-2.5 py-2 shadow-[0_-8px_24px_rgba(15,23,42,0.04)] dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[0_-18px_40px_rgba(0,0,0,0.24)] lg:h-[var(--image-chat-history-panel-height)] lg:px-3 lg:py-2.5"
      style={style}
    >
      <button
        type="button"
        aria-label={t("chat.resizeHistory")}
        title={t("chat.resizeHistoryTitle")}
        onPointerDown={onResizeStart}
        className="absolute inset-x-0 -top-1 z-20 hidden h-3 cursor-row-resize items-center justify-center transition-colors hover:bg-indigo-50/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:hover:bg-violet-500/15 lg:flex"
      >
        <span className="h-1 w-12 rounded-full bg-slate-300 dark:bg-slate-600" />
      </button>
      <div className="mb-1 flex items-center justify-between gap-3 lg:mb-2">
        <div>
          <div className="text-sm font-semibold text-slate-950 dark:text-white">{t("chat.history")}</div>
        </div>
        {branchBaseSelected ? (
          <div className="rounded-full border border-indigo-200 bg-indigo-50 px-2 py-1 text-xs font-semibold text-indigo-700 dark:border-violet-400/40 dark:bg-violet-500/15 dark:text-violet-100">
            {t("chat.clickHistoryBase")}
          </div>
        ) : null}
      </div>

      {historyBranches.length ? (
        <div className="image-chat-history-scroll flex min-h-0 flex-1 snap-x snap-mandatory gap-3 overflow-x-auto pb-1" onWheel={handleHistoryWheelScroll}>
          {historyBranches.map((branch) => (
            <HistoryBranchStrip
              key={branch.id}
              branch={branch}
              selectedGeneratedAssetId={selectedGeneratedAssetId}
              selectedTaskPlaceholderId={selectedTaskPlaceholderId}
              branchBaseAssetId={branchBaseAssetId}
              onSelectRound={onSelectRound}
              onSelectPlaceholder={onSelectPlaceholder}
              onPreviewPrompt={onPreviewPrompt}
              t={t}
            />
          ))}
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 items-center justify-center rounded-2xl border border-dashed border-slate-200 bg-slate-50 text-sm text-slate-400 dark:border-slate-700 dark:bg-slate-950/40 dark:text-slate-500">
          {t("chat.resultsAppearHere")}
        </div>
      )}
    </div>
  );
}
