# 任务：备份恢复脚本与一致点 runbook（B3）

状态：认领
类型：实现
认领者：sub-release/release-backup-restore
认领于：2026-09-07T12:37:30+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：B4 D3 恢复演练证据；不宣称 R6

按 [Issue 协议](README.md) 认领。承接 [发行基线](archive/release-readiness-baseline.md) **B3**；前置 [B2 发行物](archive/release-versioned-artifact.md) 已交付。

## 问题来源

总纲要求可恢复：数据库、媒体、Pi 持久材料与可启动配置须一致备份。当前有机制描述，无官方 dump/卷打包、校验清单与恢复 runbook 脚本。

## 做成什么样

1. 备份/恢复脚本（或明确 compose 步骤脚本）覆盖：PostgreSQL、storage 媒体、agent `/data`、部署 `.env`；可选 Redis；记录 commit/digest 与是否排空在途作业。
2. Runbook：一致点建立、恢复到新目录/实例的步骤与业务断言清单（对齐 D2）。
3. 不在本任务宣称完整 D3 隔离实跑通过（可留给 B4）；不写虚假 RPO/RTO/SLA。

## 前置与并行

- 前置：B2 归档；`release/` 安装包可用。
- 运行资源：隔离卷/项目；禁止共享 `productflow` down；不读真实生产秘密入库。
- 排他写入：备份/恢复脚本、`release/` 或 docs 中 runbook、父章程 B3、本文件。勿改 auth/product/delivery 业务实现。

## 只改这些文件

认领后补齐；预期 `scripts/release-backup*.sh` / `release-restore*.sh`、`release/README.md` 恢复节、父章程、本文件。

## 不要碰

业务 schema、评测、共享开发数据、密钥明文入库、B5 升级合同。

## 合同

- 备份对象不能只有数据库。
- 自营与自托管同一工具链。
- 完成 ≠ R6 / ≠ D3 全项实跑。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：sub-release/release-backup-restore。
- 交接：已认领。

## 证据

- 待补。
