import { ChevronLeft, ChevronRight, Download, X } from "lucide-react";
import { type ReactNode, useEffect, useId } from "react";

import { api } from "../lib/api";
import { useI18n } from "../lib/preferences";
import { buttonVariants } from "./ui/button";
import { Dialog, DialogContent, DialogTitle } from "./ui/dialog";
import { IconButton } from "./ui/icon-button";
import { cn } from "./ui/cn";

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
  const titleId = useId();
  const resolvedPrevLabel = prevLabel ?? t("galleryImagePreview.previous");
  const resolvedNextLabel = nextLabel ?? t("galleryImagePreview.next");
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "ArrowLeft" && hasPrev && onPrev) {
        event.preventDefault();
        onPrev();
      } else if (event.key === "ArrowRight" && hasNext && onNext) {
        event.preventDefault();
        onNext();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [hasPrev, hasNext, onPrev, onNext]);

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent
        hideClose
        labelledBy={titleId}
        aria-label={ariaLabel}
        data-global-agent-modal=""
        className="h-[calc(100svh-1rem)] max-h-[calc(100svh-1rem)] w-full max-w-[calc(100vw-1rem)] min-h-0 overflow-hidden p-0 sm:h-[calc(100svh-2rem)] sm:max-h-[calc(100svh-2rem)] sm:max-w-[calc(100vw-2rem)] xl:max-w-[92rem]"
        bodyClassName="grid min-h-0 h-full grid-rows-[minmax(0,1fr)_minmax(0,42svh)] p-0 lg:grid-cols-[minmax(0,1fr)_minmax(320px,380px)] lg:grid-rows-1"
      >
        <div className="group/viewer relative flex min-h-0 items-center justify-center bg-media-backdrop">
          <img src={imageUrl} alt={imageAlt} decoding="async" className="h-full max-h-full w-full select-none object-contain" />

          {hasPrev && onPrev ? (
            <IconButton
              label={resolvedPrevLabel}
              variant="secondary"
              size="toolbar"
              className="absolute left-3 top-1/2 -translate-y-1/2 bg-media-backdrop/70 text-accent-fg hover:bg-media-backdrop"
              onClick={(event) => {
                event.stopPropagation();
                onPrev();
              }}
            >
              <ChevronLeft size={24} />
            </IconButton>
          ) : null}

          {hasNext && onNext ? (
            <IconButton
              label={resolvedNextLabel}
              variant="secondary"
              size="toolbar"
              className="absolute right-3 top-1/2 -translate-y-1/2 bg-media-backdrop/70 text-accent-fg hover:bg-media-backdrop"
              onClick={(event) => {
                event.stopPropagation();
                onNext();
              }}
            >
              <ChevronRight size={24} />
            </IconButton>
          ) : null}

          {counterText ? (
            <div className="pointer-events-none absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-media-backdrop/75 px-3 py-1 text-[11px] font-semibold text-accent-fg backdrop-blur-sm">
              {counterText}
            </div>
          ) : null}
        </div>
        <aside className="flex min-h-0 flex-col border-t border-border-l1 bg-surface-raised lg:border-l lg:border-t-0">
          <div className="flex items-center justify-between border-b border-border-l1 px-4 py-3">
            <div className="min-w-0">
              <DialogTitle id={titleId} className="text-sm font-bold text-text-primary">
                {title}
              </DialogTitle>
              {subtitle ? <div className="mt-0.5 truncate text-xs text-text-muted">{subtitle}</div> : null}
            </div>
            <IconButton label={closeLabel} variant="ghost" size="md" onClick={onClose}>
              <X size={18} />
            </IconButton>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
            <div className="whitespace-pre-wrap break-words text-sm leading-6 text-text-primary">{body}</div>
            {metadataRows.length ? (
              <div className="mt-6 grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
                {metadataRows.map((row) => (
                  <div key={row.label} className="min-w-0">
                    <div className="font-semibold text-text-muted">{row.label}</div>
                    <div className="mt-1 truncate font-medium text-text-primary">{row.value}</div>
                  </div>
                ))}
              </div>
            ) : null}
            {providerNotes.length ? (
              <div className="mt-6 border-t border-border-l1 pt-4">
                <div className="text-xs font-bold uppercase text-text-muted">{providerNotesTitle}</div>
                <ul className="mt-2 space-y-1 text-xs leading-5 text-text-secondary">
                  {providerNotes.map((note) => (
                    <li key={note}>{note}</li>
                  ))}
                </ul>
              </div>
            ) : null}
          </div>
          <div className="border-t border-border-l1 p-4">
            <a
              href={api.toApiUrl(downloadUrl)}
              target="_blank"
              rel="noreferrer"
              className={cn(buttonVariants({ variant: "primary", size: "lg" }), "w-full")}
            >
              <Download size={16} />
              {downloadLabel}
            </a>
          </div>
        </aside>
      </DialogContent>
    </Dialog>
  );
}
