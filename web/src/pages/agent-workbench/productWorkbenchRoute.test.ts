import { describe, expect, it } from "vitest";

import {
  agentProductIntakeResumePath,
  productWorkbenchRouteTarget,
  type ProductWorkbenchRouteInput,
} from "./productWorkbenchRoute";

describe("productWorkbenchRouteTarget", () => {
  it("keeps an Agent product on v2 when no workflow has been materialized yet", () => {
    const bootstrap = {
      mode: "agent_v2",
      active_workflow: null,
      workflow_draft: {
        intake: { schema_version: 1, image_types: [], reference_asset_ids: [] },
        current_revision: null,
        recipe_seed: null,
        legacy_archive_seed: null,
      },
    } satisfies ProductWorkbenchRouteInput;

    expect(productWorkbenchRouteTarget(bootstrap)).toBe("agent_v2");
  });

  it("returns an unfinished empty draft to its creation workspace", () => {
    const bootstrap = {
      mode: "agent_v2",
      active_workflow: null,
      workflow_draft: {
        intake: null,
        current_revision: null,
        recipe_seed: null,
        legacy_archive_seed: null,
      },
    } satisfies ProductWorkbenchRouteInput;

    expect(productWorkbenchRouteTarget(bootstrap)).toBe("agent_intake");
    expect(agentProductIntakeResumePath("conversation/1")).toBe(
      "/products/new?workspace=conversation%2F1",
    );
  });

  it("loads legacy only for a persisted v1 workflow", () => {
    const withWorkflow = {
      mode: "legacy_v1",
      has_existing_v1_workflow: true,
    } satisfies ProductWorkbenchRouteInput;
    const withoutWorkflow = {
      mode: "legacy_v1",
      has_existing_v1_workflow: false,
    } satisfies ProductWorkbenchRouteInput;

    expect(productWorkbenchRouteTarget(withWorkflow)).toBe("legacy_v1");
    expect(productWorkbenchRouteTarget(withoutWorkflow)).toBe("legacy_empty");
  });
});
