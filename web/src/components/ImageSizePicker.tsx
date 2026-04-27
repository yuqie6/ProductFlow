import { useEffect, useMemo, useState } from "react";

import type { ImageSizeOption } from "../lib/imageSizes";
import {
  formatImageSizeValue,
  getImageSizePresetDisplay,
  normalizeImageSizeValue,
  parseImageSizeValue,
  resolveImageSize,
} from "../lib/imageSizes";

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

function frameClassName(aspect: string): string {
  if (aspect === "1:1") {
    return "h-8 w-8";
  }
  if (aspect === "2:3" || aspect === "9:16") {
    return "h-10 w-7";
  }
  if (aspect === "3:2" || aspect === "16:9") {
    return "h-7 w-10";
  }
  return "h-8 w-8";
}

export function ImageSizePicker({ value, presets, onChange, disabled = false, maxDimension }: ImageSizePickerProps) {
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
          const display = getImageSizePresetDisplay(option);
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
                  ? "border-indigo-500 bg-indigo-50 text-indigo-700 ring-2 ring-indigo-100"
                  : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900"
              }`}
            >
              <span
                className={`mb-1.5 flex items-center justify-center rounded-sm border-2 border-current text-[10px] font-black leading-none ${frameClassName(option.aspect)}`}
              >
                {display.tierLabel}
              </span>
              <span>{display.aspectLabel}</span>
              <span className="mt-0.5 text-[10px] font-medium text-slate-400">{display.dimensionLabel}</span>
            </button>
          );
        })}
      </div>
      <div className="rounded-xl border border-slate-200 bg-slate-50 p-3">
        <div className="mb-2 flex items-center justify-between gap-2">
          <div className="text-xs font-semibold text-slate-700">自定义宽高</div>
          <div className="shrink-0 text-[11px] font-medium text-slate-400">
            当前 {normalizedValue ? formatImageSizeValue(normalizedValue) : "未设置"}
          </div>
        </div>
        <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-2">
          <label className="block min-w-0">
            <span className="mb-1 block text-[10px] font-semibold uppercase tracking-widest text-slate-400">宽</span>
            <input
              value={width}
              inputMode="numeric"
              pattern="[0-9]*"
              onChange={(event) => updateCustom(event.target.value, height)}
              disabled={disabled}
              className="h-9 w-full rounded-lg border border-slate-200 bg-white px-2 text-xs text-slate-900 outline-none transition-colors focus:border-indigo-500 focus:ring-2 focus:ring-indigo-100 disabled:bg-slate-100"
              placeholder="2048"
            />
          </label>
          <span className="pb-2 text-xs text-slate-400">×</span>
          <label className="block min-w-0">
            <span className="mb-1 block text-[10px] font-semibold uppercase tracking-widest text-slate-400">高</span>
            <input
              value={height}
              inputMode="numeric"
              pattern="[0-9]*"
              onChange={(event) => updateCustom(width, event.target.value)}
              disabled={disabled}
              className="h-9 w-full rounded-lg border border-slate-200 bg-white px-2 text-xs text-slate-900 outline-none transition-colors focus:border-indigo-500 focus:ring-2 focus:ring-indigo-100 disabled:bg-slate-100"
              placeholder="2048"
            />
          </label>
        </div>
        <div className="mt-2 text-[11px] leading-5 text-slate-500">
          {customResolution ? (
            <>
              最终输出：{formatImageSizeValue(customResolution.value)}
              {customResolution.calibrated ? `（已按单边 ${maxDimension ?? 3840} 安全边界自动校准）` : ""}
            </>
          ) : (
            "请输入正整数宽高；后端仍会做最终校验。"
          )}
        </div>
      </div>
    </div>
  );
}
