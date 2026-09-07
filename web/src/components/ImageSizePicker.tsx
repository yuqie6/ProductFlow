import { useEffect, useMemo, useState } from "react";

import type { ImageSizeOption } from "../lib/imageSizes";
import {
  IMAGE_GENERATION_MIN_DIMENSION,
  formatImageSizeValue,
  getImageSizePresetDisplay,
  normalizeImageSizeValue,
  parseImageSizeValue,
  resolveImageSize,
} from "../lib/imageSizes";
import { useI18n } from "../lib/preferences";
import { ImageRatioFrame } from "./ImageRatioFrame";

interface ImageSizePickerProps {
  value: string;
  presets: ImageSizeOption[];
  onChange: (value: string) => void;
  disabled?: boolean;
  maxDimension?: number;
}

function splitSize(value: string, maxDimension?: number): { width: string; height: string } {
  const parsed = parseImageSizeValue(value, maxDimension);
  if (!parsed) {
    return { width: "", height: "" };
  }
  return { width: String(parsed.width), height: String(parsed.height) };
}

function resolveCustomDraft(width: string, height: string, maxDimension?: number) {
  if (!/^\d+$/.test(width) || !/^\d+$/.test(height)) {
    return null;
  }
  return resolveImageSize(Number(width), Number(height), maxDimension);
}

export function ImageSizePicker({ value, presets, onChange, disabled = false, maxDimension }: ImageSizePickerProps) {
  const { locale, t } = useI18n();
  const optionValues = useMemo(() => new Set(presets.map((option) => option.value)), [presets]);
  const normalizedValue = normalizeImageSizeValue(value, maxDimension);
  const selectedPreset = normalizedValue !== null && optionValues.has(normalizedValue);
  const [{ width, height }, setCustomDraft] = useState(() => splitSize(value, maxDimension));
  const customResolution = resolveCustomDraft(width, height, maxDimension);

  useEffect(() => {
    const parsed = splitSize(value, maxDimension);
    setCustomDraft(parsed);
  }, [maxDimension, value]);

  const updateCustom = (nextWidth: string, nextHeight: string) => {
    setCustomDraft({ width: nextWidth, height: nextHeight });
    const nextResolution = resolveCustomDraft(nextWidth, nextHeight, maxDimension);
    if (nextResolution) {
      onChange(nextResolution.value);
    }
  };

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-3 gap-2">
        {presets.map((option) => {
          const active = selectedPreset && option.value === normalizedValue;
          const display = getImageSizePresetDisplay(option, locale);
          return (
            <button
              key={option.value}
              type="button"
              onClick={() => {
                onChange(option.value);
              }}
              disabled={disabled}
              title={formatImageSizeValue(option.value)}
              className={`flex h-24 flex-col items-center justify-center rounded-xl border px-1.5 text-xs font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
                active
                  ? "border-accent bg-accent-soft text-accent ring-2 ring-accent dark:border-accent dark:bg-accent/18 dark:text-accent dark:ring-accent/45"
                  : "border-border-l1 bg-surface-raised text-text-secondary hover:border-border-l3 hover:text-text-primary dark:border-border-l1 dark:bg-surface-base/62 dark:text-text-secondary dark:hover:border-accent/50 dark:hover:text-accent"
              }`}
            >
              <ImageRatioFrame aspectRatio={option.aspect} label={display.tierLabel} className="mb-1.5" />
              <span>{display.aspectLabel}</span>
              <span className="mt-0.5 text-[10px] font-medium text-text-muted dark:text-text-muted">{display.dimensionLabel}</span>
            </button>
          );
        })}
      </div>
      <div className="rounded-xl border border-border-l1 bg-surface-base p-3 dark:border-border-l1 dark:bg-surface-base/50">
        <div className="mb-2 flex items-center justify-between gap-2">
          <div className="text-xs font-semibold text-text-secondary dark:text-text-primary">{t("imageSize.custom")}</div>
          <div className="shrink-0 text-[11px] font-medium text-text-muted dark:text-text-muted">
            {t("imageSize.current", { size: normalizedValue ? formatImageSizeValue(normalizedValue) : t("imageSize.unset") })}
          </div>
        </div>
        <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-2">
          <label className="block min-w-0">
            <span className="mb-1 block text-[10px] font-semibold uppercase tracking-widest text-text-muted dark:text-text-muted">
              {t("imageSize.width")}
            </span>
            <input
              value={width}
              inputMode="numeric"
              pattern="[0-9]*"
              onChange={(event) => updateCustom(event.target.value, height)}
              disabled={disabled}
              className="h-9 w-full rounded-lg border border-border-l1 bg-surface-raised px-2 text-xs text-text-primary outline-none transition-colors focus:border-accent focus:ring-2 focus:ring-accent disabled:bg-surface-subtle dark:border-border-l1 dark:bg-surface-base/80 dark:text-text-primary dark:focus:border-accent dark:focus:ring-accent/20 dark:disabled:bg-surface-base"
              placeholder="2048"
            />
          </label>
          <span className="pb-2 text-xs text-text-muted dark:text-text-muted">×</span>
          <label className="block min-w-0">
            <span className="mb-1 block text-[10px] font-semibold uppercase tracking-widest text-text-muted dark:text-text-muted">
              {t("imageSize.height")}
            </span>
            <input
              value={height}
              inputMode="numeric"
              pattern="[0-9]*"
              onChange={(event) => updateCustom(width, event.target.value)}
              disabled={disabled}
              className="h-9 w-full rounded-lg border border-border-l1 bg-surface-raised px-2 text-xs text-text-primary outline-none transition-colors focus:border-accent focus:ring-2 focus:ring-accent disabled:bg-surface-subtle dark:border-border-l1 dark:bg-surface-base/80 dark:text-text-primary dark:focus:border-accent dark:focus:ring-accent/20 dark:disabled:bg-surface-base"
              placeholder="2048"
            />
          </label>
        </div>
        <div className="mt-2 text-[11px] leading-5 text-text-muted dark:text-text-muted">
          {customResolution ? (
            <>
              {t("imageSize.output", { size: formatImageSizeValue(customResolution.value) })}
              {customResolution.calibrated
                ? t("imageSize.calibrated", { min: IMAGE_GENERATION_MIN_DIMENSION, max: maxDimension ?? 3840 })
                : ""}
            </>
          ) : (
            t("imageSize.invalid")
          )}
        </div>
      </div>
    </div>
  );
}
