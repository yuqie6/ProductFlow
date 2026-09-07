# 任务：R6 发行就绪汇总门（B6）

状态：完成
类型：证据
认领者：sub-release/release-r6-readiness-gate
认领于：2026-09-07T15:15:00+08:00
完成于：2026-09-07T15:27:00+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：干净冻结候选上重跑 G-07 / 正式 D4；总纲 R6 关闭裁定；不降低门槛

按 [Issue 协议](../README.md) 认领。承接 [B5 N→N+1](release-n-to-n1-upgrade.md) 与父章程 B6/R6。协调者审核：2026-09-07 CTO 通过——证据齐可审；**Issue 结果完成且业务门槛 FAIL（总纲 R6 未通过）**；不得标 R6 通过。

## 问题来源

B1–B5 路径与部分证据已齐；R6 仍未通过。缺在冻结候选上汇总 G-07 与所需安装/恢复/升级证据的可关闭门。

## 做成什么样

在隔离资源上对冻结候选执行/汇总：空主机或等价 D1、D3、N→N+1（优先冻结稳定版对）、所需 G-07；缺口诚实列出。**不得**在证据不齐时把 R6 标为通过。

## 前置与并行

- 前置：B5 已归档。
- 隔离项目；禁止共享 `productflow` down。
- 排他写入：本文件、父章程 R6/B6、必要时 release 文档备注。

## 只改这些文件

- `docs/audits/tasks/release-r6-readiness-gate.md`（本文件）
- `docs/audits/performance-governance.md`（B6 / R6 结论；**不得**标通过）
- `release/README.md`（B6 汇总备注一句）

## 合同

- 父章程 B6 / 总纲 R6；完成可 FAIL（缺证据则不得标通过）。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：无。
- 交接：已归档；交付随本任务提交。跟进见 [release-r6-clean-candidate-gate](../release-r6-clean-candidate-gate.md)。

## 证据

（交付方采证表保留于下文；协调者确认：等价 D1 PASS、D3/B5 沿用、G-07 FAIL、R6 未通过。）

### 窗口与资源（2026-09-07）

| 项 | 值 |
|---|---|
| 分支 | `codex/development` |
| 发行 pin（D1/D3/D4 证据面） | `0.0.0-5ed2b916b569` / sha `5ed2b916b5698cc10623f5a724acc708e2927a4a` |
| G-07 尝试起点 HEAD | `63d41d0bf7340c70d2579a67af3d86ae308b94b5` |
| G-07 结束后 HEAD（并发漂移） | `d0959fc48dc2c3b933ab5b9b3b4b3997eb021531` |
| 隔离 D1 项目 | `pf-r6-d1-20260907`；端口 `APP=59480` / `WEB=59481` |
| 共享项目 | 仅 `productflow` postgres/redis；**未** `down` |

### R6 / B6 汇总表

| 门项 | 证据来源 | 结果 | 绑定身份 |
|---|---|---|---|
| D1 空主机/等价安装 | 本窗 tarball→隔离 compose | **PASS（等价 D1 主路径）**；缺浏览器「生成不可用」 | pin `0.0.0-5ed2b916b569` |
| D3 恢复 | [B4](release-d3-restore-drill.md) | **PASS**（在途 lease **UNKNOWN**） | 同上 |
| N→N+1 / D4 | [B5](release-n-to-n1-upgrade.md) | 等价夹具 **PASS**；冻结稳定版对 **未做** | N→等价 `b5n1equiv` |
| G-07 候选全量 | 本窗 | **FAIL**（脏树；HEAD 漂移；Go/lint/build） | 起点 `63d41d0b` |
| 资源预算↔部署规模 | — | **未测** | — |
| 总纲 R6 | 本表 | **未通过** | 不得标通过 |

### G-07 失败摘要

- Go：`TestEvalObservationFixtures` catalog drift；media/generation/metrics `merchant_id required`
- Web lint：`workbenchUiState` unused（归档后协调者已修）
- Web build：shell bundle 超预算
- 非干净 checkout + 并发 HEAD 漂移 → 不可签固定候选全量 PASS

### 缺口

1. 总纲 R6 未通过；安装 pin ≠ G-07 HEAD
2. 无冻结稳定版对正式 D4
3. G-07 需干净单一冻结候选重跑
4. D1 浏览器缺 provider UI；HEAD 全量 build/push 未做
5. D3 在途 lease 夹具仍 UNKNOWN；资源预算未测

### 审核

- 审核者：CTO（本会话）；结论：证据任务**完成**；业务门槛 **R6 未通过**（诚实 FAIL）。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/release-r6-readiness-gate.md` 查询）
- 未宣称：R6 通过、正式 D4、SLA/RPO/RTO
