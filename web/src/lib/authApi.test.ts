import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("credential exchange errors", () => {
  it.each(["login", "bootstrap"])("preserves the %s cooldown and error detail", async (entry) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ detail: "Too many attempts" }),
      { status: 429, headers: { "Retry-After": "47" } },
    )));
    const input = { email: "user@example.com", password: "password" };
    const response = entry === "login"
      ? api.createSession(input)
      : api.bootstrapSession({ ...input, admin_key: "setup", merchant_name: "Merchant" });
    await expect(response).rejects.toMatchObject({
      status: 429, detail: "Too many attempts", retryAfterSeconds: 47,
    });
  });

  it.each(["", "-1", "1.5", "Infinity", "9007199254740992", "not-a-duration"])(
    "does not create a cooldown from invalid Retry-After %j", async (retryAfter) => {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(
        JSON.stringify({ detail: "Unavailable" }),
        { status: 503, headers: { "Retry-After": retryAfter } },
      )));
      await expect(api.createSession({ email: "user@example.com", password: "password" }))
        .rejects.toMatchObject({ status: 503, retryAfterSeconds: null });
    },
  );
});
