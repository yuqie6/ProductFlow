import { describe, expect, it, vi } from "vitest";

import { ApiError } from "../../lib/api";
import type { AgentProductWorkspaceSnapshot } from "../../lib/types";
import {
  isAmbiguousFinalizeError,
  parsePendingDraft,
  resolveWorkspaceRestorationId,
  submitAgentProductIntake,
} from "./intakeSubmission";

function workspace(conversationId: string, intakeFinalized = false): AgentProductWorkspaceSnapshot {
  const productId = `product-${conversationId}`;
  const draftId = `draft-${conversationId}`;
  return {
    created: true,
    intake_finalized: intakeFinalized,
    product: {
      id: productId,
      name: "Sample product",
      category: null,
      price: null,
      source_note: null,
      cover_image_asset_id: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    created_assets: [],
    workflow_draft: {
      id: draftId,
      product_id: productId,
      status: "collecting",
      current_revision_id: null,
      current_revision: null,
      current_version: 0,
      revisions: [],
      intake: null,
      final_workflow_id: null,
      recipe_seed: null,
      legacy_archive_seed: null,
      limits: {
        min_image_types: 1,
        min_images_per_type: 1,
        max_images_per_type: 6,
        max_total_images: 30,
        max_reference_assets: 6,
      },
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    conversation: {
      id: conversationId,
      product_id: productId,
      workflow_draft_id: draftId,
      harness_run_id: `run-${conversationId}`,
      status: "collecting",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
  };
}

describe("submitAgentProductIntake", () => {
  it("creates once, retains the workspace before finalize, and returns the finalized workspace", async () => {
    const events: string[] = [];
    const created = workspace("conversation-1");
    const finalized = workspace("conversation-1", true);
    const createWorkspace = vi.fn(async () => {
      events.push("create");
      return created;
    });
    const retainWorkspace = vi.fn(async () => {
      events.push("retain");
    });
    const finalizeWorkspace = vi.fn(async () => {
      events.push("finalize");
      return finalized;
    });

    await expect(
      submitAgentProductIntake({
        workspace: null,
        createWorkspace,
        retainWorkspace,
        finalizeWorkspace,
      }),
    ).resolves.toBe(finalized);

    expect(createWorkspace).toHaveBeenCalledTimes(1);
    expect(retainWorkspace).toHaveBeenCalledWith(created);
    expect(finalizeWorkspace).toHaveBeenCalledWith(created);
    expect(events).toEqual(["create", "retain", "finalize"]);
  });

  it("leaves a retained workspace reusable after finalize failure and retry creates nothing", async () => {
    const retained = workspace("conversation-2");
    let reusableWorkspace: AgentProductWorkspaceSnapshot | null = null;
    const createWorkspace = vi.fn(async () => retained);
    const retainWorkspace = vi.fn((created: AgentProductWorkspaceSnapshot) => {
      reusableWorkspace = created;
    });
    const finalizeWorkspace = vi
      .fn<(target: AgentProductWorkspaceSnapshot) => Promise<AgentProductWorkspaceSnapshot>>()
      .mockRejectedValueOnce(new Error("finalize failed"))
      .mockResolvedValueOnce(workspace("conversation-2", true));

    await expect(
      submitAgentProductIntake({
        workspace: null,
        createWorkspace,
        retainWorkspace,
        finalizeWorkspace,
      }),
    ).rejects.toThrow("finalize failed");
    expect(reusableWorkspace).toBe(retained);

    await submitAgentProductIntake({
      workspace: reusableWorkspace,
      createWorkspace,
      retainWorkspace,
      finalizeWorkspace,
    });

    expect(createWorkspace).toHaveBeenCalledTimes(1);
    expect(retainWorkspace).toHaveBeenCalledTimes(1);
    expect(finalizeWorkspace).toHaveBeenCalledTimes(2);
  });

  it("uses a restored workspace without creating or retaining it again", async () => {
    const restored = workspace("conversation-3");
    const finalized = workspace("conversation-3", true);
    const createWorkspace = vi.fn(async () => workspace("unexpected"));
    const retainWorkspace = vi.fn();
    const finalizeWorkspace = vi.fn(async () => finalized);

    await expect(
      submitAgentProductIntake({
        workspace: restored,
        createWorkspace,
        retainWorkspace,
        finalizeWorkspace,
      }),
    ).resolves.toBe(finalized);

    expect(createWorkspace).not.toHaveBeenCalled();
    expect(retainWorkspace).not.toHaveBeenCalled();
    expect(finalizeWorkspace).toHaveBeenCalledWith(restored);
  });
});

describe("finalize error ambiguity", () => {
  it("treats network and 5xx failures as ambiguous", () => {
    expect(isAmbiguousFinalizeError(new TypeError("Failed to fetch"))).toBe(true);
    expect(isAmbiguousFinalizeError(new ApiError(500, "server error"))).toBe(true);
    expect(isAmbiguousFinalizeError(new ApiError(503, "unavailable"))).toBe(true);
  });

  it("treats explicit 4xx responses as proven rejection", () => {
    expect(isAmbiguousFinalizeError(new ApiError(400, "invalid intake"))).toBe(false);
    expect(isAmbiguousFinalizeError(new ApiError(409, "conflict"))).toBe(false);
    expect(isAmbiguousFinalizeError(new ApiError(499, "rejected"))).toBe(false);
  });
});

describe("pending draft recovery", () => {
  it("prefers the URL workspace over the pending conversation", () => {
    expect(
      resolveWorkspaceRestorationId(" url-conversation ", {
        name: "Sample product",
        idempotencyKey: "draft-key",
        conversationId: "pending-conversation",
      }),
    ).toBe("url-conversation");
  });

  it("restores a pending conversation and preserves old records without conversationId", () => {
    const current = parsePendingDraft(
      JSON.stringify({
        name: "Current product",
        idempotencyKey: "current-key",
        conversationId: "conversation-4",
      }),
    );
    const legacy = parsePendingDraft(
      JSON.stringify({ name: "Legacy product", idempotencyKey: "legacy-key" }),
    );

    expect(resolveWorkspaceRestorationId(null, current)).toBe("conversation-4");
    expect(legacy).toEqual({ name: "Legacy product", idempotencyKey: "legacy-key" });
    expect(resolveWorkspaceRestorationId(null, legacy)).toBe("");
  });
});
