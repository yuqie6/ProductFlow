import { describe, expect, it } from "vitest";

import {
  getRegisteredAgentPageContext,
  registerAgentPageContext,
} from "./agentPageContext";
import type { AgentPageContextSnapshotInput } from "./types";

function context(route: string): AgentPageContextSnapshotInput {
  return {
    route,
    page_type: "media_library",
    product_id: null,
    workflow_id: null,
    selected_asset_ids: [],
    visible_asset_ids: [],
    filters: {},
    workflow_revision: null,
    library_revision: null,
    captured_at: "2026-08-18T00:00:00.000Z",
  };
}

describe("agent page context registry", () => {
  it("publishes the latest page context and removes it on owner cleanup", () => {
    const cleanup = registerAgentPageContext("page-a", context("/media-library"));

    expect(getRegisteredAgentPageContext()?.route).toBe("/media-library");

    cleanup();

    expect(getRegisteredAgentPageContext()).toBeNull();
  });

  it("does not let an old page cleanup remove a newer page context", () => {
    const cleanupA = registerAgentPageContext("page-a", context("/media-library"));
    const cleanupB = registerAgentPageContext("page-b", context("/products"));

    cleanupA();

    expect(getRegisteredAgentPageContext()?.route).toBe("/products");

    cleanupB();
  });
});
