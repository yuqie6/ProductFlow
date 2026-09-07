import { cva, type VariantProps } from "class-variance-authority";
import { Loader2 } from "lucide-react";
import { type ButtonHTMLAttributes, forwardRef } from "react";

import { cn } from "./cn";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-1.5 font-semibold outline-none transition-[background-color,border-color,color,opacity,transform] duration-fast ease-in-out focus-visible:ring-2 focus-visible:ring-focus-ring disabled:cursor-not-allowed disabled:opacity-45 motion-reduce:transition-none",
  {
    variants: {
      variant: {
        primary: "bg-accent text-accent-fg hover:bg-accent-strong",
        secondary:
          "border border-border-l1 bg-surface-raised text-text-primary hover:bg-surface-subtle",
        ghost: "text-text-secondary hover:bg-surface-subtle hover:text-text-primary",
        danger: "bg-state-error text-state-error-fg hover:brightness-110",
        dangerSoft:
          "border border-state-error/30 bg-state-error-soft text-state-error hover:border-state-error/50",
      },
      size: {
        sm: "h-11 rounded-control px-2.5 text-[11px] lg:h-8",
        md: "h-11 rounded-control px-3 text-xs lg:h-9",
        lg: "h-11 rounded-panel px-3.5 text-xs lg:h-10",
        toolbar: "h-11 rounded-control px-3 text-xs lg:h-9",
      },
    },
    defaultVariants: {
      variant: "secondary",
      size: "md",
    },
  },
);

export interface ButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  busy?: boolean;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant, size, busy = false, disabled, children, type = "button", ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      disabled={disabled || busy}
      className={cn(buttonVariants({ variant, size }), className)}
      {...props}
    >
      {busy ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : null}
      {children}
    </button>
  );
});

export { buttonVariants };
