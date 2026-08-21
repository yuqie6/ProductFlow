import { Bot, Workflow } from "lucide-react";
import type { CSSProperties, ReactNode } from "react";
import { useEffect, useRef, useState } from "react";

import { useI18n } from "../../../lib/preferences";
import { INSPECTOR_CANVAS_GAP, INSPECTOR_RAIL_WIDTH } from "../chrome/constants";
import {
  ProductWorkbenchInspector,
  type ProductWorkbenchInspectorTool,
  useProductWorkbenchInspectorState,
} from "../chrome/ProductWorkbenchInspector";

export type AgentWorkbenchMobileView = "canvas" | "agent";

export type AgentWorkbenchSidebarTool = ProductWorkbenchInspectorTool;

export function getAgentWorkbenchInspectorTrackWidth(collapsed: boolean, inspectorWidth: number): number {
  return collapsed ? INSPECTOR_RAIL_WIDTH : INSPECTOR_RAIL_WIDTH + inspectorWidth;
}

interface AgentWorkbenchRegionState {
  canvasInert: boolean;
  sidebarInert: boolean;
  agentInert: boolean;
}

interface AgentWorkbenchShellProps {
  workflowAvailable: boolean;
  canvasContent: ReactNode;
  agentContent: ReactNode;
  sidebarTools?: AgentWorkbenchSidebarTool[];
  activeSidebarTool?: string;
  onSidebarToolChange?: (toolId: string) => boolean | Promise<boolean>;
  confirmationContent?: ReactNode;
}

const COMPACT_WORKBENCH_MEDIA_QUERY = "(max-width: 1023px)";

function initialCompactWorkbench(): boolean {
  return typeof window !== "undefined"
    && typeof window.matchMedia === "function"
    && window.matchMedia(COMPACT_WORKBENCH_MEDIA_QUERY).matches;
}

export function deriveAgentWorkbenchRegionState({
  workflowAvailable,
  compact,
  mobileView,
  confirmationOpen,
  activeSidebarTool = "agent",
  sidebarCollapsed = false,
}: {
  workflowAvailable: boolean;
  compact: boolean;
  mobileView: AgentWorkbenchMobileView;
  confirmationOpen: boolean;
  activeSidebarTool?: string;
  sidebarCollapsed?: boolean;
}): AgentWorkbenchRegionState {
  const sidebarInert = confirmationOpen
    || (compact && workflowAvailable && mobileView !== "agent")
    || (!compact && workflowAvailable && sidebarCollapsed);
  return {
    canvasInert: confirmationOpen || !workflowAvailable || (compact && mobileView !== "canvas"),
    sidebarInert,
    agentInert: sidebarInert || (workflowAvailable && activeSidebarTool !== "agent"),
  };
}

export function AgentWorkbenchShell({
  workflowAvailable,
  canvasContent,
  agentContent,
  sidebarTools = [],
  activeSidebarTool = "agent",
  onSidebarToolChange,
  confirmationContent,
}: AgentWorkbenchShellProps) {
  const { t } = useI18n();
  const [mobileView, setMobileView] = useState<AgentWorkbenchMobileView>(
    workflowAvailable ? "canvas" : "agent",
  );
  const [compact, setCompact] = useState(initialCompactWorkbench);
  const inspector = useProductWorkbenchInspectorState();
  const sidebarCollapsed = inspector.collapsed;
  const inspectorWidth = inspector.width;
  const previousWorkflowAvailableRef = useRef(workflowAvailable);
  const previousSidebarToolRef = useRef(activeSidebarTool);
  const confirmationOpen = Boolean(confirmationContent);
  const resolvedActiveToolId = sidebarTools.some((tool) => tool.id === activeSidebarTool) || activeSidebarTool === "agent"
    ? activeSidebarTool
    : "agent";
  const selectedTool = sidebarTools.find((tool) => tool.id === resolvedActiveToolId) ?? null;

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }
    const media = window.matchMedia(COMPACT_WORKBENCH_MEDIA_QUERY);
    const update = () => setCompact(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  useEffect(() => {
    const previouslyAvailable = previousWorkflowAvailableRef.current;
    previousWorkflowAvailableRef.current = workflowAvailable;
    if (workflowAvailable && !previouslyAvailable) {
      setMobileView("canvas");
    } else if (!workflowAvailable) {
      setMobileView("agent");
      inspector.setCollapsed(false);
    }
  }, [inspector.setCollapsed, workflowAvailable]);

  useEffect(() => {
    if (previousSidebarToolRef.current === resolvedActiveToolId) {
      return;
    }
    previousSidebarToolRef.current = resolvedActiveToolId;
    if (workflowAvailable) {
      inspector.setCollapsed(false);
      setMobileView("agent");
    }
  }, [inspector.setCollapsed, resolvedActiveToolId, workflowAvailable]);

  const selectSidebarTool = async (toolId: string) => {
    const accepted = await onSidebarToolChange?.(toolId);
    if (accepted === false) {
      return;
    }
    inspector.setCollapsed(false);
    setMobileView("agent");
  };

  const regions = deriveAgentWorkbenchRegionState({
    workflowAvailable,
    compact,
    mobileView,
    confirmationOpen,
    activeSidebarTool: resolvedActiveToolId,
    sidebarCollapsed,
  });
  const canvasVisibleClass = workflowAvailable
    ? mobileView === "canvas"
      ? "visible opacity-100"
      : "invisible pointer-events-none opacity-0 lg:visible lg:pointer-events-auto lg:opacity-100"
    : "invisible pointer-events-none opacity-0";
  const inspectorTrackWidth = getAgentWorkbenchInspectorTrackWidth(sidebarCollapsed, inspectorWidth);
  const inspectorTools: ProductWorkbenchInspectorTool[] = [
    {
      id: "agent",
      label: t("agentWorkbench.agent"),
      railLabel: t("agentWorkbench.agentShort"),
      title: t("agentWorkbench.agent"),
      icon: <Bot size={17} />,
      content: agentContent,
      keepMounted: true,
      chrome: "embedded",
    },
    ...sidebarTools,
  ];

  return (
    <main
      data-agent-workbench-shell
      data-workbench-layout={workflowAvailable ? "canvas-sidebar" : "agent-only"}
      className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-slate-50 pb-[calc(4.5rem+env(safe-area-inset-bottom))] dark:bg-[#0b1220] lg:pb-0"
    >
      {workflowAvailable ? (
        <div
          role="tablist"
          aria-label={t("agentWorkbench.mobileView")}
          className="grid h-13 shrink-0 grid-cols-2 gap-1 border-b border-zinc-200 bg-zinc-100 p-1.5 dark:border-slate-800 dark:bg-[#080d14] lg:hidden"
        >
          <MobileViewTab
            active={mobileView === "canvas"}
            icon={<Workflow size={16} />}
            label={t("agentWorkbench.canvas")}
            onClick={() => setMobileView("canvas")}
          />
          <MobileViewTab
            active={mobileView === "agent"}
            icon={resolvedActiveToolId === "agent" ? <Bot size={16} /> : selectedTool?.icon}
            label={resolvedActiveToolId === "agent" ? t("agentWorkbench.agent") : selectedTool?.label ?? ""}
            onClick={() => setMobileView("agent")}
          />
        </div>
      ) : null}

      <div
        data-agent-workbench-work-area
        className={`relative min-h-0 flex-1 overflow-hidden lg:grid lg:transition-[grid-template-columns] lg:duration-300 lg:ease-out motion-reduce:lg:transition-none ${
          workflowAvailable ? "lg:gap-[var(--agent-workbench-inspector-gap)] lg:pr-[var(--agent-workbench-inspector-gap)]" : ""
        }`}
        style={workflowAvailable ? ({
          "--agent-workbench-inspector-gap": `${INSPECTOR_CANVAS_GAP}px`,
          gridTemplateColumns: `minmax(0, 1fr) ${inspectorTrackWidth}px`,
        } as CSSProperties) : undefined}
      >
        <section
          data-agent-workbench-canvas-slot
          aria-hidden={regions.canvasInert || undefined}
          inert={regions.canvasInert}
          className={`absolute inset-0 min-h-0 min-w-0 overflow-hidden transition-[opacity,visibility] duration-300 ease-out motion-reduce:transition-none lg:relative lg:inset-auto lg:h-full lg:min-h-0 ${canvasVisibleClass}`}
        >
          {canvasContent}
        </section>

        <ProductWorkbenchInspector
          workflowAvailable={workflowAvailable}
          tools={inspectorTools}
          activeToolId={resolvedActiveToolId}
          onToolChange={selectSidebarTool}
          collapsed={sidebarCollapsed}
          onCollapsedChange={inspector.setCollapsed}
          width={inspectorWidth}
          onResizeStart={inspector.startResize}
          ariaLabel={t("workbench.sidebar.ariaLabel")}
          resizeLabel={t("detail.resizeSidebar")}
          collapseLabel={t("detail.collapseSidebar")}
          expandLabel={t("detail.expandSidebar")}
          mobileVisible={!workflowAvailable || mobileView === "agent"}
          inert={regions.sidebarInert}
          showActiveWhenCollapsed
          desktopLayout="grid-child"
          slotDataAttribute="data-agent-workbench-agent-slot"
          collapsedDataAttribute="data-agent-workbench-collapsed-tools"
        />
      </div>

      {confirmationContent ? (
        <div
          data-agent-workbench-confirmation-layer
          className="absolute inset-0 z-40 min-h-0 overflow-hidden bg-white dark:bg-[#090d13]"
        >
          {confirmationContent}
        </div>
      ) : null}
    </main>
  );
}

function MobileViewTab({
  active,
  icon,
  label,
  onClick,
}: {
  active: boolean;
  icon?: ReactNode;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={`inline-flex min-h-10 min-w-0 items-center justify-center gap-2 rounded-md px-2 text-sm font-semibold transition-colors ${
        active
          ? "bg-white text-indigo-700 shadow-sm dark:bg-slate-800 dark:text-violet-200"
          : "text-zinc-500 hover:text-zinc-950 dark:text-slate-400 dark:hover:text-white"
      }`}
    >
      <span className="shrink-0">{icon}</span>
      <span className="min-w-0 truncate">{label}</span>
    </button>
  );
}
