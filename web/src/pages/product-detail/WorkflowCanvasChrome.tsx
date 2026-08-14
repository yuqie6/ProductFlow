import {
  Background,
  BackgroundVariant,
  ControlButton,
  Controls,
  Handle,
  NodeToolbar,
  Position,
  useReactFlow,
  useViewport,
} from "@xyflow/react";
import type { FitViewOptions, Viewport } from "@xyflow/react";
import { Focus, Grid, Loader2, Sparkles } from "lucide-react";
import type { ReactNode } from "react";
import { useCallback } from "react";

import type { CanvasInteractionMode } from "./types";

export type WorkflowCanvasPortVisualState = "idle" | "origin" | "valid-target" | "invalid-target";

const SOURCE_PORT_CLASS_NAME =
  "nodrag nopan !absolute !z-20 !h-5 !w-5 !rounded-full !border-2 !border-indigo-500 !bg-white !opacity-100 !shadow-sm transition-shadow hover:!bg-indigo-50 hover:!ring-4 hover:!ring-indigo-100 dark:!border-violet-300 dark:!bg-[#111b2d] dark:!shadow-black/30 dark:hover:!bg-violet-500/20 dark:hover:!ring-violet-400/25";
const TARGET_PORT_CLASS_NAME =
  "nodrag nopan !absolute !z-20 !h-[18px] !w-[18px] !rounded-full !border !border-slate-300 !bg-white !opacity-100 !shadow-sm transition-shadow hover:!border-indigo-400 hover:!ring-4 hover:!ring-indigo-100 dark:!border-slate-400/90 dark:!bg-[#111b2d] dark:!shadow-black/30 dark:hover:!border-violet-300 dark:hover:!ring-violet-400/20";
const PORT_STATE_CLASS_NAMES: Record<WorkflowCanvasPortVisualState, string> = {
  idle: "",
  origin:
    "!border-indigo-600 !bg-indigo-100 !ring-4 !ring-indigo-100 dark:!border-violet-200 dark:!bg-violet-500/30 dark:!ring-violet-400/25",
  "valid-target":
    "!border-emerald-500 !bg-emerald-50 !ring-4 !ring-emerald-100 dark:!border-emerald-300 dark:!bg-emerald-500/20 dark:!ring-emerald-400/25",
  "invalid-target":
    "!border-dashed !border-red-500 !bg-red-50 !opacity-75 !ring-4 !ring-red-100 dark:!border-red-300 dark:!bg-red-500/20 dark:!ring-red-400/25",
};

export function WorkflowCanvasNodePort({
  id,
  type,
  top,
  label,
  connectable,
  visualState = "idle",
  visualScale = 1,
}: {
  id?: string | null;
  type: "source" | "target";
  top: number | string;
  label: string;
  connectable: boolean;
  visualState?: WorkflowCanvasPortVisualState;
  visualScale?: number;
}) {
  return (
    <Handle
      id={id ?? undefined}
      type={type}
      position={type === "source" ? Position.Right : Position.Left}
      isConnectable={connectable}
      style={{
        top: typeof top === "number" ? `${top}%` : top,
        transform: visualScale === 1
          ? undefined
          : `translate(${type === "source" ? "50%" : "-50%"}, -50%) scale(${visualScale})`,
      }}
      className={`${type === "source" ? SOURCE_PORT_CLASS_NAME : TARGET_PORT_CLASS_NAME} ${
        PORT_STATE_CLASS_NAMES[visualState]
      } ${type === "source" ? "!right-[-10px]" : "!left-[-9px]"}`}
      title={label}
      aria-label={label}
    />
  );
}

export function WorkflowCanvasNodeToolbar({
  visible,
  children,
}: {
  visible: boolean;
  children: ReactNode;
}) {
  return (
    <NodeToolbar
      isVisible={visible}
      position={Position.Top}
      align="center"
      offset={10}
      className="nodrag nopan nowheel z-50"
    >
      <div
        data-node-action
        className="nodrag nopan nowheel flex items-center gap-1 rounded-xl border border-slate-200 bg-white/98 p-1 shadow-lg shadow-slate-950/15 backdrop-blur dark:border-slate-700/80 dark:bg-[#111a2b]/98 dark:shadow-black/40"
        onPointerDown={(event) => event.stopPropagation()}
        onClick={(event) => event.stopPropagation()}
      >
        {children}
      </div>
    </NodeToolbar>
  );
}

export function WorkflowCanvasNodeToolbarButton({
  label,
  disabled = false,
  destructive = false,
  onClick,
  children,
}: {
  label: string;
  disabled?: boolean;
  destructive?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      data-node-action
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (!disabled) {
          onClick();
        }
      }}
      disabled={disabled}
      className={`nodrag nopan nowheel inline-flex h-11 w-11 items-center justify-center rounded-lg border text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-45 lg:h-9 lg:w-9 ${
        destructive
          ? "border-red-200 bg-red-50 text-red-600 hover:border-red-300 hover:bg-red-100 hover:text-red-700 dark:border-red-400/45 dark:bg-red-500/10 dark:text-red-200 dark:hover:border-red-400/70 dark:hover:bg-red-500/18"
          : "border-transparent bg-white text-slate-700 hover:border-indigo-200 hover:bg-indigo-50 hover:text-indigo-700 dark:bg-[#111a2b] dark:text-slate-100 dark:hover:border-violet-400/55 dark:hover:bg-violet-500/14 dark:hover:text-violet-100"
      }`}
      aria-label={label}
      title={label}
    >
      {children}
      <span className="sr-only">{label}</span>
    </button>
  );
}

export interface WorkflowCanvasControlLabels {
  resetZoom: string;
  fitSelection: string;
  controls: string;
  snapToGrid: string;
  autoLayout: string;
}

export function WorkflowCanvasControls({
  labels,
  selectedNodeIds,
  onViewportCommit,
  snapToGrid,
  onToggleSnapToGrid,
  onAutoLayout,
  autoLayoutBusy = false,
  fitViewOptions,
  normalizeZoom = (zoom) => zoom,
}: {
  labels: WorkflowCanvasControlLabels;
  selectedNodeIds: string[];
  onViewportCommit: (viewport: Viewport) => void;
  snapToGrid: boolean;
  onToggleSnapToGrid: () => void;
  onAutoLayout: () => void;
  autoLayoutBusy?: boolean;
  fitViewOptions: FitViewOptions;
  normalizeZoom?: (zoom: number) => number;
}) {
  const { zoom } = useViewport();
  const reactFlow = useReactFlow();
  const duration = fitViewOptions.duration ?? 180;
  const commitCurrentViewport = useCallback(() => {
    onViewportCommit(reactFlow.getViewport());
  }, [onViewportCommit, reactFlow]);
  const commitViewportAfterControlAction = useCallback(() => {
    window.setTimeout(commitCurrentViewport, duration + 40);
  }, [commitCurrentViewport, duration]);
  const zoomTo = useCallback(
    (nextZoom: number) => {
      void reactFlow.zoomTo(normalizeZoom(nextZoom)).then(commitCurrentViewport);
    },
    [commitCurrentViewport, normalizeZoom, reactFlow],
  );
  const fitSelectedNodes = useCallback(() => {
    const selectedNodes = selectedNodeIds
      .filter((nodeId) => reactFlow.getNode(nodeId))
      .map((nodeId) => ({ id: nodeId }));
    if (!selectedNodes.length) {
      return;
    }
    void reactFlow
      .fitView({ ...fitViewOptions, nodes: selectedNodes })
      .then(commitCurrentViewport);
  }, [commitCurrentViewport, fitViewOptions, reactFlow, selectedNodeIds]);

  return (
    <Controls
      position="top-left"
      orientation="horizontal"
      showInteractive={false}
      fitViewOptions={fitViewOptions}
      onZoomIn={commitViewportAfterControlAction}
      onZoomOut={commitViewportAfterControlAction}
      onFitView={commitViewportAfterControlAction}
      aria-label={labels.controls}
      className="workflow-canvas-controls nopan nodrag nowheel z-30 !m-0 translate-x-3 translate-y-3 lg:translate-x-4 lg:translate-y-4"
    >
      <ControlButton onClick={() => zoomTo(1)} aria-label={labels.resetZoom} title={labels.resetZoom}>
        <span className="text-[11px] tabular-nums">{Math.round(normalizeZoom(zoom) * 100)}%</span>
      </ControlButton>
      <ControlButton
        onClick={fitSelectedNodes}
        disabled={!selectedNodeIds.length}
        aria-label={labels.fitSelection}
        title={labels.fitSelection}
      >
        <Focus aria-hidden="true" size={13} />
      </ControlButton>
      <ControlButton
        onClick={onToggleSnapToGrid}
        aria-label={labels.snapToGrid}
        title={labels.snapToGrid}
        className={snapToGrid ? "!bg-indigo-50 dark:!bg-violet-500/20" : ""}
      >
        <Grid aria-hidden="true" size={13} className={snapToGrid ? "text-indigo-600 dark:text-violet-400" : ""} />
      </ControlButton>
      <ControlButton
        onClick={onAutoLayout}
        disabled={autoLayoutBusy}
        aria-label={labels.autoLayout}
        title={labels.autoLayout}
      >
        {autoLayoutBusy ? (
          <Loader2 aria-hidden="true" size={13} className="animate-spin" />
        ) : (
          <Sparkles aria-hidden="true" size={13} />
        )}
      </ControlButton>
    </Controls>
  );
}

export function WorkflowCanvasGrid({ gap = 36 }: { gap?: number }) {
  return (
    <>
      <Background
        id={`workflow-grid-light-${gap}`}
        className="block dark:hidden"
        variant={BackgroundVariant.Dots}
        gap={gap}
        size={1.5}
        color="#94a3b8"
      />
      <Background
        id={`workflow-grid-dark-${gap}`}
        className="hidden dark:block"
        variant={BackgroundVariant.Dots}
        gap={gap}
        size={1.5}
        color="rgba(148, 163, 184, 0.35)"
      />
    </>
  );
}

export interface WorkflowCanvasMobileModeItem {
  key: CanvasInteractionMode;
  label: string;
  description: string;
  icon: ReactNode;
}

export function WorkflowCanvasMobileModeTabs({
  value,
  items,
  onChange,
}: {
  value: CanvasInteractionMode;
  items: WorkflowCanvasMobileModeItem[];
  onChange: (mode: CanvasInteractionMode) => void;
}) {
  return (
    <div className="grid grid-cols-3 gap-1 rounded-xl bg-slate-100 p-1 dark:bg-slate-900/85">
      {items.map((item) => (
        <button
          key={item.key}
          type="button"
          onClick={() => onChange(item.key)}
          className={`inline-flex min-h-11 min-w-0 items-center justify-center gap-2 rounded-lg px-2 text-xs font-semibold transition-colors active:scale-[0.98] focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-violet-400 ${
            value === item.key
              ? "bg-white text-indigo-700 shadow-sm dark:bg-violet-500/18 dark:text-violet-100 dark:ring-1 dark:ring-violet-300/35"
              : "text-slate-500 hover:bg-white/70 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100"
          }`}
          aria-pressed={value === item.key}
          aria-label={item.description}
          title={item.description}
        >
          {item.icon}
          <span className="truncate">{item.label}</span>
        </button>
      ))}
    </div>
  );
}
