/** 站点配置与运营管理仅向平台管理员开放。 */

import type { SessionState } from "./types";

/** 是否可进入站点配置和运营管理。 */
export function canAccessOpsSettings(session: SessionState | undefined | null): boolean {
  if (!session?.authenticated) return false;
  return session.user?.is_operator === true;
}

/** 当前账号的自有商家是否已停用。 */
export function isWorkingMerchantSuspended(session: SessionState | undefined | null): boolean {
  return session?.merchant?.status === "suspended";
}

/** 独立管理员登录后进入运营面，商家账号继续进入商品页。 */
export function authenticatedLandingPath(session: SessionState | undefined | null): string {
  return session?.authenticated && session.user.is_operator && !session.merchant ? "/ops" : "/products";
}
