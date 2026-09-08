/**
 * 工作台外壳：画布、Agent 面板和 inspector 轨道。
 *
 * 窄屏一次只让一个区域可交互。inspector 宽度与画布几何共用，避免 live 图被轨道挡住。
 */

import { Bot } from "lucide-react";
import type { CSSProperties, ReactNode } from "react";
import { useEffect, useRef, useState } from "react";

import { useI18n } from "../../../lib/preferences";
import { INSPECTOR_RAIL_WIDTH } from "../chrome/constants";
import {
  ProductWorkbenchInspector,
  type ProductWorkbenchInspectorTool,
  useProductWorkbenchInspectorState,
} from "../chrome/ProductWorkbenchInspector";
import { patchWorkbenchUiState, readWorkbenchUiState } from "../chrome/workbenchUiState";

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
  productId?: string;
  workflowAvailable: boolean;
  canvasContent: ReactNode;
  canvasOwnsDetails?: boolean;
  agentContent: ReactNode;
  sidebarTools?: AgentWorkbenchSidebarTool[];
  activeSidebarTool?: string;
  onSidebarToolChange?: (toolId: string) => boolean | Promise<boolean>;
  agentOpenRequest?: number;
  onAgentOpened?: () => void;
  confirmationContent?: ReactNode;
}

const COMPACT_WORKBENCH_MEDIA_QUERY = "(max-width: 1023px)";

function initialCompactWorkbench(): boolean {
  return typeof window !== "undefined"
    && typeof window.matchMedia === "function"
    && window.matchMedia(COMPACT_WORKBENCH_MEDIA_QUERY).matches;
}

export function deriveAgentWorkbenchRegionState({
  confirmationOpen,
  activeSidebarTool = "agent",
  sidebarCollapsed = false,
}: {
  workflowAvailable: boolean;
  compact: boolean;
  mobileView?: AgentWorkbenchMobileView;
  confirmationOpen: boolean;
  activeSidebarTool?: string;
  sidebarCollapsed?: boolean;
}): AgentWorkbenchRegionState {
  const sidebarInert = confirmationOpen || sidebarCollapsed;
  return {
    canvasInert: confirmationOpen,
    sidebarInert,
    agentInert: sidebarInert || activeSidebarTool !== "agent",
  };
}

export function AgentWorkbenchShell({
  productId,
  workflowAvailable,
  canvasContent,
  canvasOwnsDetails = false,
  agentContent,
  sidebarTools = [],
  activeSidebarTool = "agent",
  onSidebarToolChange,
  agentOpenRequest = 0,
  onAgentOpened,
  confirmationContent,
}: AgentWorkbenchShellProps) {
  const { t } = useI18n();
  const [mobileView, setMobileView] = useState<AgentWorkbenchMobileView>(
    workflowAvailable ? "canvas" : "agent",
  );
  const [compact, setCompact] = useState(initialCompactWorkbench);
  const inspectorInitialRef = useRef<boolean | null>(null);
  if (inspectorInitialRef.current === null) {
    const stored = productId ? readWorkbenchUiState(productId).inspectorCollapsed : undefined;
    inspectorInitialRef.current = stored ?? !workflowAvailable;
  }
  const inspector = useProductWorkbenchInspectorState(inspectorInitialRef.current);
  const previousCanvasDetailsRef = useRef<boolean | null>(null);
  useEffect(() => {
    if (previousCanvasDetailsRef.current === canvasOwnsDetails) return;
    previousCanvasDetailsRef.current = canvasOwnsDetails;
    // On entering results, its detail panel replaces the automatic node inspector.
    // Explicit sidebar choices after entry remain available.
    if (canvasOwnsDetails && activeSidebarTool === "details") inspector.setCollapsed(true);
  }, [canvasOwnsDetails, activeSidebarTool, inspector.setCollapsed]);
  const sidebarCollapsed = inspector.collapsed;
  const inspectorWidth = inspector.width;
  const previousWorkflowAvailableRef = useRef(workflowAvailable);
  const previousSidebarToolRef = useRef(activeSidebarTool);
  const pendingAgentOpenRequestRef = useRef<number | null>(null);
  const handledAgentOpenRequestRef = useRef(0);
  const confirmationOpen = Boolean(confirmationContent);
  const resolvedActiveToolId = sidebarTools.some((tool) => tool.id === activeSidebarTool) || activeSidebarTool === "agent"
    ? activeSidebarTool
    : "agent";
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
    if (!productId) return;
    patchWorkbenchUiState(productId, { inspectorCollapsed: inspector.collapsed });
  }, [inspector.collapsed, productId]);

  useEffect(() => {
    const previouslyAvailable = previousWorkflowAvailableRef.current;
    previousWorkflowAvailableRef.current = workflowAvailable;
    if (workflowAvailable && !previouslyAvailable) {
      setMobileView("canvas");
      inspector.setCollapsed(compact);
    } else if (!workflowAvailable) {
      setMobileView("canvas");
      inspector.setCollapsed(true);
    }
  }, [compact, inspector.setCollapsed, workflowAvailable]);

  useEffect(() => {
    if (previousSidebarToolRef.current === activeSidebarTool) {
      return;
    }
    previousSidebarToolRef.current = activeSidebarTool;
    inspector.setCollapsed(false);
    setMobileView("agent");
  }, [activeSidebarTool, inspector.setCollapsed]);

  useEffect(() => {
    if (
      agentOpenRequest <= handledAgentOpenRequestRef.current
      || confirmationOpen
    ) {
      return;
    }
    pendingAgentOpenRequestRef.current = agentOpenRequest;
    setMobileView("agent");
    if (sidebarCollapsed) {
      inspector.setCollapsed(false);
    }
  }, [agentOpenRequest, confirmationOpen, inspector.setCollapsed, sidebarCollapsed]);

  useEffect(() => {
    const request = pendingAgentOpenRequestRef.current;
    if (
      request === null
      || request !== agentOpenRequest
      || confirmationOpen
      || sidebarCollapsed
      || resolvedActiveToolId !== "agent"
    ) {
      return;
    }
    pendingAgentOpenRequestRef.current = null;
    handledAgentOpenRequestRef.current = request;
    onAgentOpened?.();
  }, [agentOpenRequest, confirmationOpen, onAgentOpened, resolvedActiveToolId, sidebarCollapsed]);

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
  const canvasVisibleClass = "visible opacity-100";
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
      data-workbench-layout="canvas-sidebar"
      className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-surface-base pb-[calc(4.5rem+env(safe-area-inset-bottom))] lg:pb-0"
    >
      <div
        data-agent-workbench-work-area
        className="relative min-h-0 flex-1 overflow-hidden lg:grid lg:transition-[grid-template-columns] lg:duration-300 lg:ease-out motion-reduce:lg:transition-none"
        style={{
          gridTemplateColumns: `minmax(0, 1fr) ${inspectorTrackWidth}px`,
        } as CSSProperties}
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
          workflowAvailable
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
          mobileVisible={!sidebarCollapsed}
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
          className="absolute inset-0 z-40 min-h-0 overflow-hidden bg-surface-raised"
        >
          {confirmationContent}
        </div>
      ) : null}
    </main>
  );
}
