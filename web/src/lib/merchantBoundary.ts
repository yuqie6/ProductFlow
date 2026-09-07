/** 商家切换时的 cache / SSE 边界：整表清空 + 世代号丢弃迟到回调。 */

import type { QueryClient } from "@tanstack/react-query";

import type { SessionState } from "./types";

let merchantGeneration = 0;

/** 当前商家世代；订阅建立时捕获，回调时比对以丢弃切换后的迟到响应。 */
export function getMerchantGeneration(): number {
  return merchantGeneration;
}

/** 商家切换时递增；返回新世代。 */
export function bumpMerchantGeneration(): number {
  merchantGeneration += 1;
  return merchantGeneration;
}

/** 从会话取当前工作商家（首个有效 membership）；无则空串。 */
export function activeMerchantId(session: SessionState | undefined | null): string {
  const memberships = session?.memberships;
  if (!memberships?.length) return "";
  const active = memberships.find((item) => item.status === "active") ?? memberships[0];
  return active?.merchant_id?.trim() ?? "";
}

/**
 * 商家切换：清空 React Query 缓存（保留 session），并 bump 世代以便 SSE/迟到回调失效。
 * 可选 onInvalidateSubscriptions 关闭 EventSource 池。
 */
export function applyMerchantSwitchBoundary(
  queryClient: QueryClient,
  options?: { onInvalidateSubscriptions?: () => void },
): number {
  const next = bumpMerchantGeneration();
  queryClient.removeQueries({
    predicate: (query) => {
      const key = query.queryKey;
      return !(Array.isArray(key) && key[0] === "session");
    },
  });
  options?.onInvalidateSubscriptions?.();
  return next;
}

/** 仅在回调仍属建立订阅时的商家世代时调用 handler。 */
export function bindMerchantGeneration<T>(
  generation: number,
  handler: (value: T) => void,
): (value: T) => void {
  return (value) => {
    if (generation !== getMerchantGeneration()) return;
    handler(value);
  };
}

export function isCurrentMerchantGeneration(generation: number): boolean {
  return generation === merchantGeneration;
}

/** 测试专用：重置世代计数。 */
export function resetMerchantGenerationForTests(value = 0): void {
  merchantGeneration = value;
}
