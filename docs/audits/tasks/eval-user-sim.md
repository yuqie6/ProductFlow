# 任务：L3 模拟用户改走独立模型

状态：开放
类型：实现
认领者：—
认领于：—
业务组：评测
父账本：agent-eval-system.md
完成后可拆：eval-sim-live.md（L3 全量 live，只登记 run_id，不改代码）

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](README.md)。执行前读取适用仓库规则、当前实现、调用链、测试和 diff。

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

```text
YYYY-MM-DD | commit=<sha> | run_id=<id> | command=just agent-evals-sim | n=5 | k= | pass^1= | artifact=
```
