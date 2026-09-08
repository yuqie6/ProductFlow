import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { OpsStatus } from "./OpsShared";

describe("native operations status projection", () => {
  it.each([
    ["waiting_user", "等你回答"], ["paused", "已暂停"], ["draft", "草稿"], ["unknown", "状态未知"],
    ["queued", "等待开始"], ["running", "正在处理"], ["succeeded", "已完成"], ["failed", "失败"],
    ["canceled", "已取消"], ["cancelled", "已取消"], ["awaiting_confirmation", "等待确认"],
    ["future_status", "future_status"],
  ])("retains the meaning of %s", (status, label) => {
    const html = renderToStaticMarkup(<OpsStatus status={status} />);
    expect(html).toContain(label);
  });
});
