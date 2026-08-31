import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { INSPECTOR_RAIL_WIDTH } from "../chrome/constants";
import { ProductWorkbenchInspector } from "../chrome/ProductWorkbenchInspector";
import {
  AgentWorkbenchShell,
  deriveAgentWorkbenchRegionState,
  getAgentWorkbenchInspectorTrackWidth,
} from "./AgentWorkbenchShell";

function renderShell(
  workflowAvailable: boolean,
  confirmation = false,
  activeSidebarTool = "agent",
): string {
  return renderToStaticMarkup(createElement(AgentWorkbenchShell, {
    workflowAvailable,
    canvasContent: createElement("div", { "data-canvas-probe": true }),
    agentContent: createElement("div", { "data-agent-probe": true }),
    activeSidebarTool,
    sidebarTools: [
      {
        id: "library",
        label: "图库",
        icon: createElement("span", null, "L"),
        content: createElement("div", { "data-library-probe": true }),
      },
    ],
    confirmationContent: confirmation
      ? createElement("div", { "data-confirmation-probe": true })
      : null,
  }));
}

function renderInspector({
  collapsed = false,
  desktopLayout,
  inert = false,
}: {
  collapsed?: boolean;
  desktopLayout?: "overlay" | "grid-child";
  inert?: boolean;
} = {}): string {
  return renderToStaticMarkup(createElement(ProductWorkbenchInspector, {
    workflowAvailable: true,
    tools: [{
      id: "agent",
      label: "Agent",
      icon: createElement("span", null, "A"),
      content: createElement("div", null, "Agent content"),
    }],
    activeToolId: "agent",
    onToolChange: () => undefined,
    collapsed,
    onCollapsedChange: () => undefined,
    width: 360,
    onResizeStart: () => undefined,
    ariaLabel: "Inspector",
    resizeLabel: "Resize",
    collapseLabel: "Collapse",
    expandLabel: "Expand",
    inert,
    desktopLayout,
  }));
}

describe("AgentWorkbenchShell", () => {
  it("keeps exactly one mounted canvas slot and Agent slot in both layout modes", () => {
    const agentOnly = renderShell(false);
    const canvasSidebar = renderShell(true);

    expect(agentOnly).toContain('data-workbench-layout="canvas-sidebar"');
    expect(canvasSidebar).toContain('data-workbench-layout="canvas-sidebar"');
    for (const markup of [agentOnly, canvasSidebar]) {
      expect(markup.match(/data-agent-workbench-agent-slot/g)).toHaveLength(1);
      expect(markup.match(/data-agent-probe/g)).toHaveLength(1);
      expect(markup.match(/data-agent-workbench-canvas-slot/g)).toHaveLength(1);
      expect(markup.match(/data-canvas-probe/g)).toHaveLength(1);
    }
  });

  it("owns expanded and collapsed desktop width through one grid track", () => {
    const source = renderShell(true);

    expect(source).toContain("lg:grid");
    expect(source).toContain("grid-template-columns:minmax(0, 1fr) 432px");
    expect(source).not.toContain("agent-workbench-inspector-gap");
    expect(source).not.toContain("padding-right");
    expect(source).not.toContain("transition-[padding");
    expect(getAgentWorkbenchInspectorTrackWidth(false, 360)).toBe(INSPECTOR_RAIL_WIDTH + 360);
    expect(getAgentWorkbenchInspectorTrackWidth(true, 360)).toBe(INSPECTOR_RAIL_WIDTH);

    const shellSource = AgentWorkbenchShell.toString();
    expect(shellSource).not.toContain("canvasPaddingRight");
    expect(shellSource).not.toContain("paddingRight");
  });

  it("keeps a bottom drawer inspector that does not equal the work-area box", () => {
    const markup = renderShell(true);

    expect(markup).toContain('data-inspector-layout="drawer"');
    expect(markup).toContain("max-h-[min(64vh,30rem)]");
    expect(markup).toContain("bottom-0");
    expect(markup).toContain("data-agent-workbench-canvas-slot");
    expect(markup).toContain("data-canvas-probe");
    const inspectorTag = markup.match(/<aside[^>]*data-product-workbench-inspector[^>]*>/)?.[0] ?? "";
    expect(inspectorTag).not.toMatch(/(?:^|[\s"])inset-0(?:[\s"]|$)/);
    expect(inspectorTag).not.toContain('data-inspector-layout="page"');
  });

  it("keeps the canvas absolute below lg and makes it a normal grid child on desktop", () => {
    const markup = renderShell(true);

    expect(markup).toMatch(/data-agent-workbench-canvas-slot[^>]*class="[^"]*absolute inset-0[^"]*lg:relative lg:inset-auto lg:h-full lg:min-h-0/);
  });

  it("keeps both regions mounted beneath confirmation and makes them inert", () => {
    const markup = renderShell(true, true);
    const state = deriveAgentWorkbenchRegionState({
      workflowAvailable: true,
      compact: false,
      mobileView: "canvas",
      confirmationOpen: true,
    });

    expect(markup).toContain("data-agent-workbench-confirmation-layer");
    expect(markup.match(/data-agent-probe/g)).toHaveLength(1);
    expect(markup.match(/data-canvas-probe/g)).toHaveLength(1);
    expect(state).toEqual({ canvasInert: true, sidebarInert: true, agentInert: true });
  });

  it("does not inert the canvas just because a live graph is missing", () => {
    expect(deriveAgentWorkbenchRegionState({
      workflowAvailable: false,
      compact: false,
      confirmationOpen: false,
    })).toEqual({ canvasInert: false, sidebarInert: false, agentInert: false });
    expect(deriveAgentWorkbenchRegionState({
      workflowAvailable: false,
      compact: true,
      confirmationOpen: false,
    })).toEqual({ canvasInert: false, sidebarInert: false, agentInert: false });
  });

  it("uses inert visibility switching only on compact layouts", () => {
    expect(deriveAgentWorkbenchRegionState({
      workflowAvailable: true,
      compact: true,
      mobileView: "canvas",
      confirmationOpen: false,
    })).toEqual({ canvasInert: false, sidebarInert: false, agentInert: false });
    expect(deriveAgentWorkbenchRegionState({
      workflowAvailable: true,
      compact: true,
      mobileView: "agent",
      confirmationOpen: false,
    })).toEqual({ canvasInert: false, sidebarInert: false, agentInert: false });
    expect(deriveAgentWorkbenchRegionState({
      workflowAvailable: true,
      compact: false,
      mobileView: "canvas",
      confirmationOpen: false,
    })).toEqual({ canvasInert: false, sidebarInert: false, agentInert: false });
    expect(deriveAgentWorkbenchRegionState({
      workflowAvailable: true,
      compact: false,
      mobileView: "canvas",
      confirmationOpen: false,
      sidebarCollapsed: true,
    })).toEqual({ canvasInert: false, sidebarInert: true, agentInert: true });
  });

  it("uses grid-child positioning only when explicitly requested", () => {
    const overlay = renderInspector();
    const gridChild = renderInspector({ desktopLayout: "grid-child" });
    const collapsedGridChild = renderInspector({ collapsed: true, desktopLayout: "grid-child", inert: true });

    expect(overlay).toContain("data-inspector-layout=\"drawer\"");
    expect(overlay).toContain("max-h-[min(64vh,30rem)]");
    expect(overlay).toContain("lg:bottom-6 lg:left-auto lg:right-6 lg:top-20");
    expect(overlay).not.toContain("lg:relative lg:z-auto lg:h-full lg:justify-self-stretch");

    expect(gridChild).toContain("data-inspector-layout=\"drawer\"");
    expect(gridChild).toContain("max-h-[min(64vh,30rem)]");
    expect(gridChild).toContain("lg:relative lg:z-auto lg:h-full lg:justify-self-stretch");
    expect(gridChild).toContain("lg:rounded-none lg:border-l lg:border-t-0 lg:shadow-none");
    expect(gridChild).not.toContain("lg:rounded-surface");
    expect(gridChild).not.toContain("lg:bottom-6 lg:left-auto lg:right-6 lg:top-20");
    expect(gridChild).toContain(`--product-workbench-inspector-width:${INSPECTOR_RAIL_WIDTH + 360}px`);

    expect(collapsedGridChild).toContain("data-product-workbench-collapsed-tools");
    expect(collapsedGridChild).toContain(`style="width:${INSPECTOR_RAIL_WIDTH}px"`);
    expect(collapsedGridChild).toContain("border-l border-border-l1 bg-surface-raised");
    expect(collapsedGridChild).toContain("lg:relative lg:inset-auto lg:z-auto lg:h-full lg:justify-self-stretch");
    expect(collapsedGridChild).toContain("lg:hidden");
    expect(collapsedGridChild).not.toContain("lg:invisible");
    expect(collapsedGridChild).toContain("aria-hidden=\"true\" inert=\"\"");
  });

  it("shows the Agent panel when agent-only layout is given a missing sidebar tool", () => {
    const markup = renderToStaticMarkup(createElement(AgentWorkbenchShell, {
      workflowAvailable: false,
      canvasContent: createElement("div", { "data-canvas-probe": true }),
      agentContent: createElement("div", { "data-agent-probe": true }),
      activeSidebarTool: "details",
      sidebarTools: [],
    }));
    const agentPanelStart = markup.indexOf('data-product-workbench-tool-panel="agent"');
    const agentPanelMarkup = markup.slice(agentPanelStart, agentPanelStart + 500);

    expect(markup).toContain('data-workbench-layout="canvas-sidebar"');
    expect(agentPanelMarkup).toContain("visible opacity-100");
    expect(agentPanelMarkup).not.toContain("invisible pointer-events-none opacity-0");
  });

  it("keeps the empty-canvas onboarding CTA visible and clickable when no graph exists", () => {
    const markup = renderToStaticMarkup(createElement(AgentWorkbenchShell, {
      workflowAvailable: false,
      canvasContent: createElement("button", { type: "button" }, "打开添加面板"),
      agentContent: createElement("div", { "data-agent-probe": true }),
    }));
    const canvasTag = markup.match(/<section[^>]*data-agent-workbench-canvas-slot[^>]*>/)?.[0] ?? "";
    const inspectorTag = markup.match(/<aside[^>]*data-product-workbench-inspector[^>]*>/)?.[0] ?? "";

    expect(markup).toContain("打开添加面板");
    expect(canvasTag).toContain("visible opacity-100");
    expect(canvasTag).not.toContain("inert");
    expect(canvasTag).not.toContain("pointer-events-none");
    expect(canvasTag).not.toContain("invisible");
    expect(inspectorTag).toContain('data-inspector-layout="drawer"');
    expect(inspectorTag).not.toContain('data-inspector-layout="page"');
    expect(deriveAgentWorkbenchRegionState({
      workflowAvailable: false,
      compact: false,
      confirmationOpen: false,
    })).toEqual({ canvasInert: false, sidebarInert: false, agentInert: false });
  });

  it("keeps the Agent mounted while lazily switching the active sidebar tool", () => {
    const agent = renderShell(true, false, "agent");
    const library = renderShell(true, false, "library");

    expect(agent.match(/data-agent-probe/g)).toHaveLength(1);
    expect(agent).not.toContain("data-library-probe");
    expect(library.match(/data-agent-probe/g)).toHaveLength(1);
    expect(library.match(/data-library-probe/g)).toHaveLength(1);
    expect(deriveAgentWorkbenchRegionState({
      workflowAvailable: true,
      compact: false,
      mobileView: "canvas",
      confirmationOpen: false,
      activeSidebarTool: "library",
    })).toEqual({ canvasInert: false, sidebarInert: false, agentInert: true });
  });
});
