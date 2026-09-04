# 任务：Skill 与既有评测题硬化

状态：开放
认领者：—
认领于：—
业务组：评测
父账本：agent-eval-system.md
完成后可拆：无（regression 仍未过门时由评测组章程再发下一轮 Skill 刀，本组员不私自发单）

读完本文件就可以改代码。认领前不要改「只改这些文件」。认领步骤见 [README.md](README.md)。不要去读其它验收账本开工。

## 做成什么样

既有 L1 任务在真模型下能复现地提高 pass，并且四条 Skill 变异至少能杀死一部分。这是产品 Skill / 任务 `expect` 修补，提交说明写「产品合同 / Skill 缺陷」。这不是 Self-Harness 进化，不要写 overlay lineage。

当前可复算基线（同 task_hash `71d48f47…`，模型 `gpt-5.6-luna`）：

- `20260904T174405Z-fa2667fa`：pass^1=0.6178，pass^3=0.4533，regression 0.6897/0.5517，门槛未过
- mutate `mutate-20260904T181102Z-6e344c6a`：kill_rate=0（3 survived / 1 unscorable）

门槛（本任务结束时登记，过不了就如实写，不要改 grader 凑数）：

- regression：pass^1 >= 0.95 且 pass^3 >= 0.90
- 同 commit、同 task_hash、同模型连续两次 k=3，`abs(delta pass^1) <= 0.05`
- mutate：scorable 变异的 kill_rate > 0
- L0：`just agent-service-test` 仍过；工具与 12 个 Graph op 覆盖仍 100%

## 只改这些文件

- `agent-service/.pi/skills/`
- 已有的 `agent-service/evals/tasks/*.json`
- 已有的 `agent-service/evals/worlds/*.json`（仅当 world 与任务 id 对不上时）
- 本文件（状态与证据）

## 不要碰

- `agent-service/evals/graders/`、`schema.ts`、`loader.ts`、`live-runner.ts`、`user-sim.ts`、`injections.ts`
- `agent-service/evals/labels/`
- `agent-service/src/`、`agent-service/harness/`
- `go/prompts/agent/runtime-policy.md`
- 其它 `docs/audits/` 文件

## 现在怎么失败

抽读过的系统性错（trial 级，不是噪声）：

- 该 propose 的多步改动却 `apply` 或直接 `succeeded`
- 图种 key 写成 `cover`/`main` 而不是任务期望的 `hero`
- 负例或诊断题不调 `load_productflow_skill` / 该读的 context
- `scope=node` 却打错 `node_id`
- 变异 `swap-apply-propose-guidance` 基线任务本身失败，突变体反而过

先读失败任务的 `expect` 和对应 `SKILL.md`，改指导或改过时的 expect。不要为了过题放宽 grader，也不要只加会过的新题。

## 合同

- 被测对象仍是生产 `PiRuntimeManager`。不要另起 harness。
- live pass 只由任务 `expect` 判定，不看 `reference.scripted_calls`。
- 转录只落 `STORAGE_ROOT/agent-evals/`，不进 git。
- 每个任务至少 3 条释义、每技能至少 10 正 + 5 负；改完跑 L0。
- 默认 k=3。

## 怎么验收

```bash
just agent-service-test
just agent-evals-coverage
just agent-evals-live
just agent-evals-diff <第一次run_id> <第二次run_id>
just agent-evals-mutate
```

两次 `agent-evals-live` 必须同一 git commit、同一 task_hash、工作树在启动时干净。本任务的 diff 提交之后再跑第二次。

抽读：每技能按 `task_id` 字母序各 1 条 pass^3=1 与 1 条 pass^3=0，trial=1。结论写在下面。

## 证据

把 run 记在这里，不要改总账本。

```text
YYYY-MM-DD | commit=<sha> | run_id=<id> | command=just agent-evals-live | n= | k=3 | pass^1= | pass^3= | task_hash= | skill_hash= | model= | artifact=
```
