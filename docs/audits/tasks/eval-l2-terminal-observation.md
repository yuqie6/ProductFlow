# 任务：L2 在 PostgreSQL 终态可见后评分

状态：开放
类型：实现
认领者：—
认领于：—
业务组：评测
父账本：agent-eval-system.md
完成后可拆：与 eval-l2-provenance 均交付后恢复 eval-state-live 前置核验

任务合同以本文件为准；认领、审核和关闭遵循 [Issue 协议](README.md)。

## 问题来源

[L2 采证核验](eval-state-live.md) 在历史 54 条 trial 中发现 5 条 terminal=running。当前 `runL2Trial` 等到 Node/Pi 终态后调用 `evalSyncTurn`，后者只 GET 一次 Go 投影；`GetTurn` 不执行同步。Node store 终态写入早于 journal 发布，PG 可能尚未收敛。旧日志无法证明每条失败的具体时序，本任务通过确定性回归验证此边界。

## 做成什么样

等待真实 Go/PG 投影达到终态后读取工具步骤并评分；非终态超时明确记为观察失败，保留 Node 与 Go 最后状态，不当作业务评分失败。等待有界，不要求终态必须等于题目预期才能返回。真实失败、取消、unknown、requires_input、awaiting_confirmation 都应保留，禁止无限等待或补写投影。

## 前置与并行

- 前置：无；从现有采证发现的 runner 缺口，不改考题或被测 Agent 行为。
- 冻结输入：Skill、任务、world、grader 与生产执行语义不变。与 eval-l2-provenance 同文件串行；eval-state-live 补跑在本任务后。
- 运行资源：确定性测试使用 httptest、已有隔离测试 DB/假 provider；不占共享真实 provider、图片组浏览器或 dev 服务。

## 只改这些文件

- `go/internal/agent/eval_state_gopg_test.go` 与对应新增 `_test.go`；必要时复用 `evalworld_test.go` 的只读 GET helper，不修改 state grader。
- 本文件；维护者同步父账本、看板与归档。

## 不要碰

- 生产 Go/Node runtime、journal、Skill、任务 JSON、world 种子、评分预期、共享服务配置。
- L2 批次身份由 eval-l2-provenance 负责；不得顺手实现。

## 现在代码在哪

`runL2Trial`、`waitAgentTurnAnyTerminal`、`evalSyncTurn`；生产 `turns.go:GetTurn`；`question_resume_gopg_test.go:waitGoTurnStatus` 已有轮询参考；`agent-service/src/turn-runtime.ts:writeJournalTerminal` 是可见性边界。先核实当前调用链与测试，复用已有终态定义。

## 合同与验收

- 父章程 L2-04/L2-05/L2-06：以真实业务投影与 tool steps 判读，不以 Node 文件快照代替 PG，不更改生产同步语义。
- 确定性回归覆盖 Node 已结束但 Go 连续 running 后终态、错误终态及时返回、超时保留双侧状态且不执行业务评分、读取失败；覆盖终态响应中的完整 tool steps。
- 运行 focused Go tests、既有 L2 fixture/grader 回归及 `just docs-check`。本 issue 不要求真实付费 live，也不宣告旧 5 条已修复为 pass；新全量批次由 eval-state-live 负责。

## 证据

- 待认领后填写实际原因、修改点、命令与结果。主代理自行执行须注明自审。
