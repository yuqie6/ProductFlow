import { useEffect, useState } from "react";

import { SelectField, type SelectFieldOption } from "./SelectField";

const LABEL_CLASS_NAME = "mb-1 block text-[11px] font-semibold text-slate-500 dark:text-slate-400";
const INPUT_CLASS_NAME = "h-9 w-full rounded-lg border border-slate-200 bg-slate-50 px-2 text-xs text-slate-900 outline-none transition-colors placeholder:text-slate-400 focus:border-indigo-500 focus:bg-white focus:ring-2 focus:ring-indigo-100 disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-400 dark:border-slate-700 dark:bg-[#111b2d] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400 dark:focus:bg-[#111b2d] dark:focus:ring-violet-400/20 dark:disabled:bg-slate-950";

interface CompactInputProps {
  label: string;
  value: string | number;
  placeholder?: string;
  inputMode?: "text" | "numeric";
  maxLength?: number;
  disabled?: boolean;
  onChange: (value: string) => void;
}

export function CompactInput({
  label,
  value,
  placeholder,
  inputMode,
  maxLength,
  disabled,
  onChange,
}: CompactInputProps) {
  return (
    <label className="block">
      <span className={LABEL_CLASS_NAME}>{label}</span>
      <input
        value={value}
        inputMode={inputMode}
        placeholder={placeholder}
        maxLength={maxLength}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
        className={INPUT_CLASS_NAME}
      />
    </label>
  );
}

interface CompactNumberInputProps {
  label: string;
  value: number | string | null;
  min?: number;
  max?: number;
  optional?: boolean;
  disabled?: boolean;
  onChange: (value: number | string | null) => void;
}

export function CompactNumberInput({
  label,
  value,
  min,
  max,
  optional = false,
  disabled,
  onChange,
}: CompactNumberInputProps) {
  const [draft, setDraft] = useState(value === null ? "" : String(value));
  const invalidDraft = draft === ""
    ? value !== null && !optional
    : !isValidNumberDraft(draft, min, max, optional);

  useEffect(() => {
    setDraft(value === null ? "" : String(value));
  }, [value]);

  return (
    <label className="block">
      <span className={LABEL_CLASS_NAME}>{label}</span>
      <input
        type="number"
        value={draft}
        min={min}
        max={max}
        disabled={disabled}
        aria-invalid={invalidDraft}
        onChange={(event) => {
          const next = event.target.value;
          setDraft(next);
          if (!next && optional) {
            onChange(null);
            return;
          }
          onChange(isValidNumberDraft(next, min, max, optional) ? Number(next) : next);
        }}
        className={`${INPUT_CLASS_NAME} ${
          invalidDraft
            ? "border-red-400 focus:border-red-500 focus:ring-red-100 dark:border-red-400/70 dark:focus:border-red-400 dark:focus:ring-red-400/20"
            : ""
        }`}
      />
    </label>
  );
}

function isValidNumberDraft(draft: string, min?: number, max?: number, optional = false): boolean {
  if (!draft) return optional;
  const parsed = Number(draft);
  return Number.isFinite(parsed)
    && (min === undefined || parsed >= min)
    && (max === undefined || parsed <= max);
}

interface CompactSelectProps {
  label: string;
  value: string;
  options: readonly SelectFieldOption[];
  disabled?: boolean;
  onChange: (value: string) => void;
}

export function CompactSelect({ label, value, options, disabled, onChange }: CompactSelectProps) {
  return (
    <div className="block">
      <span className={LABEL_CLASS_NAME}>{label}</span>
      <SelectField
        value={value}
        options={options}
        onChange={onChange}
        ariaLabel={label}
        disabled={disabled}
        radius="lg"
        visualSize="sm"
      />
    </div>
  );
}
