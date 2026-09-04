import { describe, expect, it } from "vitest";

import {
  applyServerToNodeDraft,
  createNodeDraftSession,
  flushNodeDraft,
  isVersionConflictStatus,
  nodeDraftSaveError,
  updateNodeDraft,
} from "./useNodeDraftAutosave";

describe("node draft baseline", () => {
  it("keeps the original edit version when only a sibling revision advances", () => {
    const session = createNodeDraftSession({ title: "base" }, 3);
    updateNodeDraft(session, { title: "user" });
    const result = applyServerToNodeDraft(session, { title: "base" }, 9, "冲突");
    expect(result.conflict).toBe(false);
    expect(session.editVersion).toBe(3);
    expect(session.draft).toEqual({ title: "user" });
  });

  it("keeps the dirty draft and original baseline when this node changed on the server", () => {
    const session = createNodeDraftSession({ title: "base" }, 3);
    updateNodeDraft(session, { title: "user" });
    const result = applyServerToNodeDraft(session, { title: "adopted" }, 8, "冲突");
    expect(result.conflict).toBe(true);
    expect(session.editVersion).toBe(3);
    expect(session.draft).toEqual({ title: "user" });
    expect(session.blocked?.error.message).toBe("冲突");
  });
});

describe("flushNodeDraft", () => {
  it("sends the draft baseline, then keeps later typing after the first save returns", async () => {
    const session = createNodeDraftSession("base", 3);
    updateNodeDraft(session, "first");
    let finishFirst!: (value: { edit_version: number }) => void;
    const first = new Promise<{ edit_version: number }>((resolve) => {
      finishFirst = resolve;
    });
    const versions: number[] = [];
    const drafts: string[] = [];
    const flushing = flushNodeDraft(session, {
      save: async (draft, expectedEditVersion) => {
        drafts.push(draft);
        versions.push(expectedEditVersion);
        if (drafts.length === 1) return first;
        return { edit_version: expectedEditVersion + 1 };
      },
      normalize: (value) => value,
      validate: () => null,
      versionConflictMessage: "冲突",
    });
    updateNodeDraft(session, "second");
    finishFirst({ edit_version: 4 });
    await flushing;
    expect(drafts).toEqual(["first", "second"]);
    expect(versions).toEqual([3, 4]);
    expect(session.draft).toBe("second");
    expect(session.baseline).toBe("second");
    expect(session.editVersion).toBe(5);
  });

  it("keeps the user draft and original baseline when save returns 409", async () => {
    const session = createNodeDraftSession({ title: "base" }, 3);
    updateNodeDraft(session, { title: "user" });
    await expect(flushNodeDraft(session, {
      save: async (_draft, expectedEditVersion) => {
        expect(expectedEditVersion).toBe(3);
        throw Object.assign(new Error("stale"), { status: 409 });
      },
      normalize: (value) => value,
      validate: () => null,
      versionConflictMessage: "冲突",
    })).rejects.toThrow("冲突");
    expect(session.draft).toEqual({ title: "user" });
    expect(session.editVersion).toBe(3);
    expect(session.blocked).toBeTruthy();
  });

  it("does not auto-retry a blocked save, and an explicit retry still uses the original baseline", async () => {
    const session = createNodeDraftSession("base", 3);
    updateNodeDraft(session, "user");
    session.blocked = { sequence: session.sequence, error: new Error("冲突") };
    await expect(flushNodeDraft(session, {
      save: async () => {
        throw new Error("should not save");
      },
      normalize: (value) => value,
      validate: () => null,
      versionConflictMessage: "冲突",
    })).rejects.toThrow("冲突");

    const versions: number[] = [];
    await expect(flushNodeDraft(session, {
      save: async (_draft, expectedEditVersion) => {
        versions.push(expectedEditVersion);
        throw Object.assign(new Error("stale"), { status: 409 });
      },
      normalize: (value) => value,
      validate: () => null,
      versionConflictMessage: "冲突",
      forceRetry: true,
    })).rejects.toThrow("冲突");
    expect(versions).toEqual([3]);
    expect(session.draft).toBe("user");
  });
});

describe("nodeDraftSaveError", () => {
  it("maps HTTP 409 to the inspector conflict copy", () => {
    expect(isVersionConflictStatus({ status: 409 })).toBe(true);
    expect(nodeDraftSaveError({ status: 409 }, "冲突").message).toBe("冲突");
    expect(nodeDraftSaveError(new Error("校验失败"), "冲突").message).toBe("校验失败");
  });
});
