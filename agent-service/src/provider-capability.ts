/**
 * Pi adapter 的有效 background 能力：必须同时由 provider profile 和 adapter 支持。
 * 当前生产 adapter 固定 false，网关/profile 探测不得把有效值写成 true。
 */

export const ADAPTER_BACKGROUND_RESUMABLE = false as const;

export function effectiveBackgroundResumable(profileFlag: boolean | null | undefined): boolean {
  return Boolean(profileFlag) && Boolean(ADAPTER_BACKGROUND_RESUMABLE);
}
