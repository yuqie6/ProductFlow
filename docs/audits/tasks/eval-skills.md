# 任务：修复 Skill 系统性失败并用冻结评测复验

状态：阻塞
类型：实现
认领者：主代理-agent-0905-0458
认领于：2026-09-05T14:29:45+08:00
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：无（维护者根据评测结论在 Agent 质量组决定下一轮修复，执行者不私自发单）

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](README.md)。执行前读取适用仓库规则、当前实现、调用链、测试和 diff。

2026-09-05 组织协调：本单与考题校正归同一 Agent 质量组，仍分任务串行执行。下文历史“转入 Agent 能力”保留当时记录；现有认领、Skill 写入范围、基线及验收条件不变，最终证据归当前父账本。

## 前置与并行

- 前置：无；本任务以当前产品合同和失败转录为修补依据。
- 冻结输入：两次 live 和 mutate 期间固定 `agent-service/`、`go/prompts/agent/`、任务集与模型配置；不得与共享这些输入的 L2/L5 采证、user-sim、harness 修改同时执行。
- 运行资源：固定干净 checkout 或由维护者预约完整工作区冻结窗口；run 目录独立，成对复跑结束前不修改采证 checkout 的文档或 HEAD；协调记录留在共享工作树。
- 后续候选 live 使用独立固定 checkout 和独立 run 目录，不修改共享 provider 设置、不暂停 worker、不重建共享数据库。本次 Skill 候选仍由认领者持有，原 05:00 轮释放记录不代表本轮占用状态。
- 2026-09-05T14:29:45+08:00 重新认领：考题校正 `4c8ad3e0` 已由独立会话交付；当前图片池与 dispatcher 任务不写本任务冻结输入。本次 Node L1 使用独立固定 checkout、独立 run 目录和启动时冻结的环境配置，不使用共享业务 DB 或修改 provider 设置。先保存未改 Skill 的 A 基线，B 只修改本任务 Skill；未来 C 自进化实验沿用独立对照合同，不在本任务实现。

## 做成什么样

既有 L1 任务在真模型下能复现地提高 pass，并且四条 Skill 变异至少能杀死一部分。提交说明写「产品合同 / Skill 缺陷」；Self-Harness 进化与 overlay lineage 不在本任务范围。2026-09-05 归属转入 Agent 能力，ID 保留；评测组负责题库与结果复核，不在本任务中同时修改被测行为和考题。

当前可复算基线（旧题集 `71d48f47…`，模型 `gpt-5.6-luna`，不可与新题集比较）：

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

读取失败任务的 `expect`、对应 `SKILL.md` 与产品合同，修有依据的指导缺陷。不要为了过题改 expect、放宽 grader 或只加会过的新题。题目失真影响验收时暂停本次比较并记录阻塞；评测已在 [eval-contract-alignment](archive/eval-contract-alignment.md) 冻结新题集，须用新 L1 `task_hash` `406dc178b7908384db08a836038c7b8809f0c05b0821b76d8345bd8a9a8db7fb` 重跑未修补基线与候选，历史分数不作跨题集改善证据。

## 合同

- 被测对象仍是生产 `PiRuntimeManager`。不要另起 harness。
- live pass 只由任务 `expect` 判定，不看 `reference.scripted_calls`。
- 转录只落 `STORAGE_ROOT/agent-evals/`，不进 git。
- 每个任务至少 3 条释义、每技能至少 10 正 + 5 负；改完跑 L0。
- 默认 k=3。
- 本次 A / B 对照依[评测合同](../agent-eval-system.md#人工改进与自进化对照)：A 使用校正题集后的固定 `4c8ad3e0`，完整 75 题 × 3；B 不改题目、world、模型、harness、runtime 或请求预算，只改有依据的 Skill。两次 B 复跑与 A 分别比较，旧 `71d48f47…` 只保留历史。原始结果与 arm 身份落独立 `STORAGE_ROOT=/home/cot/ProductFlow/storage-dev/skill-ab-20260905`，不占共享 latest；本轮公开题按 exposed development 处理，不宣称独立隐藏验收。

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

### 2026-09-05 人工候选

- `workflow-run-request` v2 -> v3：明确 `node` 与 `to_node` 都需要 `node_id`，目标由最新图和用户指认唯一确定。合同锚点 `go/internal/agent/workflow_requests.go` 的 `parseRunScopeSpec`；没有新增工具、权限或自动确认。
- `product-intake` v3 -> v4：从上下文 `image_type_catalog` 取实际图种 key，保留用户确认的数量，不翻译出新枚举。合同锚点 `go/internal/agent/contract.go` 的商品上下文与 `go/internal/product/intake.go` 的 `parseSelection`；没有硬编码测评答案。
- Skill README 删除「改技能必须同步 expect/reference」的过时指导，明确题目独立修订与新 hash 同题复跑。
- 候选 Skill catalog hash：`ef6f709594be4a51a029f10d05dc26bfaf27ba78db618c01f051c5398ecf1054`。候选仅上述两条 Skill 正文有行为变化；未修改任务、world、grader、runtime、harness、模型或 policy。
- 验证：`just agent-service-test` 35 文件、270 通过 / 2 跳过；`just agent-evals-coverage` 83 题、工具 24/24、Graph op 12/12；Agent service build 通过。确定性测试证明加载及既有合同未回归，不能证明真实模型已改善。
- 审核：主代理-agent-0905-0458 自审，生产指导与现有 Go 合同一致。候选随本次实现提交，通过本文件 Git 历史定位；本任务明确要求固定候选 commit 后采证，因此允许候选与最终验收分开，未满足条件不归档。
- 本轮只形成一个人工候选，未执行 B 模型调用或自动搜索。人工分析调用与时间未做完整计量，不能用于人工 / 自进化优化成本公平比较；A 的评测 tokens 与耗时单独保留。

抽读按每个 Skill 的 `task_id` 字母序选首个 pass^3=1 和 pass^3=0，均读取 trial=1 的转录；分组失败不代表所抽 trial=1 必然失败：

| Skill | pass^3=1 的首题 | pass^3=0 的首题 | trial=1 判读 |
|---|---|---|---|
| graph-editing | delete-one-node | dissolve-and-reorder | 删除唯一细节图节点并返回 revision；重排题缺输入顺序，合理追问，评分冲突已交独立任务 |
| media-library-organization | archive-asset | batch-rename | 两题均读取素材事实并提交待确认草稿；batch-rename 的 trial=1 通过，其它释义失败不能倒推本条错误 |
| product-intake | clarify-image-types | finalize-explicit-minimal-set | 缺图种时提问；最小套图 trial=1 提交 hero/detail 并复读，评分通过，但桩写后仍返旧图，不能据此证明真实展开正确 |
| run-diagnosis | contextual-failure-summary | global-multiple-workflows | 读取画布和运行详情后解释 provider_error；多工作流题追问第二个目标 ID，目标输入不足已交独立任务 |
| workflow-run-request | cancel-running-run | global-node-run | 查到唯一运行后提交取消；「主图节点」选择 node-image-1 却被期望 node-prompt-1 判错，已交目标释义校正 |

确证的行为失败另列：`run-diagnosis-retry-after-product-diagnosis` trial=3 已从详情读到 `44444444-4444-4444-8444-444444444444`，却在请求的 `source_run_id` 写成 `44444444-4444-4444-4444-444444444444`。这是原始 run 身份抄写错误，不能归咎于评分失真；本候选未声称解决该问题，后续按冻结输入复验后决定修复边界。

### 2026-09-05 完整 A 诊断批次

- run：`20260905T063224Z-07f845df`，固定干净 `4c8ad3e06aef7402a4bb91055dc3a9bb9220bbf9`，原 Skill 未改。`gpt-5.6-luna` / openai，k=3、并发 3，推理等可选参数无显式覆盖；endpoint 只保存 SHA256，不落密钥。
- 命令：`bash scripts/with_dev_env.sh node /tmp/productflow-skill-ab-qSOp8i/run-arm.mjs /tmp/productflow-skill-ab-qSOp8i/baseline A`，内部执行固定 checkout 的 `just agent-evals-live`，无 filter / suite 缩减。包装脚本副本保存在实验产物目录。
- 完整性：75 题各 trial 1/2/3，共 225；225 份转录的 run/task/trial/utterance/terminal 与记录一致。219 个冻结输入文件前后 SHA256 同为 `2d310954e4994cee34d729fee2bb5f29c1a8698e185109e977a25e7a8d93960e`，checkout 始终干净。进程已结束；退出 1 是原始评分存在失败，非运行中断。
- 身份：task hash `406dc178b7908384db08a836038c7b8809f0c05b0821b76d8345bd8a9a8db7fb`；A Skill hash `f2b4292cc0716ddc68d3515e1de2bcd205854c7e51ed1bb08dc99b5f8fff65ef`；harness hash `13e8e19ae0ba1cadc732f5f4ba6d0ffba0da08d5a859671ccb67d72bf52dae21`。
- 原始评分：162/225 通过，pass^1=0.7200；43/75 题三次全过，pass^3=0.5733。regression 29 题 / 87 trial：68/87，pass^1=0.7816、pass^3=0.6207，0.95/0.90 门槛未过。
- 分技能 pass^1 / pass^3：graph-editing 0.6222/0.4667；media-library-organization 0.7556/0.6000；product-intake 0.6444/0.5333；run-diagnosis 0.7778/0.6000；workflow-run-request 0.8000/0.6667。均为未校正的原始评分，不手工删失败重算。
- 运行窗口：2026-09-05T14:32:22.341+08:00 至 14:55:47.803+08:00，墙钟 23 分 25.462 秒；trial 累计耗时 4,205,510 ms、单条中位 16,493 ms。累计 token_count=7,373,162，usage 缺失 0；未换算费用，不能把累计 trial 时间当墙钟。
- 产物：`storage-dev/skill-ab-20260905/agent-evals/20260905T063224Z-07f845df/{run.json,trials.jsonl,summary.json,transcripts/}`；`experiment/A.json` 保留运行前后身份，`experiment/A-assessment.json` 独立标记测量资格。`run_status=complete` 仅表示产物齐全，不表示评分合同有效。
- 裁定：完整但仅供诊断，A/B 量化比较暂停；本轮未运行 B1 / B2 / mutate。公开调试题已暴露，不能作为隐藏验收。旧 0.6178 与本轮 0.7200 的题集不同，不报告为改善。

## 阻塞与交接

- 原因：首轮合同校正后，新 A 仍复现不可见目标值、非等价释义与桩读取事实缺口。当前批次只作诊断，不足以验收 B 的能力改善。
- 解除条件：[eval-observable-input-contract](archive/eval-observable-input-contract.md) 已独立交付测量修复，[生产素材读取合同](archive/agent-library-read-contract.md) 已补齐事实；完整题集仍等待 [独立观察刷新](eval-library-observation-refresh.md) 验收并重新冻结任务 / world / 桩合同。用同一可观测评测输入重新运行 A 与候选 B，不拿本轮 A 跨题集比较；既有认领与固定 A 产物保持。
- 跟进者：评测组负责独立校正，Agent 能力组负责候选与后续复跑。
- 未完成：候选两次 k=3、稳定性、系统性失败改善及 mutate kill_rate。regression 与 Self-Harness G1 / G2 均未通过，不把本候选提交写成任务完成。
- 交接：A 进程已结束，独立 checkout 与产物保留；未改共享 DB / provider。候选实现已由原认领者自审并随本次候选提交保管，保持原 owner 以待新题合同交接，不占后续评测任务的文件或 live 资源。

把 run 记在这里，不要改总账本。维护者验收时同步 Agent 能力章程的修复结论与评测章程的分数/门槛；本任务完成不改变 Self-Harness 阶段状态。

```text
YYYY-MM-DD | commit=<sha> | run_id=<id> | command=just agent-evals-live | n= | k=3 | pass^1= | pass^3= | task_hash= | skill_hash= | model= | artifact=
```
