import { describe, expect, it } from "vitest";
import { parseOverviewParams, patchOverviewParams, workRecordLink } from "./overviewModel";
import type { MerchantWorkRecord } from "../../lib/types";
const record: MerchantWorkRecord = { id: "task /1", kind: "agent_task", product_id: "product /1", session_id: "session /1", product_name: "Lamp", title: "Goal", status: "waiting_user", created_at: "2026-01-01T00:00:00Z", started_at: null, finished_at: null, failure_reason: null };
describe("merchant work route contract", () => {
  it("keeps work filters and directory filters independent", () => {
    const params = new URLSearchParams("q=lamp&page=3&sort=name_asc&work_page=4&work_days=7");
    const next = patchOverviewParams(params, { kind: "local_edit", state: "failed" });
    expect(parseOverviewParams(next)).toEqual({ days: 7, kind: "local_edit", state: "failed", page: 1, page_size: 10 });
    expect([next.get("q"), next.get("page"), next.get("sort")]).toEqual(["lamp", "3", "name_asc"]);
    expect(parseOverviewParams(new URLSearchParams("work_days=99&work_state=done&work_kind=other&work_page=100001"))).toEqual({ days: 30, kind: "all", state: "all", page: 1, page_size: 10 });
  });
  it("uses existing task and session locations without fabricated run targets", () => {
    expect(workRecordLink(record)).toBe("/products/product%20%2F1?agent_session_id=session+%2F1&agent_task_id=task+%2F1");
    expect(workRecordLink({ ...record, kind: "workflow_run" })).toBe("/products/product%20%2F1");
    expect(workRecordLink({ ...record, kind: "local_edit" })).toBe("/products/product%20%2F1");
    expect(workRecordLink({ ...record, kind: "image_session", product_id: null })).toBe("/image-chat?image_session_id=session+%2F1");
    expect(workRecordLink({ ...record, kind: "image_session", session_id: null })).toBeNull();
    expect(workRecordLink({ ...record, product_id: null })).toBeNull();
  });
});
