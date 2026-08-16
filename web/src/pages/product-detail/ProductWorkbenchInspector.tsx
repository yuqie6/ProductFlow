import { ChevronLeft, ChevronRight } from "lucide-react";
import type { CSSProperties, PointerEvent as ReactPointerEvent, ReactNode } from "react";
import { useEffect, useRef, useState } from "react";

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

export function useProductWorkbenchInspectorState() {
  const [collapsed, setCollapsed] = useState(false);
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
        ? "invisible pointer-events-none flex opacity-0"
        : "invisible pointer-events-none flex opacity-0 lg:visible lg:pointer-events-auto lg:opacity-100";
  const desktopGridChild = workflowAvailable && desktopLayout === "grid-child";

  const renderRailTools = (collapsedRail: boolean) => tools.map((tool) => (
    <div key={tool.id} className="contents">
      {tool.separatorBefore}
      <SidebarTabButton
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
        <nav
          {...collapsedData}
          data-product-workbench-collapsed-tools
          aria-label={ariaLabel}
          style={{ width: INSPECTOR_RAIL_WIDTH }}
          className={`glass-inspector absolute right-6 z-30 hidden flex-col items-center gap-2 rounded-[24px] p-2 pb-3 shadow-2xl lg:flex ${
            desktopGridChild
              ? "lg:relative lg:inset-auto lg:z-auto lg:h-full lg:justify-self-stretch lg:rounded-[24px]"
              : desktopCollapsedPositionClassName
          }`}
        >
          {railBefore}
          {renderRailTools(true)}
          <div className="mt-auto flex w-full justify-center border-t border-slate-200/40 pt-2 dark:border-white/5">
            <button
              type="button"
              onClick={() => onCollapsedChange(false)}
              className="inline-flex h-9 w-9 items-center justify-center rounded-xl text-slate-400 transition-all hover:scale-105 hover:bg-white/40 hover:text-slate-800 dark:text-slate-400 dark:hover:bg-white/5 dark:hover:text-slate-200"
              title={expandLabel}
              aria-label={expandLabel}
            >
              <ChevronLeft size={16} />
            </button>
          </div>
        </nav>
      ) : null}

      <aside
        {...slotData}
        data-product-workbench-inspector
        aria-hidden={inert || undefined}
        inert={inert}
        className={`absolute inset-0 z-30 min-h-0 min-w-0 flex-col overflow-hidden bg-white transition-[opacity,visibility] duration-300 motion-reduce:transition-none dark:bg-[#070b11] lg:flex-row ${visibilityClassName} ${
          workflowAvailable
            ? `glass-inspector lg:inset-auto lg:w-[var(--product-workbench-inspector-width)] lg:rounded-[28px] lg:shadow-[0_24px_50px_rgba(15,23,42,0.18)] dark:lg:shadow-[0_32px_64px_rgba(0,0,0,0.45)] ${
                desktopGridChild
                  ? "lg:relative lg:z-auto lg:h-full lg:justify-self-stretch"
                  : desktopPositionClassName
              }`
            : ""
        } ${workflowAvailable && collapsed ? "lg:invisible lg:pointer-events-none lg:opacity-0" : ""}`}
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
              <div className="h-12 w-1 rounded-full bg-slate-300 opacity-40 transition-all duration-300 group-hover:h-20 group-hover:opacity-100 dark:bg-slate-700 animate-handle-glow" />
            </div>

            <nav
              aria-label={ariaLabel}
              className="flex h-auto shrink-0 gap-1 overflow-x-auto border-b border-slate-200/60 bg-white/80 px-2 py-2 dark:border-white/5 dark:bg-black/10 lg:h-full lg:flex-col lg:gap-2 lg:overflow-y-auto lg:border-b-0 lg:border-r lg:px-2 lg:py-4"
              style={{ width: INSPECTOR_RAIL_WIDTH }}
            >
              {railBefore}
              {renderRailTools(false)}
              <div className="hidden w-full flex-1 lg:block" />
              <div className="hidden w-full justify-center border-t border-slate-200/40 pt-2 dark:border-white/5 lg:flex">
                <button
                  type="button"
                  onClick={() => onCollapsedChange(true)}
                  className="btn-secondary-spring inline-flex h-9 w-9 items-center justify-center rounded-xl"
                  title={collapseLabel}
                  aria-label={collapseLabel}
                >
                  <ChevronRight size={16} />
                </button>
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
                className={`absolute inset-0 min-h-0 min-w-0 overflow-hidden bg-white transition-[opacity,visibility] duration-200 dark:bg-[#070b11] ${
                  active ? "visible opacity-100" : "invisible pointer-events-none opacity-0"
                }`}
              >
                {mounted ? tool.chrome === "embedded" ? tool.content : (
                  <div className="flex h-full min-h-0 flex-col bg-transparent">
                    <header className="flex h-12 shrink-0 items-center border-b border-slate-200/50 px-4 dark:border-slate-800">
                      <span className="mr-2 text-indigo-600 dark:text-violet-400">{tool.icon}</span>
                      <h2 className="truncate text-[11px] font-bold uppercase tracking-widest text-slate-700 dark:text-slate-200">
                        {tool.label}
                      </h2>
                    </header>
                    <div
                      key={tool.contentKey}
                      className={tool.contentClassName ?? "min-h-0 flex-1 overflow-y-auto p-4 animate-spring-slide-in"}
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
