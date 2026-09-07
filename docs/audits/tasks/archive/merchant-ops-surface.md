# 任务：运营面最小集（B9）

状态：完成
类型：实现
认领者：sub-merchant/merchant-ops-surface
认领于：2026-09-07T14:50:00+08:00
完成于：2026-09-07T15:00:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B10 双商隔离门；完整隔离门未过不开放第二外部商

按 [Issue 协议](../README.md) 认领。承接 [B8](merchant-queue-frontend.md) 与覆盖矩阵 **B9 / A3–A4、A8**。协调者审核：2026-09-07 CTO 通过——auth/settings/api 与 Vitest opsAccess 复测；未宣称 MP-B / B10 / 完整 MP-D。

## 问题来源

实例 settings / generation-queue 可能被商家角色触及；商家启停与 Op 支持会话合同未钉；停用商家后仍可能写入。

## 做成什么样

A3：settings* / unlock / export-import / provider-* **仅 Op**（商家角色 403；导出无商家密钥明文）。A4：generation-queue 仅 Op 或合同固定的只读聚合，不得向商家角色泄漏他商任务细节。商家启停字段：Op 停用后商家成员不可写业务。A8：支持会话/审计 **合同草案**（可先不实现完整 MP-D UI，但入口与审计字段可测或显式占位）。正例：Op 停用后写拒。反例：商家角色碰 settings。仍不开放第二外部商；不宣称 MP-B。

## 前置与并行

- 前置：B8 已归档。
- 排他写入：settings/auth 运营门禁、商家启停字段与写拒绝、A8 草案/占位、父章程 B9、本文件。
- 勿改 Skill/grader；勿开放第二外部商注册。

## 只改这些文件

实际交付：

- `go/internal/auth/`：`RequireOperatorIf`、停用写拒绝中间件、`SetMerchantStatus`、`merchant_status` 投影、A8 `support.go` 合同与 `/api/ops/*` 占位、`ops_test.go`
- `go/internal/settings/`：`OperatorOnly` 注入；generation-queue 合同注释（只读聚合）
- `go/cmd/productflow-api/main.go` + `routes_contract_test.go`：挂载门禁与新路由
- `web/src`：Op settings 门禁、商家启停 API/面板、`opsAccess` 测
- `docs/audits/merchant-platform.md`：B9 / A3–A4 / A8 状态
- 本文件

## 不要碰

- Skill/grader；B10 双商种子与全矩阵门；第二外部商开放；完整 MP-D 产品化（超出合同草案）。

## 现在代码在哪

- 合同：[merchant-platform.md](../../merchant-platform.md) A3/A4/A8 / 批次 B9
- settings：`go/internal/settings/http.go`
- 身份/商家：`go/internal/auth`
- 队列视图：generation-queue 路由
- 支持草案：`go/internal/auth/support.go`

## 合同

- 矩阵 A3–A4、A8 草案；完成 ≠ MP-B / ≠ B10 / ≠ 完整 MP-D。

## 怎么验收

- 正测：Op 可读写 settings；Op 停用商家后成员写业务拒
- 反测：商家角色 settings / 敏感队列细节 403；导出无明文密钥
- 命令：见证据

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已归档；交付随本任务提交。

## 证据

- 命令 / 日期 / 结果（协调者复测 2026-09-07）：
  - `bash scripts/with_dev_env.sh bash -lc 'cd go && go test ./internal/auth/ ./internal/settings/ ./cmd/productflow-api/ -count=1 -p 1'` → ok
  - `pnpm --dir web exec vitest run src/lib/opsAccess.test.ts` → 3 passed
  - `just docs-check` → passed（归档后）
- 实现要点：
  - A3：settings* + unlock/export/import/provider-* 在门禁开启时 `RequireOperatorIf`；商家角色 403
  - A4：generation-queue 同 Op 门禁；投影仍为计数聚合，无商家/任务明细
  - 启停：`PATCH /api/merchants/:id/status`；`RejectSuspendedMerchantWrites` + 服务层 `requireMerchantActive`
  - A8：`GET /api/ops/support-contract` 合同草案；会话 CRUD 501 占位
  - 前端：非 Op 隐藏/拦截 `/settings`；Op 面板启停 + 合同说明
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/merchant-ops-surface.md` 查询）
- 审核者：CTO（本会话）；结论：通过。可拆 B10 双商隔离门。
- Issue 结果：完成。剩余缺口：B10/MP-B；完整 MP-D；仍不开放第二外部商直至 B10 通过。
