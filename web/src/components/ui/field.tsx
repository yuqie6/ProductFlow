import { useEffect, useId, useLayoutEffect, useRef, useState, type InputHTMLAttributes, type ReactNode, type TextareaHTMLAttributes } from "react";

import { cn } from "./cn";

const FIELD_LABEL_CLASS = "mb-1 block text-label font-semibold text-text-muted";
const CONTROL_CLASS =
  "w-full rounded-control border border-border-l1 bg-surface-subtle px-2.5 text-xs text-text-primary outline-none transition-[border-color,box-shadow,background-color] duration-fast placeholder:text-text-muted focus:border-accent focus:bg-surface-raised focus:ring-2 focus:ring-focus-ring disabled:cursor-not-allowed disabled:opacity-45";

export function Field({
  label,
  htmlFor,
  error,
  hint,
  children,
}: {
  label?: string;
  htmlFor?: string;
  error?: string | null;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div className="block">
      {label ? (
        <label htmlFor={htmlFor} className={FIELD_LABEL_CLASS}>
          {label}
        </label>
      ) : null}
      {children}
      {error ? (
        <p role="alert" className="mt-1.5 text-label text-state-error">
          {error}
        </p>
      ) : hint ? (
        <p className="mt-1.5 text-label text-text-muted">{hint}</p>
      ) : null}
    </div>
  );
}

export function Input({
  label,
  error,
  className,
  id,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & { label?: string; error?: string | null }) {
  const generatedId = useId();
  const inputId = id ?? generatedId;
  return (
    <Field label={label} htmlFor={inputId} error={error}>
      <input
        id={inputId}
        className={cn(CONTROL_CLASS, "h-11 lg:h-form-row", error && "border-state-error focus:border-state-error focus:ring-state-error/20", className)}
        {...props}
      />
    </Field>
  );
}

export function NumberInput({
  label,
  value,
  min,
  max,
  optional = false,
  disabled,
  onChange,
}: {
  label: string;
  value: number | string | null;
  min?: number;
  max?: number;
  optional?: boolean;
  disabled?: boolean;
  onChange: (value: number | string | null) => void;
}) {
  const [draft, setDraft] = useState(value === null ? "" : String(value));
  const invalidDraft = draft === ""
    ? value !== null && !optional
    : !isValidNumberDraft(draft, min, max, optional);

  useEffect(() => {
    setDraft(value === null ? "" : String(value));
  }, [value]);

  return (
    <Field label={label}>
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
        className={cn(
          CONTROL_CLASS,
          "h-11 lg:h-form-row",
          invalidDraft && "border-state-error focus:border-state-error focus:ring-state-error/20",
        )}
      />
    </Field>
  );
}

function isValidNumberDraft(draft: string, min?: number, max?: number, optional = false): boolean {
  if (!draft) return optional;
  const parsed = Number(draft);
  return Number.isFinite(parsed)
    && (min === undefined || parsed >= min)
    && (max === undefined || parsed <= max);
}

const TEXTAREA_LINE_HEIGHT_PX = 19;
const TEXTAREA_VERTICAL_PADDING_PX = 16;

export function TextArea({
  label,
  value,
  onChange,
  minRows = 2,
  maxRows,
  error,
  className,
  ...props
}: Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "onChange" | "value"> & {
  label?: string;
  value: string;
  onChange: (value: string) => void;
  minRows?: number;
  maxRows?: number;
  error?: string | null;
}) {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const generatedId = useId();
  const inputId = props.id ?? generatedId;
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
    <Field label={label} htmlFor={inputId} error={error}>
      <textarea
        {...props}
        id={inputId}
        ref={textareaRef}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        rows={minRows}
        style={{ minHeight, ...props.style }}
        className={cn(CONTROL_CLASS, "resize-none py-2 leading-relaxed", className)}
      />
    </Field>
  );
}

export { FIELD_LABEL_CLASS, CONTROL_CLASS };
