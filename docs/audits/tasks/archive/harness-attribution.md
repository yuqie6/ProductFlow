# 任务：生产与评测使用同一冻结壳身份（Self-Harness P2）

状态：完成
类型：实现
认领者：主代理-agent-0905-0458
认领于：2026-09-05T05:20:42+08:00
业务组：Agent 能力
父账本：agent-self-harness.md
完成后可拆：harness-traces.md（P2b）、harness-miner.md（P3）；分别核对冻结输入与开发集隔离前置，不同时发布 P4–P7

本文件限定交付合同；按 [Issue 协议](../README.md) 确认认领后调查和设计。P2 完成前不实施 P2b–P7。

## 问题来源

父章程 P2 / D-12 要求生产模型调用和评测能追溯实际执行的领域壳。发布时 [P1](harness-artifact.md) 已在 `ab4eb8b7` 交付可哈希冻结工件，尚未将 hash 写入生产 invocation、checkpoint、eval `run.json` 或健康观察。

## 做成什么样

生产每个新模型请求的 `before_model_request` checkpoint 与 `agent_model_invocations` 都记录本次 Pi 实际使用的 `harness_hash`；eval 的 `run.json` 记录相同工件身份。现有 Agent 健康端点可观察已加载的 hash，不从磁盘另读一个可能与在途 Turn 不同的版本。无热切、提案或行为变化，不把字段存在当能力提升。

## 前置与并行

- 前置：P1 已完成并提交；不依赖行为评分或人工标签。
- 冻结输入：本任务不改 Skill、题目、world、grader、模型配置；改变 runtime 和 eval metadata，不能与同 checkout 的 Agent live 采证并行。其它评测实现需先检查源码路径占用。
- 运行资源：Node 隔离夹具、Go 按仓库 testdb 规则使用包级测试库，禁止清空共享 dev DB 或改共享 provider；健康测试使用临时端口。图片采池的浏览器、storage 与共享 provider 保持原认领。
- 本轮认领确认：主代理核对 P1 已提交、无剩余源码 diff；图片组仅占用其任务与采池资源，无路径交集。本会话自行执行并自审 P2，不启动真实模型或共享 dev 变更。

## 只改这些文件

- `agent-service/src/harness.ts`、`pi-runtime.ts`：冻结对象的唯一所有者与实际装配身份。
- `agent-service/src/turn-runtime.ts`、`contracts.ts`、`runtime-manager.ts` 及 Pi/HTTP 直接测试：只改 checkpoint/健康归因链；`server.ts` 直接返回 manager 健康对象，无需改路由，不重构调度、lease、journal。
- `agent-service/evals/run-storage.ts` 及各 runner 的 run metadata 归因接线、直接测试；不改试验矩阵、评分规则或 trial 业务。
- `go/internal/agent/` 中 checkpoint 解析、验证、invocation 持久化及直接回归；`go/internal/platform/db/schema/` 中 invocation 模型与 schema 合同；`go/cmd/productflow-migrate/` 的必要迁移回归。
- Go L2 `eval_state_gopg_test.go` 的 `run.json` 从本次启动的 Pi `/healthz` 取得身份；只改 metadata，不改题目、评分或 trial 语义。
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
- 交付定位：随本任务提交，通过本归档文件 Git 历史定位。
- 实现：`DEPLOYED_HARNESS` 为 Node 装配、checkpoint、健康和 Node eval 唯一冻结对象；Go L2 从本次启动的 Pi `/healthz` 取值。新增 nullable `agent_model_invocations.harness_hash`，新请求严格验证 lowercase SHA-256，重放不能改变身份；历史 NULL 不回填。
- 边界测试：Node 实际 fake-provider checkpoint 与健康一致；L1/L3/L5 runner metadata 接线、storage 文件持久化；Go HTTP 拒绝缺失、非十六进制、大小写、长度、空白与错误类型，验证事务回滚、同请求重放、改 hash 冲突、历史 NULL 不被补写；rollback-only 迁移测试模拟旧表补列并两次 Apply，历史行保留 NULL。
- 完整跨进程证据：`TestDurableAnswerCreatesNewAttemptAndInjectsPiToolResult` 使用真实 Pi SDK + 本地 fake Responses + Go HTTP + PG，重启问答后两个 attempt 的 checkpoint / invocation 与运行中健康端点、实际 L2 `run.json` 一致。该测试没有使用真实付费模型。
- `just agent-service-test`：32 文件通过，221 passed / 2 skipped；`pnpm --dir agent-service build` 通过。
- `bash scripts/with_dev_env.sh go test -C go ./internal/agent -run 'TestModelInvocation|TestHarnessHashSchema|TestL2Metadata' -count=1 -timeout 3m` 通过。
- `bash scripts/with_dev_env.sh go test -C go ./internal/agent ./internal/platform/db/schema ./cmd/productflow-migrate -count=1 -p 1 -timeout 6m` 最终通过；migrate 命令包无测试文件，schema 补列证据来自 agent 专项与 schema 包。
- 首轮 Go 包级库执行出现既有 `TestRecoverUnfinishedTurnsPreservesExpiredHasMore` 计数失败；单测重复 5 次曾有一次不同的 has_more 失败。基线 `0cec9396` 相同单测重复 5 次通过。新增归因测试补充清理自己的活动 projection；独立 binary 派生库 `/tmp/productflow_p2_agent.test` 全包通过，常规包级库再次全包通过。未修改 recovery 生产逻辑；首次失败的根因未作确定结论。
- `just agent-evals-coverage`：83 tasks，tools 24/24，ops 12/12；`just docs-check`、`git diff --check` 通过。
- 构建后的冻结 hash 仍为 `13e8e19ae0ba1cadc732f5f4ba6d0ffba0da08d5a859671ccb67d72bf52dae21`，与 P1 一致。无 Skill、工件指令、题目、grader 或 provider 设置变更。
- 审核：主代理-agent-0905-0458 自审完整 exclusive diff、未跟踪测试及跨层契约；非独立审核。图片组章程、采池任务与其看板认领不纳入提交。
- 部署限制：未迁移共享 dev DB、未重启共享服务。部署需 `just go-migrate` 加列并协调 Go/Node 版本；旧 Node 无 hash 请求会被拒绝，须等活动 Turn 排空。P1 完整 Docker 镜像构建的网络缺口仍保留。
- 阶段裁定：Self-Harness P2 完成；G1/G2、能力涨分、自动晋升与评测组的独立出口均未完成。P2b/P3 后续按其依赖发布。
