# 任务：同一 Agent Turn 的后续问题答案不与前一问题冲突

状态：开放
类型：实现
认领者：—
认领于：—
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：维护者核对 L3 复验前置，不自动发布全量采分

任务合同以本文件为准；遵循 [Issue 协议](README.md)。取得确认认领后才调查和实现。

## 问题来源

[eval-user-sim](archive/eval-user-sim.md) 的固定 live `20260904T234034Z-93b42b6d`，`product-intake-sim-two-round-clarify` 使用生产 PiRuntimeManager：同一 turn `3973de99-5191-4a9b-83dc-8c248cc1424b` 的第一个 question `question_988110b0-c867-4c97-99bf-843c83b32e70` 回答后恢复执行；第二个 question `question_60f32f27-1615-42d9-bad8-af8701d05bc9` 提交不同答案，返回 `the question already has a different answer`。转录在该 run 的 `transcripts/product-intake-sim-two-round-clarify-1.json`，包含两次独立用户回答和问题 ID。

现场已确认 `answerQuestion -> resume` 的生产调用顺序。根因需认领后沿答案读写与恢复链定位，不能假定只需删掉一个冲突判断。按平台的执行恢复/幂等职责交付，评测消费结果。

## 做成什么样

商家在同一 turn 连续回答不同问题时，后一个问题能接收自己的答案并恢复同一 Pi session。相同问题相同答案仍幂等，不重复 tool result；相同问题不同答案仍冲突；过期问题不能覆盖当前问题。重启后的答案读取同样必须按正确问题归属。

## 前置与并行

- 前置：无；模拟器接线和真实失败证据随 eval-user-sim 交付。
- 冻结输入：任务 JSON、world、Skill、harness/policy、grader 不变。不得与其他生产 runtime 写者或共享输入的 live 批次冲突。
- 运行资源：确定性 fake provider 优先。Go 回归使用隔离测试 DB；真实复验使用固定 checkout、独立 run 目录，不修改共享 dev provider、暂停服务或重建业务 DB。

## 只改这些文件

- 有界调查 `agent-service/src/turn-runtime.ts` 的 `answerQuestion`、`storedQuestionAnswer`、`resumeQuestion`，以及 manager、恢复工具结果和 Go 同问题答案投影的读写调用链。
- 修改前由维护者确认根因及实际文件范围。预计生产 Node 问题答案 owner 与贴近其边界的已有测试；若 Go 持久化或恢复也受影响，收证后纳入同一因果切片。
- 本文件；父账本、必要活文档和索引由维护者整合。

## 不要碰

- 不改 L3 任务、模拟用户、grader 或通过阈值来避开 409，不改 Skill/policy。
- 不复活旧 runtime，不加旧形状兼容或数据回填，不重构整个 journal。

## 合同与验收

- 追踪 `question/requested -> question/answered -> queued/resume -> Pi tool result`，确认 answer 身份同时包含 turn 和 question，旧问题不能匹配新问题。
- 确定性回归必须先复现当前错误，再覆盖同 turn 两个不同问题、同问题重复相同/不同答案、晚到旧问题答案及重启恢复；验证不注入两份结果，不丢 journal 事件，保留 fencing/ACK 合同。
- `just agent-service-test`、`pnpm --dir agent-service run build`、`just docs-check`。如改 Go，按 live 调用边界补 Go focused/race 回归。
- 固定候选运行 `PRODUCTFLOW_RUN_AGENT_EVALS=1 PRODUCTFLOW_AGENT_EVAL_FILTER=product-intake-sim-two-round-clarify just agent-evals-sim`，登记 run_id 和完整转录。随机模型若未触发两问，不得冒充第二问已通过；确定性生产 manager 回归必须覆盖两问。该 live 的其他业务/题目失败独立报告，不要求修改 grader 获得 pass。

## 交接

- 发布者：主代理-agent-0905-0458。无实现 diff、无执行进程或新资源占用；待确认认领。
- 证据基线：`320aea4a74282b2edf4b05517049122fa040ba27`，生产代码未随 eval-user-sim 修改；该任务修复归档不得覆写上述历史 run。
