import { describe, expect, it } from "vitest";

import { canAccessOpsSettings, isWorkingMerchantSuspended } from "./opsAccess";
import type { SessionState } from "./types";

describe("opsAccess", () => {
  it("allows operator when access is required", () => {
    const session: SessionState = {
      authenticated: true,
      access_required: true,
      user: { id: "u1", email: "op@t.local", display_name: "Op", is_operator: true },
    };
    expect(canAccessOpsSettings(session)).toBe(true);
  });

  it("denies merchant editor when access is required", () => {
    const session: SessionState = {
      authenticated: true,
      access_required: true,
      user: { id: "u2", email: "ed@t.local", display_name: "Ed", is_operator: false },
    };
    expect(canAccessOpsSettings(session)).toBe(false);
  });

  it("detects suspended working merchant", () => {
    const session: SessionState = {
      authenticated: true,
      access_required: true,
      memberships: [
        {
          merchant_id: "m1",
          merchant_name: "停用商",
          role: "editor",
          status: "active",
          merchant_status: "suspended",
        },
      ],
    };
    expect(isWorkingMerchantSuspended(session)).toBe(true);
  });
});
