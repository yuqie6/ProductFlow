import { ChevronLeft, ChevronRight } from "lucide-react";
import type { CSSProperties, PointerEvent as ReactPointerEvent, ReactNode } from "react";
import { useEffect, useRef, useState } from "react";

import { IconButton } from "../../../components/ui/icon-button";
import { INSPECTOR_RAIL_WIDTH, MAX_INSPECTOR_WIDTH, MIN_INSPECTOR_WIDTH } from "./constants";
import { SidebarTabButton } from "./SidebarTabButton";
import { clamp, readStoredNumber } from "./utils";

const INSPECTOR_WIDTH_STORAGE_KEY = "productflow.workflow.inspectorWidth";

export interface ProductWorkbenchInspectorTool {
  id: string;
  label: string;
  railLabel?: string;
  title?: string;
  icon: ReactNode;
  content: ReactNode;
  keepMounted?: boolean;
  chrome?: "standard" | "embedded";
  contentClassName?: string;
  contentKey?: string;
  separatorBefore?: ReactNode;
}

interface ProductWorkbenchInspectorProps {
  workflowAvailable: boolean;
  tools: ProductWorkbenchInspectorTool[];
  activeToolId: string;
  onToolChange: (toolId: string) => void;
  collapsed: boolean;
  onCollapsedChange: (collapsed: boolean) => void;
  width: number;
  onResizeStart: (event: ReactPointerEvent<HTMLDivElement>) => void;
  ariaLabel: string;
  resizeLabel: string;
  collapseLabel: string;
  expandLabel: string;
  mobileVisible?: boolean;
  desktopOnly?: boolean;
  inert?: boolean;
  railBefore?: ReactNode;
  showActiveWhenCollapsed?: boolean;
  desktopLayout?: "overlay" | "grid-child";
  desktopPositionClassName?: string;
  desktopCollapsedPositionClassName?: string;
  slotDataAttribute?: `data-${string}`;
  collapsedDataAttribute?: `data-${string}`;
}

export function useProductWorkbenchInspectorState(initialCollapsed = false) {
  const [collapsed, setCollapsed] = useState(() => initialCollapsed || (
    typeof window !== "undefined"
    && typeof window.matchMedia === "function"
    && window.matchMedia("(max-width: 1023px)").matches
  ));
  const [width, setWidth] = useState(() =>
    clamp(
      readStoredNumber(INSPECTOR_WIDTH_STORAGE_KEY, 360),
      MIN_INSPECTOR_WIDTH,
      MAX_INSPECTOR_WIDTH,
    ),
  );
  const cleanupResizeRef = useRef<(() => void) | null>(null);

  useEffect(() => () => cleanupResizeRef.current?.(), []);

  const startResize = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    cleanupResizeRef.current?.();

    const startX = event.clientX;
    const startWidth = width;
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    let finished = false;

    const move = (moveEvent: PointerEvent) => {
      const next = clamp(
        startWidth + startX - moveEvent.clientX,
        MIN_INSPECTOR_WIDTH,
        MAX_INSPECTOR_WIDTH,
      );
      setWidth(next);
      window.localStorage.setItem(INSPECTOR_WIDTH_STORAGE_KEY, String(next));
    };
    const stop = () => {
      if (finished) {
        return;
      }
      finished = true;
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      cleanupResizeRef.current = null;
    };

    cleanupResizeRef.current = stop;
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
    window.addEventListener("pointercancel", stop);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
  };

  return {
    collapsed,
    setCollapsed,
    width,
    startResize,
  };
}

export function ProductWorkbenchInspector({
  workflowAvailable,
  tools,
  activeToolId,
  onToolChange,
  collapsed,
  onCollapsedChange,
  width,
  onResizeStart,
  ariaLabel,
  resizeLabel,
  collapseLabel,
  expandLabel,
  mobileVisible = true,
  desktopOnly = false,
  inert = false,
  railBefore,
  showActiveWhenCollapsed = false,
  desktopLayout = "overlay",
  desktopPositionClassName = "lg:bottom-6 lg:left-auto lg:right-6 lg:top-20",
  desktopCollapsedPositionClassName = "lg:left-auto lg:right-6 lg:top-20",
  slotDataAttribute,
  collapsedDataAttribute,
}: ProductWorkbenchInspectorProps) {
  const slotData = slotDataAttribute ? { [slotDataAttribute]: "" } : {};
  const collapsedData = collapsedDataAttribute ? { [collapsedDataAttribute]: "" } : {};
  const inspectorStyle = workflowAvailable
    ? ({ "--product-workbench-inspector-width": `${INSPECTOR_RAIL_WIDTH + width}px` } as CSSProperties)
    : undefined;
  const visibilityClassName = desktopOnly
    ? "hidden lg:flex"
    : mobileVisible
      ? "visible flex opacity-100"
      : workflowAvailable && collapsed
        ? "invisible pointer-events-none flex opacity-0 lg:invisible"
        : "invisible pointer-events-none flex opacity-0 lg:visible lg:pointer-events-auto lg:opacity-100";
  const desktopGridChild = workflowAvailable && desktopLayout === "grid-child";

  const renderRailTools = (collapsedRail: boolean) => tools.map((tool) => (
    <div key={tool.id} className="contents">
      {tool.separatorBefore}
      <SidebarTabButton
        toolId={tool.id}
        active={collapsedRail ? showActiveWhenCollapsed && activeToolId === tool.id : activeToolId === tool.id}
        icon={tool.icon}
        label={tool.railLabel ?? tool.label}
        title={tool.title ?? tool.label}
        onClick={() => onToolChange(tool.id)}
      />
    </div>
  ));

  return (
    <>
      {workflowAvailable && collapsed ? (
        <>
          <button
            type="button"
            data-product-workbench-drawer-handle
            onClick={() => onCollapsedChange(false)}
            className="glass-inspector absolute inset-x-4 bottom-3 z-30 flex h-11 items-center justify-center rounded-surface text-xs font-semibold text-text-secondary shadow-elev-2 lg:hidden"
            aria-label={expandLabel}
          >
            {expandLabel}
          </button>
          <nav
            {...collapsedData}
            data-product-workbench-collapsed-tools
            aria-label={ariaLabel}
            style={{ width: INSPECTOR_RAIL_WIDTH }}
            className={`absolute right-6 z-30 hidden flex-col items-center gap-2 p-2 pb-3 lg:flex ${desktopGridChild
                ? "border-l border-border-l1 bg-surface-panel lg:relative lg:inset-auto lg:z-auto lg:h-full lg:justify-self-stretch"
                : desktopCollapsedPositionClassName
              } ${desktopGridChild ? "" : "glass-inspector rounded-surface shadow-elev-3"}`}
          >
            {railBefore}
            {renderRailTools(true)}
            <div className="mt-auto flex w-full justify-center border-t border-border-l1 pt-2">
              <IconButton
                label={expandLabel}
                size="toolbar"
                onClick={() => onCollapsedChange(false)}
              >
                <ChevronLeft size={16} />
              </IconButton>
            </div>
          </nav>
        </>
      ) : null}

      <aside
        {...slotData}
        data-product-workbench-inspector
        data-inspector-layout={workflowAvailable ? "drawer" : "page"}
        aria-hidden={inert || undefined}
        inert={inert}
        className={`absolute z-30 min-h-0 min-w-0 flex-col overflow-hidden bg-surface-panel transition-[opacity,visibility] duration-300 motion-reduce:transition-none lg:flex-row ${visibilityClassName} ${workflowAvailable
            ? `inset-x-0 bottom-0 top-auto h-[min(64vh,30rem)] max-h-[min(64vh,30rem)] rounded-t-surface border-t border-border-l1 shadow-elev-3 lg:inset-auto lg:h-auto lg:max-h-none lg:w-[var(--product-workbench-inspector-width)] ${desktopGridChild
              ? "lg:relative lg:z-auto lg:h-full lg:justify-self-stretch lg:rounded-none lg:border-l lg:border-t-0 lg:shadow-none"
              : desktopPositionClassName
            } ${desktopGridChild ? "" : "glass-inspector lg:rounded-surface"}`
            : "inset-0"
          } ${workflowAvailable && collapsed ? "hidden lg:hidden" : ""}`}
        style={inspectorStyle}
      >
        {workflowAvailable ? (
          <>
            <div
              role="separator"
              aria-label={resizeLabel}
              onPointerDown={onResizeStart}
              className="group absolute left-0 top-0 z-30 hidden h-full w-2.5 cursor-col-resize items-center justify-center lg:flex"
            >
              <div className="h-12 w-0.5 rounded-full bg-text-muted opacity-0 transition-[height,opacity] duration-slow group-hover:h-20 group-hover:opacity-60 motion-reduce:transition-none" />
            </div>

            <nav
              aria-label={ariaLabel}
              className="flex h-auto w-full shrink-0 gap-1 overflow-x-auto border-b border-border-l1 bg-surface-panel px-2 py-2 lg:h-full lg:w-[72px] lg:flex-col lg:gap-2 lg:overflow-y-auto lg:border-b-0 lg:border-r lg:px-2 lg:py-4"
            >
              {railBefore}
              {renderRailTools(false)}
              <IconButton
                label={collapseLabel}
                size="toolbar"
                className="ml-auto lg:hidden"
                onClick={() => onCollapsedChange(true)}
              >
                <ChevronRight size={16} />
              </IconButton>
              <div className="hidden w-full flex-1 lg:block" />
              <div className="hidden w-full justify-center border-t border-border-l1 pt-2 lg:flex">
                <IconButton
                  label={collapseLabel}
                  size="toolbar"
                  onClick={() => onCollapsedChange(true)}
                >
                  <ChevronRight size={16} />
                </IconButton>
              </div>
            </nav>
          </>
        ) : null}

        <div className="relative min-h-0 min-w-0 flex-1 overflow-hidden">
          {tools.map((tool) => {
            const active = tool.id === activeToolId;
            const mounted = active || tool.keepMounted;
            return (
              <section
                key={tool.id}
                data-product-workbench-tool-panel={tool.id}
                aria-hidden={!active || undefined}
                inert={!active}
                className={`absolute inset-0 min-h-0 min-w-0 overflow-hidden bg-surface-panel transition-[opacity,visibility] duration-200 ${active ? "visible opacity-100" : "invisible pointer-events-none opacity-0"
                  }`}
              >
                {mounted ? tool.chrome === "embedded" ? tool.content : (
                  <div className="flex h-full min-h-0 flex-col bg-transparent">
                    <header className="flex h-12 shrink-0 items-center border-b border-border-l1 px-4">
                      <span className="mr-2 text-text-secondary">{tool.icon}</span>
                      <h2 className="truncate text-xs font-semibold text-text-secondary">
                        {tool.label}
                      </h2>
                    </header>
                    <div
                      key={tool.contentKey}
                      className={tool.contentClassName ?? "min-h-0 flex-1 overflow-y-auto p-4 motion-safe:animate-node-reveal"}
                    >
                      {tool.content}
                    </div>
                  </div>
                ) : null}
              </section>
            );
          })}
        </div>
      </aside>
    </>
  );
}
