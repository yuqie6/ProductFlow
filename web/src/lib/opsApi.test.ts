import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("explicit merchant operations API", () => {
  it("encodes merchant and product identities in every target path and keeps opaque cursors opaque", async () => {
    const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ items: [] })));
    vi.stubGlobal("fetch", fetch);
    await api.listOpsMerchants({ page: 2, page_size: 20, q: "A & B", status: "suspended" });
    expect(fetch.mock.calls[0][0]).toBe("/api/ops/merchants?page=2&page_size=20&q=A+%26+B&status=suspended");
    await api.getOpsMerchant("merchant/a");
    expect(fetch.mock.calls[1][0]).toBe("/api/ops/merchants/merchant%2Fa");
    await api.listOpsProducts("merchant/a", { page: 1, page_size: 20, q: "lamp", sort: "name_asc" });
    expect(fetch.mock.calls[2][0]).toBe("/api/ops/merchants/merchant%2Fa/products?page=1&page_size=20&q=lamp&sort=name_asc");
    await api.listOpsProductAssets("merchant/a", "product/b", { after: "opaque+/=", limit: 20 });
    expect(fetch.mock.calls[3][0]).toBe("/api/ops/merchants/merchant%2Fa/products/product%2Fb/image-assets?directory_kind=all&sort=created_desc&limit=20&after=opaque%2B%2F%3D");
    await api.listOpsTasks("merchant/a", { page: 3, page_size: 20, product_id: "product/b" });
    expect(fetch.mock.calls[4][0]).toBe("/api/ops/merchants/merchant%2Fa/tasks?page=3&page_size=20&product_id=product%2Fb");
    await api.listOpsActions("merchant/a", { page: 1, page_size: 20, product_id: "product/b" });
    expect(fetch.mock.calls[5][0]).toBe("/api/ops/merchants/merchant%2Fa/actions?page=1&page_size=20&product_id=product%2Fb");
  });
  it("writes versioned facts without graph adoption and retains the caller's adjustment key on retry", async () => {
    const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ ok: true })));
    vi.stubGlobal("fetch", fetch);
    const facts = { expected_fact_version: 3, expected_fact_set_version_id: "version-3", name: "Lamp", category: null, price: null, source_note: null, facts: [{ key: "material", value: "steel", source_type: "user" as const }] };
    await api.updateOpsProductFacts("merchant", "product", facts);
    expect(fetch.mock.calls[0][0]).toBe("/api/ops/merchants/merchant/products/product/facts");
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(facts);
    expect(JSON.parse(fetch.mock.calls[0][1].body)).not.toHaveProperty("update_node_ids");
    const adjustment = { idempotency_key: "one-action", delta_units: -12, reason: "Correction" };
    await api.adjustOpsQuota("merchant", adjustment);
    await api.adjustOpsQuota("merchant", adjustment);
    expect(fetch.mock.calls[1][1].body).toBe(fetch.mock.calls[2][1].body);
    expect(JSON.parse(fetch.mock.calls[2][1].body)).toEqual(adjustment);
    fetch.mockImplementation(async () => new Response(null, { status: 204 }));
    await expect(api.deleteOpsProduct("merchant", "product")).resolves.toBeUndefined();
    expect(fetch.mock.calls[3][1].method).toBe("DELETE");
  });
});
