import { cva, type VariantProps } from "class-variance-authority";
import { Loader2 } from "lucide-react";
import { type ButtonHTMLAttributes, forwardRef, type ReactNode } from "react";

import { cn } from "./cn";
import { Tooltip } from "./tooltip";

const iconButtonVariants = cva(
  "inline-flex shrink-0 items-center justify-center outline-none transition-[background-color,border-color,color,opacity,transform] duration-fast ease-in-out focus-visible:ring-2 focus-visible:ring-focus-ring disabled:cursor-not-allowed disabled:opacity-45 motion-reduce:transition-none",
  {
    variants: {
      variant: {
        ghost: "border border-transparent text-text-secondary hover:bg-surface-subtle hover:text-text-primary",
        secondary: "border border-border-l1 bg-surface-raised text-text-secondary hover:bg-surface-subtle hover:text-text-primary",
        primary: "border border-transparent bg-accent text-accent-fg hover:bg-accent-strong",
        danger:
          "border border-state-error/30 bg-state-error-soft text-state-error hover:border-state-error/50",
      },
      size: {
        sm: "h-11 w-11 rounded-control lg:h-7 lg:w-7",
        md: "h-11 w-11 rounded-control lg:h-9 lg:w-9",
        toolbar: "h-11 w-11 rounded-control lg:h-9 lg:w-9",
      },
    },
    defaultVariants: {
      variant: "ghost",
      size: "toolbar",
    },
  },
);

export interface IconButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof iconButtonVariants> {
  label: string;
  busy?: boolean;
  tooltip?: boolean;
  tooltipContent?: ReactNode;
}

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  {
    className,
    variant,
    size,
    label,
    busy = false,
    disabled,
    tooltip = true,
    tooltipContent,
    children,
    type = "button",
    ...props
  },
  ref,
) {
  const isDisabled = disabled || busy;
  const button = (
    <button
      ref={ref}
      type={type}
      disabled={isDisabled}
      aria-label={label}
      title={!tooltip || isDisabled ? label : undefined}
      className={cn(iconButtonVariants({ variant, size }), className)}
      {...props}
    >
      {busy ? <Loader2 size={16} className="animate-spin" aria-hidden="true" /> : children}
      <span className="sr-only">{label}</span>
    </button>
  );
  if (!tooltip || isDisabled) {
    return button;
  }
  return (
    <Tooltip content={tooltipContent ?? label}>
      {button}
    </Tooltip>
  );
});

export { iconButtonVariants };
