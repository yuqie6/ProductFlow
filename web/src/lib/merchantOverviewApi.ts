import { request } from "./api";
import type { MerchantOverview, MerchantOverviewInput } from "./types";

/** The directory loads this query with its route; shared HTTP behavior remains in api.ts. */
export function getMerchantOverview(input: MerchantOverviewInput): Promise<MerchantOverview> {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(input)) params.set(key, String(value));
  return request(`/api/v2/products/overview?${params}`);
}
