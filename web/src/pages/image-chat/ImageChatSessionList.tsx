import { History, Loader2, MessagesSquare, Trash2 } from "lucide-react";

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
  t,
}: ImageChatSessionListProps) {
  const containerClassName =
    variant === "desktop"
      ? "flex gap-3 overflow-x-auto p-3 lg:min-h-0 lg:flex-1 lg:flex-col lg:gap-2 lg:overflow-x-visible lg:overflow-y-auto"
      : "min-h-0 flex-1 space-y-2 overflow-y-auto p-3";

  return (
    <div className={containerClassName}>
      {isLoading ? (
        <div className="flex justify-center py-12 text-slate-400">
          <Loader2 size={18} className="animate-spin" />
        </div>
      ) : items.length ? (
        items.map((item) => (
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
        ))
      ) : (
        <div className="rounded-2xl border border-dashed border-slate-200 p-5 text-center text-sm text-slate-500 dark:border-slate-700 dark:text-slate-400">
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
      ? "border-indigo-300 bg-indigo-50 shadow-sm shadow-indigo-100 ring-1 ring-indigo-200/80 dark:border-violet-500/80 dark:bg-violet-500/14 dark:shadow-violet-950/30 dark:ring-violet-400/45"
      : "border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50 dark:border-slate-700/75 dark:bg-[#151f33] dark:hover:border-violet-500/45 dark:hover:bg-[#1a2740]"
  }`;
  const selectClassName =
    variant === "desktop"
      ? "flex w-full items-center gap-3 p-2.5 pr-10 text-left"
      : "flex min-h-20 w-full items-center gap-3 p-2.5 pr-12 text-left active:scale-[0.99] focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-violet-400";
  const deleteClassName =
    variant === "desktop"
      ? "absolute right-2 top-2 inline-flex h-7 w-7 items-center justify-center rounded-lg bg-white/95 text-slate-400 opacity-100 shadow-sm ring-1 ring-slate-200 transition-colors hover:text-red-600 disabled:opacity-60 dark:bg-slate-950/88 dark:text-slate-400 dark:ring-slate-700 dark:hover:text-red-300 md:opacity-0 md:group-hover:opacity-100"
      : "absolute right-2 top-2 inline-flex h-11 w-11 items-center justify-center rounded-xl bg-white/95 text-slate-400 shadow-sm ring-1 ring-slate-200 transition-colors active:scale-[0.98] hover:text-red-600 disabled:opacity-60 dark:bg-slate-950/88 dark:text-slate-400 dark:ring-slate-700 dark:hover:text-red-300";

  return (
    <div className={cardClassName}>
      <button type="button" onClick={() => onSelectSession(item.id)} className={selectClassName}>
        <div className="relative flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-slate-100 text-slate-400 ring-1 ring-slate-200 dark:bg-[#0a1020] dark:text-slate-400 dark:ring-slate-600/80">
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
          {active ? <div className="absolute inset-0 ring-2 ring-inset ring-indigo-500/60 dark:ring-violet-400/80" /> : null}
        </div>
        <div className="min-w-0 flex-1">
          <div className={`truncate text-sm font-semibold ${active ? "text-indigo-950 dark:text-white" : "text-slate-900 dark:text-slate-100"}`}>
            {item.title}
          </div>
          <div className="mt-1 flex items-center gap-1.5 text-[11px] text-slate-500 dark:text-slate-300">
            <History size={11} />
            <span>{t("chat.roundCount", { count: item.rounds_count })}</span>
          </div>
          <div className="mt-0.5 truncate text-[11px] text-slate-400 dark:text-slate-500">{formatDateTime(item.updated_at)}</div>
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
