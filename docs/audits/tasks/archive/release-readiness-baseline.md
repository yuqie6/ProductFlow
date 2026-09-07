# 任务：冻结自托管发行与恢复差距

状态：完成
类型：证据
认领者：sub-release/release-readiness-baseline
认领于：2026-09-07T11:55:25+08:00
完成于：2026-09-07T12:10:00+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：按证据发布固定发行物/生产配置、备份恢复和稳定版升级切片；不对开发站直接运行发布脚本

按 [Issue 协议](../README.md) 认领并确认所有权后调查。协调者审核：2026-09-07 CTO 确认覆盖清单、差距分类、D1–D4 与 B1–B6 可执行；nginx 上游名与 Compose 不一致已对照源码核实。R6 未通过属合同内正确声明。

## 问题来源

用户要求可自托管多商家 SaaS 并自行运营。[总纲第 9 节](../../../ROADMAP.md#9-自托管与自营站点交付) 已确定单主机 Compose 首版和稳定版升级责任。当前有 Compose 与健康检查脚本，但未取得完整发行、备份恢复和升级证据，不能把 `scripts/release.sh` 成功等同于商用版可交付。

## 做成什么样

在父章程给出当前运行单元、版本/镜像构建、必要配置、持久数据与密钥来源、外露端口、迁移流程的完整清单。区分已可用机制、缺实现、缺验证和需要 Operator 决策的项，形成隔离干净安装、数据库与媒体一致备份、恢复后权限/资产/任务校验、稳定 N→N+1 升级的可执行演练合同及后续修复批次。

## 前置与并行

- 前置：当前源码与部署文件可读取；总纲第 9、10 节与平台既有 G-07 合同已固定。
- 冻结输入：认领时 HEAD `d6709c4aacb2e26bb30ab70a99d08b1dca05f487`；部署相关文件以该基线只读核对。
- 运行资源：本任务只读源码和非敏感配置样例，不启动/停止 Docker、worker、DB，不读取 `.env` 秘密或真实存储内容。实际安装/恢复另任务预约隔离资源。
- 排他写入：本文件与父章程，和本组其它写者串行。

## 只改这些文件

- `docs/audits/performance-governance.md` 中发行差距、演练合同和批次。
- 本文件；归档和共享索引由协调者处理。

## 不要碰

当前实例、真实数据库、provider/密钥、部署脚本或业务实现、现有评测资源。不得运行 `just dev`、`just dev-stop` 或 `scripts/release.sh`；不得写入已通过 R6 的结论。

## 现在代码在哪

`docker-compose.yml`、`docker-compose.staging.yml`、`scripts/release.sh`、各服务 Dockerfile、`go/cmd/productflow-migrate/`、`go/internal/platform/db/schema/`、`go/internal/media/`、`go/internal/platform/storage/`、`agent-service/` 的持久路径及配置样例。读实际路径和调用者，不能仅重述总纲。

## 合同

- 首版共用同一产品发行物；自营实例不靠特例补配置。单主机 local storage 是允许的明确支持范围，不预设 S3/Kubernetes。
- 备份对象包括恢复所需数据库、媒体、必要 Pi 持久资料和解密配置，说明一致备份点及运行中作业如何处置。不能只备数据库。
- 首个稳定版起的升级与实验/V1/v2 迁移区分，后者不新增支持。列出需要随升级实施同步修订的现行文档规则，不在本证据任务扩改工程制度。
- 列全未知输入，不杜撰已达到的容量、停机时长、RPO/RTO 或服务级别。

## 怎么验收

逐运行单元将“配置输入→持久目录/表→备份项→恢复动作→业务断言”对应起来，检查 Docker 卷、端口、启动依赖和迁移代码。每条差距有可归属修复任务、必需环境与验收命令；至少推演空主机、缺凭据、恢复缺媒体/密钥、迁移失败、重启有 unknown 作业五类情况。

运行 `just docs-check` 与 `git diff --check`，自审清单有无遗漏路径、秘密或未验证完成声明。完成表示发行差距与演练合同明确，真实安装/恢复/升级以及 R6 仍未通过。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：认领后登记的执行者（已交付，等待协调者分配后续批次）。
- 交接：仅文档交付；未占用运行资源；未 git commit / 未 push。

## 证据

- 源码基线：`d6709c4aacb2e26bb30ab70a99d08b1dca05f487`。
- 父章程：[performance-governance.md §自托管发行与恢复基线](../../performance-governance.md#self-host-release-baseline) —— 运行单元、配置/密钥、端口、持久与备份对象、迁移流程、差距四分类、五类推演、D1–D4 演练合同、B1–B6 修复批次、未知输入。
- 审核者：CTO（本会话）；结论：通过，可拆 B1。
- 关键缺口摘录：`web/nginx.conf` 上游 `productflow-backend` ≠ Compose 服务 `productflow-go-api`；无版本化发行物与备份脚本；provider 密钥为 PG 明文（无独立密文层）；开发 Compose 默认映射 PG/Redis/metrics。
- 未执行：Docker up/down、`just release`、真实备份恢复、R6。未读 `.env` 或真实 storage。
- 验证：`just docs-check`；`git diff --check -- docs/audits/performance-governance.md docs/audits/tasks/release-readiness-baseline.md`。
- 交付定位：随本任务工作树修改；归档后用 Git 历史定位。
