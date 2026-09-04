# 任务：修复 Skill 系统性失败并用冻结评测复验

状态：阻塞
类型：实现
认领者：—
认领于：—
业务组：Agent 能力
父账本：agent-self-harness.md
完成后可拆：无（维护者根据评测结论在 Agent 能力组决定下一轮修复，执行者不私自发单）

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](README.md)。执行前读取适用仓库规则、当前实现、调用链、测试和 diff。

## 前置与并行

- 前置：无；本任务以当前产品合同和失败转录为修补依据。
- 冻结输入：两次 live 和 mutate 期间固定 `agent-service/`、`go/prompts/agent/`、任务集与模型配置；不得与共享这些输入的 L2/L5 采证、user-sim、harness 修改同时执行。
- 运行资源：固定干净 checkout 或由维护者预约完整工作区冻结窗口；run 目录独立，成对复跑结束前不写文档、不改变 HEAD。
- 后续候选 live 使用独立固定 checkout 和独立 run 目录，不修改共享 provider 设置、不暂停 worker、不重建共享数据库。2026-09-05 本轮已释放 Skill 文件与运行资源占用，详见阻塞与交接。

## 做成什么样

既有 L1 任务在真模型下能复现地提高 pass，并且四条 Skill 变异至少能杀死一部分。提交说明写「产品合同 / Skill 缺陷」；Self-Harness 进化与 overlay lineage 不在本任务范围。2026-09-05 归属转入 Agent 能力，ID 保留；评测组负责题库与结果复核，不在本任务中同时修改被测行为和考题。

当前可复算基线（同 task_hash `71d48f47…`，模型 `gpt-5.6-luna`）：

- `20260904T174405Z-fa2667fa`：pass^1=0.6178，pass^3=0.4533，regression 0.6897/0.5517，门槛未过
- mutate `mutate-20260904T181102Z-6e344c6a`：kill_rate=0（3 survived / 1 unscorable）

业务门槛（完整登记，未达标不得宣称评测阶段通过，不要改 grader 凑数）：

- regression：pass^1 >= 0.95 且 pass^3 >= 0.90
- 同 commit、同 task_hash、同模型连续两次 k=3，`abs(delta pass^1) <= 0.05`
- mutate：scorable 变异的 kill_rate > 0
- L0：`just agent-service-test` 仍过；工具与 12 个 Graph op 覆盖仍 100%

本 issue 的完成条件：修补有明确产品合同依据；L0 与覆盖通过；固定候选提交连续两次有效 k=3 复跑满足上述稳定性要求；至少一个原有系统性失败得到复验改善，且 scorable 变异 kill_rate > 0。regression 绝对门槛单独登记，未达到时由维护者保留父章程未通过并决定是否发布下一轮。缺少这些完成证据时保持认领或阻塞。

## 只改这些文件

- `agent-service/.pi/skills/`
- 本文件（状态与证据）

## 不要碰

- `agent-service/evals/tasks/`、`worlds/`；发现过时 expect 或 world 错配时提交合同依据给维护者，由评测组独立修订并冻结后再复验
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

读取失败任务的 `expect`、对应 `SKILL.md` 与产品合同，修有依据的指导缺陷。不要为了过题改 expect、放宽 grader 或只加会过的新题。题目失真影响验收时暂停本次比较并记录阻塞；评测修订题目后须用新 task_hash 重跑未修补基线与候选，历史分数不作跨题集改善证据。

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

候选修补通过 L0、自审和维护者审核后提交，issue 仍是认领。两次 `agent-evals-live` 都在该提交之后运行，必须同一 git commit、同一 task_hash，整个复跑窗口工作树干净。两次跑完再登记证据；不得第一次跑后提交代码再拿第二次比较。

抽读：每技能按 `task_id` 字母序各 1 条 pass^3=1 与 1 条 pass^3=0，trial=1。结论写在下面。

## 证据

2026-09-05T05:00+08:00，主代理-agent-0905-0458 认领后复核冻结题集与原始转录，尚未修改任何 Skill、runtime 或评测源码，未启动 live。旧 run `20260904T174405Z-fa2667fa` 的 trial=1 有以下可复核冲突：

- `graph-editing-negative-unknown-node`：读 context 后调用 `ask_user`，终态 `requires_input`，只因 expect 要求 `succeeded` 失败；生产 `runtime-policy.md` 规定缺信息用 `ask_user`，graph-editing 规定零匹配提问。
- `workflow-run-request-negative-off-topic-delete-graph`：用户要求清空画布，加载图编辑 Skill 并提交批量删除提案，终态 `awaiting_confirmation`，只因 expect 要求 `succeeded` 失败。生产运行时加载当前 scope 的完整 Skill catalog，不以题目的 `skill` 字段限制路由；批量删除的确认合同见 graph-editing。
- `graph-editing-negative-off-topic-weather`：未写入业务数据，明确说明无法访问实时天气；只因要求加载无关 Skill 失败。`src/skills.ts` 的目录指令只要求匹配 description/triggers 时加载。
- `run-diagnosis-inspect-failed-node`：已读 run detail 并报告返回的节点/错误；仍强制要求 `get_node_detail_v1`。现有 Skill 允许详情已充分时不重复读，是否需要节点配置取决于建议内容，应由评测组按产品合同裁定，不能为过题强加无条件读取。

证据目录：`storage-dev/agent-evals/20260904T174405Z-fa2667fa/`，对应 `trials.jsonl` 与 `transcripts/<task_id>-1.json`。这是历史运行的当前文件复核，不冒充本轮 live。审核：主代理自审，判定题目失真影响验收，暂停比较，不采信旧分数为能力结论。

## 阻塞与交接

- 原因：现有冻结 expect 与生产按意图路由、结构化提问、按需加载 Skill 的合同冲突。
- 解除条件：[eval-contract-alignment.md](eval-contract-alignment.md) 独立审核并冻结修订题集；以新 task_hash 重跑未修补基线和候选，不能跨题集比较历史分数。P1 若已交付，基线与候选须使用同一 P1 runtime/policy。
- 跟进者：Agent 能力组主代理协调评测组；题目/评分归评测组，不在本任务修改。
- 交接：主代理-agent-0905-0458 于 2026-09-05T04:57:57+08:00 认领，05:00 完成有界复核后确认无源码 diff、无运行进程、无冻结资源，清空认领者并释放占用。当前只有本任务协调记录，未形成 Skill 候选提交。

把 run 记在这里，不要改总账本。维护者验收时同步 Agent 能力章程的修复结论与评测章程的分数/门槛；本任务完成不改变 Self-Harness 阶段状态。

```text
YYYY-MM-DD | commit=<sha> | run_id=<id> | command=just agent-evals-live | n= | k=3 | pass^1= | pass^3= | task_hash= | skill_hash= | model= | artifact=
```
