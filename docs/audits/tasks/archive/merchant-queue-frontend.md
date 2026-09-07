# 任务：队列与前端商家边界（B8）

状态：完成
类型：实现
认领者：sub-merchant/merchant-queue-frontend
认领于：2026-09-07T14:36:00+08:00
完成于：2026-09-07T14:50:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B9 运营面；完整隔离门未过不开放第二商家

按 [Issue 协议](../README.md) 认领。承接 [B7](merchant-agent-tools.md) 与覆盖矩阵 **B8 / J\***。协调者审核：2026-09-07 CTO 通过——queue merchant 测、Vitest 47、recovery 编译通过；`async_dispatches.merchant_id` 由 migrate AddColumn；未宣称 MP-B / B10。

## 问题来源

矩阵 J\*：async_dispatches / 五 Actor 重试与 Restage 可能丢商家快照；前端 queryKey / EventSource 缺 merchant 边界，切换或迟到响应可能串商。

## 做成什么样

五 Actor 受理快照含 `merchant_id`；Stage/Restage/recovery 保持原商家；改信封商家无效。前端 queryKey（或切换时整表清空）与 SSE 取消含商家边界；迟到响应不进新商 UI。正例：混合队列下本商 UI 正确。反例：改信封商家；旧订阅；切换后迟到。仍不开放第二商；不宣称 MP-B。

## 前置与并行

- 前置：B7 已归档。
- 排他写入：`go/internal/platform/queue` 及相关 recovery、notify 必要接线、前端 merchant/SSE/queryKey 边界、父章程 B8、本文件。
- 勿改 Skill/grader；勿开放第二商注册。

## 只改这些文件

实际交付：

- `go/internal/platform/db/schema/models.go`：`async_dispatches.merchant_id` 受理快照列
- `go/internal/platform/queue/`：`Dispatch.MerchantID`、Stage/Requeue 不可改写快照、`merchant_test.go` 正反测
- `go/internal/{graph,delivery,imagesession,localedit,agent}/recovery.go`：Restage 绑定聚合原商家
- `contracts/queue.md`：商家快照合同
- `web/src/lib/merchantBoundary.ts`（+test）：切换清空 query + 世代丢弃迟到回调
- `web/src/App.tsx`：商家切换边界
- `web/src/pages/workbench/agent/conversation/runtime.ts`：`disposeAllConversationRuntimes`
- EventSource 接线：`GraphCanvasPanel` / `ImageChatPage` / `useAgentTurnEvents` / `GlobalAgentDock`
- `docs/audits/merchant-platform.md`：B8 / J\* 状态
- 本文件

未改：Skill/grader、第二商开放、看板 README、notify 通道语义（仍仅唤醒）。

## 不要碰

- Skill/grader；第二商开放；MP-B 宣称；重做 B0–B7 已交付隔离除非接线必需。

## 现在代码在哪

- 合同：[merchant-platform.md](../../merchant-platform.md) 矩阵 J\* / 批次 B8
- 队列：`go/internal/platform/queue`；Actor 列表见父章程
- 各域 recovery：graph / imagesession / delivery / localedit / agent
- 前端：`web/src` queryKey、EventSource（agent / image-session / graph run）

## 合同

- 矩阵 J\*；完成 ≠ MP-B / ≠ B10。

## 怎么验收

- 正测：混合队列本商 UI/任务正确；Restage 商家不变
- 反测：改信封商家无效；切换后旧 SSE/迟到响应丢弃
- 命令：见证据

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已归档；交付随本任务提交。

## 证据

- 跨商/改商策略：`async_dispatches.merchant_id` 受理快照；已有快照被 context 改写 → **409 Conflict**；Restage 无商家 ctx 或同商 ctx 保持原商家。
- 实现要点：
  - J1：Stage/Requeue/StageForActor 写入并冻结 `MerchantID`；五 Actor recovery Restage 绑定 product/session/conversation 原商家
  - J2：混合队列两 Actor 不同商家 Restage 后快照互不串
  - J3：notify 载荷仍仅为聚合/dispatch id（注释钉死不授予数据）
  - J4/J5：`applyMerchantSwitchBoundary` 清空非 session query + `disposeAllConversationRuntimes`；SSE 回调绑商家世代，迟到丢弃
- 测试（协调者复测 2026-09-07）：
  - `bash scripts/with_dev_env.sh bash -lc 'cd go && go test ./internal/platform/queue/ -count=1 -p 1 -run "Merchant|Stage|Restage"'` → ok
  - `pnpm --dir web exec vitest run src/lib/merchantBoundary.test.ts src/pages/workbench/agent/conversation/runtime.test.ts src/pages/workbench/canvas/graphRunEvents.test.ts src/pages/image-chat/sessionEvents.test.ts src/pages/workbench/agent/useAgentTurnEvents.test.ts` → 47 passed
  - recovery 五包编译：placeholder run → ok
  - 列补齐：`AsyncDispatches.MerchantID` 经 migrate `AddColumn`（模型已注册 AllModels）
- 自审：第二商仍不开放；未改 Skill/grader；**未宣称 MP-B / B10**
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/merchant-queue-frontend.md` 查询）

- 审核者：CTO（本会话）；结论：通过。可拆 B9 运营面最小集。
- Issue 结果：完成。剩余缺口：B9/B10；双商切换 E2E 属 B10。
