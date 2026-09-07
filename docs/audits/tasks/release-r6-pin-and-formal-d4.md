# 任务：R6 候选 pin 对齐与正式 D4

状态：开放
类型：证据
认领者：—
认领于：—
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：总纲 R6 关闭裁定（仅当 pin=G-07、正式 D4 PASS 且其余 R6 项齐）；资源预算可另门

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。

## 问题来源

[干净候选门](archive/release-r6-clean-candidate-gate.md) 已在 `e8cb494d`+最窄修补上使 G-07 PASS，并诚实记录：**总纲 R6 仍未通过**。缺口为发行 pin≠G-07 候选、无正式冻结稳定版对 D4、资源预算未测。修补随干净候选门交付入库后，需对新 commit 打 pin 并跑正式 D4。

## 做成什么样

1. 以**干净候选门交付 commit**（含 6 修补的那次提交）为 G-07 源码身份；对该 SHA 执行 `release-build-images`（或诚实记录 TLS/网络失败）与 `release-pack`，产生新不可变 pin `VERSION-<sha12>`。
2. 以旧 pin `0.0.0-5ed2b916b569` 为 N、新 pin 为 N+1，在隔离项目上跑**正式 D4**（`release-upgrade.sh` 主路径 + migrate fail-stop）；**不得**用同镜像等价 retag 冒充冻结对。
3. 资源预算↔部署规模：本窗若不做须标明缺口，不得因此伪称 R6 通过。
4. 更新父章程与 `release/README.md`；**全项未齐不得标 R6 通过**。

## 前置与并行

- 前置：干净候选门已归档完成；协调树已提交修补。
- 冻结输入：交付 commit SHA；禁止窗口内再改被测运行时行为。
- 隔离 Docker 项目；禁止共享 `productflow` down。
- 排他写入：本文件、父章程 R6/D4 条目、`release/README.md`、必要时 pack 产出路径备注；不改 Skill/grader。

## 只改这些文件

- `docs/audits/tasks/release-r6-pin-and-formal-d4.md`（本文件）
- `docs/audits/performance-governance.md`（R6 / D4；**不得**标 R6 通过除非证据齐）
- `release/README.md`（一句 pin/D4 备注）
- 若 pack 脚本发现合同缺口：最窄修复 `scripts/release-*.sh` / `release/` 模板（须列入证据）

## 不要碰

- 把 R6 标为通过而缺 pin=G-07 或正式 D4；用 B5 等价 retag 冒充冻结对；Skill/grader；共享 `productflow` down。

## 现在代码在哪

- G-07 PASS 证据：[release-r6-clean-candidate-gate](archive/release-r6-clean-candidate-gate.md)
- B5 等价夹具（≠正式 D4）：[release-n-to-n1-upgrade](archive/release-n-to-n1-upgrade.md)
- 脚本：`scripts/release-build-images.sh` / `release-pack.sh` / `release-upgrade.sh`
- 父章程：[performance-governance.md](../performance-governance.md) D4/B6/R6

## 合同

- 总纲 R6；完成可 FAIL；缺 pin 对齐或正式 D4 则不得关闭 R6。
- 正式 D4 ≠ B5 等价 retag。

## 怎么验收

- 新 pin 的 `VERSION` / `images.env` / 包内 sha 与交付 commit 一致（或诚实映射表）
- 隔离 D4：升级主路径四项 health；`UPGRADE_SIMULATE_MIGRATE_FAIL` fail-stop + 钉回 N
- 父章程结论与证据链接一致；未宣称 SLA/RPO/RTO

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无未提交 diff、运行进程或资源占用；实际阻塞时按现场更新。

## 证据

- 命令 / 日期 / 结果：
- 基线 commit / run_id / artifact（适用时）：
- 交付定位：随本任务提交（用 `git log --follow -- <归档任务路径>` 查询）
- 审核者 / 结论：
- Issue 结果 / 业务门槛结果 / 剩余缺口：
