# 任务：§5.4 局部修图核心路径复验

状态：完成
类型：证据
认领者：sub-r2/delivery-r2-local-edit-retest
认领于：2026-09-07T18:11:00+08:00
审核材料提交于：2026-09-07T18:18:00+08:00
完成于：2026-09-07T18:20:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：R2 关闭裁定；≠假标 R2

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

[R2 核心路径门](delivery-r2-core-path-gate.md) 本门 PASS，但 **§5.4 修图与候选** 标为缺口（依赖旧归档未重跑）。导出叠层已修。关闭 R2 前须用当前栈复验局部编辑→候选→采用可达。

## 做成什么样

1. 隔离 mock：对一商品成果图做局部编辑（或既有 local-edit e2e），确认可进候选/结果并可选采用；桌面 1440 与手机 390 至少各一轮可达截图。
2. 对照 §5.4 列已齐/缺口；诚实 FAIL 可接受。
3. 更新父章程；**不得**因本门宣称 R2 全过。

## 前置与并行

- 前置：delivery-r2-core-path-gate、delivery-export-overlay-fix 已归档。
- 隔离 mock；勿 down 共享 productflow。

## 只改这些文件

- 本文件、父章程、必要时窄 e2e 扩展
- 证据目录（gitignore）

## 不要碰

- 假标 R2；额度/OCR/主体提取包。

## 合同

- ROADMAP §5.4；完成可 FAIL。

## 怎么验收

- 命令 + 截图；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：审核材料已提交；**未提交 git**；状态保持认领，等维护者归档。隔离栈已停（API `29402` / Web `29403` / Redis DB3 / PG `productflow_r2_local_edit_retest`）；未 `docker compose down` 共享 productflow。

## 证据

### 本门结论

- **本门（§5.4 局部编辑→结果/采用可达复验）：PASS**（2026-09-07）
- **总纲 R2 整体：未通过**（不得因本门假标全过；Brand/批跑等仍缺）

### 命令 / 日期 / 结果

| 命令 | 日期 | exit / 结果 |
|---|---|---|
| 隔离栈：`APP_PORT=29402` + Vite `29403` + worker/dispatcher `REDIS_URL=…/3`；独立 PG `productflow_r2_local_edit_retest`（不 down 共享 productflow / 29282） | 2026-09-07 | healthz 200；bootstrap Operator+商家；prompt/image mock 绑定；额度种子 |
| `PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 WEB_BASE_URL=http://127.0.0.1:29403 PRODUCTFLOW_LOCAL_EDIT_EVIDENCE_DIR=… pnpm --dir web exec playwright test e2e/canvas-local-edit.spec.ts …` | 2026-09-07T18:16+08:00 | **2 passed / 25.3s** |
| `just docs-check` | 2026-09-07 | Documentation contract check passed |

入口：`just web-e2e-canvas-local-edit`（需隔离 mock 栈 + 当前 auth bootstrap/密码登录）。

### 路径表（持久 ID）

证据目录：`storage-dev/audits/delivery-r2-local-edit-retest/`（gitignore；含截图、`path-table.json`、playwright-output/trace、isolated-logs）。

| 步骤 | 核对 |
|---|---|
| create + generate | product `0ac84e28-…` / image node `16523aeb-…` / source asset `dc792ac0-…` |
| local edit result | result asset `020b03a7-…`；`origin_type=local_edit`；`parent_asset_id=dc792ac0-…`；采用前节点 preview 仍为源图 |
| adopt / revert | UI「采用为当前结果」改 preview；「撤销采用」恢复源图；源图字节不变 |
| submit failure | 拦截 `POST **/image-edits/**/submit` → 503；节点当前图不变 |

### 视口证据

| 视口 | 文件 |
|---|---|
| 1440×960 结果可达（未采用） | `desktop-1440x960-local-edit-result.png` |
| 390×844 结果可达 | `mobile-390x844-local-edit-result.png` |
| 1440×960 已采用（撤销采用可见） | `desktop-1440x960-local-edit-adopted.png` |

### §5.4 已齐 / 缺口

| 条款 | 判定 | 说明 |
|---|---|---|
| 选中图后局部编辑入口 | **已齐（检查器）** | `data-graph-node-local-edit` → 对话框；mock 能力 `supported` |
| 生成式编辑保留基图/区域/指令/provider/结果来源 | **已齐（mock）** | 谱系 `parent_asset_id`；UI 示供应商 Mock；结果态前源 preview 不变 |
| 采用前对照 | **部分已齐** | 源图/结果切换 + 对比区可达；拖动对照未专测 |
| 失败不替换原图 | **已齐** | 提交 503 路径；源字节在采用/撤销后不变 |
| 采用与撤销 | **已齐** | 采用改节点当前资产；可撤销 |
| 确定性文字层改字 | **缺口** | 本门未覆盖可重复排版文字层 |
| 真实 OpenAI/Gemini 质量 | **缺口** | mock only；Gemini 仍不支持 |
| 成果主视图直接修图入口 | **未宣称** | 本门走检查器入口，非成果列表主入口矩阵 |

### 自审与剩余差距

- 共享 `productflow_dev` 当时有商家无 User，`needs_bootstrap=true` 但 bootstrap 因商家已存在冲突；本门用独立 PG 库采证，未改共享库用户态。
- `web/e2e/liveGraph.ts` `loginAsAdmin` 对齐当前 email/password（及 bootstrap）登录面；否则旧 admin_key 单字段登录在 HEAD 不可达。
- mock 预览区可呈灰图占位，合同以资产身份、origin/parent、采用/撤销与 UI 可达判定，不以像素美学为门。
- **未宣称 R2 全过**；Brand 实体、无限制批跑、真实 provider 修图质量仍为组外/后续缺口。

### 是否需要协调者同步

- 审核归档；看板保持认领直至维护者关闭。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/delivery-r2-local-edit-retest.md` 查询）；大图留证据目录（gitignore）。
- 审核者 / 结论：主代理自审通过（2026-09-07）。核对 path-table 与 1440/390 截图非空；本门 PASS；≠R2 全过。
- Issue 结果 / 业务门槛结果 / 剩余缺口：Issue 证据齐（本门 PASS）；业务门槛总纲 **R2 未通过**；§5.4 检查器 mock 路径已齐，确定性文字层与真实 provider 仍缺。
