import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "./api";
import { ownMerchantId } from "./accountBoundary";
import {
  isQuotaEntryPriceReady,
  resolveQuotaEntryPrice,
  type QuotaEntryPriceLookup,
} from "./quotaPrice";

export type MerchantQuotaEntryPriceState = {
  merchantId: string;
  lookup: QuotaEntryPriceLookup;
  ready: boolean;
  /** True while session/merchant or price catalog is still resolving. */
  pending: boolean;
  /** True when confirm/generate must be disabled (missing merchant, load, error, or bad price). */
  blocksAction: boolean;
};

/**
 * Read merchant catalog price for one entry code (display / gate only; no ledger writes).
 */
export function useMerchantQuotaEntryPrice(entryCode: string): MerchantQuotaEntryPriceState {
  const sessionQuery = useQuery({
    queryKey: ["session"],
    queryFn: api.getSessionState,
  });
  const merchantId = ownMerchantId(sessionQuery.data);
  const quotaPriceQuery = useQuery({
    queryKey: ["merchant-quota-price", merchantId],
    queryFn: () => api.getMerchantQuotaPrice(merchantId),
    enabled: Boolean(merchantId),
    staleTime: 60_000,
  });
  const lookup = useMemo(
    () => resolveQuotaEntryPrice(quotaPriceQuery.data, entryCode),
    [entryCode, quotaPriceQuery.data],
  );
  const ready = isQuotaEntryPriceReady(lookup);
  const pending =
    sessionQuery.isLoading ||
    !merchantId ||
    quotaPriceQuery.isLoading ||
    (quotaPriceQuery.isFetching && !quotaPriceQuery.data);
  const blocksAction =
    !merchantId ||
    pending ||
    sessionQuery.isError ||
    quotaPriceQuery.isError ||
    !ready;
  return { merchantId, lookup, ready, pending, blocksAction };
}
