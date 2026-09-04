# 任务：L2 记录内容身份与实际运行配置

状态：开放
类型：实现
认领者：—
认领于：—
业务组：评测
父账本：agent-eval-system.md
完成后可拆：与 eval-l2-terminal-observation 均交付后恢复 eval-state-live 前置核验

任务合同以本文件为准；认领、审核和关闭遵循 [Issue 协议](README.md)。

## 问题来源

[L2 采证核验](eval-state-live.md) 确认 `writeEvalRunJSON` 的 task_set_hash 仅覆盖 ID 加 NUL，题目与 world 改动不改变身份；缺少实际加载 Skill 与有效 provider/reasoning 身份。P2 已记录真实 `/healthz` harness_hash，本任务保留该来源，不用磁盘推算代替已加载身份。

## 做成什么样

新 L2 run 能追溯本次实际选择的任务内容及所用 world、已加载 Skill、有效模型/provider/reasoning、代码与脏工作树身份。关键身份缺失或无效时在运行 trial 前失败，不能输出貌似可比较的完整批次。不得回填历史产物。哈希 canonical 化复用现有逻辑或提供与现有格式明确区分的版本，不新增宽泛兼容读取器。

## 前置与并行

- 前置：无；认领前核对 eval-l2-terminal-observation 占用，同一 Go runner 必须串行。
- 冻结输入：不更改 Skill、任务、world、grader；新增诊断身份不得改变 Pi 执行行为。真实采证另由 eval-state-live 执行。
- 运行资源：隔离 httptest/假 provider/临时产物；不改共享 DB/provider、图片组浏览器、dev 服务或真实旧 run。

## 只改这些文件

- `go/internal/agent/eval_state_gopg_test.go`、`harness_attribution_test.go` 与必要新增评测身份 `_test.go`。
- 若现有 health 未暴露实际加载配置，调查后由维护者确认最窄 Node health/loader 诊断身份修改范围，再补本合同；不得先实现生产侧改动。
- 本文件；维护者同步父账本、看板与归档。

## 不要碰

- 被测 Agent prompt、Skill、生产工具、执行权限和状态转换；任务与 grader；旧批次 metadata；L2 终态等待由独立 issue 负责。

## 现在代码在哪

`writeEvalRunJSON`、`LoadEvalTasks`、`LoadEvalWorlds` 与 `agent-service` health、Skill loader、L1 run metadata/集合内容哈希；取得认领后追踪实际配置来源，复用已有身份定义。

## 合同与验收

- 父章程证据格式与 L2-01/L2-06：同 ID 修改 utterance/expect/world 必须改变相关内容身份，任务选择变化须可辨识，稳定等价输入具有稳定身份。
- 覆盖有效已加载身份、缺失/畸形身份拒绝、不能将本地目录或 env 请求值冒充远端 effective 值、脏工作树记录与秘密不落日志。元数据写入错误必须向上报告。
- 运行 focused Go tests；如涉及 Node 则按其包规则运行相关测试与 build；`just docs-check`。真实模型 pass 与本实现任务分开验收。

## 证据

- 待认领后填写实际来源链、命令与结果。主代理自行执行须注明自审。
