import { Maximize2, Minimize2 } from "lucide-react";

interface ProductWorkbenchCanvasChromeToggleProps {
  collapsed: boolean;
  maximizeLabel: string;
  restoreLabel: string;
  onToggle: () => void;
}

export function ProductWorkbenchCanvasChromeToggle({
  collapsed,
  maximizeLabel,
  restoreLabel,
  onToggle,
}: ProductWorkbenchCanvasChromeToggleProps) {
  const label = collapsed ? restoreLabel : maximizeLabel;
  return (
    <div data-canvas-control className="pointer-events-none absolute right-3 top-3 z-30 lg:right-4 lg:top-4">
      <button
        type="button"
        onClick={onToggle}
        className="pointer-events-auto inline-flex h-11 w-11 items-center justify-center rounded-xl border border-zinc-200 bg-white/90 text-zinc-600 shadow-sm backdrop-blur transition-colors active:scale-[0.98] hover:bg-white hover:text-zinc-900 dark:border-slate-700/80 dark:bg-[#151f33]/92 dark:text-slate-300 dark:shadow-black/20 dark:hover:bg-[#1a2740] dark:hover:text-white lg:h-9 lg:w-9 lg:rounded-lg"
        aria-label={label}
        title={label}
      >
        {collapsed ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
      </button>
    </div>
  );
}
