# 任务：让 L1 图工具反馈来自真实 Go 语义

状态：完成
类型：实现
认领者：主代理-graph-obs-0906-1740
认领于：2026-09-06T17:40:00+08:00
完成后可拆：恢复 eval-development-baseline 的完整冻结开发采证

遵循 [Issue 协议](../README.md)，解除前置并确认认领后执行。本任务由协调主代理依据用户“质量能够用于自动化测评”的目标发布，不改变生产 Skill 或降低测评门槛。

## 问题来源

第三批 `20260905T131829Z-35b08c45` 的解散重排和禁止批量直接 apply 两题出现缺失结构观察。`stub-world.ts` 仅在操作数组精确命中预录快照时处理结构修改，否则向模型返回 `eval_unobservable`。这些反馈无法证明真实服务会接受或拒绝同一操作。

进一步只读检查发现 `update_node_config` 直接替换本地 config 后返回成功，不经过 Go 配置校验；第三批该题 trial 1/2 的顶层 `config.design_goal` 被桩接受，但题目期望 `config.prompt.design_goal`。不能据此把当前评分期望改成接受另一字段，须对照稳定后的生产合同裁定。现有 `TestEvalStructuralWriteFixtures` 只采集删一个节点与断一条边，不能代表完整可达的图修改反馈。

`3e5b3972` 与 `e48ab7fd` 已阻断不可测记录进入开发导出或能力比较。这防止错误消费，没有解决真实观察覆盖不足。

## 做成什么样

L1 图工具执行、拒绝及后续读取由同一真实 Go 合同决定。合法操作组合不得因未录入精确数组而获得虚构拒绝；非法配置不得被桩伪造为成功。不得在 TypeScript 复制图执行语义，也不得只为第三批出现的两个数组补特例。

## 前置与并行

- 前置：[节点重构](node-detail-redesign.md) 已在 `44078831` 提交；后续 [节点合同补齐](node-detail-contract-completion.md) 已完成实现与验收，固定版本通过该归档的 Git 历史定位；执行本任务前仍需确认提交成功、当前占用和隔离资源。字段变更与跨消费者回归见该归档「固定合同交接」，不得在未提交改动上生成正式观察证据。
- 已交付：读取义务修复 `8cf6e08a`，不可测导出 `3e5b3972`，未知终态边界 `e48ab7fd`。
- 采集使用独立固定 checkout、隔离测试 PostgreSQL 与独立 STORAGE_ROOT。先核实测试宿主的数据库命名、清理和并发，不能只凭独立进程宣称隔离；不修改共享 provider、DB 或 worker。
- 本任务不需要真实模型请求；有效离线/Go 反馈对齐完成后再由开发基线任务付费采证。

## 修改与调查范围

- `agent-service/evals/stub-world.ts`、`go-world.ts`、`live-runner.ts`、相关测试及必要 fixtures/world/task 合同校正。
- `go/internal/agent/eval_user_sim_host_test.go`、`eval_observation_fixture_test.go` 与必要的 Agent 评测测试辅助；优先复用已有 Go 宿主与播种模型。
- 入口/文档只更新真实新增的依赖、资源和验证合同。生产 Go 节点实现、Skill/harness 不由本任务改动。
- 当前宿主只加载 L3；直接复用还会覆盖读写方法、绕过桩的故障注入。接入前逐项检查上下文、graph revision、身份映射、read/write 错误和 source request 参数，不能只打开一个开关便宣称等价。

## 合同

- 同一 trial 的上下文、节点详情、写入反馈与写后读取必须指向同一状态；失败事务无部分本地效果，未知结果不伪造成功或失败。
- 409/读取失败等已冻结注入继续生效，重试使用真实最新版本。成功与拒绝必须保留实际参数、次数和观察状态。
- 业务拒绝、宿主基础设施故障与缺失观察分开；不得把宿主所有错误一律映射为业务 422。
- 新增组合、操作顺序、删除连带边/组成员、解散、重排、配置合法性和连续多次写入均由生产语义验证。
- L1 仍评分工具/操作/写入/终态；采用 Go 反馈不代表完成 L2 持久化质量或 L3 用户决策验收。
- 旧三批不可拼接、改写或回填。现有不可测导出与未知终态拒绝规则保留。

## 用户追加：节点合同快照漂移

2026-09-06 用户要求跟进合同变更对 Agent/eval 的影响。本节记录合同回归会话的诊断交接，不解除节点合同前置、不自行认领，也不修改原冻结批次。

- 当前 `agent-service/evals/fixtures/catalog.json` 仍包含旧 `design_goals` 及 generation_spec 下的 `text_policy`、`text_language`；`intake-results.json` 同样保存旧生成参数。`stub-world.ts` 实际加载两份快照，线上 Agent 则读取 Go 当前 Catalog，存在可观察的合同漂移。
- 诊断命令 `env -u DATABASE_URL -u PRODUCTFLOW_UPDATE_EVAL_FIXTURES go test -C go ./internal/agent -run '^TestEvalObservationFixtures$' -count=1` 失败，报 `eval observation fixture drift`，定位 `catalog.json`。失败发生在数据库初始化之前；这仅证明 Catalog 漂移，不是 intake 的 Go+PG 对照证据。
- 同时 `just agent-service-check-contracts` 通过。其生成物检查不覆盖 Catalog/intake 快照，不能单独作为本任务验收依据。
- 稳定合同交付后，从同一固定 Go 版本刷新当前 Catalog/intake 观察，验证所有输出节点配置和默认文字设置；保留旧批次原始快照与评分身份，不回填历史结果。
- 对真实 Go 反馈增加明确拒绝样例：generation_spec 带退役文字字段、creative_brief 带退役 design_goals，以及对应新字段的合法写入。apply、proposal、写后读取与失败无部分效果均须对齐；不得仅更新 fixture 后继续由 stub 直接接受任意 config。
- 在新付费 L1 启动前执行并通过观察一致性检查；启动入口应在漂移时明确失败，避免仅靠 Node 生成合同检查漏检。该入口改动仍受本任务固定输入、资源和开发基线协调要求约束。
- 2026-09-06：[快照同步](eval-node-snapshot-sync.md) 已从固定 HEAD `31478a8e` 刷新 Catalog/intake；当前 fixtures 不再含 `design_goals` 或 `generation_spec.text_policy`/`text_language`。上列漂移诊断描述刷新前状态。本任务仍须补齐真实 Go 图操作反馈、退役字段拒绝样例与付费启动门槛，不得只靠已刷新快照关闭。

## 怎么验收

- 确定性 Node 回归与真实 Go+PG 对照：合法组合、错误配置、成功后的读取、失败无部分效果、409 重试及宿主故障分类。
- 参考题与非参考操作变体都必须覆盖，不能只执行 expected 的 scripted_calls 自证。
- 受影响 eval 测试、TypeScript、生成合同、相关 Go 测试与 docs-check；明确记录实际 DB、固定版本、清理结果及未验证项。
- 若改变 L1 运行资源合同，修改前由协调者确认并更新开发基线任务；不得在已启动的批次中替换环境。

## 阻塞与交接

- 2026-09-06T17:40:00+08:00：用户确认原执行会话已关闭，由本会话接手半成品。节点合同补齐与 Catalog/intake 快照均已归档；前置已具备。协调者释放失联的 eval-skills 占用后确认本单认领。不修改共享 provider、不启动付费 L1。
- 跟进者：主代理-graph-obs-0906-1740。
- 交接：无既有本任务代码 diff 或运行进程。原始证据仍由开发基线保管：`storage-dev/eval-development-20260905-r3/agent-evals/20260905T131829Z-35b08c45/`。

## 证据

- 命令 / 日期 / 结果：
  - 2026-09-06：`bash scripts/with_dev_env.sh bash -lc 'PRODUCTFLOW_RUN_AGENT_EVALS_GOPG=1 pnpm --dir agent-service exec vitest run evals/graph-authority.test.ts evals/go-world.test.ts --maxWorkers=1'` → 2 files / 7 tests passed，28.05s。testdb 由 `newEvalLibraryServer` → `testdb.IsolatedMigrated` 按宿主进程建隔离库，未写共享 `productflow_dev`。
  - 同日：`just agent-service-test` → 37 files passed / 2 skipped，311 passed / 9 skipped。默认 Vitest 不启动 L1 图宿主。
  - 同日：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/agent -count=1 -timeout 180s'` → `ok github.com/yuqie6/productflow/internal/agent 86.959s`，含 `TestEvalGraphWriteAuthority`、`TestEvalHostErrorStatus`、`TestEvalObservationFixtures`。
  - 同日：`just agent-evals-coverage` → tools 24/24、ops 12/12、complete=true。
  - 同日：`just docs-check` 与 `git diff --check` 随交付提交执行。未调用收费模型，未启动第四次付费 L1。
- 基线 commit / run_id / artifact：实现对照 `61426ed6`。Catalog/intake 卖点文案随已提交 `61426ed6` 提示词刷新两处 description，使 `TestEvalObservationFixtures` 与当前 Catalog 一致。旧三批不回填。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/eval-graph-observation-authority.md` 查询）。
- 审核者 / 结论：主代理-graph-obs-0906-1740 自审通过。L1 graph overlay（`PRODUCTFLOW_EVAL_HOST_LAYER=l1`）绑定 apply/propose/discard/context/node；`apperr` 保留原 HTTP 状态，基础设施为 500 `eval_host`。绑定宿主后跳过本地 revision 预校验，409 注入不伪造涨版本。Go 覆盖解散、错误路径 `design_goal`、退役 `design_goals` / `generation_spec.text_policy`、合法嵌套写入、多步 apply 拒绝、删除连带边、propose 拒绝无部分效果。Node GOPG 覆盖解散、非法/合法 config、409 后按 Go revision 重试、propose 退役字段 409。
- Issue 结果 / 业务门槛结果 / 剩余缺口：实现完成。D-02 改为 `部分完成`：catalog/intake 仍用 fixtures；付费 L1 与 Skill A/B 仍由协调者冻结新身份后执行。L1 graph-editing 现依赖隔离 testdb 与 `DATABASE_URL`，资源合同已写入 ARCHITECTURE 与 [开发基线](../eval-development-baseline.md)。eval-skills 仍阻塞。
