# 任务：商家成图任务闭环采证门

状态：完成
类型：证据
认领者：sub-ux/delivery-merchant-image-loop-gate
认领于：2026-09-07T19:25:00+08:00
审核材料提交于：2026-09-07T19:20:00+08:00
完成于：2026-09-07T19:30:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：批跑；真实 provider 修图质量；≠假标 R2 全过

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

外部审查：平台脚手架 ≫ 商家稳定出图。试用额度与 compose 交付已接线后，须用**可观察状态/文件**证明一条商家任务闭环：创建商品 → 生成/合成 → 检查/改稿（文字或规格）→ 采用 → 导出 → 第二商品 Brand 复用且清旧身份。方向 2 要求操作证据，非仅文档宣称。

## 做成什么样

1. 固定输入（夹具或本机可复现步骤）；记录每步后的持久状态（DB/资产文件/导出包），不是只截 UI。
2. 覆盖：创建；至少一图位出图（允许 mock/检查器或真实 provider，标注）；采用；导出文件存在；第二商品复用 Brand/视觉且事实/身份不串。
3. 默认入口是否切成果视图：有操作证据则记；无则标缺口。
4. 结论可为本门 PASS / FAIL；**不得**单独宣布总纲 R2 通过。
5. 更新父章程/ROADMAP R2 残余一句；`just docs-check`。

## 前置与并行

- 前置：R2 核心路径/导出/§5.4/Brand B0–B1/试用额度/compose 交付已归档。
- 运行资源：可复用测试 DB；真实 provider 须声明预算；勿与评委改题并行写。
- 排他：本任务、父章程、必要时最窄 e2e/fixture；勿改 quota/graph 主链除非发现阻塞根因交回。

## 只改这些文件

- 本文件；证据产物路径写清
- `docs/audits/canvas-test-system.md`（或体验组父章程）与必要时 ROADMAP R2 一句
- 必要时 `web` e2e / go 夹具最窄

## 不要碰

- 假标 R2；批跑产品化；开放第二商注册。

## 合同

- ROADMAP §5 / R2 子集；完成 ≠ R2 关闭。

## 怎么验收

- 逐步证据表 + 命令；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已审核归档；本门 PASS（mock）；**≠R2 全过**。

## 证据

### 本门结论

- **本门（商家成图任务闭环）：PASS**（2026-09-07；**mock provider**）
- **总纲 R2 整体：未通过**（不得因本门假标全过）

### 命令 / 日期 / 结果

| 命令 | 日期 | exit / 结果 |
|---|---|---|
| 隔离栈：`APP_PORT=29412` + Vite `29413` + worker/dispatcher `REDIS_URL=…/5`；独立 PG `productflow_merchant_image_loop`（不 down 共享 productflow / 29282） | 2026-09-07 | healthz 200；HEAD 二进制含 brands / brand-selection / delivery-adoptions；bootstrap 登录 |
| `PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 WEB_BASE_URL=http://127.0.0.1:29413 PRODUCTFLOW_MERCHANT_LOOP_EVIDENCE_DIR=… pnpm --dir web exec playwright test e2e/delivery-merchant-image-loop.spec.ts …` | 2026-09-07T19:16+08:00 | **1 passed / 22.4s** |
| `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/brand/ ./internal/visualsystem/ ./internal/product/ -run "Brand\|Inheritance\|brand" -count=1 -p 1'` | 2026-09-07 | **PASS**（Brand CRUD / 选定合并 / 继承） |
| `just docs-check` | 2026-09-07T19:17+08:00 | Documentation contract check passed |

入口：`just web-e2e-delivery-merchant-image-loop`（需隔离 mock 栈 + 当前 auth bootstrap）。

### 逐步证据表（PASS/FAIL）

| 步骤 | 判定 | 持久核对 |
|---|---|---|
| 创建商品 | **PASS** | product `3d8ffaa7-…` / graph `3c94bf3c-…` / image node `4d754815-…` |
| 改稿事实 | **PASS** | PUT `/facts` 确认 `闭环规格=闭环规格-1788779750394`；名称 `闭环商品-1788779750394`；检查器可见持久名 |
| 改稿文案 | **PASS** | prompt `design_goal=闭环改稿目标-1788779750394`；`document_origin=authored` |
| 出图（mock） | **PASS** | source asset `b56d51bc-2253-4c73-a075-b930b50fb73b`；**provider=mock** |
| 默认入口成果 | **PASS** | 清 `mainView` 偏好后 reload → `data-graph-main-view-panel=results` |
| 采用 | **PASS** | adoption `947f50e9-…` 钉住同一 `source_asset_id` |
| 导出 | **PASS** | ZIP `adoption-export.zip`；item sha256=`47d8c7a0e9e10057621c906651c4d4d148330725aea1499f917945c7a4fa6d1d` |
| 第二商品 Brand 复用 | **PASS** | Brand `c39e44c1-…`；第二商品 `6ea4706b-…`；inheritance `brand_style_merged`；图 JSON / 事实无源 product/asset/规格串入 |

证据目录：`storage-dev/audits/delivery-merchant-image-loop-gate/`（gitignore；含 `path-table.json`、`adoption-export.zip`、截图、`playwright.log`、`go-brand-tests.log`、`isolated-logs`）。

### 视口证据

| 视口 | 文件 |
|---|---|
| 1440×960 事实已改 | `desktop-1440x960-facts-edited.png` |
| 1440×960 条件默认成果 | `desktop-1440x960-default-results.png` |
| 1440×960 已采用+导出 | `desktop-1440x960-adopted-export.png` |
| 390×844 配方预览 | `mobile-390x844-recipe-preview.png` |
| 390×844 第二商品成果 | `mobile-390x844-second-results.png` |

### 自审与剩余差距

- **mock only**：未跑真实 provider / compose 像素保真；出图标注 mock。
- 检查器「确认事实」UI 曾出现 `Cannot read properties of null (reading 'length')`；本门事实改稿改走 `PUT /facts` 持久化 + 检查器核对，**未宣称 UI 确认按钮可用**。
- 配方可携带 prompt 模板文案；身份不串以 product/asset/fact 键为准。
- 共享 just-dev（29282）API 当时无 `/api/v3/brands`；本门用隔离 HEAD 采证。
- **≠R2 全过**：批跑、确定性文字层、真实修图质量、完整多品牌 UI 仍缺。

### 是否需要协调者同步

父章程与 ROADMAP R2 **诚实**更新为本门 PASS、R2 整体仍未通过。已归档。

- 交付定位：随本任务提交
- 审核者 / 结论：维护者通过（2026-09-07）；本门 PASS（mock）；**≠R2 全过**
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：完成
  - 业务门槛：总纲 **R2 未通过**
  - 剩余缺口：批跑；真实 provider；检查器确认事实 UI 告警；≠假标 R2。
