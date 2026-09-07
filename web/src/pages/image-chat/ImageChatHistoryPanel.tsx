import { useEffect, useRef, type CSSProperties, type PointerEvent as ReactPointerEvent } from "react";
import { ChevronDown, Loader2 } from "lucide-react";

import type { PromptPreview } from "../../components/PromptPreviewDialog";
import { getVerticalWheelMappedScrollLeft } from "./resizableLayout";
import type { ImageHistoryBranch } from "./branching";
import type { ImageChatTranslate } from "./display";
import { HistoryBranchStrip } from "./HistoryBranchStrip";

function LoadMoreHistoryButton({
  compact,
  loading,
  onLoadMore,
  t,
}: {
  compact?: boolean;
  loading: boolean;
  onLoadMore: () => void;
  t: ImageChatTranslate;
}) {
  return (
    <button
      type="button"
      onClick={onLoadMore}
      disabled={loading}
      className={`inline-flex shrink-0 items-center justify-center gap-1.5 rounded-xl border border-border-l1 bg-surface-raised text-xs font-semibold text-text-secondary transition-colors hover:border-accent hover:text-accent disabled:cursor-wait disabled:opacity-60 dark:border-border-l1 dark:bg-surface-base/55 dark:text-text-secondary dark:hover:border-accent/60 dark:hover:text-accent ${
        compact ? "min-h-9 px-2.5" : "min-h-10 w-full px-3"
      }`}
    >
      {loading ? <Loader2 size={14} className="animate-spin" /> : <ChevronDown size={14} />}
      <span>{t("chat.loadMoreHistory")}</span>
    </button>
  );
}

function handleHistoryWheelScroll(event: WheelEvent, container: HTMLDivElement) {
  if (event.ctrlKey) {
    return;
  }
  const nextScrollLeft = getVerticalWheelMappedScrollLeft(container, event);
  if (nextScrollLeft === null) {
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
  variant?: "desktop" | "mobileDrawer";
  style?: CSSProperties;
  hasMoreHistory?: boolean;
  isLoadingMoreHistory?: boolean;
  onLoadMoreHistory?: () => void;
  onResizeStart?: (event: ReactPointerEvent<HTMLButtonElement>) => void;
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
  variant = "desktop",
  style,
  hasMoreHistory = false,
  isLoadingMoreHistory = false,
  onLoadMoreHistory,
  onResizeStart,
  onSelectRound,
  onSelectPlaceholder,
  onPreviewPrompt,
  t,
}: ImageChatHistoryPanelProps) {
  const desktopHistoryScrollRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const container = desktopHistoryScrollRef.current;
    if (!container) {
      return;
    }
    const handleWheel = (event: WheelEvent) => handleHistoryWheelScroll(event, container);
    container.addEventListener("wheel", handleWheel, { passive: false });
    return () => {
      container.removeEventListener("wheel", handleWheel);
    };
  }, [variant, historyBranches.length]);

  if (variant === "mobileDrawer") {
    return (
      <div className="flex min-h-0 flex-1 flex-col bg-surface-raised dark:bg-surface-panel">
        {historyBranches.length ? (
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-2 py-3">
            {hasMoreHistory && onLoadMoreHistory ? (
              <LoadMoreHistoryButton loading={isLoadingMoreHistory} onLoadMore={onLoadMoreHistory} t={t} />
            ) : null}
            {historyBranches.map((branch) => (
              <HistoryBranchStrip
                key={branch.id}
                branch={branch}
                selectedGeneratedAssetId={selectedGeneratedAssetId}
                selectedTaskPlaceholderId={selectedTaskPlaceholderId}
                branchBaseAssetId={branchBaseAssetId}
                variant="mobileDrawer"
                onSelectRound={onSelectRound}
                onSelectPlaceholder={onSelectPlaceholder}
                onPreviewPrompt={onPreviewPrompt}
                t={t}
              />
            ))}
          </div>
        ) : (
          <div className="flex min-h-0 flex-1 items-center justify-center px-2 py-6">
            <div className="flex min-h-24 w-full items-center justify-center rounded-2xl border border-dashed border-border-l1 bg-surface-base px-2 text-center text-xs text-text-muted dark:border-border-l1 dark:bg-surface-base/40 dark:text-text-muted">
              {t("chat.resultsAppearHere")}
            </div>
          </div>
        )}
      </div>
    );
  }

  return (
    <div
      className="relative hidden shrink-0 flex-col border-t border-border-l1 bg-surface-raised/95 px-2.5 py-2 shadow-[0_-8px_24px_rgba(15,23,42,0.04)] dark:border-border-l1/80 dark:bg-surface-panel dark:shadow-[0_-18px_40px_rgba(0,0,0,0.24)] lg:flex lg:h-[var(--image-chat-history-panel-height)] lg:px-3 lg:py-2.5"
      style={style}
    >
      {onResizeStart ? (
        <button
          type="button"
          aria-label={t("chat.resizeHistory")}
          title={t("chat.resizeHistoryTitle")}
          onPointerDown={onResizeStart}
          className="absolute inset-x-0 -top-1 z-20 hidden h-3 cursor-row-resize items-center justify-center transition-colors hover:bg-accent-soft/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:hover:bg-accent/15 lg:flex"
        >
          <span className="h-1 w-12 rounded-full bg-surface-subtle dark:bg-surface-subtle" />
        </button>
      ) : null}
      <div className="mb-1 flex items-center justify-between gap-3 lg:mb-2">
        <div>
          <div className="text-sm font-semibold text-text-primary dark:text-white">{t("chat.history")}</div>
        </div>
        <div className="flex items-center gap-2">
          {hasMoreHistory && onLoadMoreHistory ? (
            <LoadMoreHistoryButton compact loading={isLoadingMoreHistory} onLoadMore={onLoadMoreHistory} t={t} />
          ) : null}
          {branchBaseSelected ? (
            <div className="rounded-full border border-accent bg-accent-soft px-2 py-1 text-xs font-semibold text-accent dark:border-accent/40 dark:bg-accent/15 dark:text-accent">
              {t("chat.clickHistoryBase")}
            </div>
          ) : null}
        </div>
      </div>

      {historyBranches.length ? (
        <div
          ref={desktopHistoryScrollRef}
          className="image-chat-history-scroll flex min-h-0 flex-1 gap-3 overflow-x-auto overscroll-x-contain pb-1"
        >
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
        <div className="flex min-h-0 flex-1 items-center justify-center rounded-2xl border border-dashed border-border-l1 bg-surface-base text-sm text-text-muted dark:border-border-l1 dark:bg-surface-base/40 dark:text-text-muted">
          {t("chat.resultsAppearHere")}
        </div>
      )}
    </div>
  );
}
