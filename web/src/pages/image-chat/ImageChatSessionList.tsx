import { ChevronDown, History, Loader2, MessagesSquare, Trash2 } from "lucide-react";

import { api } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import type { ImageSessionSummary } from "../../lib/types";
import type { ImageChatTranslate } from "./display";

interface ImageChatSessionListProps {
  items: ImageSessionSummary[];
  isLoading: boolean;
  selectedSessionId: string | null;
  deletingSessionId: string | null;
  deletionEnabled: boolean;
  variant: "desktop" | "mobile";
  onSelectSession: (sessionId: string) => void;
  onDeleteSession: (sessionId: string) => void;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
  t: ImageChatTranslate;
}

export function ImageChatSessionList({
  items,
  isLoading,
  selectedSessionId,
  deletingSessionId,
  deletionEnabled,
  variant,
  onSelectSession,
  onDeleteSession,
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
  t,
}: ImageChatSessionListProps) {
  const containerClassName =
    variant === "desktop"
      ? "flex gap-3 overflow-x-auto p-3 lg:min-h-0 lg:flex-1 lg:flex-col lg:gap-2 lg:overflow-x-visible lg:overflow-y-auto"
      : "min-h-0 flex-1 space-y-2 overflow-y-auto p-3";

  return (
    <div className={containerClassName}>
      {isLoading ? (
        <div className="flex gap-3 overflow-x-auto lg:flex-col lg:gap-2 lg:overflow-x-visible">
          {[1, 2, 3].map((i) => (
            <div
              key={i}
              className={`flex shrink-0 items-center gap-3 rounded-2xl border border-border-l2 bg-surface-raised p-2.5 shadow-sm dark:border-border-l2/80 dark:bg-surface-panel ${
                variant === "desktop" ? "w-64 lg:w-auto" : "w-full"
              }`}
            >
              <div className="h-16 w-16 shrink-0 rounded-xl bg-surface-subtle dark:bg-surface-base animate-shimmer" />
              <div className="flex-1 space-y-2">
                <div className="h-4 w-3/4 rounded bg-surface-subtle dark:bg-surface-panel/60 animate-shimmer" />
                <div className="h-3 w-1/2 rounded bg-surface-subtle dark:bg-surface-panel/60 animate-shimmer" />
                <div className="h-3 w-1/3 rounded bg-surface-subtle dark:bg-surface-panel/60 animate-shimmer" />
              </div>
            </div>
          ))}
        </div>
      ) : items.length ? (
        <>
          {items.map((item) => (
            <ImageChatSessionCard
              key={item.id}
              item={item}
              active={item.id === selectedSessionId}
              deleting={deletingSessionId === item.id}
              deletionEnabled={deletionEnabled}
              variant={variant}
              onSelectSession={onSelectSession}
              onDeleteSession={onDeleteSession}
              t={t}
            />
          ))}
          {hasNextPage ? (
            <button
              type="button"
              onClick={onLoadMore}
              disabled={isFetchingNextPage}
              className="inline-flex min-h-10 w-full shrink-0 items-center justify-center gap-2 rounded-xl border border-border-l1 bg-surface-raised px-3 text-xs font-semibold text-text-secondary transition-colors hover:border-accent hover:text-accent disabled:cursor-wait disabled:opacity-60 dark:border-border-l1 dark:bg-surface-base/55 dark:text-text-secondary dark:hover:border-accent/60 dark:hover:text-accent"
            >
              {isFetchingNextPage ? <Loader2 size={14} className="animate-spin" /> : <ChevronDown size={14} />}
              <span>{t("chat.loadMoreSessions")}</span>
            </button>
          ) : null}
        </>
      ) : (
        <div className="rounded-2xl border border-dashed border-border-l1 p-5 text-center text-sm text-text-muted dark:border-border-l1 dark:text-text-muted">
          {t("chat.noSessions")}
        </div>
      )}
    </div>
  );
}

interface ImageChatSessionCardProps {
  item: ImageSessionSummary;
  active: boolean;
  deleting: boolean;
  deletionEnabled: boolean;
  variant: "desktop" | "mobile";
  onSelectSession: (sessionId: string) => void;
  onDeleteSession: (sessionId: string) => void;
  t: ImageChatTranslate;
}

function ImageChatSessionCard({
  item,
  active,
  deleting,
  deletionEnabled,
  variant,
  onSelectSession,
  onDeleteSession,
  t,
}: ImageChatSessionCardProps) {
  const cardClassName = `group relative overflow-hidden rounded-2xl border transition-all ${
    variant === "desktop" ? "w-64 shrink-0 lg:w-auto " : ""
  }${
    active
      ? "border-accent bg-accent-soft shadow-sm  ring-1 ring-accent/80 dark:border-accent/80 dark:bg-accent/14  dark:ring-accent/45"
      : "border-border-l1 bg-surface-raised hover:border-border-l3 hover:bg-surface-base dark:border-border-l1/75 dark:bg-surface-panel dark:hover:border-accent/45 dark:hover:bg-surface-subtle"
  }`;
  const selectClassName =
    variant === "desktop"
      ? "flex w-full items-center gap-3 p-2.5 pr-10 text-left"
      : "flex min-h-20 w-full items-center gap-3 p-2.5 pr-12 text-left active:scale-[0.99] focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:focus-visible:ring-accent";
  const deleteClassName =
    variant === "desktop"
      ? "absolute right-2 top-2 inline-flex h-7 w-7 items-center justify-center rounded-lg bg-surface-raised/95 text-text-muted opacity-100 shadow-sm ring-1 ring-border-l1 transition-colors hover:text-state-error disabled:opacity-60 dark:bg-surface-base/88 dark:text-text-muted dark:ring-border-l1 dark:hover:text-state-error md:opacity-0 md:group-hover:opacity-100"
      : "absolute right-2 top-2 inline-flex h-11 w-11 items-center justify-center rounded-xl bg-surface-raised/95 text-text-muted shadow-sm ring-1 ring-border-l1 transition-colors active:scale-[0.98] hover:text-state-error disabled:opacity-60 dark:bg-surface-base/88 dark:text-text-muted dark:ring-border-l1 dark:hover:text-state-error";

  return (
    <div className={cardClassName}>
      <button type="button" onClick={() => onSelectSession(item.id)} className={selectClassName}>
        <div className="relative flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-surface-subtle text-text-muted ring-1 ring-border-l1 dark:bg-surface-base dark:text-text-muted dark:ring-border-l3/80">
          {item.latest_generated_asset ? (
            <img
              src={api.toApiUrl(item.latest_generated_asset.thumbnail_url)}
              alt={item.title}
              loading="lazy"
              decoding="async"
              className="h-full w-full object-cover"
            />
          ) : (
            <MessagesSquare size={18} />
          )}
          {active ? <div className="absolute inset-0 ring-2 ring-inset ring-accent/60 dark:ring-accent/80" /> : null}
        </div>
        <div className="min-w-0 flex-1">
          <div className={`truncate text-sm font-semibold ${active ? "text-accent dark:text-white" : "text-text-primary dark:text-text-primary"}`}>
            {item.title}
          </div>
          <div className="mt-1 flex items-center gap-1.5 text-[11px] text-text-muted dark:text-text-secondary">
            <History size={11} />
            <span>{t("chat.roundCount", { count: item.rounds_count })}</span>
          </div>
          <div className="mt-0.5 truncate text-[11px] text-text-muted dark:text-text-muted">{formatDateTime(item.updated_at, t.locale)}</div>
        </div>
      </button>
      <button
        type="button"
        aria-label={t("chat.deleteSession")}
        onClick={() => onDeleteSession(item.id)}
        disabled={deleting || !deletionEnabled}
        title={deletionEnabled ? t("chat.deleteSession") : t("chat.deleteDisabled")}
        className={deleteClassName}
      >
        {deleting ? <Loader2 size={variant === "desktop" ? 13 : 14} className="animate-spin" /> : <Trash2 size={variant === "desktop" ? 13 : 15} />}
      </button>
    </div>
  );
}
