import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { applyAccountSwitchBoundary, resetAccountGenerationForTests } from "../../lib/accountBoundary";
import { PreferencesProvider } from "../../lib/preferences";
import type { ProductFactsResponse } from "../../lib/types";
import { OpsProductPage } from "../OpsProductPage";
import { OpsFactsEditor } from "./OpsFactsEditor";
import { OpsQuota } from "./OpsQuota";

const fixtures = vi.hoisted(() => ({ mutations: [] as Array<Record<string, unknown>>, client: null as QueryClient | null }));
const facts: ProductFactsResponse = { product: { id: "p", name: "Private product", category: null, price: null, source_note: null, cover_image_asset_id: null, created_at: "2026-09-08T00:00:00Z", updated_at: "2026-09-08T00:00:00Z" }, fact_set: null };
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQueryClient: () => fixtures.client,
    useMutation: (options: Record<string, unknown>) => {
      fixtures.mutations.push(options);
      return { isPending: false, isSuccess: false, isError: false, error: null, reset: vi.fn(), mutate: vi.fn() };
    },
    useQuery: ({ queryKey }: { queryKey: unknown[] }) => ({
      isSuccess: true, isPending: false, isError: false,
      data: queryKey[0] === "session" ? { authenticated: true, preferences: { locale: "zh-CN", theme: "system" }, user: { id: "operator", is_operator: true }, merchant: null }
        : queryKey[0] === "ops-merchant" ? { id: "m", name: "Merchant", status: "active" }
        : queryKey[0] === "ops-facts" ? facts
        : queryKey[0] === "ops-quota" ? { merchant_id: "m", available_units: 10, reserved_units: 0 }
        : { items: [], total: 0, page: 1, page_size: 20 },
    }),
  };
});

beforeEach(() => {
  resetAccountGenerationForTests();
  fixtures.mutations.length = 0;
  fixtures.client = new QueryClient();
});

describe("late operations mutation responses", () => {
  it.each(["facts", "quota", "delete"])("does not restore cache or invalidate the new account after a late %s response", async (kind) => {
    const client = fixtures.client!;
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const content = kind === "facts" ? <OpsFactsEditor merchantId="m" productId="p" initial={facts} />
      : kind === "quota" ? <OpsQuota merchantId="m" />
      : <Routes><Route path="/ops/merchants/:merchantId/products/:productId" element={<OpsProductPage />} /></Routes>;
    renderToStaticMarkup(<MemoryRouter initialEntries={["/ops/merchants/m/products/p"]}><PreferencesProvider>{content}</PreferencesProvider></MemoryRouter>);
    const mutation = fixtures.mutations[1];
    client.setQueryData(["ops-facts", "m", "p"], facts);
    applyAccountSwitchBoundary(client);
    client.setQueryData(["new-account"], { private: "new account" });
    const response = kind === "quota" ? { merchant_id: "m", available_units: 99, reserved_units: 0 } : facts;
    if (typeof mutation.onSuccess !== "function") throw new Error("Missing success callback");
    await Reflect.apply(mutation.onSuccess, null, [response]);
    if (typeof mutation.onSettled === "function") await Reflect.apply(mutation.onSettled, null, [response]);
    expect(client.getQueryData(["ops-facts", "m", "p"])).toBeUndefined();
    expect(client.getQueryData(["ops-quota", "m"])).toBeUndefined();
    expect(client.getQueryData(["new-account"])).toEqual({ private: "new account" });
    expect(invalidate).not.toHaveBeenCalled();
  });
});
