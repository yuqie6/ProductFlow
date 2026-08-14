import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import {
  AgentWorkbenchShell,
  deriveAgentWorkbenchRegionState,
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

describe("AgentWorkbenchShell", () => {
  it("keeps exactly one mounted canvas slot and Agent slot in both layout modes", () => {
    const agentOnly = renderShell(false);
    const canvasSidebar = renderShell(true);

    expect(agentOnly).toContain('data-workbench-layout="agent-only"');
    expect(canvasSidebar).toContain('data-workbench-layout="canvas-sidebar"');
    for (const markup of [agentOnly, canvasSidebar]) {
      expect(markup.match(/data-agent-workbench-agent-slot/g)).toHaveLength(1);
      expect(markup.match(/data-agent-probe/g)).toHaveLength(1);
      expect(markup.match(/data-agent-workbench-canvas-slot/g)).toHaveLength(1);
      expect(markup.match(/data-canvas-probe/g)).toHaveLength(1);
    }
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

  it("uses inert visibility switching only on compact layouts", () => {
    expect(deriveAgentWorkbenchRegionState({
      workflowAvailable: true,
      compact: true,
      mobileView: "canvas",
      confirmationOpen: false,
    })).toEqual({ canvasInert: false, sidebarInert: true, agentInert: true });
    expect(deriveAgentWorkbenchRegionState({
      workflowAvailable: true,
      compact: true,
      mobileView: "agent",
      confirmationOpen: false,
    })).toEqual({ canvasInert: true, sidebarInert: false, agentInert: false });
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
