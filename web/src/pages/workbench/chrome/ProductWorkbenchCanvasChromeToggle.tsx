import { Maximize2, Minimize2 } from "lucide-react";

import { IconButton } from "../../../components/ui/icon-button";

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
    <IconButton
      data-canvas-control
      label={label}
      size="toolbar"
      className="pointer-events-auto"
      onClick={onToggle}
    >
      {collapsed ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
    </IconButton>
  );
  if (embedded) return button;
  return (
    <div data-canvas-control className="pointer-events-none absolute right-3 top-3 z-30 lg:right-4 lg:top-4">
      {button}
    </div>
  );
}
