import * as TabsPrimitive from "@radix-ui/react-tabs";
import type { ComponentPropsWithoutRef, ReactNode } from "react";

import { cn } from "./cn";

export const tabListClassName =
  "inline-flex min-h-9 items-center rounded-control border border-border-l1 bg-surface-subtle p-0.5";
export const tabTriggerClassName =
  "inline-flex min-h-8 min-w-0 flex-1 items-center justify-center rounded-md px-3 text-xs font-semibold text-text-secondary outline-none transition-colors duration-fast hover:text-text-primary focus-visible:ring-2 focus-visible:ring-focus-ring disabled:cursor-not-allowed disabled:opacity-45 data-[state=active]:bg-surface-raised data-[state=active]:text-text-primary aria-[pressed=true]:bg-surface-raised aria-[pressed=true]:text-text-primary";

export function Tabs({
  value,
  onValueChange,
  children,
  className,
}: {
  value: string;
  onValueChange: (value: string) => void;
  children: ReactNode;
  className?: string;
}) {
  return (
    <TabsPrimitive.Root value={value} onValueChange={onValueChange} className={className}>
      {children}
    </TabsPrimitive.Root>
  );
}

export function TabList({
  children,
  className,
  "aria-label": ariaLabel,
  ...rest
}: {
  children: ReactNode;
  className?: string;
  "aria-label"?: string;
} & ComponentPropsWithoutRef<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      aria-label={ariaLabel}
      className={cn(
        tabListClassName,
        className,
      )}
      {...rest}
    >
      {children}
    </TabsPrimitive.List>
  );
}

export function TabTrigger({
  value,
  children,
  disabled,
  className,
  ...rest
}: {
  value: string;
  children: ReactNode;
  disabled?: boolean;
  className?: string;
} & Omit<ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>, "value">) {
  return (
    <TabsPrimitive.Trigger
      value={value}
      disabled={disabled}
      className={cn(
        tabTriggerClassName,
        className,
      )}
      {...rest}
    >
      {children}
    </TabsPrimitive.Trigger>
  );
}

export function TabPanel({
  value,
  children,
  className,
}: {
  value: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <TabsPrimitive.Content value={value} className={cn("min-h-0 outline-none", className)}>
      {children}
    </TabsPrimitive.Content>
  );
}
