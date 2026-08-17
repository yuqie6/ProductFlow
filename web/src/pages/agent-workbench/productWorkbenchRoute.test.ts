import { describe, expect, it } from "vitest";

import {
  agentProductIntakeResumePath,
  productWorkbenchRouteTarget,
  type ProductWorkbenchRouteInput,
} from "./productWorkbenchRoute";

describe("productWorkbenchRouteTarget", () => {
  it("keeps an Agent product on v2 when no workflow has been materialized yet", () => {
    const bootstrap = {
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

  it("keeps an archive rebuild in the Agent workbench instead of creation intake", () => {
    const bootstrap = {
      active_workflow: null,
      workflow_draft: {
        intake: null,
        current_revision: null,
        recipe_seed: null,
        legacy_archive_seed: {
          id: "seed-1",
          workflow_draft_id: "draft-1",
          product_id: "product-1",
          archive_kind: "workflow",
          archive_id: "archive-1",
          archive_title: "旧工作流",
          archive_status: null,
          source_product_id: "product-1",
          source_profile: "legacy",
          archive_schema_version: 1,
          payload_sha256: "a".repeat(64),
          counts: {},
          schema_version: 1,
          created_at: "2026-08-15T00:00:00Z",
        },
      },
    } satisfies ProductWorkbenchRouteInput;

    expect(productWorkbenchRouteTarget(bootstrap)).toBe("agent_v2");
  });

  it("returns an unfinished empty draft to its creation workspace", () => {
    const bootstrap = {
      active_workflow: null,
      workflow_draft: {
        intake: null,
        current_revision: null,
        recipe_seed: null,
        legacy_archive_seed: null,
      },
    } satisfies ProductWorkbenchRouteInput;

    expect(productWorkbenchRouteTarget(bootstrap)).toBe("agent_intake");
    expect(agentProductIntakeResumePath("conversation/1", "session/1")).toBe(
      "/products/new?workspace=conversation%2F1&agent_session_id=session%2F1",
    );
    expect(agentProductIntakeResumePath("conversation/1", "session/1", "task/1")).toBe(
      "/products/new?workspace=conversation%2F1&agent_session_id=session%2F1&agent_task_id=task%2F1",
    );
  });
});
