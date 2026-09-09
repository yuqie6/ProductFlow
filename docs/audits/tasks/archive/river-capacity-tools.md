# 任务：容量与故障采证工具适配 River

状态：完成
类型：实现
认领者：river_tools
认领于：2026-09-09T16:15:00+08:00
完成后可拆：无

## 问题来源

[统一队列实现](pg-queue-integration.md)需要复用原有 A100/B20 和故障验证，采证工具仍读取旧信封字段。

## 做成什么样

工具读取原生 river_job 并绑定业务 queue_execution_id，保留原有容量、公平与故障验收合同；不把 attempted_at 当首次派发时间，不用指标改名隐藏分母或测量缺口。

## 前置与并行

候选 `/tmp/productflow-pg-queue-0909` 的 River 五类入口已实现。job args 为 actor、aggregate_id、merchant_id、execution_id；state 原生八状态，attempt/attempted_at/finalized_at。原 dispatcher 仅业务恢复，已去除 --interval/--limit，保留 --watch/--recovery-interval。

## 修改范围与所有权

仅候选 scripts/saas_capacity/ 全目录。不是唯一开发者，不覆盖他人。主代理拥有生产、文档、Go 测试；其它子代理仅业务包测试。无提交/推送/重置权限。只运行工具单元测试，不启动、停止或占用容量/共享服务；主代理负责完整实测。

## 合同与验收

更新旧表/字段读取、启动参数、指标映射及对应测试。保留 A100、B20 每五秒到达、max3、provider2秒、B p95≤10秒合同。执行 Python 单元测试、shell 语法检查，自审完整 diff 并报告运行完整公平/故障门的准确命令与前置。生产接口问题交主代理。

## 阻塞与交接

无。交付后等待审阅，不扩展任务。

## 证据

结果见下方最终审核；交付随本任务提交。

### 最终审核

主代理修复 repair_existing 的 River 观测分母缺口，删除镜像源码字符串断言；32 项 Python 单测和 shell 语法通过，固定候选公平及故障实测完成。采证始终绑定 actor、商家、业务 ID 和执行轮。 固定候选 ff23c2aa；详细现场结果由父任务持有。审核者 root，子代理自审后由主代理集成审阅，随本任务交付。
