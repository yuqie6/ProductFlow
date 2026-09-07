import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("personal account wire", () => {
  it("encodes opaque session ids and cursors without disclosing password proofs in URLs", async () => {
    const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ ok: true })));
    vi.stubGlobal("fetch", fetch);
    await api.getAccountSessions({ after: "a/b+?=", limit: 20 });
    expect(fetch.mock.calls[0][0]).toBe("/api/account/sessions?after=a%2Fb%2B%3F%3D&limit=20");
    await api.revokeAccountSession("a/b?");
    expect(fetch.mock.calls[1][0]).toBe("/api/account/sessions/a%2Fb%3F");
    expect(fetch.mock.calls[1][1].method).toBe("DELETE");
    const proof = { email: "person@example.com", challenge_id: "opaque", code: "123456", new_password: "new-password" };
    await api.confirmPasswordRecovery(proof);
    expect(fetch.mock.calls[2][0]).toBe("/api/auth/password-recovery/confirm");
    expect(JSON.parse(fetch.mock.calls[2][1].body)).toEqual(proof);
    await api.changePassword({ current_password: "old-password", new_password: "new-password" });
    expect(fetch.mock.calls[3][0]).toBe("/api/account/password");
    expect(JSON.parse(fetch.mock.calls[3][1].body)).toEqual({ current_password: "old-password", new_password: "new-password" });
  });
  it("preserves accepted recovery metadata and a merchant-less operator", async () => {
    const challenge = { challenge_id: "opaque", expires_in_seconds: 600, resend_after_seconds: 60 };
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(challenge), { status: 202 }));
    vi.stubGlobal("fetch", fetch);
    await expect(api.requestPasswordRecovery("unknown@example.com")).resolves.toEqual(challenge);
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ email: "unknown@example.com" });
    const account = { user: { id: "operator", email: "ops@example.com", display_name: "Ops", is_operator: true }, merchant: null };
    fetch.mockImplementation(async () => new Response(JSON.stringify(account)));
    await expect(api.getAccount()).resolves.toEqual(account);
    await api.updateAccount({ display_name: "New name" });
    expect(fetch.mock.calls[2][1].method).toBe("PATCH");
    expect(JSON.parse(fetch.mock.calls[2][1].body)).toEqual({ display_name: "New name" });
  });
});
