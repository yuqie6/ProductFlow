import { beforeEach, describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";

import {
  accountIdentity,
  applyAccountSwitchBoundary,
  bindAccountGeneration,
  getAccountGeneration,
  isCurrentAccountGeneration,
  ownMerchantId,
  resetAccountGenerationForTests,
  sameAccountIdentity,
} from "./accountBoundary";
import type { SessionState } from "./types";

const ordinarySession: SessionState = {
  authenticated: true, preferences: { locale: "zh-CN", theme: "system" },
  access_required: false,
  user: { id: "user-1", email: "user@example.com", display_name: "User", is_operator: false },
  merchant: { id: "merchant-1", name: "Shop", status: "active" },
};

describe("accountBoundary", () => {
  beforeEach(() => {
    resetAccountGenerationForTests(0);
  });

  it("reads the direct merchant and includes user identity", () => {
    expect(accountIdentity(ordinarySession)).toEqual({ userId: "user-1", merchantId: "merchant-1" });
    expect(ownMerchantId(ordinarySession)).toBe("merchant-1");
    expect(accountIdentity({
      ...ordinarySession,
      user: { ...ordinarySession.user!, id: "operator-1", is_operator: true },
      merchant: null,
    })).toEqual({ userId: "operator-1", merchantId: "" });
    expect(accountIdentity({ authenticated: false, access_required: true })).toBeNull();
  });

  it("treats different users with the same merchant as different accounts", () => {
    expect(sameAccountIdentity(
      { userId: "user-1", merchantId: "merchant-1" },
      { userId: "user-2", merchantId: "merchant-1" },
    )).toBe(false);
  });

  it("clears account-scoped queries and closes subscriptions on identity change", () => {
    const client = new QueryClient();
    client.setQueryData(["session"], { authenticated: true });
    client.setQueryData(["products"], [{ id: "p1" }]);
    client.setQueryData(["graph-runs", "p1"], { items: [] });
    let closed = 0;

    const before = getAccountGeneration();
    const next = applyAccountSwitchBoundary(client, {
      onInvalidateSubscriptions: () => {
        closed += 1;
      },
    });

    expect(next).toBe(before + 1);
    expect(client.getQueryData(["session"])).toEqual({ authenticated: true });
    expect(client.getQueryData(["products"])).toBeUndefined();
    expect(client.getQueryData(["graph-runs", "p1"])).toBeUndefined();
    expect(closed).toBe(1);
  });

  it("drops late callbacks after the account generation changes", () => {
    const generation = getAccountGeneration();
    const seen: string[] = [];
    const handler = bindAccountGeneration(generation, (value: string) => {
      seen.push(value);
    });

    handler("before");
    expect(seen).toEqual(["before"]);

    applyAccountSwitchBoundary(new QueryClient());
    expect(isCurrentAccountGeneration(generation)).toBe(false);
    handler("late");
    expect(seen).toEqual(["before"]);
  });
});
