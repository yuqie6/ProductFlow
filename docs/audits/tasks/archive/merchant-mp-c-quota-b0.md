# 任务：MP-C 商家额度账本 B0（预留/结算骨架）

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-quota-b0
认领于：2026-09-07T17:17:00+08:00
完成于：2026-09-07T17:25:30+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：生图/Agent 入口接预留；Op 邀请额度；≠R5 全过 / ≠真实支付

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

方向 1 要求额度贯通。R1/MP-B 已过；**MP-C 未实现**。ROADMAP §8：操作事实、成本估计、商业额度三账分离；生成前原子预留、幂等、完成/取消/unknown/调账可追踪。平台侧已有调用事实（如 Agent invocation usage）；缺商家商业额度账本。

## 做成什么样

1. 落地商家额度最小账本（整数最小单位、币种字段可先固定内部单位）：余额/负债、价格版本 id（可占位）、预留、结算、释放、人工调账事件；**每条绑定 `merchant_id`**。
2. 服务端 API 或内部 service：`Reserve` / `Settle` / `Release` / `Adjust`（Op）；相同幂等键不重复预留/结算；unknown 不自动当零消费释放。
3. 至少一条自动化：并发双预留不超余额；重放幂等；取消释放；unknown 保持待核对。
4. **本批可不**改生图主路径接线（可留 hook/注释）；接线另发。不接真实支付。更新父章程 MP-C 行。≠宣称 R5 通过。

## 前置与并行

- 前置：R1/B10 已归档。
- 排他：`go/internal` 额度新包或 `auth`/`billing` 新目录、schema migrate、本文件、父章程；与 R2/图片 live 文件不重叠。

## 只改这些文件

- schema / migrate（新表）
- `go/internal/...` 额度包与测试
- 必要时 HTTP Op/商家只读余额
- `docs/audits/merchant-platform.md`
- 本文件

## 不要碰

- 支付 webhook；放开 CreateMerchant；Skill/grader；假标 R5。

## 合同

- ROADMAP §8.1–8.2 / MP-C；完成 ≠ 全入口计费 ≠ 公开收费。

## 怎么验收

- `just go-migrate` 相关 + 包测试；`just docs-check`。
- 父章程 MP-C 更新为「B0 骨架已交付」或诚实缺口。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
  - `2026-09-07`（执行者）：`bash scripts/with_dev_env.sh bash -lc 'just go-migrate && go test -C go ./internal/quota/ ./internal/platform/db/schema/ -count=1 -p 1'` → migrate OK；`internal/quota` PASS；`internal/platform/db/schema` PASS。
  - `2026-09-07T09:25:13Z`（维护者复测）：`go test -C go ./internal/quota/ ./internal/platform/db/schema/ -count=1 -p 1` → PASS。
  - 覆盖：并发双预留不超余额；同键幂等预留；取消 Release；MarkUnknown 保持 `pending_reconciliation` 且拒绝当零消费释放；Adjust 幂等；Settle 可从待核对收口。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/merchant-mp-c-quota-b0.md` 查询）
- 审核者 / 结论：主代理自审通过（2026-09-07）。合同 B0 齐：三表+service+包测；unknown 不自动释放；≠ R5 / 全入口 / 支付。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：**完成**（B0 骨架）。
  - 业务门槛：MP-C **未全过**；R5 **未通过**。
  - 剩余缺口：生图/Agent 入口接线；Op/商家余额 HTTP；价格版本真实目录；unknown 到期运营策略；全入口归属可解释。
