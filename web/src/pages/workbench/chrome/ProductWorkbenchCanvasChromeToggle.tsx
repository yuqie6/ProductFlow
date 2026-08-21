import { Maximize2, Minimize2 } from "lucide-react";

interface ProductWorkbenchCanvasChromeToggleProps {
  collapsed: boolean;
  maximizeLabel: string;
  restoreLabel: string;
  onToggle: () => void;
  embedded?: boolean;
}

export function ProductWorkbenchCanvasChromeToggle({
  collapsed,
  maximizeLabel,
  restoreLabel,
  onToggle,
  embedded = false,
}: ProductWorkbenchCanvasChromeToggleProps) {
  const label = collapsed ? restoreLabel : maximizeLabel;
  const button = (
    <button
      type="button"
      data-canvas-control
      onClick={onToggle}
      className="pointer-events-auto inline-flex h-11 w-11 items-center justify-center rounded-lg text-text-secondary transition-colors duration-fast hover:bg-surface-subtle hover:text-text-primary active:scale-[0.98] motion-reduce:active:scale-100 lg:h-9 lg:w-9"
      aria-label={label}
      title={label}
    >
      {collapsed ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
    </button>
  );
  if (embedded) return button;
  return (
    <div data-canvas-control className="pointer-events-none absolute right-3 top-3 z-30 lg:right-4 lg:top-4">
      {button}
    </div>
  );
}
