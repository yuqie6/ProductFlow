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
import { Expand, Focus, Grid, Loader2, Sparkles } from "lucide-react";
import type { ReactNode } from "react";
import { useCallback } from "react";

import type { CanvasInteractionMode } from "./workflowCanvasInteraction";

export type WorkflowCanvasPortVisualState = "idle" | "origin" | "valid-target" | "invalid-target" | "missing";

const SOURCE_PORT_CLASS_NAME =
  "nodrag nopan !absolute !z-20 !h-5 !w-5 !rounded-full !border-2 !border-slate-700 !bg-white !opacity-100 !shadow-[0_0_0_2px_#fff,0_1px_2px_rgba(15,23,42,0.18)] hover:!bg-slate-50 dark:!border-slate-200 dark:!bg-[#111b2d] dark:!shadow-[0_0_0_2px_#0d1424,0_1px_2px_rgba(0,0,0,0.45)] dark:hover:!bg-slate-800";
const TARGET_PORT_CLASS_NAME =
  "nodrag nopan !absolute !z-20 !h-5 !w-5 !rounded-full !border-2 !border-slate-500 !bg-slate-50 !opacity-100 !shadow-[0_0_0_2px_#fff,0_1px_2px_rgba(15,23,42,0.18)] hover:!border-slate-700 hover:!bg-white dark:!border-slate-300 dark:!bg-[#111b2d] dark:!shadow-[0_0_0_2px_#0d1424,0_1px_2px_rgba(0,0,0,0.45)] dark:hover:!border-white";
const PORT_STATE_CLASS_NAMES: Record<WorkflowCanvasPortVisualState, string> = {
  idle: "",
  origin:
    "!border-slate-900 !bg-slate-800 !shadow-[0_0_0_3px_#e2e8f0] dark:!border-white dark:!bg-slate-200 dark:!shadow-[0_0_0_3px_#1e293b]",
  "valid-target":
    "!border-emerald-700 !bg-emerald-50 !shadow-[0_0_0_3px_#d1fae5] dark:!border-emerald-300 dark:!bg-emerald-950/70 dark:!shadow-[0_0_0_3px_#14532d]",
  "invalid-target":
    "!border-dashed !border-red-600 !bg-red-50 !shadow-[0_0_0_3px_#fee2e2] dark:!border-red-400 dark:!bg-red-950/50 dark:!shadow-[0_0_0_3px_#7f1d1d]",
  missing:
    "!border-red-600 !bg-red-100 !shadow-[0_0_0_3px_#fecaca] dark:!border-red-400 dark:!bg-red-950/80 dark:!shadow-[0_0_0_3px_#7f1d1d]",
};

export function WorkflowCanvasNodePort({
  id,
  type,
  top,
  label,
  connectable,
  presentationHidden = false,
  visualState = "idle",
  visualScale = 1,
  colorClass = "",
}: {
  id?: string | null;
  type: "source" | "target";
  top: number | string;
  label: string;
  connectable: boolean;
  presentationHidden?: boolean;
  visualState?: WorkflowCanvasPortVisualState;
  visualScale?: number;
  colorClass?: string;
}) {
  const interactionEnabled = connectable && !presentationHidden;
  return (
    <Handle
      id={id ?? undefined}
      type={type}
      position={type === "source" ? Position.Right : Position.Left}
      isConnectable={interactionEnabled}
      isConnectableStart={interactionEnabled}
      isConnectableEnd={interactionEnabled}
      style={{
        top: typeof top === "number" ? `${top}%` : top,
        transform: visualScale === 1
          ? undefined
          : `translate(${type === "source" ? "50%" : "-50%"}, -50%) scale(${visualScale})`,
        visibility: presentationHidden ? "hidden" : undefined,
        pointerEvents: presentationHidden ? "none" : undefined,
      }}
      className={`${type === "source" ? SOURCE_PORT_CLASS_NAME : TARGET_PORT_CLASS_NAME} ${
        colorClass
      } ${PORT_STATE_CLASS_NAMES[visualState]} ${type === "source" ? "!right-[-10px]" : "!left-[-9px]"}`}
      title={label}
      aria-label={label}
      aria-hidden={presentationHidden || undefined}
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
          : "border-transparent bg-white text-slate-700 hover:border-slate-200 hover:bg-slate-50 hover:text-slate-900 dark:bg-[#111a2b] dark:text-slate-100 dark:hover:border-slate-600 dark:hover:bg-slate-800"
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
  fitView: string;
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
      showFitView={false}
      fitViewOptions={fitViewOptions}
      onZoomIn={commitViewportAfterControlAction}
      onZoomOut={commitViewportAfterControlAction}
      aria-label={labels.controls}
      className="workflow-canvas-controls nopan nodrag nowheel z-30 !m-0 translate-x-3 translate-y-3 lg:translate-x-4 lg:translate-y-4"
    >
      <ControlButton onClick={() => zoomTo(1)} aria-label={labels.resetZoom} title={labels.resetZoom}>
        <span className="text-[11px] tabular-nums">{Math.round(normalizeZoom(zoom) * 100)}%</span>
      </ControlButton>
      <ControlButton
        onClick={() => {
          void reactFlow.fitView(fitViewOptions).then(commitCurrentViewport);
        }}
        aria-label={labels.fitView}
        title={labels.fitView}
      >
        <Expand aria-hidden="true" size={13} />
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
        className={snapToGrid ? "!bg-slate-100 dark:!bg-slate-800" : ""}
      >
        <Grid aria-hidden="true" size={13} className={snapToGrid ? "text-slate-800 dark:text-slate-100" : ""} />
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
          className={`inline-flex min-h-11 min-w-0 items-center justify-center gap-2 rounded-lg px-2 text-xs font-semibold transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-slate-400 ${
            value === item.key
              ? "bg-white text-slate-900 shadow-sm dark:bg-slate-800 dark:text-slate-100"
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
