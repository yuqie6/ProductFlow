# 任务：R6 资源预算与部署规模对应

状态：开放
类型：证据
认领者：—
认领于：—
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：总纲 R6 关闭裁定汇总门（仅当本门与既有 D1/D3/D4/G-07/pin 证据齐）

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。

## 问题来源

[pin + 正式 D4](archive/release-r6-pin-and-formal-d4.md) 已交付 pin `0.0.0-67b0f3092158`（≡`67b0f309`）与正式 D4 PASS，并诚实记录：**总纲 R6 仍未通过**，主缺口为 ROADMAP R6 的「资源预算与部署规模对应」未测。父章程禁止把未测容量写成 SLA。

## 做成什么样

1. 在隔离 compose 上起 pin `0.0.0-67b0f3092158`（可用既有 pack `/tmp/pf-r6-d4-20260907/packs/…` 或同 pin 重 pack）；采证**空闲稳态**与**轻负载**（至少一次四项 health + 一次登录或 `/api` 探针）下各服务容器的 CPU/内存占用（`docker stats` 或等价），并记录宿主可用核数/内存。
2. 写出**部署规模对应表**：默认单副本拓扑（go-api / worker / dispatcher / agent / web / postgres / redis）→ 观测到的资源足迹区间；对照代码/配置中的并发与 admission 上限（如 generation capacity、worker 并发相关 env），说明「推荐最小主机规格」的**观测依据**，**不得**写成产品 SLA/RPO/RTO。
3. 缺口诚实列出（未测满载、未测多商、未测 staging 双副本等）。
4. 更新父章程与必要时 `release/README.md` 一句；**不得单独因本门标 R6 通过**——R6 关闭另开汇总或由维护者裁定。

## 前置与并行

- 前置：pin+正式 D4 已归档；冻结 pin `0.0.0-67b0f3092158` / SHA `67b0f309…`。
- 隔离项目；禁止共享 `productflow` down。
- 排他写入：本文件、父章程资源预算/R6 条目、必要时 `release/README.md`。

## 只改这些文件

- `docs/audits/tasks/release-r6-resource-budget.md`（本文件）
- `docs/audits/performance-governance.md`（资源预算证据；**不得**假标 R6 通过）
- `release/README.md`（可选一句）

## 不要碰

- 杜撰 SLA/RPO/RTO；把观测数字写成保证吞吐；Skill/grader；共享 `productflow` down。

## 现在代码在哪

- pin/D4 证据：[release-r6-pin-and-formal-d4](archive/release-r6-pin-and-formal-d4.md)
- 容量/admission 历史：父章程 PERF 节与 `go/internal` generation capacity
- 发行拓扑：`release/` + `docker-compose*.yml`

## 合同

- ROADMAP R6「资源预算与部署规模对应」；完成可 FAIL；观测证据 ≠ SLA。
- 本门 PASS 不自动关闭总纲 R6。

## 怎么验收

- 隔离栈 pin 身份可核对；stats 原始日志入库证据目录
- 对应表含拓扑、观测区间、对照的配置上限、明确非 SLA 声明
- 父章程与证据链接一致；`just docs-check` PASS

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无未提交 diff、运行进程或资源占用；实际阻塞时按现场更新。

## 证据

- 命令 / 日期 / 结果：
- 基线 commit / run_id / artifact（适用时）：
- 交付定位：随本任务提交
- 审核者 / 结论：
- Issue 结果 / 业务门槛结果 / 剩余缺口：
