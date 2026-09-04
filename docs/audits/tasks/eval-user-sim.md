# 任务：L3 模拟用户改走独立模型

状态：未开始

读完本文件就可以改代码。不要去读其它验收账本开工。

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
