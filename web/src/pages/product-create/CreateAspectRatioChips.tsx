import { ImageRatioFrame, parseAspectRatio } from "../../components/ImageRatioFrame";

import { CREATE_ASPECT_RATIO_PRESETS } from "./imageTypeSelection";

interface CreateAspectRatioChipsProps {
  value: string;
  disabled?: boolean;
  labelledBy?: string;
  className?: string;
  onChange: (value: string) => void;
}

export function CreateAspectRatioChips({
  value,
  disabled = false,
  labelledBy,
  className = "",
  onChange,
}: CreateAspectRatioChipsProps) {
  const presetList: readonly string[] = CREATE_ASPECT_RATIO_PRESETS;
  const options = presetList.includes(value) ? presetList : [...presetList, value];

  return (
    <div
      role="radiogroup"
      aria-labelledby={labelledBy}
      className={`flex flex-wrap gap-1 ${className}`}
    >
      {options.map((ratio) => {
        const selected = value === ratio && Boolean(parseAspectRatio(ratio));
        return (
          <button
            key={ratio}
            type="button"
            role="radio"
            aria-checked={selected}
            disabled={disabled}
            onClick={() => onChange(ratio)}
            className={`flex h-11 w-11 shrink-0 flex-col items-center justify-center rounded-lg border text-[10px] font-semibold tabular-nums transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60 ${selected
                ? "border-accent/60 bg-accent-soft/70 text-accent-strong"
                : "border-border-l1 bg-surface-raised text-text-secondary hover:border-border-l3 hover:text-text-primary"
              }`}
          >
            <ImageRatioFrame aspectRatio={ratio} size="sm" />
            <span>{ratio}</span>
          </button>
        );
      })}
    </div>
  );
}
