import type { MerchantOverviewInput, MerchantWorkKind, MerchantWorkRecord } from "../../lib/types";

export const WORK_KINDS = ["agent_task", "workflow_run", "image_session", "local_edit"] as const satisfies readonly MerchantWorkKind[];
export const WORK_STATES = ["all", "active", "waiting", "unknown", "failed"] as const;
export const WORK_PAGE_SIZE = 10;

export function parseOverviewParams(params: URLSearchParams): MerchantOverviewInput {
  const kind = params.get("work_kind");
  const state = params.get("work_state");
  const page = Number(params.get("work_page"));
  return {
    days: params.get("work_days") === "7" ? 7 : 30,
    kind: WORK_KINDS.find((value) => value === kind) ?? "all",
    state: WORK_STATES.find((value) => value === state) ?? "all",
    page: Number.isInteger(page) && page >= 1 && page <= 100000 ? page : 1,
    page_size: WORK_PAGE_SIZE,
  };
}

export function patchOverviewParams(params: URLSearchParams, patch: Partial<Omit<MerchantOverviewInput, "page_size">>): URLSearchParams {
  const next = new URLSearchParams(params);
  if (patch.days !== undefined || patch.kind !== undefined || patch.state !== undefined) next.delete("work_page");
  for (const [key, value] of Object.entries(patch)) {
    const isDefault = key === "page" ? value === 1 : key === "days" ? value === 30 : value === "all";
    if (isDefault) next.delete(`work_${key}`);
    else next.set(`work_${key}`, String(value));
  }
  return next;
}

export function workRecordLink(record: MerchantWorkRecord): string | null {
  if (record.kind === "image_session") return record.session_id ? `/image-chat?${new URLSearchParams({ image_session_id: record.session_id })}` : null;
  if (!record.product_id) return null;
  const product = `/products/${encodeURIComponent(record.product_id)}`;
  return record.kind === "agent_task" && record.session_id
    ? `${product}?${new URLSearchParams({ agent_session_id: record.session_id, agent_task_id: record.id })}`
    : product;
}
