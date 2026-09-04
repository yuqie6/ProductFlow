# 任务：L2 PostgreSQL 终态采证

状态：开放
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
