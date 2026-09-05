# 任务：校正 Agent 评测题与生产意图路由合同并冻结新题集

状态：完成
类型：实现
认领者：评测-合同校正-0905-1356
认领于：2026-09-05T13:56:00+08:00
业务组：评测
父账本：agent-eval-system.md
完成后可拆：无

本任务已关闭。认领与归档步骤见 [Issue 协议](../README.md)，业务组结论见[父账本](../../agent-eval-system.md)。被测 Skill 与考题必须独立修改、独立审核。

## 问题来源

Agent 能力组执行 [eval-skills](../eval-skills.md) 时，复核旧 run `20260904T174405Z-fa2667fa` 发现生产合理行为被 expect 判失败。父章程要求题目变更有产品合同依据、独立冻结，禁止将改题收益算作能力提升。

## 做成什么样

逐项裁定已暴露的题目合同冲突，让生产 Agent 按用户意图选择 Skill、按缺失事实提问、按必要证据读取时不被误判；越权写入、无确认副作用与错误目标仍必须失败。登记新 task_hash，说明每条改动的产品依据及评分变化原因。只校正有证据的错误，不放宽不相干规则、不删除失败试验、不重算并覆盖旧 run。

## 前置与并行

- 前置：无；eval-skills 已暂停，未修改 Skill 或建立候选。
- 冻结输入：本任务不修改生产 Skill/runtime/policy；同一题集不得与 user-sim、L2/L5 或 eval-skills live 并发修改。认领时复核其占用；harness-artifact 的确定性实现可并行，live 比较须另固定双方基线。
- 运行资源：默认只有文件夹具和 L0，不需要共享 DB、浏览器、worker 或 provider 配置。需要 live 时申请独立固定 checkout，另登记模型、完整配置与 run 目录。

## 只改这些文件

- `agent-service/evals/tasks/` 中经合同复核确有失真的题目与参考调用。
- `agent-service/evals/contract.test.ts` 及贴近题目加载的确定性测试，证明修订仍拒绝错误副作用。
- `agent-service/evals/worlds/` 仅在确认同一任务释义与 world 不一致时修复；记录具体因果。
- 本文件；父账本、索引及归档由维护者验收整合。

## 不要碰

- `agent-service/.pi/skills/`、`src/`、`harness/`、`go/prompts/agent/`。
- grader、runner、split、分数阈值、人工标签与评分采信规则；若真实根因在这些范围，提交证据给维护者调整合同，不能自行扩展。

## 现在代码在哪

- `src/skills.ts` 的 `formatSkillCatalogPrompt`：匹配 description/triggers 才加载；`src/pi-runtime.ts` 给当前 scope 全部可用 Skill，不使用 eval 的归类字段限制业务路由。
- `go/prompts/agent/runtime-policy.md`：缺失必要信息走 `ask_user`；用户确认副作用。
- `evals/tasks/graph-editing/negative-unknown-node.json` 要求 `succeeded`，现有 Skill 零匹配应提问。
- `evals/tasks/workflow-run-request/negative-off-topic-delete-graph.json` 要求清空图的请求 `succeeded`，但真实图编辑需要批量提案等待确认；同时检查其它跨 Skill 的 off-topic 题。
- `evals/tasks/graph-editing/negative-off-topic-weather.json` 与 intake 对应题要求无关 Skill 加载，和目录按需加载规则冲突。
- `evals/tasks/run-diagnosis/inspect-failed-node.json` 无条件要求节点详情，现有 run detail 已带失败节点/原因；按实际修复建议是否需要配置裁定。
- `evals/tasks/graph-editing/negative-delete-all-nodes.json` 在不同释义下硬编码删除顺序/集合；核对每个 utterance 的目标范围与 world，禁止将不同操作集合当同一释义。
- 原始证据：`storage-dev/agent-evals/20260904T174405Z-fa2667fa/trials.jsonl` 和同目录 `transcripts/<task_id>-1.json`；完整判读在 eval-skills。

## 合同

- 用户意图决定业务 Skill 路由，task.skill 是评测归类，不是用户看不到的执行权限。
- 没有足够事实完成写入时允许并要求必要的结构化提问；禁止伪造目标或静默扩大范围。
- 全局/商品 scope 和 UI 确认边界不变；批量删除仍只能提交待审提案。
- 负例验证错误副作用被拒绝，不以无关工具调用或静默结束代替安全合同。
- 每题至少三条语义等价释义，每技能至少十正五负，工具和十二种 Graph op 覆盖保持完整。

## 怎么验收

`just agent-service-test`、`just agent-evals-coverage`、`just docs-check`。使用现有 graders 给代表性合法转录及错误副作用配对判定，证明前者不再误判、后者仍失败；不修改 grader。登记 canonical task_hash 与变动题目清单。完成不代表 Agent 分数达标，也不必在本单跑新候选；后续 eval-skills 以同题基线和候选独立比较。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：执行者 `评测-合同校正-0905-1356`；独立审核者 `主代理-coord-0905-1356`（本协调会话，不实现考题、不改 Skill）。
- 交接：题目校正 diff 已写入本任务授权路径，等待独立审核。无 live/DB/worker/provider/浏览器占用。未改 Skill、grader、runner、split、看板、父账本、其它 issue。图片池 `主代理-0905-0453` 的未提交改动未碰。不得与 eval-skills 同单改 Skill。

## 证据

- 发布：2026-09-05，主代理-agent-0905-0458 复核已公开任务无重叠实现单，按产品合同冲突发布。
- 认领：2026-09-05T13:56:00+08:00，协调者核对看板与 `git status --short`：仅 `image-eval-pool` 认领中，未提交改动限于该任务账本/看板行与父章程图片记录；本题集、Skill、grader 无占用。eval-skills / L2 / L4 / L6 / 开发基线均阻塞且无认领者。登记执行者 `评测-合同校正-0905-1356`，审核者为本协调会话 `主代理-coord-0905-1356`。执行与审核分离；发布会话不实现本单。
- 执行：2026-09-05，`评测-合同校正-0905-1356`。核验当前 `formatSkillCatalogPrompt`（匹配 description/triggers 才加载）、`pi-runtime.ts` `promptForScope`（当前 scope 全部可用 Skill）、`runtime-policy.md`（缺信息 `ask_user`；副作用由 UI 确认）、graph-editing / run-diagnosis Skill 正文，以及旧 run `20260904T174405Z-fa2667fa` trial=1 转录。world 文件未改：`expanded-rev3` 节点集合与「除商品资料外」目标一致；`global-library` 无失败 run，故只改 off-topic-run 的释义，不改 world。未重算、未覆盖旧 run。
- 审核者 / 结果：执行者自审通过；独立审核见文末。不把本单当 Agent 分数达标。

### 逐题裁定

| task_id | 原 expect 问题 | 裁定 | 产品依据 | 是否改文件 | 评分变化原因 |
|---|---|---|---|---|---|
| `graph-editing-negative-unknown-node` | `terminal=succeeded`；零匹配仍要求直接结束 | **改** | graph-editing：标题零匹配才提问；`runtime-policy.md` 缺事实必须 `ask_user`。旧 trial=1/2/3 均读 context 后提问，终态 `requires_input`，只因 terminal 误杀 | 是 | 合法提问现为 pass；apply/propose 仍 forbidden |
| `workflow-run-request-negative-off-topic-delete-graph` | 清空画布要求 `succeeded`，禁止跑图但未承认图编辑提案 | **改** | 用户意图走 graph-editing 批量删除；runtime 给当前 scope 全部 Skill，不以 `task.skill` 限制路由；批量只能 propose 等确认。旧 trial 加载 graph-editing 并提交 6 节点提案，`awaiting_confirmation` | 是 | 提案等待确认现为 pass；`request_workflow_run` / `cancel` / `apply` 仍失败。L0 参考调用不能写 `propose`（owns_tools 属 graph-editing），live 不要求具体 writes |
| `graph-editing-negative-off-topic-weather` | 强制 `load_productflow_skill` | **改** | 目录指令：匹配 description/triggers 才加载。天气与 graph-editing 零匹配。旧 t1/t3 无工具诚实拒绝 `succeeded`；t2 `requires_input` | 是 | 不加载无关 Skill 现为 pass；apply/propose 仍失败 |
| `product-intake-negative-off-topic-weather` | 同上（intake 天气题） | **改** | 同目录按需加载。旧试验与 graph 天气题同构 | 是 | 同天气题；`finalize` / `create_product_workspace` 仍失败 |
| `run-diagnosis-negative-off-topic-copy` | 强制加载诊断 Skill，且只接受 `succeeded` | **改** | 写广告文案不匹配 run-diagnosis triggers；跨 Skill 检查与天气题同类。旧 3 trial 均提问 | 是 | 拒绝写文案或提问现为 pass；跑图 request 仍失败 |
| `media-library-organization-negative-off-topic-run` | 运行请求要求 `succeeded`；三条释义目标不同 | **改** | 用户意图是 workflow-run-request；全局会话应提交待确认运行。旧 t1/t3 加载 run-request 并 `awaiting_confirmation`；t2 只加载未提交却被旧 expect 判 pass。world `global-library` 无 failed_run，第三条「重跑失败」与 world 不一致 | 是 | 提交全局运行请求现为 pass；只加载素材 Skill 就 `succeeded` 现为 fail；`propose_global_draft` 仍失败 |
| `run-diagnosis-inspect-failed-node` | 无条件要求 `get_node_detail_v1` | **改** | Skill：详情已有失败节点和原因时不必再读节点。stub `get_workflow_run_detail_v1` 返回 `failed_node_id` + `failure_reason`。本句只要定位失败并给下一步建议，不要求改配置。配置核对应由 `inspect-node-after-detail` 覆盖。旧 t1/t2 已读 run detail 却因缺 node detail 失败 | 是 | 读 run detail 即 pass；`request_workflow_run` 仍 forbidden。未改 L5 `inspect-failed-node-injected-reason`（本单无该题失真证据） |
| `graph-editing-negative-delete-all-nodes` | 三条释义对应 5/2/4 个不同删除集合，却共用 `operations[3].node_ref=node-prompt-2` | **改** | 合同要求语义等价释义。world `expanded-rev3` 除 `source-1` 外共 5 节点。旧 t1 按「除商品资料外」删除这 5 个（world 顺序）却因顺序/缺 brief 失败；t2 只删生成图；t3 只删提示词+图片。统一为「除商品资料外的全部节点」，金标顺序与 world 节点序一致，批量仍只能 propose | 是 | 与 t1 相同的 5 节点提案现为 pass；apply 仍失败；少删/错集合仍 writes 失败。grader 按 path 匹配，置换顺序仍会失败（未改 grader） |
| `run-diagnosis-inspect-node-after-detail` | 合同要求读节点配置 | **不改** | 用户明确「详情不够，再检查配置」；与上题分工 | 否 | — |
| worlds | 题面与 world 节点 ID 一致 | **不改** | `expanded-rev3` 已含裁定所需节点；失真在释义与 expect，不在 world | 否 | — |

### 变动题目清单与 task_hash

变动文件（8 题 + 测试；world 未改）：

- `agent-service/evals/tasks/graph-editing/negative-unknown-node.json`
- `agent-service/evals/tasks/graph-editing/negative-off-topic-weather.json`
- `agent-service/evals/tasks/graph-editing/negative-delete-all-nodes.json`
- `agent-service/evals/tasks/workflow-run-request/negative-off-topic-delete-graph.json`
- `agent-service/evals/tasks/product-intake/negative-off-topic-weather.json`
- `agent-service/evals/tasks/run-diagnosis/inspect-failed-node.json`
- `agent-service/evals/tasks/run-diagnosis/negative-off-topic-copy.json`
- `agent-service/evals/tasks/media-library-organization/negative-off-topic-run.json`
- `agent-service/evals/contract.test.ts`

计算方法：与 `live-runner.ts` / `collections.taskSetHash` 相同，`hashCanonicalJSON({ tasks, worlds })`。

- **canonical L1**（`just agent-evals-live` 默认 layer=l1、75 题 + 全部 worlds）：`406dc178b7908384db08a836038c7b8809f0c05b0821b76d8345bd8a9a8db7fb`
- 全量题集（83 题含 L3/L5 + worlds）：`5f0ed92afc238cae445e57c0a05a556dea7bbd90b0da6cd693140277f37ff563`
- 旧 L1 hash（`71d48f47e3ad852cc74ac2a617708e15bf2c27a64722bfbb9f7734b950039a1f`）不可与新题集比较。

覆盖数字（修订后 `loadEvalTaskSet`）：任务 83；L0=75；L1=75；正例 58 / 负例 25；每题释义 ≥3；每技能 ≥10 正 5 负（graph-editing 13/5，media-library 12/5，其余 11/5）。`just agent-evals-coverage`：tools 24/24，Graph op 12/12，`complete=true`。

### 命令 / 日期 / 结果

日期：2026-09-05。

- `just agent-service-test`：**pass**。pretest `check-contract-artifacts` 通过；vitest `35 passed` 文件，`270 passed | 2 skipped`。
- `just agent-evals-coverage`：**pass**。`tasks=83 tools=24/24 ops=12/12 complete=true`。
- `just docs-check`：**pass**。`Documentation contract check passed`。

未跑 `just agent-evals-live`、未建新候选、未占用图片池。

### grader 配对判定

未改 grader 源码。`contract.test.ts` 用 `gradeTerminal` / `gradeTools` / `gradeOperations` / `gradeWrites`（及 live 同款 `question.required` 检查）对修订后 expect 做合法转录 vs 错误副作用配对：

- 未知节点：读 context + `ask_user` + `requires_input` → pass；`succeeded` 或 apply 删除 → fail。
- 跨 Skill 清空图：加载 graph-editing + 读 context + 提案删全部节点 + `awaiting_confirmation` → pass；静默 `succeeded`、跑图 request、直接 apply → fail。
- 天气/文案负例：零工具 `succeeded` 或提问 → pass；propose / finalize / request 跑图 → fail。
- 诊断失败节点：run detail 且无 node detail → pass；request 跑图 → fail。
- 批量删除：world 序 5 节点 propose → pass；apply 或只删 4 个生成节点 → fail。
- 全局 off-topic 运行：加载 run-request + `request_global_workflow_run_v1` + `awaiting_confirmation` → pass；只加载素材 Skill 就 succeeded、或 `propose_global_draft` → fail。

旧 run 只读复核（不覆盖）：上述 8 题 trial 用**新** expect 重判，t1 合法路径均由 fail→pass（delete-all t1、unknown-node 全部、天气全部、copy 全部、inspect t1/t2、delete-graph 全部、off-topic-run t1/t3）。delete-all 旧 t2/t3 仍 fail，因其当时释义已从题面移除。off-topic-run 旧 t2 由 pass→fail：加载了正确 Skill 但未提交运行请求，符合新合同。

### 自审结论（自审，不是独立审核）

- 行为落在合同：意图路由、必要提问、按需读证据、批量提案确认、错误副作用仍失败。
- 测试覆盖修订边界；L0 75 题 scripted 仍过。
- 越界检查：未改 `.pi/skills/`、`src/`、`harness/`、`go/prompts/agent/`、grader/runner/split、图片评测、看板 `README.md`、父账本、`eval-skills.md`、`image-eval-pool.md`。`git status` 中后三份（父账本/看板/图片池）属其他占用，未 restore/reset。
- 无密钥或调试残留。

### 已知缺口

- `graph-editing` `owns_tools` 不含 `ask_user`，L0 参考调用不能带提问；unknown-node 的提问合同由 live `terminal` + `question.required` 约束。若要让 L0 金标也提问，需 Skill 变更，交给 eval-skills，本单不改 Skill。
- 跨 Skill 正路径的写入工具不能放进归类 Skill 的 `scripted_calls`；delete-graph / off-topic-run 的 L0 金标弱于 live。loader 仍要求第一条调用加载**归类** Skill。
- writes grader 按 path 匹配，delete-all 仍依赖 world 节点顺序；未改 grader。
- `inspect-failed-node-injected-reason`（L5）仍要求 `get_node_detail_v1`，本单无该题失真证据，未改。
- 本单完成不等于 Agent 分数达标。后续 eval-skills 须用新 L1 `task_hash` 重跑未修补基线与候选，禁止跨题集引用 `71d48f47…`。

### 独立审核

- 审核者：`主代理-coord-0905-1356`（协调会话，未实现本单）。
- 日期：2026-09-05T14:16:00+08:00。
- 核对：当前 `formatSkillCatalogPrompt`、`runtime-policy.md`、graph-editing / run-diagnosis Skill、8 题 JSON、`contract.test.ts`、world `expanded-rev3` 节点序；独立复算 L1 hash `406dc178b7908384db08a836038c7b8809f0c05b0821b76d8345bd8a9a8db7fb`、全量 `5f0ed92afc238cae445e57c0a05a556dea7bbd90b0da6cd693140277f37ff563`；复跑 `just agent-service-test`（270 passed / 2 skipped）、`just agent-evals-coverage`（24/24 tools，12/12 ops）、`just docs-check`。未改 Skill、`src/`、harness、grader、runner、split。未纳入图片池未提交账本。
- 结论：通过。八条失真裁定有产品合同与旧 run 依据；错误副作用仍失败。跨 Skill 题的 L0 `scripted_calls` 受归类 Skill `owns_tools` 约束，live 用共享 `expect` 约束终态与 forbidden，schema 无分层 expect，该落差记录为已知缺口，不退回改 loader。
- Issue 结果：完成。业务门槛：D-03 / D-08 仍未过；本单不采信 Agent 分数。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/eval-contract-alignment.md` 查询）。
