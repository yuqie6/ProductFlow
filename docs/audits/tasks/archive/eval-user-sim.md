# 任务：L3 模拟用户改走独立模型

状态：完成
类型：实现
认领者：主代理-agent-0905-0458
认领于：2026-09-05T07:19:13+08:00
业务组：评测
父账本：agent-eval-system.md
完成后可拆：维护者已发布 agent-question-answer-identity；sim-live 等生产问题与确认/评分合同缺口解除后核对

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](../README.md)。执行前读取适用仓库规则、当前实现、调用链、测试和 diff。

## 前置与并行

- 前置：任务 JSON 已有 `user_sim` 合同；如现场不符则阻塞并交维护者调整范围。
- 冻结输入：live 时固定 `agent-service/`、`go/prompts/agent/` 与两侧模型配置；与共享输入的 Skill、harness 改动串行。
- 运行资源：使用独立 run 目录，固定 checkout 或预约冻结窗口；单元测试不需要独占生产服务。

## 做成什么样

`just agent-evals-sim` 里扮演用户的那一侧每次用独立 `complete()` 按任务里的隐藏目标、事实表和应答策略说话。被测 Agent 仍走生产 `PiRuntimeManager`。

现在 `evals/user-sim.ts` 只回放 `user_sim.scripted_answers`。保留 scripted 作为无模型单测夹具；live 路径必须另调模型。

## 只改这些文件

- `agent-service/evals/user-sim.ts`
- `agent-service/evals/user-sim.test.ts`
- 本文件

## 不要碰

- `evals/tasks/*.json`（L3 任务的 `user_sim` 字段已经在；缺字段就停，不要自己改任务 JSON）
- `evals/graders/`、`live-runner.ts`、Skill 目录
- Go 代码、Web、其它账本

## 合同

- 被测 Agent 仍是生产 manager；不要给用户模拟另起一套工具世界。
- `requires_input` 继续走 `manager.answerQuestion`。
- graph proposal / global draft / run request 的 confirm/discard 仍用现有脚本策略，不要在本任务里接 Go HTTP。
- 未确认前不得 `finalize_product_intake_v1` / `apply_graph_change_set_v1`。现有 `unconfirmedWriteCount` 保留。
- 转录只落 `STORAGE_ROOT/agent-evals/`。

环境：用户模拟模型可用 `AGENT_EVAL_USER_SIM_MODEL`，缺省与被测模型相同但必须是另一次调用。

## 怎么验收

```bash
pnpm --dir agent-service exec vitest run evals/user-sim.test.ts
just agent-service-test
PRODUCTFLOW_RUN_AGENT_EVALS=1 just agent-evals-sim
```

单测要证明 live 路径调用了用户模型，而不是只读 `scripted_answers`。历史 run `20260904T154151Z-788cb11e` 是 5/5 fail，不能当作本任务通过。把新的 `run_id` 写在下面。

## 证据

- 实现：live 回答与后续话语使用 Pi SDK `complete()`，读取隐藏目标、事实、策略和已执行的脚本决定；不读取脚本答案文本，不向用户模型开放工具。模型失败、空回答、过长回答不回退脚本。用户模型默认与 Agent 同名，另调请求；`AGENT_EVAL_USER_SIM_MODEL` 可独立覆盖。
- 当前调用链确认：`manager.answerQuestion` 保存答案并置 `queued`；`manager.resume` 才唤醒 waiter。模拟器已补齐此顺序，重复追问留在同一 turn。每个观察到的 turn 的最后状态被保留，跨 turn 本地工具与桩调用合并评分，不改 grader。
- 用户请求上限 90 秒，模拟器问题等待 120 秒；每次问答计入 `max_turns`。转录保存用户侧请求次数、回答、用量、端点摘要与各 turn 状态；不保存 key 或原始 provider URL。
- 确定性验证：`pnpm --dir agent-service exec vitest run evals/user-sim.test.ts` 15 passed；`just agent-service-test` 35 files、266 passed、2 skipped；`pnpm --dir agent-service run build` 通过。测试包含 live 入口调用独立模型、answer/resume 顺序、重复问题不另开 turn、首 turn Skill 不漏记、脚本决定上下文、失败不回退与交互次数上限。
- live 使用两个独立固定 checkout，基线均为 `320aea4a74282b2edf4b05517049122fa040ba27` 加本任务补丁；没有以共享工作树的 claim 状态冒充 clean commit。两个副本只修改本任务两份 TS 文件，依赖复制自通过全量测试的安装目录；外层临时 `package.json` 固定 pnpm 10.32.1。模型配置在各进程启动时从现有 dev 环境加载，不改 `.env.dev`、DB provider 或运行中的共享服务。
- 第一批 `20260904T233219Z-2eb8eda1`：n=5、k=1、2/5 pass。缺少 `resume` 导致两条问题流程超时，跨 turn 观察遗漏首轮本地 Skill；丢弃后没有把已决定状态传给用户模型。此批用于诊断，不作最终验收。219 份 `agent-service/` 与 `go/prompts/agent/` 跟踪输入按 `path + NUL + bytes + NUL` 哈希，运行前后均为 `c79bd003e25ab776edcc14d726476a91e31702d8106d8bb7f8c1830e389fc979`。原始结果与候选补丁保留于 `STORAGE_ROOT/agent-evals/20260904T233219Z-2eb8eda1/`。
- 修正后的固定批次 `20260904T234034Z-93b42b6d`：n=5、k=1、2/5 pass、pass^1=0.4；全部五条 trial 和 summary 已落盘，命令退出 1 表示业务判定未全通过。两侧模型均为 openai/gpt-5.6-luna，Agent reasoning 未设置；用户侧独立 `complete()` 共 4 次，全部完成，用户侧 totalTokens 合计 19,227，五条 duration 合计 111,311ms。用户 tokens 不冒充被测 Agent token_count，后者仍 unavailable。
- 此批 219 份输入运行前后 hash 均为 `bb46cecbdba13fe449caecf950a4432c15a6677558173b4189e4ecdd6c2fb492`；补丁 hash=`38def2ce625c7186ae02f8f917b28a5b0ac90383f1a2737dd9f6768b7ec1ff3e`。固定副本 `/tmp/productflow-user-sim-YBqs1N/checkout-v2`，补丁及前后哈希保存在同 run 目录 `candidate.patch` / `frozen-inputs.json`。Node 旧 `worktree_hash` 只反映状态列表，两批值相同，不能用它区分候选；本任务以额外内容哈希和补丁区分，不把基线 commit 写成候选已提交身份。
- 逐项判读：素材整理与运行请求均停 `awaiting_confirmation`，脚本确认，pass；提案拒绝改口停 `succeeded`，原 expect 要求 `awaiting_confirmation`，fail；缺信息重命名已在同一 turn 回答并实际调用 rename，但既有全局 `userAgreed` 计数仍判 `unconfirmed writes: 1`，未放宽 grader；intake 的不同第二个 question ID 回答返回 `the question already has a different answer`，交 [agent-question-answer-identity](agent-question-answer-identity.md) 修复生产边界。
- task_hash=`5eac3a5ffb216f6fe44c13c9f843aacfb59494b7caa8c21f3799a490f1f62c19`，Skill hash=`f2b4292cc0716ddc68d3515e1de2bcd205854c7e51ed1bb08dc99b5f8fff65ef`，harness hash=`13e8e19ae0ba1cadc732f5f4ba6d0ffba0da08d5a859671ccb67d72bf52dae21`；任务、grader、Skill 和生产运行时代码未改。仅本地脚本确认，未验证 Go HTTP confirm/discard；本次不得宣称 L3 五流程通过或 Agent 能力涨分。
- 审核者：主代理-agent-0905-0458，自审。核对两份实现/测试完整 diff、固定副本内容、全部 trial 和用户回答。实现与有效采证完成；组内 L3/P3 仍部分完成。所有本任务模型与测试进程已退出，无共享服务变更；临时固定副本保留用于追溯。
- 交付定位：随本任务提交，`git log --follow -- docs/audits/tasks/archive/eval-user-sim.md` 查询。归档前执行 `just docs-check` 与 `git diff --check`。
