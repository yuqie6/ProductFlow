import type { ReactNode } from "react";

import type { ImageToolOptions } from "../lib/types";

interface ImageToolControlsProps {
  value: ImageToolOptions;
  onChange: (value: ImageToolOptions) => void;
  surface?: "card" | "plain";
}

function parseOptionalNumber(value: string): number | null {
  if (!value.trim()) {
    return null;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

export function ImageToolControls({ value, onChange, surface = "card" }: ImageToolControlsProps) {
  const update = (next: Partial<ImageToolOptions>) => onChange({ ...value, ...next });
  const containerClassName =
    surface === "card" ? "rounded-2xl border border-slate-200 bg-white p-4" : "space-y-3";
  return (
    <div className={containerClassName}>
      <div className="mb-3 text-sm font-semibold text-slate-950">Provider</div>
      <div className="grid grid-cols-2 gap-2">
        <CompactInput
          label="Tool"
          value={value.model ?? ""}
          placeholder="默认"
          onChange={(next) => update({ model: next || null })}
        />
        <CompactSelect
          label="质量"
          value={value.quality ?? ""}
          onChange={(next) => update({ quality: (next || null) as ImageToolOptions["quality"] })}
        >
          <option value="">默认</option>
          <option value="auto">Auto</option>
          <option value="low">Low</option>
          <option value="medium">Medium</option>
          <option value="high">High</option>
        </CompactSelect>
        <CompactSelect
          label="格式"
          value={value.output_format ?? ""}
          onChange={(next) => update({ output_format: (next || null) as ImageToolOptions["output_format"] })}
        >
          <option value="">默认</option>
          <option value="png">PNG</option>
          <option value="jpeg">JPEG</option>
          <option value="webp">WebP</option>
        </CompactSelect>
        <CompactInput
          label="压缩"
          value={value.output_compression ?? ""}
          inputMode="numeric"
          placeholder="默认"
          onChange={(next) => update({ output_compression: parseOptionalNumber(next) })}
        />
        <CompactSelect
          label="审核"
          value={value.moderation ?? ""}
          onChange={(next) => update({ moderation: (next || null) as ImageToolOptions["moderation"] })}
        >
          <option value="">默认</option>
          <option value="auto">Auto</option>
          <option value="low">Low</option>
        </CompactSelect>
        <CompactSelect
          label="Action"
          value={value.action ?? ""}
          onChange={(next) => update({ action: (next || null) as ImageToolOptions["action"] })}
        >
          <option value="">默认</option>
          <option value="auto">Auto</option>
          <option value="generate">Generate</option>
          <option value="edit">Edit</option>
        </CompactSelect>
        <CompactSelect
          label="Fidelity"
          value={value.input_fidelity ?? ""}
          onChange={(next) => update({ input_fidelity: (next || null) as ImageToolOptions["input_fidelity"] })}
        >
          <option value="">默认</option>
          <option value="low">Low</option>
          <option value="high">High</option>
        </CompactSelect>
        <CompactInput
          label="Partial"
          value={value.partial_images ?? ""}
          inputMode="numeric"
          placeholder="默认"
          onChange={(next) => update({ partial_images: parseOptionalNumber(next) })}
        />
        <CompactInput
          label="n"
          value={value.n ?? ""}
          inputMode="numeric"
          placeholder="默认"
          onChange={(next) => update({ n: parseOptionalNumber(next) })}
        />
      </div>
    </div>
  );
}

function CompactInput({
  label,
  value,
  placeholder,
  inputMode,
  onChange,
}: {
  label: string;
  value: string | number;
  placeholder?: string;
  inputMode?: "text" | "numeric";
  onChange: (value: string) => void;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-[11px] font-semibold text-slate-500">{label}</span>
      <input
        value={value}
        inputMode={inputMode}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="h-9 w-full rounded-lg border border-slate-200 bg-slate-50 px-2 text-xs text-slate-900 outline-none transition-colors placeholder:text-slate-400 focus:border-indigo-500 focus:bg-white focus:ring-2 focus:ring-indigo-100"
      />
    </label>
  );
}

function CompactSelect({
  label,
  value,
  onChange,
  children,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  children: ReactNode;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-[11px] font-semibold text-slate-500">{label}</span>
      <select
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="h-9 w-full rounded-lg border border-slate-200 bg-slate-50 px-2 text-xs text-slate-900 outline-none transition-colors focus:border-indigo-500 focus:bg-white focus:ring-2 focus:ring-indigo-100"
      >
        {children}
      </select>
    </label>
  );
}
