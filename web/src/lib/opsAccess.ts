/** 运营面最小门禁：实例 settings / 支持合同仅站点 Operator。 */

import type { SessionState } from "./types";

/** 是否可进入系统配置（矩阵 A3）。 */
export function canAccessOpsSettings(session: SessionState | undefined | null): boolean {
  if (!session?.authenticated) return false;
  if (!session.access_required) return true;
  return Boolean(session.user?.is_operator);
}

/** 当前工作商家是否已停用。 */
export function isWorkingMerchantSuspended(session: SessionState | undefined | null): boolean {
  const memberships = session?.memberships;
  if (!memberships?.length) return false;
  const active = memberships.find((item) => item.status === "active") ?? memberships[0];
  return active?.merchant_status === "suspended";
}
