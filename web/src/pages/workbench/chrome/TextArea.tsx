import { useLayoutEffect, useRef } from "react";

interface TextAreaProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
  minRows?: number;
  maxRows?: number;
  placeholder?: string;
  onBlur?: () => void;
}

const TEXTAREA_LINE_HEIGHT_PX = 19;
const TEXTAREA_VERTICAL_PADDING_PX = 16;

export function TextArea({
  label,
  value,
  onChange,
  minRows = 2,
  maxRows,
  placeholder,
  onBlur,
}: TextAreaProps) {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const minHeight = minRows * TEXTAREA_LINE_HEIGHT_PX + TEXTAREA_VERTICAL_PADDING_PX;

  useLayoutEffect(() => {
    const textarea = textareaRef.current;
    if (!textarea) {
      return;
    }
    textarea.style.height = "auto";
    const maxHeight =
      maxRows === undefined ? Number.POSITIVE_INFINITY : maxRows * TEXTAREA_LINE_HEIGHT_PX + TEXTAREA_VERTICAL_PADDING_PX;
    const nextHeight = Math.max(minHeight, Math.min(textarea.scrollHeight, maxHeight));
    textarea.style.height = `${nextHeight}px`;
    textarea.style.overflowY = textarea.scrollHeight > maxHeight ? "auto" : "hidden";
  }, [maxRows, minHeight, value]);

  return (
    <label className="block">
      <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-400">
        {label}
      </span>
      <textarea
        ref={textareaRef}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onBlur={onBlur}
        placeholder={placeholder}
        rows={minRows}
        style={{ minHeight }}
        className="w-full resize-none px-3 py-2 text-xs leading-relaxed outline-none textarea-premium"
      />
    </label>
  );
}
