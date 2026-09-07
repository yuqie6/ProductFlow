/** 账号身份变化时的 cache / SSE 边界：清理账号数据并丢弃迟到回调。 */

import type { QueryClient } from "@tanstack/react-query";

import type { SessionState } from "./types";

export interface AccountIdentity {
  userId: string;
  merchantId: string;
}

let accountGeneration = 0;

/** 从已认证会话取得账号身份；Operator 没有自有商家时 merchantId 为空。 */
export function accountIdentity(session: SessionState | undefined | null): AccountIdentity | null {
  if (!session?.authenticated) return null;
  const userId = session.user?.id?.trim() ?? "";
  if (!userId) return null;
  return {
    userId,
    merchantId: session.merchant?.id?.trim() ?? "",
  };
}

/** 当前账号直接归属的商家；没有归属时返回空串。 */
export function ownMerchantId(session: SessionState | undefined | null): string {
  return session?.merchant?.id?.trim() ?? "";
}

export function sameAccountIdentity(
  left: AccountIdentity | null | undefined,
  right: AccountIdentity | null | undefined,
): boolean {
  return left?.userId === right?.userId && left?.merchantId === right?.merchantId;
}

/** 当前账号世代；订阅建立时捕获，回调时比对以丢弃切换后的迟到响应。 */
export function getAccountGeneration(): number {
  return accountGeneration;
}

/** 账号变化时递增；返回新世代。 */
export function bumpAccountGeneration(): number {
  accountGeneration += 1;
  return accountGeneration;
}

/** 注销或换账号时清空账号缓存并关闭账号订阅；session 查询保留以完成路由切换。 */
export function applyAccountSwitchBoundary(
  queryClient: QueryClient,
  options?: { onInvalidateSubscriptions?: () => void },
): number {
  const next = bumpAccountGeneration();
  queryClient.removeQueries({
    predicate: (query) => {
      const key = query.queryKey;
      return !(Array.isArray(key) && key[0] === "session");
    },
  });
  options?.onInvalidateSubscriptions?.();
  return next;
}

/** 仅在回调仍属建立订阅时的账号世代时调用 handler。 */
export function bindAccountGeneration<T>(
  generation: number,
  handler: (value: T) => void,
): (value: T) => void {
  return (value) => {
    if (generation !== getAccountGeneration()) return;
    handler(value);
  };
}

export function isCurrentAccountGeneration(generation: number): boolean {
  return generation === accountGeneration;
}

/** 测试专用：重置世代计数。 */
export function resetAccountGenerationForTests(value = 0): void {
  accountGeneration = value;
}
