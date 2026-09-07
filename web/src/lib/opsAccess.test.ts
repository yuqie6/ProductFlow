import { describe, expect, it } from "vitest";

import { canAccessOpsSettings, isWorkingMerchantSuspended } from "./opsAccess";
import type { SessionState } from "./types";

describe("opsAccess", () => {
  it("allows operator when access is required", () => {
    const session: SessionState = {
      authenticated: true,
      access_required: true,
      user: { id: "u1", email: "op@t.local", display_name: "Op", is_operator: true },
      merchant: null,
    };
    expect(canAccessOpsSettings(session)).toBe(true);
  });

  it("denies ordinary account even when the general access gate is off", () => {
    const session: SessionState = {
      authenticated: true,
      access_required: false,
      user: { id: "u2", email: "ed@t.local", display_name: "Ed", is_operator: false },
      merchant: null,
    };
    expect(canAccessOpsSettings(session)).toBe(false);
  });

  it("detects suspended working merchant", () => {
    const session: SessionState = {
      authenticated: true,
      access_required: true,
      user: { id: "u3", email: "owner@t.local", display_name: "Owner", is_operator: true },
      merchant: { id: "m1", name: "停用商", status: "suspended" },
    };
    expect(isWorkingMerchantSuspended(session)).toBe(true);
  });
});
