import {
  Background,
  BackgroundVariant,
  Handle,
  NodeToolbar,
  Position,
  useReactFlow,
  useViewport,
} from "@xyflow/react";
import type { FitViewOptions, Viewport } from "@xyflow/react";
import { Expand, Focus, Grid, Minus, Plus, Sparkles } from "lucide-react";
import type { ReactNode } from "react";
import { useCallback } from "react";

import { cn } from "../../../components/ui/cn";
import { IconButton } from "../../../components/ui/icon-button";
import { tabListClassName, tabTriggerClassName } from "../../../components/ui/tabs";
import { Tooltip } from "../../../components/ui/tooltip";
import type { CanvasInteractionMode } from "./workflowCanvasInteraction";

export type WorkflowCanvasPortVisualState = "idle" | "origin" | "valid-target" | "invalid-target" | "missing";

const SOURCE_PORT_CLASS_NAME =
  "nodrag nopan !absolute !z-20 !h-5 !w-5 !rounded-full !border-2 !border-port-source !bg-surface-raised !opacity-100 !shadow-[0_0_0_2px_var(--color-port-ring)] hover:!bg-surface-subtle";
const TARGET_PORT_CLASS_NAME =
  "nodrag nopan !absolute !z-20 !h-5 !w-5 !rounded-full !border-2 !border-port-target !bg-surface-subtle !opacity-100 !shadow-[0_0_0_2px_var(--color-port-ring)] hover:!border-port-source hover:!bg-surface-raised";
const PORT_STATE_CLASS_NAMES: Record<WorkflowCanvasPortVisualState, string> = {
  idle: "",
  origin:
    "!border-text-primary !bg-text-primary !shadow-[0_0_0_3px_var(--color-border-l1)]",
  "valid-target":
    "!border-port-valid !bg-port-valid-soft !shadow-[0_0_0_3px_var(--color-port-valid-soft)]",
  "invalid-target":
    "!border-dashed !border-port-invalid !bg-port-invalid-soft !shadow-[0_0_0_3px_var(--color-port-invalid-soft)]",
  missing:
    "!border-port-invalid !bg-port-invalid-soft !shadow-[0_0_0_3px_var(--color-port-invalid-soft)]",
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
      className={`${type === "source" ? SOURCE_PORT_CLASS_NAME : TARGET_PORT_CLASS_NAME} ${colorClass
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
      className="nodrag nopan nowheel z-toolbar"
    >
      <div
        data-node-action
        className="nodrag nopan nowheel flex items-center gap-1 rounded-panel border border-border-l1 bg-surface-raised/98 p-1 shadow-elev-2 backdrop-blur"
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
  onMouseEnter,
  onMouseLeave,
  onFocus,
  onBlur,
  children,
}: {
  label: string;
  disabled?: boolean;
  destructive?: boolean;
  onClick: () => void;
  onMouseEnter?: () => void;
  onMouseLeave?: () => void;
  onFocus?: () => void;
  onBlur?: () => void;
  children: ReactNode;
}) {
  return (
    <IconButton
      label={label}
      disabled={disabled}
      variant={destructive ? "danger" : "ghost"}
      size="toolbar"
      data-node-action
      className="nodrag nopan nowheel"
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      onFocus={onFocus}
      onBlur={onBlur}
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (!disabled) {
          onClick();
        }
      }}
    >
      {children}
    </IconButton>
  );
}

export interface WorkflowCanvasControlLabels {
  resetZoom: string;
  zoomIn: string;
  zoomOut: string;
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
  topInset,
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
  topInset?: number;
}) {
  const { zoom } = useViewport();
  const reactFlow = useReactFlow();
  const duration = fitViewOptions.duration ?? 180;
  const commitCurrentViewport = useCallback(() => {
    onViewportCommit(reactFlow.getViewport());
  }, [onViewportCommit, reactFlow]);
  const zoomTo = useCallback(
    (nextZoom: number) => {
      void reactFlow.zoomTo(normalizeZoom(nextZoom)).then(commitCurrentViewport);
    },
    [commitCurrentViewport, normalizeZoom, reactFlow],
  );
  const zoomIn = useCallback(() => {
    void reactFlow.zoomIn({ duration }).then(commitCurrentViewport);
  }, [commitCurrentViewport, duration, reactFlow]);
  const zoomOut = useCallback(() => {
    void reactFlow.zoomOut({ duration }).then(commitCurrentViewport);
  }, [commitCurrentViewport, duration, reactFlow]);
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
    <div
      role="toolbar"
      aria-label={labels.controls}
      className="workflow-canvas-controls nopan nodrag nowheel absolute left-3 top-3 z-toolbar flex items-center lg:left-4 lg:top-4"
      style={topInset == null ? undefined : { top: topInset }}
    >
      <IconButton
        label={labels.zoomOut}
        size="toolbar"
        className="nodrag nopan nowheel rounded-none"
        onClick={zoomOut}
      >
        <Minus size={14} aria-hidden="true" />
      </IconButton>
      <IconButton
        label={labels.resetZoom}
        size="toolbar"
        className="nodrag nopan nowheel min-w-11 rounded-none lg:min-w-9"
        onClick={() => zoomTo(1)}
      >
        <span className="px-0.5 text-[11px] tabular-nums">{Math.round(normalizeZoom(zoom) * 100)}%</span>
      </IconButton>
      <IconButton
        label={labels.zoomIn}
        size="toolbar"
        className="nodrag nopan nowheel rounded-none"
        onClick={zoomIn}
      >
        <Plus size={14} aria-hidden="true" />
      </IconButton>
      <IconButton
        label={labels.fitView}
        size="toolbar"
        className="nodrag nopan nowheel rounded-none"
        onClick={() => {
          void reactFlow.fitView(fitViewOptions).then(commitCurrentViewport);
        }}
      >
        <Expand aria-hidden="true" size={14} />
      </IconButton>
      <IconButton
        label={labels.fitSelection}
        size="toolbar"
        className="nodrag nopan nowheel rounded-none"
        disabled={!selectedNodeIds.length}
        onClick={fitSelectedNodes}
      >
        <Focus aria-hidden="true" size={14} />
      </IconButton>
      <IconButton
        label={labels.snapToGrid}
        size="toolbar"
        className={cn("nodrag nopan nowheel rounded-none", snapToGrid && "bg-surface-subtle text-text-primary")}
        onClick={onToggleSnapToGrid}
      >
        <Grid aria-hidden="true" size={14} />
      </IconButton>
      <IconButton
        label={labels.autoLayout}
        size="toolbar"
        className="nodrag nopan nowheel rounded-none"
        busy={autoLayoutBusy}
        onClick={onAutoLayout}
      >
        <Sparkles aria-hidden="true" size={14} />
      </IconButton>
    </div>
  );
}

export function WorkflowCanvasGrid({ gap = 36 }: { gap?: number }) {
  return (
    <Background
      id={`workflow-grid-${gap}`}
      variant={BackgroundVariant.Dots}
      gap={gap}
      size={1.5}
      color="var(--color-canvas-dot)"
    />
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
    <div
      role="group"
      data-workflow-canvas-mobile-mode-tabs
      className={cn(tabListClassName, "grid w-full grid-cols-3 gap-1 p-1")}
    >
      {items.map((item) => (
        <Tooltip key={item.key} content={item.description}>
          <button
            type="button"
            onClick={() => onChange(item.key)}
            className={cn(tabTriggerClassName, "min-h-11 gap-2")}
            aria-pressed={value === item.key}
            aria-label={item.description}
          >
            {item.icon}
            <span className="truncate">{item.label}</span>
          </button>
        </Tooltip>
      ))}
    </div>
  );
}
