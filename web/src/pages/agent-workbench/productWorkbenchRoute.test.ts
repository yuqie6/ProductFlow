import { describe, expect, it } from "vitest";

import type { AgentWorkbenchBootstrap } from "../../lib/types";
import { productWorkbenchRouteTarget } from "./productWorkbenchRoute";

describe("productWorkbenchRouteTarget", () => {
  it("keeps an Agent product on v2 when no workflow has been materialized yet", () => {
    const bootstrap = {
      mode: "agent_v2",
      active_workflow: null,
    } as AgentWorkbenchBootstrap;

    expect(productWorkbenchRouteTarget(bootstrap)).toBe("agent_v2");
  });

  it("loads legacy only for a persisted v1 workflow", () => {
    const withWorkflow = {
      mode: "legacy_v1",
      has_existing_v1_workflow: true,
    } as AgentWorkbenchBootstrap;
    const withoutWorkflow = {
      mode: "legacy_v1",
      has_existing_v1_workflow: false,
    } as AgentWorkbenchBootstrap;

    expect(productWorkbenchRouteTarget(withWorkflow)).toBe("legacy_v1");
    expect(productWorkbenchRouteTarget(withoutWorkflow)).toBe("legacy_empty");
  });
});
