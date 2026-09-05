# 任务：同一 Agent Turn 的后续问题答案不与前一问题冲突

状态：完成
类型：实现
认领者：主代理-agent-0905-0458
认领于：2026-09-05T07:49:52+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：维护者核对 L3 复验前置，不自动发布全量采分

任务合同以本文件为准；遵循 [Issue 协议](../README.md)。取得确认认领后才调查和实现。

## 问题来源

[eval-user-sim](eval-user-sim.md) 的固定 live `20260904T234034Z-93b42b6d`，`product-intake-sim-two-round-clarify` 使用生产 PiRuntimeManager：同一 turn `3973de99-5191-4a9b-83dc-8c248cc1424b` 的第一个 question `question_988110b0-c867-4c97-99bf-843c83b32e70` 回答后恢复执行；第二个 question `question_60f32f27-1615-42d9-bad8-af8701d05bc9` 提交不同答案，返回 `the question already has a different answer`。转录在该 run 的 `transcripts/product-intake-sim-two-round-clarify-1.json`，包含两次独立用户回答和问题 ID。

现场已确认 `answerQuestion -> resume` 的生产调用顺序。根因需认领后沿答案读写与恢复链定位，不能假定只需删掉一个冲突判断。按平台的执行恢复/幂等职责交付，评测消费结果。

## 做成什么样

商家在同一 turn 连续回答不同问题时，后一个问题能接收自己的答案并恢复同一 Pi session。相同问题相同答案仍幂等，不重复 tool result；相同问题不同答案仍冲突；过期问题不能覆盖当前问题。重启后的答案读取同样必须按正确问题归属。

## 前置与并行

- 前置：无；模拟器接线和真实失败证据随 eval-user-sim 交付。
- 冻结输入：任务 JSON、world、Skill、harness/policy、grader 不变。不得与其他生产 runtime 写者或共享输入的 live 批次冲突。
- 运行资源：确定性 fake provider 优先。Go 回归使用隔离测试 DB；真实复验使用固定 checkout、独立 run 目录，不修改共享 dev provider、暂停服务或重建业务 DB。

## 只改这些文件

- 有界调查 `agent-service/src/turn-runtime.ts` 的 `answerQuestion`、`storedQuestionAnswer`、`resumeQuestion`，以及 manager、恢复工具结果和 Go 同问题答案投影的读写调用链。
- 2026-09-05 维护者确认当前 owner：`agent-service/src/question-resume.ts` 与 `turn-runtime.ts`；测试为 `question-resume.test.ts`、`pi-runtime.test.ts`、`pi-runtime.e2e.test.ts`。Go 的 `execution.go` 新问题投影未清旧答案，`turns.go` 答案写入未拒绝已存不同答案，纳入本切片；贴近边界的回归放现有 `journal_regression_test.go` / `http_test.go`，必要的两问 Go+PG+Pi 集成放 `question_resume_gopg_test.go`。不改 schema、wire 或题库。
- 本文件；父账本、必要活文档和索引由维护者整合。

## 不要碰

- 不改 L3 任务、模拟用户、grader 或通过阈值来避开 409，不改 Skill/policy。
- 不复活旧 runtime，不加旧形状兼容或数据回填，不重构整个 journal。

## 合同与验收

- 追踪 `question/requested -> question/answered -> queued/resume -> Pi tool result`，确认 answer 身份同时包含 turn 和 question，旧问题不能匹配新问题。
- 确定性回归必须先复现当前错误，再覆盖同 turn 两个不同问题、同问题重复相同/不同答案、晚到旧问题答案及重启恢复；验证不注入两份结果，不丢 journal 事件，保留 fencing/ACK 合同。
- `just agent-service-test`、`pnpm --dir agent-service run build`、`just docs-check`。如改 Go，按 live 调用边界补 Go focused/race 回归。
- 固定候选运行 `PRODUCTFLOW_RUN_AGENT_EVALS=1 PRODUCTFLOW_AGENT_EVAL_FILTER=product-intake-sim-two-round-clarify just agent-evals-sim`，登记 run_id 和完整转录。随机模型若未触发两问，不得冒充第二问已通过；确定性生产 manager 回归必须覆盖两问。该 live 的其他业务/题目失败独立报告，不要求修改 grader 获得 pass。

## 实现与证据

- 认领与审核者：主代理-agent-0905-0458，自审。认领已在协调工作树登记并复读确认；执行未委派。本任务随交付提交，`git log --follow -- docs/audits/tasks/archive/agent-question-answer-identity.md` 定位。
- 根因：Node 的 `storedAnswerFromEvents` 按整个 Turn 最后一条 `question/answered` 取值，把第一问答案用于第二问冲突检查、timeout 和重启恢复；Go `foldJournalProjection` 的新问题不清 `question_answer_json`，`persistQuestionAnswer` 也未落实已有不同答案不可覆盖的注释合同。
- 修复：返回绑定 `{questionID, answer}` 的记录，默认只读最近请求的问题，指定 question ID 用于重复提交检查；恢复注入沿用同一身份，不重新无条件取最后答案。新问题 PG 投影清旧答案，exact journal replay 不重复 fold；答案更新通过 PG 原子谓词核对等待状态、当前 ID 与已有答案，避免并发读后覆盖。不新增 schema、wire、兼容分支或数据回填。
- 新增确定性并发用例发现同问题同时提交两份相同答案会写出重复 journal。单个 TurnRuntime 的回答/超时写入共用串行临界区，重复提交返回现有结果。没有全局锁，不改变 ACK、lease 或 fencing。
- RED 证据：Node 既有 dead-waiter 测试扩成两问后复现 `the question already has a different answer`；Go 新问题回归复现 `second question inherited previous answer`，不同答案 HTTP 回归得到 503 而非预期 409；Pi E2E 并发相同答案复现 3 条回答事件而非 2 条。修复后对应回归通过。
- Node focused：`pnpm --dir agent-service exec vitest run src/question-resume.test.ts src/pi-runtime.test.ts src/pi-runtime.e2e.test.ts`，3 files、49 passed，5.21s。覆盖两问、同答案并发/顺序重试、不同答案冲突、旧答案迟到不清新问题、未回答第二问从磁盘重启后保持等待、第二问超时不被旧答案抑制。Pi 最终实际请求中仅两份 tool result，答案分别为 first/second，journal seq 连续。
- Go focused：`bash scripts/with_dev_env.sh go test -C go ./internal/agent -run 'Test(DurableAnswerCreatesNewAttempt|NewQuestionJournal|AnswerQuestionUnavailableDoesNotOverwrite|ConcurrentDifferentQuestion)' -count=1 -race -timeout 4m`，15.846s 通过。三个持久化/并发/journal 回归 `-count=10 -race`，7.464s 通过。
- Go + PG + Pi：`TestDurableAnswerCreatesNewAttemptAndInjectsPiToolResult` 保留单问并增加两问变体。真实 Go HTTP 回答第一问，PG 出现不同第二问且旧答案为 NULL；SIGKILL Node，第二问答案在 gateway 不可用时仍落 PG，重启相同 session 文件并接管新 attempt，同一 Turn 成功。最终真实 provider 请求核对 `call-question-1/2` 及各自答案，回答事件数量与连续 seq 正确。普通完成结果含既有 schema/data envelope，恢复注入仍是既有 bare answer payload；本任务没有统一这两种既有格式。
- 全量：`just agent-service-test`，35 files、269 passed、2 skipped，18.01s；`pnpm --dir agent-service run build` 通过；`bash scripts/with_dev_env.sh go test -C go ./internal/agent -count=1 -race -timeout 10m`，89.361s 通过。Go 使用隔离测试 DB 与 fake provider，不改共享 dev provider。

## 固定 Live 复验

- 候选为 `7a7df1f305535b529399e48127f99c04bc5b1ae8` 加本任务 10 份代码/测试文件补丁，固定 checkout `/tmp/productflow-question-identity-ArvccX/checkout`；不是已提交的 clean 候选。依赖从通过全量测试的安装复制，外层临时 package.json 固定 pnpm 10.32.1。没有复制密钥或修改共享 `.env.dev`、DB provider、dev 服务。
- 命令：`bash scripts/with_dev_env.sh env STORAGE_ROOT=/home/cot/ProductFlow/storage-dev PRODUCTFLOW_RUN_AGENT_EVALS=1 PRODUCTFLOW_AGENT_EVAL_FILTER=product-intake-sim-two-round-clarify just --justfile /tmp/productflow-question-identity-ArvccX/checkout/justfile --working-directory /tmp/productflow-question-identity-ArvccX/checkout agent-evals-sim`。
- run_id=`20260905T001434Z-90c24d87`，n=1、k=1、0/1 pass，20,506ms，退出 1。Agent 与独立用户均为 openai/gpt-5.6-luna，Agent reasoning 未设置；用户调用 1 次，4,804 totalTokens，被测 Agent tokens 仍 unavailable。
- 完整转录 `STORAGE_ROOT/agent-evals/20260905T001434Z-90c24d87/transcripts/product-intake-sim-two-round-clarify-1.json`。同一 Turn `7d3cd158-3bd5-4e33-b994-126661aaa2d0` 只请求问题 `question_d20916fc-3d8c-4d90-9cb7-1df8bc30ddeb`，用户回答后恢复并终态 succeeded；未触发第二问。grader 报 `required tool was not called: get_product_workflow_context_v1`，转录也确实没有该调用。不改题目、Skill、policy 或 grader 来获得 pass；本次不能作为 live 连续两问通过证据。
- `git ls-files -z agent-service go` 的 627 份跟踪输入，按每个 path + NUL + bytes + NUL 计算 SHA256，运行前后均为 `bddd397bafffd084ef7feb569324e43c0402c64281fdcfac4c401e0a5d4c2b47`；补丁 SHA256=`9466d662b68c483025f0a93be7fe2dd93bd5783233af74d22af8be0dab0a8430`。同 run 保留 `candidate.patch` 与 `frozen-inputs.json`；旧 Node worktree_hash 只描述状态列表，不能当内容身份。
- task_set_hash=`c49599b4604cc87525e93b73e30cbaaf1c5f170d3d810eb81cc2e0bc3f45e332`，Skill hash=`f2b4292cc0716ddc68d3515e1de2bcd205854c7e51ed1bb08dc99b5f8fff65ef`，harness hash=`13e8e19ae0ba1cadc732f5f4ba6d0ffba0da08d5a859671ccb67d72bf52dae21`。原始失败 run 不覆写。

## 验收结论

- 自审完成：核对全部 10 份代码/测试 diff、所有答案读写调用点、固定候选内容和完整 trial。生产两问身份修复及确定性恢复合同验收完成；L3 五流程、真实模型连续两问和行为评分缺口仍未通过，交既有能力/评测任务按前置处理，不开重叠修复单。
- 所有本任务测试和 live 进程已退出；临时固定 checkout 与原始产物保留用于追溯，无共享 runtime 资源占用。归档和活文档随代码提交，提交前运行 `just docs-check` 与 `git diff --check`；其他会话图池改动不纳入。
