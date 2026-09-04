# 任务：生产与评测使用同一冻结壳身份（Self-Harness P2）

状态：开放
类型：实现
认领者：—
认领于：—
业务组：Agent 能力
父账本：agent-self-harness.md
完成后可拆：harness-traces.md（P2b）、harness-miner.md（P3）；分别核对冻结输入与开发集隔离前置，不同时发布 P4–P7

本文件限定交付合同；按 [Issue 协议](README.md) 确认认领后调查和设计。P2 完成前不实施 P2b–P7。

## 问题来源

父章程 P2 / D-12 要求生产模型调用和评测能追溯实际执行的领域壳。[P1](archive/harness-artifact.md) 已在 `ab4eb8b7` 交付可哈希冻结工件，尚未将 hash 写入生产 invocation、checkpoint、eval `run.json` 或健康观察。

## 做成什么样

生产每个新模型请求的 `before_model_request` checkpoint 与 `agent_model_invocations` 都记录本次 Pi 实际使用的 `harness_hash`；eval 的 `run.json` 记录相同工件身份。现有 Agent 健康端点可观察已加载的 hash，不从磁盘另读一个可能与在途 Turn 不同的版本。无热切、提案或行为变化，不把字段存在当能力提升。

## 前置与并行

- 前置：P1 已完成并提交；不依赖行为评分或人工标签。
- 冻结输入：本任务不改 Skill、题目、world、grader、模型配置；改变 runtime 和 eval metadata，不能与同 checkout 的 Agent live 采证并行。其它评测实现需先检查源码路径占用。
- 运行资源：Node 隔离夹具、Go 按仓库 testdb 规则使用包级测试库，禁止清空共享 dev DB 或改共享 provider；健康测试使用临时端口。图片采池的浏览器、storage 与共享 provider 保持原认领。

## 只改这些文件

- `agent-service/src/harness.ts`、`pi-runtime.ts`：冻结对象的唯一所有者与实际装配身份。
- `agent-service/src/turn-runtime.ts`、`contracts.ts`、`productflow.ts`、`server.ts`：只改 checkpoint/健康归因链；确认具体 owner 后调整到实际负责文件，不重构调度、lease、journal。
- `agent-service/evals/run-storage.ts` 及各 runner 的 run metadata 归因接线、直接测试；不改试验矩阵、评分规则或 trial 业务。
- `go/internal/agent/` 中 checkpoint 解析、验证、invocation 持久化及直接回归；`go/internal/platform/db/schema/` 中 invocation 模型与 schema 合同；`go/cmd/productflow-migrate/` 的必要迁移回归。
- `docs/ARCHITECTURE.md` / `.en.md`、harness README 的当前归因事实；任务/父章程/索引/归档由维护者整合。

## 不要碰

- 生产 Skill、行为指令、冻结权限段、Graph Command、工具 schema、UI journal 完整参数存储。
- Miner、overlay 内容、Steer、playbook、Promoter、接受规则、评分与数据集划分。
- 旧数据回填、兼容 serializer、新增旧路由别名或凭空生成历史 hash。

## 当前锚点

`agent-service/src/harness.ts` 的 `loadHarness` 与 `pi-runtime.ts` 模块冻结对象是 P1 实际基线；生产 `checkpointModelRequest` / `before_model_request` 经 ProductFlow client 进入 Go checkpoint handler 和 invocation 模型。`evals/live-runner.ts` 与 `run-storage.ts` 记录现有 commit/skill/task/model metadata。`server.ts` 的现有健康端点是 `/healthz`，沿用真实路由，不因章程简称 `/health` 新增别名。具体读写者须在修改前全量搜索。

## 合同与验收

- Node 装配、checkpoint、健康观察与 eval provenance 引用同一已加载不可变对象；在途执行不因源文件变化重标身份。
- `harness_hash` 为规范 SHA-256；Go 边界拒绝非法新请求，新的 invocation 与 checkpoint 原始记录一致，重放不能覆盖已有调用的身份。历史行保持 NULL，不回填。
- 保留 `skill_catalog_hash`、task hash、provider/model 与既有记录；不把基础 Skill 或模型差异伪装成工件相同。
- P1 尚无 parent/lineage，健康观察若包含这些字段明确为空，不捏造谱系。实际 provider/model 按现有 provider 观察合同展示，不写入全局工件 hash。
- `just agent-service-test`、`pnpm --dir agent-service build`、相关 Go agent/schema/migrate 确定性测试与 `just docs-check`。回归验证 Node 实际发出的 checkpoint、Go HTTP→DB 读回、健康与 eval 文件一致；新增字段不能破坏默认 metadata 报告和生产 E2E。

## 证据

- 发布：2026-09-05，Agent 组主代理核对 P1 归档及现有看板，无重叠归因任务。
- 实现、审核与验证结果待登记；提交定位随完成交付，不先宣称 P2 完成。
