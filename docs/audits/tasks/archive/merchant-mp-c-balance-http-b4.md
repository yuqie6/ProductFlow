# 任务：MP-C B4 商家/Op 余额 HTTP 面

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-balance-http-b4
认领于：2026-09-07T18:07:00+08:00
完成于：2026-09-07T18:16:30+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：R5 关闭裁定（仍须全入口核对）；≠真实支付

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

B0–B3 账本与主入口已接线，但**商家/Op 无法经 HTTP 查看余额或 Op 调账**。运营与自助解释缺面。

## 做成什么样

1. 商家只读：当前 `available`/`reserved`/币种/`price_version_id`（本商）。
2. Op：同一只读 + `Adjust`（幂等键、原因、actor）；权限与现有 Op 门禁一致。
3. 自动化：跨商读拒绝；Adjust 幂等；不足调账冲突。
4. ≠支付 webhook；≠宣称 R5。更新父章程。

## 前置与并行

- 前置：B3 已归档。
- 排他：HTTP/auth 最窄、`quota` 只读接线、本任务、父章程；勿改主体提取/导出叠层。

## 只改这些文件

- HTTP 路由与测试
- 父章程、本文件

## 不要碰

- 支付；假标 R5；Skill/grader。

## 合同

- ROADMAP §8 / MP-C 余额面；完成 ≠ R5 关闭。

## 怎么验收

- 包测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
  - `2026-09-07`（执行者）：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/quota/ ./cmd/productflow-api/ -count=1 -p 1'` → `internal/quota` PASS；`cmd/productflow-api` PASS（含路由合同）。
  - `2026-09-07`（执行者）：`just docs-check` → passed。
  - 覆盖：`TestMerchantQuotaCrossMerchantDenied`（跨商 GET 403；本商只读零余额投影）；`TestOpAdjustIdempotentAndInsufficientConflict`（Op Adjust 幂等、Op GET、非 Op 403、超额扣减 409）。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/merchant-mp-c-balance-http-b4.md` 查询）
- 审核者 / 结论：主代理自审通过（2026-09-07）。复测 `./internal/quota` + `./cmd/productflow-api` PASS；跨商拒绝/幂等/超额冲突齐。≠R5 ≠支付。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：**完成**（B4 余额 HTTP）。
  - 业务门槛：MP-C 主账本+入口+余额面已齐；**R5 未通过**（全入口归属/真实单价/支付仍缺）。
  - 剩余缺口：全入口核对；真实价格版本；支付；生产零余额运营策略。
