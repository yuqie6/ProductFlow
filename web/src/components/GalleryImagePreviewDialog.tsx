import { Download, X } from "lucide-react";
import type { ReactNode } from "react";

import { api } from "../lib/api";

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
}: GalleryImagePreviewDialogProps) {
  return (
    <div
      className="fixed inset-0 z-[80] flex items-center justify-center bg-slate-950/86 p-3 backdrop-blur-sm sm:p-4"
      role="dialog"
      aria-modal="true"
      aria-label={ariaLabel}
      onClick={onClose}
    >
      <div
        className="grid max-h-[96svh] w-full max-w-[92rem] overflow-y-auto rounded-lg bg-white shadow-2xl lg:grid-cols-[minmax(0,1fr)_380px] lg:overflow-hidden"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex min-h-[320px] max-h-[55svh] items-center justify-center bg-slate-950 lg:min-h-[520px] lg:max-h-none">
          <img src={imageUrl} alt={imageAlt} decoding="async" className="max-h-[96svh] w-full object-contain" />
        </div>
        <aside className="flex min-h-0 flex-col border-l border-slate-200">
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
