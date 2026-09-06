# 任务：同步节点重构后的 Agent 评测观察快照

状态：完成
类型：实现
认领者：主代理-node-sync-0906-1703
认领于：2026-09-06T17:03:28+08:00
完成后可拆：无

## 问题来源

用户在节点重构 `0658ef30` 交付后明确要求同步 Agent 评测快照。当时 `agent-service/evals/fixtures/catalog.json` 仍含旧 `design_goals` 及 `generation_spec` 下的 `text_policy`、`text_language`；`intake-results.json` 同样保存旧生成参数。诊断命令 `env -u DATABASE_URL -u PRODUCTFLOW_UPDATE_EVAL_FIXTURES go test -C go ./internal/agent -run '^TestEvalObservationFixtures$' -count=1` 在 Catalog 比较阶段失败。本任务从 [图观察权威](../eval-graph-observation-authority.md) 拆出 Catalog/intake 观察同步及一致性回归；该任务继续拥有真实图操作反馈、拒绝语义与付费评测启动门槛，不因本任务完成关闭。

## 做成什么样

从当前已提交节点合同的固定 Go 版本生成 Catalog/intake 观察；全部输出节点配置通过当前 Catalog 校验，文字默认值与 `ImageTypeFamily` 一致；当前 fixtures 不含退役节点形状。无 update 模式时 Go/PG 观察一致性检查通过。同步不能证明图操作桩已具备完整真实语义。

## 所有权与资源

- 协调主代理确认本任务独占 `agent-service/evals/fixtures/` 中需更新的观察、必要消费测试及 `go/internal/agent/eval_observation_fixture_test.go` 的一致性校验；不修改生产 Skill、运行时、评分题目和历史批次。
- 使用当前已提交节点合同的固定独立 checkout 与 testdb 包隔离数据库；产物位于 `/tmp/productflow-node-sync-0906`。不变更共享 provider、DB 或 worker，不调用收费模型。
- 图片质量任务持有 `go/prompts/`，Agent Skill 任务不占本任务评测文件或运行资源；本任务不碰其未提交改动。

## 验收

- 从固定 Go 版本生成观察，检查全部输出节点配置与文字默认值；当前 fixtures 不含退役节点形状。
- 不开启 update 模式时 Go/PG 观察一致性检查通过；受影响 Node 测试、类型检查和生成合同检查通过。
- 检查完整 diff、自审、选择性提交与归档、`just docs-check`；明确同步不能证明图操作桩已具备完整真实语义。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：主代理-node-sync-0906-1703
- 交接：2026-09-06T17:03:28+08:00 用户指定当前会话接手。承接上一认领者 `主代理-node-sync-0906-0148` 的未提交 fixture/测试 diff；产物目录当时不存在。本轮从固定 HEAD checkout 重新生成观察，与接手 diff 字节一致，未沿用脏工作树里的 `go/prompts/`。

## 证据

- 命令 / 日期 / 结果：
  - 2026-09-06：独立 checkout `/tmp/productflow-node-sync-0906/checkout` 固定 `31478a8e`（含节点合同 `0658ef30`）。`PRODUCTFLOW_UPDATE_EVAL_FIXTURES=1 STORAGE_ROOT=/tmp/productflow-node-sync-0906/storage bash scripts/with_dev_env.sh go test -C <checkout>/go ./internal/agent -run '^TestEvalObservationFixtures$' -count=1` PASS（1.392s）。生成物与协调工作树已有 `catalog.json` / `intake-results.json` 字节相同。
  - 同日：`env -u PRODUCTFLOW_UPDATE_EVAL_FIXTURES` 复跑 `TestEvalObservationFixtures` PASS（1.17s）；`TestEvalObservationFixtures|TestEvalStructuralWriteFixtures` PASS（1.373s）。testdb 为包隔离库 `productflow_dev_gotest_agent`，未写 `productflow_dev`，未改共享 provider/worker。
  - 同日：`pnpm --dir agent-service exec vitest run evals/stub-world.test.ts evals/contract.test.ts` 2 files / 21 tests passed；`just agent-service-check-contracts` 通过；`tsc --noEmit --strict` 检查 `evals/stub-world.ts` 与对应测试通过。未调用收费模型。
- 基线 commit / run_id / artifact：观察生成基线 `31478a8e`；本地产物 `/tmp/productflow-node-sync-0906`。未采真实模型批次。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/eval-node-snapshot-sync.md` 查询）。
- 审核者 / 结论：主代理-node-sync-0906-1703 自审通过。Catalog 字段相对旧快照：`creative_brief` 以 `goal`/`key_messages`/`required_elements` 替换 `design_goals`/`required_copy`；`image_prompt` 增加 `text_settings`；`image_generation` 增加 `prompt_overrides`/`text_override`，删除 `generation_spec.text_policy`/`text_language` 与 `visual_overlay.prohibitions`。四道 intake 题共 12 个 `image_prompt`：infographic（含 `selling_point`、`specifications`）为 `policy=required, language=zh-CN`，其余为 `none/null`。`image_type_catalog` 与已提交 HEAD 一致，未吸入未提交的 `go/prompts/`。`stub-world.ts` 的 `update_node_config` 仍直接替换本地 config；结构未录入快照仍返回 `eval_unobservable`。本单不证明图操作桩具备完整真实 Go 语义。
- Issue 结果 / 业务门槛结果 / 剩余缺口：实现完成。图操作真实反馈、退役字段拒绝样例与付费 L1 启动门槛仍由 [图观察权威](../eval-graph-observation-authority.md) 持有。
