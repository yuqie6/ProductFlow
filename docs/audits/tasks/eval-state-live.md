# 任务：L2 PostgreSQL 终态采证

状态：阻塞
类型：证据
认领者：—
认领于：—
业务组：评测
父账本：agent-eval-system.md
完成后可拆：与 eval-adversarial-live 都有有效 run_id 后，由维护者核对是否可发布 eval-nightly；失败修复按根因另行发单

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](README.md)。执行前读适用仓库规则、当前 runner、测试和 diff。

## 做成什么样

核验已有 L2 全量产物，登记至少 15 题、每 Skill 至少 3 题、k=3 的有效运行报告和四类 PostgreSQL world 的 state 判读。有效 FAIL 可完成本次采证，P2 业务出口继续未通过。旧产物已满足本次范围时直接核验并关闭，不为生成新 run_id 重复付费；若文件丢失、不可复算或基线已失效，则登记原因后在固定版本补跑。

## 前置与并行

- 前置：核验 `20260904T185620Z-87f8a800` 对应产物是否仍存在；补跑需测试 PostgreSQL、真实 provider key 和 `PRODUCTFLOW_RUN_AGENT_EVALS_L2=1`。
- 冻结输入：补跑全程固定 `agent-service/`、`go/internal/agent/`、`go/internal/graph/`、`go/prompts/agent/` 与模型配置。共享 checkout 时与 Skill、loader、harness 等输入修改串行，不能只检查启动时干净。
- 运行资源：按现有 runner 隔离商品/会话与 Node/Pi 进程；使用测试 DB，禁止重建共享 dev 数据。固定 checkout 或预约完整冻结窗口，独立 run 目录。

## 只改这些文件

- 本文件

## 不要碰

- 任何生产代码、Skill、任务 JSON、grader；失败交维护者按根因发布修复。
- L5 与生产 mine 不属于本 issue。

## 现在代码在哪

`go/internal/agent/eval_state_gopg_test.go` 调用现有 Node/Pi 与 PostgreSQL，`evalworld_test.go` 提供 world，`evaltask.go` 加载任务。入口为 `just agent-evals-state`。

## 合同

- 父章程 L2-01/L2-04/L2-06：至少 15 题、每 Skill 至少 3 题、k=3，断言真实 Go/PG 状态，不能用桩工具调用代替。
- 被测路径保持生产 `PiRuntimeManager`；任务失败不更改 grader 凑分。
- 原始产物只落 `STORAGE_ROOT/agent-evals/`，本文件记脱敏摘要、commit、run_id、模型、任务/Skill hash、命令、n/k、结果与路径。

## 怎么验收

检查已有 run 的 `run.json`、trial 结果与任务分布，区分完整有效 FAIL、基础设施中止和缺失样本；从产物核实以下历史摘要。必要时补跑：

```bash
PRODUCTFLOW_RUN_AGENT_EVALS_L2=1 just agent-evals-state
```

完成条件：上述规模的有效产物可定位、结果可核验、失败分类和 P2 判读齐全。缺配置或只跑了一部分不算完成。

## 证据

继承自 [原任务记录](archive/eval-live-layers.md)，本次拆分未重新验证产物：

- run_id=`20260904T185620Z-87f8a800`，commit=`a321efddab622af261e9aa4d47d109092c5890be`，18×k=3，28/54 pass，26 failed，墙钟 1441s。
- 旧记录指出 media-library、intake 及部分终态投影失败；state 断言未过，不能当 P2 出口。
- 待补：产物核验日期、完整 hash/模型字段、审核者、issue 结果与剩余缺口。
- 认领确认：2026-09-05 主代理在协调工作树登记并复核；仅核验已登记历史批次，无源码 diff，不申请 live 资源。图片组的浏览器/provider/storage 占用不变，考题校正仍未认领；本任务由主代理自审，不冒充独立考题审核。

### 2026-09-05 产物核验

- 核验基线 `077ade66`；读取 `storage-dev/agent-evals/20260904T185620Z-87f8a800/{run.json,trials.jsonl,transcripts/}`。原始产物未修改，未重新调用 provider。
- 18 题、54 条 trial、54 份 transcript，task/trial 无重复；逐条 run/task/trial/utterance/errors 身份一致。28 pass、26 failed 与旧摘要一致。该 Go runner 不生成 summary.json，缺此文件本身不证明中止。
- Skill 分布及通过数：graph-editing 4 题、7/12；media-library-organization 4 题、0/12；product-intake 3 题、4/9；run-diagnosis 4 题、9/12；workflow-run-request 3 题、8/9。数量满足规模要求，不能据此认定批次有效。
- run.json 记录 commit=`a321efddab622af261e9aa4d47d109092c5890be`、model=`gpt-5.6-luna`、task_set_hash=`250fce5dac7dd0fbd89b4e6a313a94f5680bf0dad1c9ccf584d6876efe9e96b4`。以记录中任务 ID 顺序加 NUL 复算即得到该 hash；未覆盖题目正文、expect 或 world。未记录 Skill hash、有效 provider/reasoning 或工作树身份；历史缺少 harness_hash 属于归因功能上线前的事实，不回填。当前 P2 实现仅补齐 harness，其他缺口仍在。
- 五条 trial 将非终态 `running` 记录为 terminal：场景组提案、注入标题改名、素材归档、注入名称素材改名、诊断后重试各一次。代码链为 `runL2Trial -> waitAgentTurnAnyTerminal(Node) -> evalSyncTurn(一次 Go GET)`；`GetTurn` 只读 PG，`writeJournalTerminal` 更新 Node store 后才发布 journal。存在可观察窗口；旧产物没有双侧状态与 journal 时序，不能断言五次均由同一竞态导致。
- 其他失败包括素材 pending draft 缺失 12 次、intake image type spec 缺失 3 次、graph 改名/工具缺失、run request 缺失，以及 requires_input 与预期不符。计数可重算，但工具契约误判、Agent 行为失败与未收敛投影需分别复核，暂不转成 Skill 优化输入。
- PG 判读入口 `gradeEvalState` 确实读取 graph projection、pending graph proposal、run request/source_run_id、library draft 与 intake；transcript 只保留错误和资源 ID，没有当时实际 state 快照。旧测试 DB 不是可依赖的历史快照，不能从当前 DB 追认旧值。
- 审核者：主代理-agent-0905-0458（自审）。结论：历史数量与文件完整性核验完成，但本 issue 尚未完成有效采证，P2 业务出口仍未通过。

## 阻塞与交接

- 原因：旧批次运行身份不足且含非终态样本，不能作为当前合同要求的有效全量 FAIL 签收；新 runner 已补齐观察与身份机制。考题合同已独立校正，仍待用新题集冻结完整输入并安排新批次。
- 解除条件：[PG 终态观察](archive/eval-l2-terminal-observation.md) 与 [L2 批次身份](archive/eval-l2-provenance.md) 已交付确定性回归；[考题合同校正](archive/eval-contract-alignment.md) 已冻结新题集。核验 inputs.json 与 l2-content-v1 身份，只有 run_status=complete 可作为有效采证候选；不得补写旧批次缺失身份或删除失败项。
- 跟进者：主代理-agent-0905-0458。
- 交接：无源码 diff、live 进程或冻结资源；协调记录随新任务发布移交，释放本 issue 占用。图片组资源保持不变。
