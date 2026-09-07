# 任务：R2 常用操作核心路径验收门

状态：完成
类型：证据
认领者：sub-r2/delivery-r2-core-path-gate
认领于：2026-09-07T17:17:00+08:00
审核材料提交于：2026-09-07T17:42:00+08:00
完成于：2026-09-07T17:48:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：总纲 R2 关闭裁定（仅当本门与缺口齐）；≠R3

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

方向 2 / ROADMAP **R2**：按第 5 节完成创建、改稿、采用、交付、复用；检查实际持久状态和文件，桌面/手机核心任务可达。现有成果视图、采用快照、条件默认与记忆视图已交付，但 **R2 仍标整体未验收**；浏览器整链 opt-in 多未跑。

## 做成什么样

1. 固定一商品路径证据（隔离 mock 或 skip-Agent 夹具，**不强制真实 provider**）：创建可执行工作流 → 成果视图可见产出 → 交付采用 → 导出包与采用版一致 → 重跑不覆盖已采用 → 配方/视觉复用到第二商品且清旧身份（可复用既有 API/E2E）。
2. 桌面 `1440×960` 与手机 `390×844` 至少各采一轮核心可达性（创建入口、成果切换、采用/导出入口）；截图或 Playwright trace 入库证据目录。
3. 对照 ROADMAP §5.1–5.6 列「已齐 / 缺口」；缺口诚实列出（批量无限制批跑等可标非本门）。
4. 更新 `docs/ROADMAP.md` R2 行仅当维护者后续裁定；本门可 FAIL；更新父章程。**不得**因局部 E2E 假标 R2 全过——若证据不足则保持未通过并写清缺口。

## 前置与并行

- 前置：delivery-adoption / workbench results / brand-visual-reuse / remember-view 已归档。
- 隔离 mock 栈；`PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1` 类 opt-in；禁止共享 `productflow` down。

## 只改这些文件

- 本文件、父章程、必要时 ROADMAP R2（仅诚实状态）
- `web/e2e/delivery-r2-core-path.spec.ts`、`justfile`（`web-e2e-delivery-r2-core-path`）
- 证据目录 `storage-dev/audits/delivery-r2-core-path-gate/`（gitignore）

## 不要碰

- 真实付费批跑；改 Skill/grader；开放第二商；假标 R2。

## 合同

- ROADMAP R2 / §5；完成可 FAIL。
- 持久状态与文件哈希/资产 ID 须可核对。

## 怎么验收

- 路径表 + 命令/exit + 视口证据；`just docs-check`。
- 父章程与 ROADMAP 叙述一致。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：审核材料已提交；**未提交 git**；状态保持认领，等维护者归档。

## 证据

### 本门结论

- **本门（核心路径采证）：PASS**（2026-09-07）
- **总纲 R2 整体：未通过**（不得因本门假标全过；§5 仍有缺口，见下表）

### 命令 / 日期 / 结果

| 命令 | 日期 | exit / 结果 |
|---|---|---|
| `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/delivery/ -count=1 -run TestDeliveryAdoption'` | 2026-09-07 | exit 0；采用不可变版本 + 导出一致 |
| 隔离栈：`APP_PORT=29392` + Vite `29393` + worker/dispatcher `REDIS_URL=…/1`（不 down 共享 `productflow` / 29282） | 2026-09-07 | healthz 200；挂载 adoption/visual 路由（共享 just-dev API 二进制过旧，`404 page not found`） |
| `PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 WEB_BASE_URL=http://127.0.0.1:29393 pnpm --dir web exec playwright test e2e/delivery-r2-core-path.spec.ts …` | 2026-09-07T17:41+08:00 | **2 passed / 24.7s** |
| `just docs-check` | 2026-09-07 | Documentation contract check passed |

入口：`just web-e2e-delivery-r2-core-path`（需已挂载 adoption 的 API + mock 可绑定栈）。

### 路径表（持久 ID / 哈希）

证据目录：`storage-dev/audits/delivery-r2-core-path-gate/`（gitignore；含 `path-table.json`、`adoption-export.zip`、截图、playwright-output/trace）。

| 步骤 | 核对 |
|---|---|
| create | product `37a0933b-…` / graph `df8494e1-…` / node `1b1a780b-…` |
| generate | source asset `5746df96-da4a-4b24-bc49-f04923a6078f` |
| adopt | version `0bd948ee-…` 钉住同一 `source_asset_id` |
| export | ZIP item `source_asset_id` = 采用版；`sha256=96148fff9146b3c9ee6e12886d4071766dd967d54c495db5aa43ec27e5f1ecb6`（与 manifest.measured 一致）；parent 谱系保留 |
| rerun | mock 同 digest **复用**同一 asset id；采用版本 id 与 slot 仍钉 `5746df96-…`（不可变） |
| visual | 创建并选定方案 `5e450c4a-…` / version `8aab1027-…` |
| recipe 第二商品 | product `bad0a284-…`；图 JSON 不含源 product/asset id；节点无旧 bound/preview/artifact |

### 视口证据

| 视口 | 文件 |
|---|---|
| 1440×960 成果 | `desktop-1440x960-results.png` |
| 1440×960 已采用+导出入口 | `desktop-1440x960-adopted-export.png` |
| 390×844 创建/成果可达 | `mobile-390x844-create-results-reachability.png` |
| 390×844 配方预览 | `mobile-390x844-recipe-preview.png` |
| 390×844 第二商品成果 | `mobile-390x844-results.png` |

### §5.1–5.6 已齐 / 缺口

| 节 | 判定 | 说明 |
|---|---|---|
| 5.1 导航与工作区 | **部分已齐** | 成果/流程切换、结果列表、1440/390 核心可达已证；未覆盖 1280 全矩阵、长名/空态/错误态、手机高级连线密度 |
| 5.2 创建与方案确认 | **部分已齐** | skip-Agent 直接创建可执行工作流已证；Agent 辅助补齐、付费前消耗核对、草稿恢复未在本门跑 |
| 5.3 生成、等待和恢复 | **部分已齐** | 节点 mock 生成成功；排队/未知结果/关页恢复整链未重跑 |
| 5.4 修图与候选 | **缺口（本门）** | 局部编辑依赖既有归档 [canvas-local-edit-flow](canvas-local-edit-flow.md)；本门未重跑改稿对比 |
| 5.5 采用与交付 | **已齐（核心）** | 成果采用 → 导出与采用 asset/hash 一致 → 重跑不改采用快照；导出 UI 点击受画布工具条叠层干扰，字节核对走同合同 API；`qualified=false`（unchecked） |
| 5.6 下一商品与批量 | **部分已齐** | 配方第二商品清旧身份 + 视觉方案选定已证；Brand 表仍占位；**无限制批跑非本门** |

### 自审与剩余差距

- 共享 just-dev（29282）API 构建过旧，无 delivery-adoptions / visual-systems 路由；本门用隔离 HEAD API/worker（29392 + Redis DB1）采证，未 `docker compose down`。
- 共享库曾空 `merchants`（`merchant_id required`）；采证前种子唯一「开发商家」`a100a38e-…`。
- 成果「导出已采用交付」按钮可被画布右上控件拦截；E2E 断言按钮可见/可点后以 API 导出核对哈希。
- mock 重跑可能复用同一 asset id；合同以采用快照指针不可变判定，不以「必出新 asset」强求。
- 未宣称 R2 全过；未跑真实 provider、批量商品生成、Brand 实体。

### 是否需要协调者同步

父章程与 ROADMAP R2 **诚实**更新为：本门 PASS、R2 整体仍未通过。看板认领字段保持至维护者归档。

- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/delivery-r2-core-path-gate.md` 查询）；大图/ZIP 留 `storage-dev/audits/delivery-r2-core-path-gate/`（gitignore）。
- 审核者 / 结论：主代理自审通过（2026-09-07）。核对 path-table、导出 PNG sha256=`96148fff…` 与 manifest.measured 一致、1440/390 截图非空；诚实「本门 PASS / R2 未通过」成立。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：**完成**（本门采证 PASS）。
  - 业务门槛：总纲 **R2 未通过**。
  - 剩余缺口：§5.4 修图本门未重跑；Brand/批跑；导出 UI 叠层；共享 just-dev API 过旧须隔离。
