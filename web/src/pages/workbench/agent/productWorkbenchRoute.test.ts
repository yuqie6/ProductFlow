import { describe, expect, it } from "vitest";

import { ApiError } from "../../../lib/api";
import type { GraphProjection } from "../../../lib/types";
import {
  agentProductIntakeResumePath,
  productWorkbenchRouteTarget,
  resolveProductWorkbenchSurface,
  type ProductWorkbenchRouteInput,
} from "./productWorkbenchRoute";

describe("productWorkbenchRouteTarget", () => {
  it("keeps an Agent product on the workbench when no workflow has been persisted yet", () => {
    const bootstrap = {
      workflow_draft: {
        intake: { schema_version: 1, image_types: [], reference_asset_ids: [] },
        current_revision: null,
        recipe_seed: null,
        legacy_archive_seed: null,
      },
    } satisfies ProductWorkbenchRouteInput;

    expect(productWorkbenchRouteTarget(bootstrap)).toBe("agent");
  });

  it("keeps an archive rebuild in the Agent workbench instead of creation intake", () => {
    const bootstrap = {
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

    expect(productWorkbenchRouteTarget(bootstrap)).toBe("agent");
  });

  it("returns an unfinished empty draft to its creation workspace", () => {
    const bootstrap = {
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

describe("resolveProductWorkbenchSurface", () => {
  const graph = { id: "graph-1" } as GraphProjection;
  const agent = {
    workflow_draft: {
      intake: { schema_version: 1, image_types: [], reference_asset_ids: [] },
      current_revision: { id: "revision-1" },
      recipe_seed: null,
      legacy_archive_seed: null,
    },
  } satisfies ProductWorkbenchRouteInput;
  const intake = {
    workflow_draft: {
      intake: null,
      current_revision: null,
      recipe_seed: null,
      legacy_archive_seed: null,
    },
  } satisfies ProductWorkbenchRouteInput;

  it("keeps the Agent conversation when a v3 graph already exists", () => {
    expect(resolveProductWorkbenchSurface({
      graph,
      graphPending: false,
      graphError: null,
      agent,
      agentPending: false,
      agentError: null,
    })).toEqual({ kind: "agent", bootstrap: agent });
  });

  it("uses the graph workbench only when the product has no Agent workspace", () => {
    expect(resolveProductWorkbenchSurface({
      graph,
      graphPending: false,
      graphError: null,
      agentPending: false,
      agentError: new ApiError(409, "商品还没有 Agent 工作区"),
    })).toEqual({ kind: "graph", graph });
  });

  it("returns unfinished empty drafts to creation intake", () => {
    expect(resolveProductWorkbenchSurface({
      graphPending: false,
      graphError: new ApiError(404, "商品工作流不存在"),
      agent: intake,
      agentPending: false,
      agentError: null,
    }).kind).toBe("intake");
  });

  it("keeps an empty draft on the Agent workbench when a v3 graph already exists", () => {
    expect(resolveProductWorkbenchSurface({
      graph,
      graphPending: false,
      graphError: null,
      agent: intake,
      agentPending: false,
      agentError: null,
    })).toEqual({ kind: "agent", bootstrap: intake });
  });

  it("waits until graph and Agent queries settle", () => {
    expect(resolveProductWorkbenchSurface({
      graph,
      graphPending: false,
      graphError: null,
      agentPending: true,
      agentError: null,
    }).kind).toBe("loading");
  });
});
