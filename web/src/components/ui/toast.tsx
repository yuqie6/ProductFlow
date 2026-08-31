import { Toaster as SonnerToaster, toast } from "sonner";

export function AppToaster() {
  return (
    <SonnerToaster
      position="bottom-center"
      theme="light"
      offset={{ bottom: "1rem" }}
      mobileOffset={{ bottom: "5rem" }}
      toastOptions={{
        classNames: {
          toast:
            "rounded-panel border border-border-l1 bg-surface-raised text-text-primary shadow-elev-2",
          title: "text-xs font-medium text-text-primary",
          description: "text-[11px] text-text-secondary",
        },
      }}
    />
  );
}

export { toast };
