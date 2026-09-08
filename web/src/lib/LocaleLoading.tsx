import { Loader2, RotateCcw } from "lucide-react";
import { Button } from "../components/ui/button";
import type { Locale } from "./i18n";

// These small recovery labels must be available even when the selected resource cannot load.
const labels = {
  "zh-CN": { loading: "正在加载语言", failed: "语言暂时无法加载，请重试。", retry: "重试" },
  "en-US": { loading: "Loading language", failed: "The language could not be loaded. Please try again.", retry: "Retry" },
  "ja-JP": { loading: "言語を読み込み中", failed: "言語を読み込めませんでした。再試行してください。", retry: "再試行" },
  "vi-VN": { loading: "Đang tải ngôn ngữ", failed: "Không thể tải ngôn ngữ. Vui lòng thử lại.", retry: "Thử lại" },
};
export function LocaleLoading({ locale, failed, retry, sessionPending }: { locale: Locale; failed: boolean; retry: () => void; sessionPending: boolean }) {
  const text = labels[locale];
  return <div className="flex min-h-screen items-center justify-center bg-surface-base px-6 text-text-secondary" data-locale-loading={locale}>
    {failed ? <div className="max-w-sm space-y-5 text-center"><p role="alert" className="text-sm leading-6">{text.failed}</p><Button onClick={retry}><RotateCcw size={16} aria-hidden="true" />{text.retry}</Button></div> : <div role="status" className="flex items-center gap-3"><Loader2 size={24} className="animate-spin motion-reduce:animate-none" aria-hidden="true" /><span className="sr-only">{sessionPending ? "ProductFlow" : text.loading}</span></div>}
  </div>;
}
