import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("credential exchange errors", () => {
  it("sends registration proofs in the body and preserves server challenge metadata", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ challenge_id: "challenge", retry_after_seconds: 60 })));
    vi.stubGlobal("fetch", fetchMock);
    await expect(api.requestRegistrationCode("member@example.com")).resolves.toEqual({ challenge_id: "challenge", retry_after_seconds: 60 });
    expect(fetchMock.mock.calls[0][0]).toContain("/api/auth/registration-code");
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ email: "member@example.com" });
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ ok: true })));
    const proof = { email: "member@example.com", challenge_id: "challenge", code: "123456", password: "password", merchant_name: "Merchant" };
    await api.registerAccount(proof);
    expect(fetchMock.mock.calls[1][0]).toContain("/api/auth/register");
    expect(fetchMock.mock.calls[1][0]).not.toContain("123456");
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual(proof);
  });
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
