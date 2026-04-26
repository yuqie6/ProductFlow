import { useEffect, useMemo, useState } from "react";

import type { ImageSizeOption } from "../lib/imageSizes";
import { formatImageSizeValue, normalizeImageSizeValue, parseImageSizeValue, resolveImageSize } from "../lib/imageSizes";

interface ImageSizePickerProps {
  value: string;
  presets: ImageSizeOption[];
  onChange: (value: string) => void;
  disabled?: boolean;
}

function splitSize(value: string): { width: string; height: string } {
  const parsed = parseImageSizeValue(value);
  if (!parsed) {
    return { width: "", height: "" };
  }
  return { width: String(parsed.width), height: String(parsed.height) };
}

function resolveCustomDraft(width: string, height: string) {
  if (!/^\d+$/.test(width) || !/^\d+$/.test(height)) {
    return null;
  }
  return resolveImageSize(Number(width), Number(height));
}

export function ImageSizePicker({ value, presets, onChange, disabled = false }: ImageSizePickerProps) {
  const optionValues = useMemo(() => new Set(presets.map((option) => option.value)), [presets]);
  const normalizedValue = normalizeImageSizeValue(value);
  const selectedPreset = normalizedValue !== null && optionValues.has(normalizedValue);
  const [customMode, setCustomMode] = useState(!selectedPreset);
  const [{ width, height }, setCustomDraft] = useState(() => splitSize(value));
  const customResolution = resolveCustomDraft(width, height);

  useEffect(() => {
    const parsed = splitSize(value);
    setCustomDraft(parsed);
    setCustomMode(!selectedPreset);
  }, [selectedPreset, value]);

  const updateCustom = (nextWidth: string, nextHeight: string) => {
    setCustomDraft({ width: nextWidth, height: nextHeight });
    const nextResolution = resolveCustomDraft(nextWidth, nextHeight);
    if (nextResolution) {
      onChange(nextResolution.value);
    }
  };

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
        {presets.map((option) => {
          const active = !customMode && option.value === normalizedValue;
          return (
            <button
              key={option.value}
              type="button"
              onClick={() => {
                setCustomMode(false);
                onChange(option.value);
              }}
              disabled={disabled}
              className={`rounded-md border px-3 py-2 text-left text-xs leading-5 transition-colors ${
                active
                  ? "border-zinc-900 bg-zinc-900 text-white"
                  : "border-zinc-200 bg-white text-zinc-600 hover:border-zinc-300 hover:text-zinc-900"
              }`}
            >
              <span className="block font-medium">{option.label}</span>
              <span className="block opacity-70">{option.description}</span>
            </button>
          );
        })}
        <button
          type="button"
          onClick={() => setCustomMode(true)}
          disabled={disabled}
          className={`rounded-md border px-3 py-2 text-left text-xs leading-5 transition-colors ${
            customMode
              ? "border-zinc-900 bg-zinc-900 text-white"
              : "border-zinc-200 bg-white text-zinc-600 hover:border-zinc-300 hover:text-zinc-900"
          }`}
        >
          <span className="block font-medium">自定义宽高</span>
          {value ? <span className="block opacity-70">{formatImageSizeValue(value)}</span> : null}
        </button>
      </div>
      {customMode ? (
        <div className="space-y-2">
          <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-2">
            <label className="block">
              <span className="mb-1 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">宽</span>
              <input
                value={width}
                inputMode="numeric"
                pattern="[0-9]*"
                onChange={(event) => updateCustom(event.target.value, height)}
                disabled={disabled}
                className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
                placeholder="2048"
              />
            </label>
            <span className="pb-2 text-xs text-zinc-400">×</span>
            <label className="block">
              <span className="mb-1 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">高</span>
              <input
                value={height}
                inputMode="numeric"
                pattern="[0-9]*"
                onChange={(event) => updateCustom(width, event.target.value)}
                disabled={disabled}
                className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
                placeholder="2048"
              />
            </label>
          </div>
          <div className="text-[11px] leading-5 text-zinc-500">
            {customResolution ? (
              <>
                最终输出：{formatImageSizeValue(customResolution.value)}
                {customResolution.calibrated ? "（已按单边 3840 安全边界自动校准）" : ""}
              </>
            ) : (
              "请输入正整数宽高；后端仍会做最终校验。"
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}
