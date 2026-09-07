# 任务：商品选定 Brand 与继承合并 B1

状态：完成
类型：实现
认领者：sub-mpc/merchant-brand-product-select-b1
认领于：2026-09-07T18:35:00+08:00
完成于：2026-09-07T18:48:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：多品牌 UI；≠R2 全过 / ≠跨商分享

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

[Brand 实体 B0](merchant-brand-entity-b0.md) 已建 `brands` CRUD，但商品尚未选定 Brand；`ResolveInheritance` 在有 BrandID 时仍 `brand_exists_no_style_merge`，不合并品牌层 style/colors。

## 做成什么样

1. 商品可持久化选定本商家 Brand（如 `products.brand_id` 可空 FK；跨商/异商 Brand 拒绝）。
2. 继承解析：选定 Brand 且可解析品牌风格载荷时，合并 style/colors（优先级仍：商品覆盖 > 视觉方案版本 > **品牌** > 默认）；成功后品牌层 `Active`；无 Brand → `brand_not_selected`。
3. 品牌风格来源：优先 Brand.`visual_system_id` 指向的本商家方案当前/钉住版本（合同写清）；不得引入事实/身份键。
4. 自动化：同商选定+合并正测；跨商 Brand 拒绝；无 Brand 占位；`just docs-check`。
5. 更新父章程 / 必要时 image-quality IQ-CF-07 一行；**≠宣称 R2/R3**；≠完整多品牌 UI。

## 前置与并行

- 前置：Brand B0 已归档 `63a14b67` 或其后含 `brands`。
- 排他：`product` schema/`brand`/`visualsystem` 最窄、本任务、父章程。
- 勿改：web `visualReuse.ts`（并行任务）、quota 价格目录、评委。

## 只改这些文件

- schema migrate（`products.brand_id` 等）
- `go/internal/product`、`go/internal/brand`、`go/internal/visualsystem` 最窄接线
- 父章程；必要时 `image-quality.md`
- 本文件

## 不要碰

- 假标 R2；跨商分享；web fallback（另单）。

## 合同

- ROADMAP §6.3 / IQ-CF-07；完成 ≠ R2 关闭。

## 怎么验收

- 包测 + migrate 回归；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
  - `2026-09-07T18:43:58+08:00`：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./cmd/productflow-api/ ./internal/platform/db/schema/ ./internal/visualsystem/ ./internal/product/ ./internal/brand/ -count=1 -p 1'` → **PASS**
  - 同日：`just docs-check` → Documentation contract check passed
- 交付定位：随本任务提交（执行者未 commit / 未 push；状态保持认领待维护者审核）
- 审核者 / 结论：主代理自审通过（2026-09-07）。product/visualsystem/schema/api 复跑 PASS；≠R2/R3。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - **已交付**：`products.brand_id` 可空 FK（同商选定校验；跨商 404）；`GET/PUT /api/v3/products/:id/brand-selection`；`ResolveInheritance` 合并 Brand.`visual_system_id` **当前（最新）版本** style/colors（优先级：商品覆盖 > 选定方案版本 > 品牌 > 默认）；无 Brand → `brand_not_selected`；有 Brand 无可解析风格 → `brand_exists_no_style_merge`；可解析 → 品牌层 Active + `brand_style_merged`。
  - **≠** R2/R3 通过；≠完整多品牌 UI；≠跨商分享。
  - 合同：品牌风格来源 = Brand.`visual_system_id` → 该方案最新版本（非商品钉住品牌版本）。
