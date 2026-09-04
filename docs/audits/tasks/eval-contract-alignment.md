# 任务：校正 Agent 评测题与生产意图路由合同并冻结新题集

状态：开放
类型：实现
认领者：—
认领于：—
业务组：评测
父账本：agent-eval-system.md
完成后可拆：无；完成后维护者核对 eval-skills 的解除条件

任务合同以本文件为准；按 [Issue 协议](README.md) 确认认领后调查。被测 Skill 与考题必须独立修改、独立审核。

## 问题来源

Agent 能力组执行 [eval-skills](eval-skills.md) 时，复核旧 run `20260904T174405Z-fa2667fa` 发现生产合理行为被 expect 判失败。父章程要求题目变更有产品合同依据、独立冻结，禁止将改题收益算作能力提升。

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

- 原因：无；待评测组确认认领。
- 跟进者：Agent 能力组主代理协调评测组。
- 交接：没有实现 diff 或占用运行资源；本单不能与被测 Skill 同单交付。

## 证据

- 发布：2026-09-05，主代理-agent-0905-0458 复核已公开任务无重叠实现单，按产品合同冲突发布。
- 审核者 / 结果：待评测组独立执行与维护者验收，不把发布当实现完成。
