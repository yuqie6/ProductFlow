# 任务：版本化发行物与锁定安装包

状态：开放
类型：实现
认领者：—
认领于：—
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：B3 备份/恢复脚本；空主机 D1 实跑可与本任务分证据或同隔离窗口预约

按 [Issue 协议](README.md) 认领。承接 [发行基线](archive/release-readiness-baseline.md) **B2**；前置 [B1 上游/端口](archive/release-compose-proxy-overlay.md) 已交付。

## 问题来源

B1 修好 web→API，但仍依赖 git checkout + 本地 build。总纲要求固定版本镜像与配置样例，空主机按发行物安装，不依赖作者本地 storage。

## 做成什么样

1. 不可变镜像 tag 约定与构建/推送（或本地 registry）脚本；锁定 compose + env 样例 + 版本文件组成的安装包。
2. 文档：无 git 工作树的空主机用发行物完成 D1 方向安装步骤（可与本任务同窗口隔离实跑，或另发证据单；合同须写清）。
3. 不实现完整备份（B3）或 N→N+1（B5）；不宣称 R6。

## 前置与并行

- 前置：B1 归档；prod-ports overlay 可用。
- 运行资源：隔离 compose/registry；禁止共享 `productflow` down。
- 排他写入：发行脚本、版本文件、compose 锁定片段、README 安装节、父章程 B2、本文件。

## 只改这些文件

认领后调查补齐；预期 `scripts/`、compose/版本清单、README、父章程与本文件。

## 不要碰

业务逻辑、schema、评测、共享开发数据卷、密钥入库。

## 合同

- 自营与自托管同一发行物。
- 安装不依赖隐藏作者配置。
- 诚实记录构建环境限制（若 registry TLS 等仍失败，不得伪装 HEAD 全量 build 已通）。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
