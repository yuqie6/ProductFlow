# 任务：MP-C 价格版本目录骨架 B0

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-price-catalog-b0
认领于：2026-09-07T18:35:00+08:00
完成于：2026-09-07T18:44:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：入口展示单价；unknown 运营策略；≠R5 全过 / ≠支付

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

[R5 裁定](merchant-r5-close-ruling.md)：**未通过**。阻塞项含真实价格版本缺失——仍 `pv-placeholder-v0` 与固定占位单位，生成前无可核验单价目录。

## 做成什么样

1. 最小价格版本目录：可持久化至少一个版本 id + 条目（入口或动作码 → 内部单位单价）；migrate 权威；勿 AutoMigrate。
2. `quota.Reserve` 可选用目录中的 `price_version_id`（非法版本拒绝）；默认版本可种子，替换纯字符串常量作为**唯一**真相源（可保留常量作默认 id 指向种子行）。
3. Op 或只读 HTTP：至少能列出当前默认版本与单价（最小面即可）；商家侧可读当前生效版本摘要更佳。
4. 自动化：种子版本存在；Reserve 用已知版本成功；未知版本拒绝；`just docs-check`。
5. 更新父章程；**≠宣称 R5 通过**；≠真实支付/法币。

## 前置与并行

- 前置：B0–B4、localedit、source-note 已归档。
- 排他：`go/internal/quota/**`、schema 价格表、本任务、父章程；必要时最小 HTTP。
- 勿改：Brand B1、web visualReuse、评委。

## 只改这些文件

- `go/internal/quota/` + schema/migrate
- 最小 HTTP（若做）
- `docs/audits/merchant-platform.md`
- 本文件

## 不要碰

- 假标 R5；支付 webhook；开放第二商。

## 合同

- ROADMAP §8.2 价格版本子集；完成 ≠ R5 关闭。

## 怎么验收

- 包测 + migrate；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
  - `bash scripts/with_dev_env.sh bash -lc 'cd go && go test ./internal/quota/ ./internal/platform/db/schema/ -count=1 -p 1 -timeout 15m'` → **PASS**（2026-09-07T18:37:00+08:00 附近；quota 1.399s，schema 8.481s）
  - `just docs-check` → **PASS**（Documentation contract check passed；2026-09-07T18:37:42+08:00）
- 交付定位：随本任务提交
  - Schema：`quota_price_versions` / `quota_price_entries` + ExtraDDL 种子 `pv-placeholder-v0`（5 入口单价 1 iu）
  - `quota.Reserve`：目录外 `price_version_id` → 400「未知价格版本」
  - HTTP：`GET /api/ops/quota/price-versions/default`；`GET /api/merchants/:merchant_id/quota/price`
  - 父章程已记价格目录骨架；**≠宣称 R5 通过**；**≠真实支付**
- 审核者 / 结论：主代理自审通过（2026-09-07）。quota/schema 复跑 PASS；≠R5/支付。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：实现合同已齐（待维护者验收归档）
  - 业务门槛：**≠ R5 关闭**（入口展示单价产品面、unknown 到期策略、真实支付仍开）
  - 剩余缺口：入口侧按目录展示/计价接线；unknown 运营时限；支付
