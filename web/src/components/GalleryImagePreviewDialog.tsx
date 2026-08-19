import { ChevronLeft, ChevronRight, Download, X } from "lucide-react";
import { type ReactNode, useEffect } from "react";

import { api } from "../lib/api";
import { useI18n } from "../lib/preferences";

export interface GalleryPreviewMetadataRow {
  label: string;
  value: string;
}

interface GalleryImagePreviewDialogProps {
  ariaLabel: string;
  imageUrl: string;
  imageAlt: string;
  title: string;
  subtitle?: string;
  body: ReactNode;
  metadataRows?: GalleryPreviewMetadataRow[];
  providerNotes?: string[];
  providerNotesTitle: string;
  downloadUrl: string;
  downloadLabel: string;
  closeLabel: string;
  onClose: () => void;
  hasPrev?: boolean;
  hasNext?: boolean;
  onPrev?: () => void;
  onNext?: () => void;
  prevLabel?: string;
  nextLabel?: string;
  counterText?: string;
}

export function GalleryImagePreviewDialog({
  ariaLabel,
  imageUrl,
  imageAlt,
  title,
  subtitle,
  body,
  metadataRows = [],
  providerNotes = [],
  providerNotesTitle,
  downloadUrl,
  downloadLabel,
  closeLabel,
  onClose,
  hasPrev = false,
  hasNext = false,
  onPrev,
  onNext,
  prevLabel,
  nextLabel,
  counterText,
}: GalleryImagePreviewDialogProps) {
  const { t } = useI18n();
  const resolvedPrevLabel = prevLabel ?? t("galleryImagePreview.previous");
  const resolvedNextLabel = nextLabel ?? t("galleryImagePreview.next");
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      } else if (event.key === "ArrowLeft" && hasPrev && onPrev) {
        event.preventDefault();
        onPrev();
      } else if (event.key === "ArrowRight" && hasNext && onNext) {
        event.preventDefault();
        onNext();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [hasPrev, hasNext, onPrev, onNext, onClose]);

  return (
    <div
      className="fixed inset-0 z-[80] flex items-center justify-center overflow-hidden bg-slate-950/86 p-2 backdrop-blur-sm sm:p-4"
      role="dialog"
      aria-modal="true"
      aria-label={ariaLabel}
      onClick={onClose}
    >
      <div
        className="grid h-[calc(100svh-1rem)] max-h-[calc(100svh-1rem)] w-full max-w-[calc(100vw-1rem)] min-h-0 grid-rows-[minmax(0,1fr)_minmax(0,42svh)] overflow-hidden rounded-lg bg-white shadow-2xl sm:h-[calc(100svh-2rem)] sm:max-h-[calc(100svh-2rem)] sm:max-w-[calc(100vw-2rem)] lg:grid-cols-[minmax(0,1fr)_minmax(320px,380px)] lg:grid-rows-1 xl:max-w-[92rem] animate-spring-pop-in"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="group/viewer relative flex min-h-0 items-center justify-center bg-slate-950">
          <img src={imageUrl} alt={imageAlt} decoding="async" className="h-full max-h-full w-full select-none object-contain" />

          {hasPrev && onPrev ? (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onPrev();
              }}
              className="absolute left-3 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-slate-900/70 text-white shadow-lg backdrop-blur-sm transition-all hover:scale-110 hover:bg-slate-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-white sm:h-12 sm:w-12"
              aria-label={resolvedPrevLabel}
              title={resolvedPrevLabel}
            >
              <ChevronLeft size={24} />
            </button>
          ) : null}

          {hasNext && onNext ? (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onNext();
              }}
              className="absolute right-3 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-slate-900/70 text-white shadow-lg backdrop-blur-sm transition-all hover:scale-110 hover:bg-slate-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-white sm:h-12 sm:w-12"
              aria-label={resolvedNextLabel}
              title={resolvedNextLabel}
            >
              <ChevronRight size={24} />
            </button>
          ) : null}

          {counterText ? (
            <div className="pointer-events-none absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-slate-950/75 px-3 py-1 text-[11px] font-semibold text-slate-200 backdrop-blur-sm">
              {counterText}
            </div>
          ) : null}
        </div>
        <aside className="flex min-h-0 flex-col border-t border-slate-200 lg:border-l lg:border-t-0">
          <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3">
            <div className="min-w-0">
              <div className="text-sm font-bold text-slate-950">{title}</div>
              {subtitle ? <div className="mt-0.5 truncate text-xs text-slate-500">{subtitle}</div> : null}
            </div>
            <button
              type="button"
              onClick={onClose}
              className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-950"
              aria-label={closeLabel}
            >
              <X size={18} />
            </button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
            <div className="whitespace-pre-wrap break-words text-sm leading-6 text-slate-800">{body}</div>
            {metadataRows.length ? (
              <div className="mt-6 grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
                {metadataRows.map((row) => (
                  <div key={row.label} className="min-w-0">
                    <div className="font-semibold text-slate-400">{row.label}</div>
                    <div className="mt-1 truncate font-medium text-slate-800">{row.value}</div>
                  </div>
                ))}
              </div>
            ) : null}
            {providerNotes.length ? (
              <div className="mt-6 border-t border-slate-200 pt-4">
                <div className="text-xs font-bold uppercase text-slate-400">{providerNotesTitle}</div>
                <ul className="mt-2 space-y-1 text-xs leading-5 text-slate-600">
                  {providerNotes.map((note) => (
                    <li key={note}>{note}</li>
                  ))}
                </ul>
              </div>
            ) : null}
          </div>
          <div className="border-t border-slate-200 p-4">
            <a
              href={api.toApiUrl(downloadUrl)}
              target="_blank"
              rel="noreferrer"
              className="inline-flex w-full items-center justify-center rounded-lg bg-slate-950 px-3 py-2.5 text-sm font-semibold text-white transition-colors hover:bg-indigo-700"
            >
              <Download size={16} className="mr-2" />
              {downloadLabel}
            </a>
          </div>
        </aside>
      </div>
    </div>
  );
}
