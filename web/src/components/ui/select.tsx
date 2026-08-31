import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";

import { cn } from "./cn";

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

export interface SelectGroup {
  label: string;
  options: SelectOption[];
}

export function Select({
  id,
  value,
  onChange,
  options = [],
  groups = [],
  ariaLabel,
  disabled = false,
  size = "md",
  className,
}: {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  options?: readonly SelectOption[];
  groups?: readonly SelectGroup[];
  ariaLabel?: string;
  disabled?: boolean;
  size?: "sm" | "md";
  className?: string;
}) {
  const triggerSize = size === "sm"
    ? "h-11 pl-2.5 pr-9 text-xs lg:h-9"
    : "h-11 pl-3 pr-10 text-sm lg:h-10";
  const selectedLabel = [...options, ...groups.flatMap((group) => group.options)]
    .find((option) => option.value === value)?.label ?? "";
  return (
    <SelectPrimitive.Root value={value} onValueChange={onChange} disabled={disabled}>
      <SelectPrimitive.Trigger
        id={id}
        aria-label={ariaLabel}
        className={cn(
          "relative flex w-full items-center rounded-control border border-border-l1 bg-surface-subtle text-left font-medium text-text-primary outline-none transition-[border-color,background-color] duration-fast hover:border-border-l3 focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-focus-ring disabled:cursor-not-allowed disabled:opacity-45",
          triggerSize,
          className,
        )}
      >
        <span className="block min-w-0 flex-1 truncate">{selectedLabel}</span>
        <SelectPrimitive.Icon asChild>
          <ChevronDown
            size={size === "sm" ? 14 : 16}
            className="pointer-events-none absolute right-2.5 text-text-muted"
          />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          sideOffset={4}
          className="z-dropdown max-h-64 w-[var(--radix-select-trigger-width)] overflow-hidden rounded-panel border border-border-l1 bg-surface-raised p-1 shadow-elev-2"
        >
          <SelectPrimitive.Viewport>
            {groups.length
              ? groups.map((group) => (
                <SelectPrimitive.Group key={group.label}>
                  <SelectPrimitive.Label className="px-2.5 py-1.5 text-[10px] font-bold uppercase tracking-wider text-text-muted">
                    {group.label}
                  </SelectPrimitive.Label>
                  {group.options.map((option) => (
                    <SelectItem key={`${group.label}-${option.value}`} option={option} size={size} />
                  ))}
                </SelectPrimitive.Group>
              ))
              : options.map((option) => (
                <SelectItem key={option.value} option={option} size={size} />
              ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
}

function SelectItem({
  option,
  size,
}: {
  option: SelectOption;
  size: "sm" | "md";
}) {
  return (
    <SelectPrimitive.Item
      value={option.value}
      disabled={option.disabled}
      className={cn(
        "flex cursor-pointer items-center gap-2 rounded-control text-left font-medium text-text-secondary outline-none data-[disabled]:cursor-not-allowed data-[disabled]:opacity-45 data-[highlighted]:bg-surface-subtle data-[highlighted]:text-text-primary data-[state=checked]:bg-accent-soft data-[state=checked]:text-accent",
        size === "sm"
          ? "min-h-11 px-2 py-1.5 text-xs lg:min-h-8"
          : "min-h-11 px-2.5 py-2 text-sm lg:min-h-9",
      )}
    >
      <SelectPrimitive.ItemText>
        <span className="min-w-0 flex-1 truncate">{option.label}</span>
      </SelectPrimitive.ItemText>
      <SelectPrimitive.ItemIndicator>
        <Check size={14} className="shrink-0" />
      </SelectPrimitive.ItemIndicator>
    </SelectPrimitive.Item>
  );
}
