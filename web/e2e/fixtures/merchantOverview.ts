import type { MerchantOverview } from "../../src/lib/types";
export function emptyMerchantOverview(): MerchantOverview {
  const counts = () => ({ active: 0, waiting: 0, unknown: 0, recent_failed: 0 });
  return { as_of: "2026-09-08T00:00:00Z", recent_window: { days: 30, from: "2026-08-09T00:00:00Z", to: "2026-09-08T00:00:00Z" }, products: { total: 0, current_adopted: 0 }, work: { by_source: { agent_task: counts(), workflow_run: counts(), image_session: counts(), local_edit: counts() }, records: { items: [], total: 0, page: 1, page_size: 10 } } };
}
