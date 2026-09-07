import { describe, expect, it, beforeEach } from "vitest";
import { QueryClient } from "@tanstack/react-query";

import {
  activeMerchantId,
  applyMerchantSwitchBoundary,
  bindMerchantGeneration,
  getMerchantGeneration,
  isCurrentMerchantGeneration,
  resetMerchantGenerationForTests,
} from "./merchantBoundary";
import type { SessionState } from "./types";

describe("merchantBoundary", () => {
  beforeEach(() => {
    resetMerchantGenerationForTests(0);
  });

  it("reads the first active membership merchant", () => {
    const session: SessionState = {
      authenticated: true,
      access_required: false,
      memberships: [
        { merchant_id: "m-revoked", merchant_name: "旧", role: "owner", status: "revoked" },
        { merchant_id: "m-active", merchant_name: "现", role: "editor", status: "active" },
      ],
    };
    expect(activeMerchantId(session)).toBe("m-active");
    expect(activeMerchantId(undefined)).toBe("");
  });

  it("clears merchant-scoped queries and bumps generation on switch", () => {
    const client = new QueryClient();
    client.setQueryData(["session"], { authenticated: true });
    client.setQueryData(["products"], [{ id: "p1" }]);
    client.setQueryData(["graph-runs", "p1"], { items: [] });
    let closed = 0;

    const before = getMerchantGeneration();
    const next = applyMerchantSwitchBoundary(client, {
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

  it("drops late callbacks after merchant generation bumps", () => {
    const gen = getMerchantGeneration();
    const seen: string[] = [];
    const handler = bindMerchantGeneration(gen, (value: string) => {
      seen.push(value);
    });

    handler("before");
    expect(seen).toEqual(["before"]);

    applyMerchantSwitchBoundary(new QueryClient());
    expect(isCurrentMerchantGeneration(gen)).toBe(false);
    handler("late");
    expect(seen).toEqual(["before"]);
  });
});
