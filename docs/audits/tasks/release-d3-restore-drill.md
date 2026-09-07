# 任务：D3 恢复演练证据（B4）

状态：开放
类型：证据
认领者：—
认领于：—
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：B5 N→N+1；不宣称 R6

按 [Issue 协议](README.md) 认领。承接 [B3 备份恢复](archive/release-backup-restore.md) 与父章程 D3。

## 问题来源

B3 已交付备份/恢复脚本与数据面冒烟；缺完整 D3：新目录/实例恢复后权限、资产、任务与关键服务健康的可复核证据。

## 做成什么样

1. 用官方 `release-backup` / `release-restore` 在隔离项目做一次全栈恢复演练（真实或接近真实的 compose 镜像，非仅 stub 数据面）。
2. 记录：登录/会话、媒体 HTTP、Agent health、在途作业收敛（或明确 unknown）、版本身份与 CHECKSUMS。
3. 证据写入本文件与父章程 B4 行；缺口诚实列出。不写虚假 RPO/RTO/SLA；不宣称 R6。

## 前置与并行

- 前置：B3 已归档。
- 隔离 compose 项目与卷；禁止共享 `productflow` down；不入库真实生产秘密。
- 排他写入：本文件、父章程 B4 结论、必要时 `release/README.md` 演练备注。勿改业务 schema/auth/product。

## 只改这些文件

认领后补齐。

## 合同

- 父章程 D3 / B4；完成 ≠ R6 / ≠ B5。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
