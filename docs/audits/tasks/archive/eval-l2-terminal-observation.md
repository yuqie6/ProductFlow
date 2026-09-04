# 任务：L2 在 PostgreSQL 终态可见后评分

状态：完成
类型：实现
认领者：主代理-agent-0905-0458
认领于：2026-09-05T06:49:37+08:00
业务组：评测
父账本：agent-eval-system.md
完成后可拆：与 eval-l2-provenance 均交付后恢复 eval-state-live 前置核验

任务合同以本文件为准；认领、审核和关闭遵循 [Issue 协议](../README.md)。

## 问题来源

[L2 采证核验](../eval-state-live.md) 在历史 54 条 trial 中发现 5 条 terminal=running。修复前 `runL2Trial` 等到 Node/Pi 终态后调用 `evalSyncTurn`，后者只 GET 一次 Go 投影；`GetTurn` 不执行同步。Node store 终态写入早于 journal 发布，PG 可能尚未收敛。旧日志无法证明每条失败的具体时序，本任务通过确定性回归验证此边界。

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

- 2026-09-05 认领由主代理在协调工作树登记并复核，基线 `e7911bda`。图片组占用未改，未调用真实 provider，未重启共享 dev 服务。
- 先添加 HTTP 序列回归：连续两次 running，第三次 succeeded。旧 helper 稳定失败：`status=running reads=1; must wait for PG projection`。修复后的同一读取边界得到 succeeded、3 次读取及终态 tool steps。
- `runL2Trial` 在 Node 结束后调用 `waitEvalGoTurnTerminal`，使用已有 `agentServer.doContext` 保留认证与商品/全局路径，每 200ms 读取 PG 投影，总 deadline 30s，覆盖 HTTP 请求和响应体读取。合法终态沿用原 Node 等待列表，集中为 `evalTurnTerminal`；不要求匹配题目预期才退出。
- 超时、HTTP/网络/解码错误返回 observation error，不调用业务 grader。trial 的 status 为 observation_failed、terminal 为 null；trial details 与 transcript 记录 node_status/go_status。未知 PG 状态不伪造终态；非观察错误的历史分类未扩大修改。旧 `evalSyncTurn` 删除，无调用残留。
- 六个新增顶层测试覆盖投影延迟、全部六种终态、商品路径和 cookie、三种活动状态超时、HTTP/JSON/连接错误、deadline 中断响应体读取及完整 trial 落盘。完整 trial 使用隔离 PostgreSQL world 和假 Node 终态，注入 GET 503，断言仅有观察错误，故意设置的不可能工具/状态/节点条件未被评分。
- `go test -C go ./internal/agent -run '^TestEvalGoTerminal' -count=1` 通过（1.440s）。带开发环境的 focused 回归包含完整 trial、四类 world、既有 state grader，通过（3.654s），PG 用例未 skip。
- `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/agent -run "^(TestEvalGoTerminal|TestEvalL2TrialObservationFailureSkipsBusinessGrading|TestEvalWorldsSeedFourKinds|TestEvalStateGraderSeesRename|TestLoadEvalTasks)" -count=5 -race'` 通过（13.851s）；共 10 个顶层测试重复 5 次，无 race 报告。
- 审核者：主代理-agent-0905-0458（自审）。生产 runtime、journal、Skill、任务 JSON、world 种子、state grader 与批次身份未改；仅 runner、测试、任务证据与父章程更新。
- 归档后 `just docs-check` 与 `git diff --check` 通过；没有新增持久进程或需要释放的 live 资源。
- Issue 结果：完成；交付定位随本任务提交。业务门槛：L2 仍未通过，旧 5 条未重判；等待 eval-l2-provenance、合同核对和固定版本新采证。
