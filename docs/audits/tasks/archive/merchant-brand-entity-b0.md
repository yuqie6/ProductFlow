# 任务：Brand 实体骨架 B0

状态：完成
类型：实现
认领者：sub-mpc/merchant-brand-entity-b0
认领于：2026-09-07T18:23:00+08:00
完成于：2026-09-07T18:34:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：视觉继承接品牌层；多品牌 UI；≠R2 全过 / ≠跨商分享

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

总纲 §5.6 / §6.3 与 R2 缺口：Brand 表仍占位（`brand_table_not_ready`）。体验组视觉方案与 IQ-CF-07 继承已交付，但商家内多品牌实体未建，无法去掉占位。

## 做成什么样

1. 商家内 Brand 持久化：至少 `id`、`merchant_id`、名称、时间戳；schema 经 migrate（CreateTable/AddColumn，勿 AutoMigrate）。
2. 商家作用域 CRUD（或 Create/List/Get/Update 最小集）+ 跨商拒绝自动化。
3. 为视觉继承预留关联点：Brand 可挂/指向视觉方案或版本的外键字段**或**明确下一切片接线合同；本门**可不**改完 `ResolveInheritance` 品牌色合并，但须去掉「表不存在」类占位，改为「未选定品牌」等诚实状态。
4. 更新父章程与必要时 image-quality/canvas 对 Brand 占位的交叉说明；**≠宣称 R2/R3 通过**；≠跨商家分享。

## 前置与并行

- 前置：CF-B5 / brand-visual-reuse 已归档。
- 排他：Brand schema/包、migrate 模型、本任务、父章程；视觉继承接线若必需则最窄改 `visualsystem`。
- 勿改：quota/localedit/source-note 本并行任务文件范围；评委；CreateMerchant 开放。

## 只改这些文件

- Brand 包或 `go/internal/` 下新建最小模块 + `platform/db/schema` + migrate
- HTTP/用例最窄面
- `docs/audits/merchant-platform.md`；必要时 `image-quality.md` / `canvas-test-system.md` 占位说明一行
- 本文件

## 不要碰

- 假标 R2；开放第二互不信任商；跨商 Brand 市场。

## 现在代码在哪

- 占位：`visualsystem` 继承返回 `brand_table_not_ready`；表清单见父章程商家根表。
- 视觉方案：`go/internal/visualsystem/`。

## 合同

- ROADMAP §6.3 Brand 属商家；完成 ≠ R2 关闭 ≠ 完整多品牌 UI。

## 怎么验收

- migrate + 包测/HTTP：同商 CRUD、跨商 404/403；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
  - `2026-09-07T18:31:34+08:00`：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/platform/db/schema/ ./internal/brand/ ./internal/visualsystem/ ./internal/recipe/ ./cmd/productflow-api/ -count=1 -p 1'` → **PASS**（schema / brand / visualsystem / recipe / productflow-api）
  - 同日：`go test -C go ./internal/platform/db/schema/ -run "TestBrandsTablePresentAfterApply|TestApplyExistingHeadKeepsSchema" -count=1 -v'` → **PASS**
  - 同日：`just docs-check` → Documentation contract check passed
- 交付定位：随本任务提交（执行者未 commit / 未 push；状态保持认领待维护者审核）
- 审核者 / 结论：主代理自审通过（2026-09-07）。schema/brand/visualsystem/recipe/api 复跑 PASS；表缺失占位已去除；≠R2/R3。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - **已交付**：`brands` 表（CreateTable/AddColumn + ExtraDDL FK/索引/fill 触发器）；`go/internal/brand` List/Create/Get/Update；`/api/v3/brands`；跨商 404；`visual_system_id` 同商家挂接校验；继承占位由 `brand_table_not_ready` 改为 `brand_not_selected` / `brand_exists_no_style_merge`。
  - **残余（下一切片）**：商品选定 Brand（如 `product.brand_id`）；`ResolveInheritance` 品牌色/风格全量合并；多品牌 UI；前端 `visualReuse.ts` 本地 fallback 仍写旧 reason（本门未改 web）；`docs/ARCHITECTURE.md` 所有权表未增 Brand 行。
  - **≠** R2/R3 通过；≠跨商家分享；≠开放第二商。
