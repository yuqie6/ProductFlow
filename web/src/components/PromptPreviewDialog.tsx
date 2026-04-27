import { X } from "lucide-react";

export interface PromptPreview {
  title: string;
  text: string;
  meta?: string;
}

interface PromptPreviewDialogProps {
  preview: PromptPreview;
  onClose: () => void;
}

export function PromptPreviewDialog({ preview, onClose }: PromptPreviewDialogProps) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/35 p-4 backdrop-blur-sm">
      <div className="max-h-[82vh] w-full max-w-2xl overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl shadow-slate-950/20">
        <div className="flex items-start justify-between gap-4 border-b border-slate-200 px-5 py-4">
          <div className="min-w-0">
            <div className="text-sm font-semibold text-slate-950">{preview.title}</div>
            {preview.meta ? <div className="mt-1 text-xs text-slate-500">{preview.meta}</div> : null}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900"
            aria-label="关闭 Prompt 预览"
          >
            <X size={16} />
          </button>
        </div>
        <div className="max-h-[60vh] overflow-y-auto px-5 py-4">
          <pre className="whitespace-pre-wrap break-words rounded-xl bg-slate-50 p-4 text-sm leading-6 text-slate-800">
            {preview.text}
          </pre>
        </div>
      </div>
    </div>
  );
}
