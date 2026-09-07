# 任务：R6 干净冻结候选重跑门

状态：完成
类型：证据
认领者：sub-release/release-r6-clean-candidate-gate
认领于：2026-09-07T15:28:00+08:00
完成于：2026-09-07T15:45:00+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：对本候选 commit 打发行 pin + 正式冻结稳定版对 D4；总纲 R6 关闭裁定（仅当 pin=G-07、正式 D4 与所需证据齐且全 PASS）

按 [Issue 协议](../README.md) 认领。承接 [B6 汇总 FAIL](release-r6-readiness-gate.md)。协调者审核：2026-09-07 CTO 通过——G-07 干净候选 PASS 可记；业务门槛 **R6 仍未通过**；不得标 R6 通过。

## 问题来源

B6 已诚实记 R6 未通过：G-07 脏树/HEAD 漂移 FAIL；安装 pin 与 G-07 HEAD 非同一候选；无冻结稳定版对正式 D4。

## 做成什么样

在**干净固定 checkout** 上选定**单一冻结候选**（发行 pin = G-07 源码身份或明确映射）；无缓存重跑 G-07；优先在冻结稳定版对上补正式 D4（若仍无稳定对则诚实记缺口，不得用等价 retag 冒充）。资源预算若本窗不做须标明。**全项未齐不得标 R6 通过。**

## 前置与并行

- 前置：B6 汇总已归档（FAIL）。
- 冻结输入：认领时钉死 commit / 发行 tag；禁止共享工作树并发提交污染窗口。
- 隔离项目；禁止共享 `productflow` down。
- 排他写入：本文件、父章程 R6 条目、必要时 release 备注；修 G-07 红灯时最窄修补另列清单。

## 只改这些文件

- `docs/audits/tasks/archive/release-r6-clean-candidate-gate.md`（本文件）
- `docs/audits/performance-governance.md`（R6 / G-07 结论；**不得**标 R6 通过）
- `release/README.md`（一句备注）
- 最窄红灯修补：
  - `agent-service/evals/fixtures/catalog.json`
  - `agent-service/evals/fixtures/intake-results.json`
  - `go/internal/media/prune_test.go`
  - `go/internal/platform/generation/snapshot_test.go`
  - `go/internal/platform/metrics/http_test.go`
  - `web/src/pages/workbench/agent/ProductWorkbenchSurface.tsx`

## 不要碰

- 把 B6 FAIL 改写成 PASS；降低 G-07 / D4 门槛；Skill/grader。

## 现在代码在哪

- 失败证据：[release-r6-readiness-gate](release-r6-readiness-gate.md)
- 父章程：[performance-governance.md](../../performance-governance.md) B6
- Go 红灯：agent catalog fixture；media/generation/metrics `merchant_id`
- Web：bundle 预算；lint 已在协调树修过 unused

## 合同

- 总纲 R6；完成可 FAIL；缺干净候选或 D4 缺口则不得关闭 R6。

## 怎么验收

- 干净 `git status`；起点/终点同一 HEAD
- G-07 逐步 exit 表；D4 或诚实缺口
- 父章程 R6 结论与证据链接一致

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已归档；交付随本任务提交。跟进见 [release-r6-pin-and-formal-d4](release-r6-pin-and-formal-d4.md)（已完成）→ [release-r6-resource-budget](../release-r6-resource-budget.md)。

## 证据

### 冻结身份（2026-09-07）

| 项 | 值 |
|---|---|
| 分支 | `codex/development` |
| 起点 / 终点 HEAD | `e8cb494d7c29a109f1379c24a10b137dd42d1c50`（短 `e8cb494d`） |
| 干净 checkout | 独立 worktree `/tmp/pf-r6-clean-20260907/checkout`（detached `e8cb494d`） |
| 采证目录 | `/tmp/pf-r6-clean-20260907/`（`freeze.txt` / `timeline.txt` / `results.txt` / 分步 `*.log`） |
| G-07 源码身份 | `e8cb494d` **+** 上表 6 个最窄修补（交付 commit 入库后即为该 commit；窗口内 HEAD 未漂移） |
| 发行 pin 映射 | 现有安装 pin `0.0.0-5ed2b916b569`（`5ed2b916b569…`）**≠** 本候选；**本窗未**对本候选 `release-pack` / 新 pin |
| 共享 compose | 仅复用 `productflow` postgres/redis healthy；**未** `down` |

### G-07 逐步 exit 表

在独立 checkout 上，对 `e8cb494d`+修补，无缓存（Go `-count=1`）重跑：

| 步骤 | 命令要点 | exit | 备注 |
|---|---|---|---|
| docs-check | `just docs-check` | 0 | |
| diff-check | `git diff --check`（修补路径） | 0 | |
| go-migrate | `productflow-migrate` | 0 | |
| schema-migrate-tests | `go test ./internal/platform/db/schema -count=1 -p 1` | 0 | fresh/upgrade |
| go-test-nocache | `go test ./... -count=1 -p 1` | 0 | ~4m27s；B6 三类红灯已消 |
| agent-service-test | `agent-service` vitest run | 0 | 初跑 `pnpm` CLI 11 vs pin 10.32.1 失败；改 `./node_modules/.bin/vitest run` → **311 passed / 9 skipped** |
| web-test | `pnpm --dir web test:run` | 0 | |
| web-lint | `pnpm --dir web lint` | 0 | |
| web-build | `CI=true just web-build`（含 bundle 预算） | 0 | shell raw 429257 / gzip 130173（预算 550000/170000） |
| docs-check-final | `just docs-check` | 0 | |

**G-07（本候选）**：**PASS**（修补后固定 HEAD 窗口）。

### 红灯最窄修补说明

1. Eval fixtures：`PRODUCTFLOW_UPDATE_EVAL_FIXTURES=1` 重生成 `catalog.json` / `intake-results.json`（对齐 `produce_route`；**未**改 Skill/grader）。
2. `merchant_id required`：generation/metrics 测试 `MustDevMerchantID`；media 避免 auth 循环依赖，直接 `INSERT merchants` + `products.merchant_id`。
3. Shell bundle：侧栏 `GraphAddNodePanel` / `GraphLibraryPanel` / `GraphNodeInspector` / `GraphRunsPanel` / `RecipeLibraryPanel` 改为 `React.lazy` + `Suspense`（**未**抬高预算数字）。

### 协调者复验（主工作树，修补落地后）

| 检查 | 结果 |
|---|---|
| `go test ./internal/media ./internal/platform/generation ./internal/platform/metrics -count=1 -p 1` | PASS |
| agent-service vitest（311/9） | PASS |
| `ProductWorkbenchSurface` 相关 vitest | PASS（8） |
| `CI=true just web-build` | exit 0 |

### D4 / 资源预算

| 项 | 结果 |
|---|---|
| 冻结稳定版对正式 D4 | **缺口**：仓库仅有发行 pin `0.0.0-5ed2b916b569` 与历史 tag `v0.1.0`；无第二冻结稳定 pin。不得用 B5 等价 retag 冒充。本窗未跑正式 D4。 |
| 资源预算↔部署规模 | **本窗未测**（标明缺口） |
| 发行 pin = G-07 身份 | **未齐**：明确映射为「pin 仍旧 / G-07 为 e8cb494d+修补」；缺本候选 pack |

### 父章程结论

- **总纲 R6：仍未通过**（不得关闭）。
- 依据：G-07 在干净 checkout 修补候选上 PASS；正式 D4 仍缺；pin≠本候选；资源预算未测。

### 未宣称

- **未宣称** R6 通过、正式 D4、SLA/RPO/RTO、本候选安装 pin。
- 未 push / reset。

### 审核

- 审核者：CTO（本会话）；结论：证据任务**完成**；G-07 干净候选 PASS 可采信；业务门槛 **R6 未通过**。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/release-r6-clean-candidate-gate.md` 查询）
- Issue 结果 / 业务门槛结果 / 剩余缺口：完成；**R6 未通过**；剩余＝对本交付 commit pack 新 pin、正式冻结稳定版对 D4、资源预算（可选另门）。
