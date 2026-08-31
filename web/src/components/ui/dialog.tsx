import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ComponentPropsWithoutRef, ReactNode } from "react";

import { cn } from "./cn";
import { Button } from "./button";

function DialogLayer({ children }: { children: ReactNode }) {
  if (typeof document === "undefined") {
    return <>{children}</>;
  }
  return <DialogPrimitive.Portal>{children}</DialogPrimitive.Portal>;
}

export function Dialog({
  open,
  onOpenChange,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      {children}
    </DialogPrimitive.Root>
  );
}

export function DialogContent({
  title,
  description,
  children,
  footer,
  className,
  labelledBy,
  describedBy,
  size = "md",
  hideClose = false,
  closeLabel,
  onClose,
  bodyClassName,
  placement = "center",
  ...contentProps
}: {
  title?: string;
  description?: string;
  children?: ReactNode;
  footer?: ReactNode;
  className?: string;
  labelledBy?: string;
  describedBy?: string;
  size?: "sm" | "md" | "lg" | "xl";
  hideClose?: boolean;
  closeLabel?: string;
  onClose?: () => void;
  bodyClassName?: string;
  placement?: "center" | "right";
} & Omit<ComponentPropsWithoutRef<typeof DialogPrimitive.Content>, "title">) {
  return (
    <DialogLayer>
      <DialogPrimitive.Overlay className="fixed inset-0 z-dialog bg-overlay backdrop-blur-[2px]" />
      <DialogPrimitive.Content
        aria-labelledby={labelledBy}
        aria-describedby={describedBy}
        className={cn(
          "fixed z-dialog overflow-hidden border border-border-l1 bg-surface-raised text-text-primary shadow-elev-3 outline-none",
          placement === "center" &&
            "left-1/2 top-1/2 w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 rounded-panel",
          placement === "right" &&
            "inset-y-0 right-0 h-dvh w-full rounded-none border-y-0 border-r-0 sm:max-w-[448px]",
          placement === "center" && size === "sm" && "max-w-sm",
          placement === "center" && size === "md" && "max-w-md",
          placement === "center" && size === "lg" && "max-w-lg",
          placement === "center" && size === "xl" && "max-w-3xl",
          className,
        )}
        {...contentProps}
      >
        {title ? (
          <div className="flex items-start justify-between gap-3 border-b border-border-l1 px-5 py-4">
            <div className="min-w-0">
              <DialogPrimitive.Title className="text-base font-semibold text-text-primary">
                {title}
              </DialogPrimitive.Title>
              {description ? (
                <DialogPrimitive.Description className="mt-1 text-sm leading-6 text-text-secondary">
                  {description}
                </DialogPrimitive.Description>
              ) : (
                <DialogPrimitive.Description className="sr-only">
                  {title}
                </DialogPrimitive.Description>
              )}
            </div>
            {hideClose ? null : (
              <DialogPrimitive.Close asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-11 w-11 shrink-0 px-0 lg:h-8 lg:w-8"
                  aria-label={closeLabel ?? "Close"}
                  onClick={onClose}
                >
                  <X size={16} aria-hidden="true" />
                </Button>
              </DialogPrimitive.Close>
            )}
          </div>
        ) : null}
        {children ? <div className={cn("px-5 py-4", bodyClassName)}>{children}</div> : null}
        {footer ? (
          <div className="flex justify-end gap-2 border-t border-border-l1 bg-surface-subtle px-5 py-3">
            {footer}
          </div>
        ) : null}
      </DialogPrimitive.Content>
    </DialogLayer>
  );
}

export const DialogClose = DialogPrimitive.Close;
export const DialogTitle = DialogPrimitive.Title;
export const DialogDescription = DialogPrimitive.Description;
