import { describe, expect, it, vi } from "vitest";

import type { LegacyArchiveKind, LegacyArchiveListItem } from "../lib/types";
import {
  getOrCreateLegacyArchiveRebuildKey,
  legacyArchiveRebuildPath,
  resolveLegacyArchiveRebuildTarget,
} from "./LegacyHistoryPage";

function archiveItem(kind: LegacyArchiveKind, productId: string | null): LegacyArchiveListItem {
  return {
    kind,
    id: `${kind}-archive-1`,
    source_id: `${kind}-source-1`,
    source_key: null,
    product_id: productId,
    product_name: productId ? "测试商品" : null,
    title: "旧设计",
    description: null,
    source_status: null,
    archive_schema_version: 1,
    payload_sha256: "a".repeat(64),
    source_updated_at: null,
    created_at: "2026-08-15T10:00:00Z",
    counts: {},
  };
}

describe("legacy archive Agent rebuild decisions", () => {
  it("uses the original product for product-bound workflow and Agent archives", () => {
    expect(resolveLegacyArchiveRebuildTarget(archiveItem("workflow", "product-1"))).toEqual({
      mode: "direct",
      targetProductId: "product-1",
    });
    expect(resolveLegacyArchiveRebuildTarget(archiveItem("canvas_agent_thread", "product-2"))).toEqual({
      mode: "direct",
      targetProductId: "product-2",
    });
  });

  it("requires explicit product selection only for user templates", () => {
    expect(resolveLegacyArchiveRebuildTarget(archiveItem("user_template", null))).toEqual({
      mode: "select_product",
    });
    expect(resolveLegacyArchiveRebuildTarget(archiveItem("workflow", null))).toEqual({ mode: "unavailable" });
  });

  it("reuses the same idempotency key for a retry and isolates different targets", () => {
    const keys = new Map<string, string>();
    const createId = vi.fn().mockReturnValueOnce("request-1").mockReturnValueOnce("request-2");
    const archive = archiveItem("user_template", null);

    expect(getOrCreateLegacyArchiveRebuildKey(keys, archive, "product-1", createId)).toBe(
      "legacy-rebuild:request-1",
    );
    expect(getOrCreateLegacyArchiveRebuildKey(keys, archive, "product-1", createId)).toBe(
      "legacy-rebuild:request-1",
    );
    expect(getOrCreateLegacyArchiveRebuildKey(keys, archive, "product-2", createId)).toBe(
      "legacy-rebuild:request-2",
    );
    expect(createId).toHaveBeenCalledTimes(2);
  });

  it("returns the exact Agent session route created by a rebuild", () => {
    expect(legacyArchiveRebuildPath("product/1", "session/1")).toBe(
      "/products/product%2F1?agent_session_id=session%2F1",
    );
    expect(legacyArchiveRebuildPath("product/1", null)).toBe("/products/product%2F1");
  });
});
