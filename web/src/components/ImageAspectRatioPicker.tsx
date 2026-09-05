import { useEffect, useMemo, useState } from "react";

import { useI18n } from "../lib/preferences";
import { formatAspectRatio, ImageRatioFrame, parseAspectRatio } from "./ImageRatioFrame";

export const DEFAULT_IMAGE_ASPECT_RATIOS = [
  "1:1",
  "4:5",
  "3:4",
  "2:3",
  "9:16",
  "5:4",
  "4:3",
  "3:2",
  "16:9",
] as const;

interface ImageAspectRatioPickerProps {
  value: string;
  onChange: (value: string) => void;
  presets?: readonly string[];
  disabled?: boolean;
  allowCustom?: boolean;
}

function customDraft(value: string): { width: string; height: string } {
  const parsed = parseAspectRatio(value);
  return parsed
    ? { width: String(parsed.width), height: String(parsed.height) }
    : { width: "", height: "" };
}

export function ImageAspectRatioPicker({
  value,
  onChange,
  presets = DEFAULT_IMAGE_ASPECT_RATIOS,
  disabled = false,
  allowCustom = true,
}: ImageAspectRatioPickerProps) {
  const { t } = useI18n();
  const presetValues = useMemo(() => new Set(presets), [presets]);
  const [{ width, height }, setDraft] = useState(() => customDraft(value));
  const normalized = formatAspectRatio(width, height);

  useEffect(() => {
    setDraft(customDraft(value));
  }, [value]);

  const updateCustom = (nextWidth: string, nextHeight: string) => {
    setDraft({ width: nextWidth, height: nextHeight });
    const next = formatAspectRatio(nextWidth, nextHeight);
    if (next) {
      onChange(next);
    }
  };

  return (
    <div className="space-y-3" data-image-aspect-ratio-picker>
      <div className="grid grid-cols-3 gap-2">
        {presets.map((ratio) => {
          const active = value === ratio;
          return (
            <button
              key={ratio}
              type="button"
              onClick={() => onChange(ratio)}
              disabled={disabled}
              aria-pressed={active}
              className={`flex h-20 flex-col items-center justify-center rounded-xl border px-1.5 text-xs font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
                active
                  ? "border-indigo-500 bg-indigo-50 text-indigo-700 ring-2 ring-indigo-100 dark:border-violet-400 dark:bg-violet-500/18 dark:text-violet-50 dark:ring-violet-400/45"
                  : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900 dark:border-slate-700 dark:bg-slate-950/62 dark:text-slate-300 dark:hover:border-violet-400/50 dark:hover:text-violet-100"
              }`}
            >
              <ImageRatioFrame aspectRatio={ratio} />
              <span className="mt-1">{ratio}</span>
            </button>
          );
        })}
      </div>

      {!allowCustom && !presetValues.has(value) ? <p className="text-xs text-state-warning">{t("imageAspect.current", { ratio: value })}</p> : null}
      {allowCustom ? <div className={`rounded-xl border p-3 ${
        !presetValues.has(value) && parseAspectRatio(value)
          ? "border-indigo-200 bg-indigo-50/60 dark:border-violet-400/40 dark:bg-violet-500/10"
          : "border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-slate-950/50"
      }`}>
        <div className="mb-2 flex items-center justify-between gap-2">
          <div className="text-xs font-semibold text-slate-700 dark:text-slate-100">{t("imageAspect.custom")}</div>
          <div className="shrink-0 text-[11px] font-medium text-slate-400 dark:text-slate-500">
            {t("imageAspect.current", { ratio: value })}
          </div>
        </div>
        <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-2">
          <label className="block min-w-0">
            <span className="mb-1 block text-[10px] font-semibold text-slate-400 dark:text-slate-500">
              {t("imageAspect.width")}
            </span>
            <input
              value={width}
              inputMode="numeric"
              pattern="[0-9]*"
              maxLength={3}
              onChange={(event) => updateCustom(event.target.value, height)}
              disabled={disabled}
              className="h-9 w-full rounded-lg border border-slate-200 bg-white px-2 text-xs text-slate-900 outline-none transition-colors focus:border-indigo-500 focus:ring-2 focus:ring-indigo-100 disabled:bg-slate-100 dark:border-slate-700 dark:bg-slate-900/80 dark:text-slate-100 dark:focus:border-violet-400 dark:focus:ring-violet-400/20 dark:disabled:bg-slate-950"
              placeholder="16"
            />
          </label>
          <span className="pb-2 text-xs text-slate-400 dark:text-slate-500">:</span>
          <label className="block min-w-0">
            <span className="mb-1 block text-[10px] font-semibold text-slate-400 dark:text-slate-500">
              {t("imageAspect.height")}
            </span>
            <input
              value={height}
              inputMode="numeric"
              pattern="[0-9]*"
              maxLength={3}
              onChange={(event) => updateCustom(width, event.target.value)}
              disabled={disabled}
              className="h-9 w-full rounded-lg border border-slate-200 bg-white px-2 text-xs text-slate-900 outline-none transition-colors focus:border-indigo-500 focus:ring-2 focus:ring-indigo-100 disabled:bg-slate-100 dark:border-slate-700 dark:bg-slate-900/80 dark:text-slate-100 dark:focus:border-violet-400 dark:focus:ring-violet-400/20 dark:disabled:bg-slate-950"
              placeholder="9"
            />
          </label>
        </div>
        <div className={`mt-2 text-[11px] leading-5 ${normalized ? "text-slate-500 dark:text-slate-400" : "text-red-600 dark:text-red-300"}`}>
          {normalized ? t("imageAspect.valid", { ratio: normalized }) : t("imageAspect.invalid")}
        </div>
      </div> : null}
    </div>
  );
}
