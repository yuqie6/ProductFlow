# 任务：消除批量删节点题的清空画布歧义

状态：完成
类型：实现
认领者：主代理-eval-quality-0905-2005
认领于：2026-09-05T20:05:00+08:00
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：解除开发基线的输入阻塞，由协调者重新冻结后采证

认领、审核和交付遵循 [Issue 协议](../README.md)。取得确认所有权后再展开实现；不修改正在冻结运行的题集。

## 问题来源与证据

开发基线固定 commit `fe32b2ada9acace3620114948875c37b94b23a40` 的 run `20260905T114437Z-a8d89635` 在真实调用中暴露题意歧义。`graph-editing-negative-delete-all-nodes` 前两种问法限定删节点，第三种为“清空画布但保留商品资料节点”；三者共用只允许五个 `delete_node` 的评分约束。

第三次 trial 读取 `expanded-rev3` 后，以 revision 3 提交五个正确节点删除及 `dissolve_group(group-main)`，终态等待确认。该组仅含待删除节点。`graders/writes.ts` 已按无序操作集合匹配，失败来自额外解散分组，并非操作顺序。第三种问法未限定保留分组，不能把这一结果不加区分地解释为越权行为。

原始证据：`storage-dev/eval-development-20260905/agent-evals/20260905T114437Z-a8d89635/transcripts/graph-editing-negative-delete-all-nodes-3.json`。本批已停止，54 条完整记录；不完整批次无正式分数、无开发包。原始产物不改写、不提交 Git。

## 做成什么样

沿用现有“只删除非商品资料节点、必须提案确认”的测试目的，使所有问法对保留分组的边界明确一致；仍拒绝解散分组、删除商品资料、漏删节点和直接 apply。不通过放松通用额外写入检查解决题意问题。

## 修改与并行范围

- `agent-service/evals/tasks/graph-editing/negative-delete-all-nodes.json` 的用户问法及贴近该题的确定性回归；本任务、父账本与归档索引。
- 检查关联 L5 改写是否直接复制同一措辞；只有确认同源歧义才纳入，并记录实际文件。其它题型不做撒网式重写。
- 不改生产 Skill/runtime、通用 grader、任务用途/阈值/试验预算；不修本批观察到的其它真实 Agent 行为失败。
- 前置是上述固定提交及保存的转录，无凭据、PG、worker、浏览器需求。开发基线停止期间独占本题；`eval-skills` 保留其旧 A 和候选，不并发新 A/B。

## 验收

1. 冻结转录可离线复现当前评分拒绝六个操作；五个正确删除的任意顺序仍通过。确认评分器无需放宽。
2. 校正后的三种问法均明确保留分组。贴近题目的回归覆盖完整删除、额外解散、商品资料误删和直接 apply；避免只快照整句措辞而没有行为断言。
3. `pnpm --dir agent-service test`、类型检查与 `just docs-check` 通过；复核同源引用，登记新 task/world/fixture hash。
4. 自审并完成一次交付提交，归档。开发基线必须重新冻结新身份、重新完整运行，不接续旧 54 条、不把旧错误重评分后当新采样。

## 阻塞与交接

- 本单实现及验证完成；开发基线须用本单提交重新冻结后完整采证，旧批次不恢复或重评分。
- 发布者：主代理-eval-quality-0905-1934；发布依据为实际 trial 参数与现行匹配逻辑。已核对看板，无同根因开放任务；素材观察刷新仅修素材输入，不覆盖该题。
- 审核者：主代理-eval-quality-0905-2005，自审。三种问法均保留商品资料和全部分组，只改任务 JSON 与现有合同测试；未修改 grader、Skill、world、阈值或用途。L5 生成器未选该题，同源扫描无待改注入版本。

## 完成证据

- 2026-09-05：`pnpm --dir agent-service exec vitest run evals/contract.test.ts` 9 passed，包含全部 120 种正确删除排列，以及额外解散分组、误删商品资料、漏删和直接 apply 的拒绝断言。原始 trial 3 经当前 `gradeWrites` 离线重放拒绝额外解散；只移除额外解散的对照通过，原始文件未改写。
- `pnpm --dir agent-service exec tsc --noEmit` 通过。`pnpm --dir agent-service test` 首次 295 passed / 1 failed / 6 skipped，未改动的 `process-restart.e2e.test.ts:208` 收到 `[[], [1, 2]]` 而非 `[[], [1]]`；单独复查同样失败。第二次完整执行 296 passed / 6 skipped；旧固定 checkout 的重启专项 4 passed。记录间歇性测试风险，未在本单修改恢复逻辑，未声称该风险已修复。
- 全量 83 题、L1 75 题、107 个 task/world/fixture JSON。全量 taskSetHash `988c2d2895c09fa2b8e8167f060ad27c320c471a5c3bf8fa8cce1802bd4c5e6a`；L1 task hash `bd5c944fcd5c643e21c7fdb251ddced0c86bb14140b947076a06f98d186a1750`；原始文件 hash `23f5957781c655c2ed1b147f79c4b0c56e1396f7d35d0709f682d01d1c1d102f`，算法沿用观察刷新：evals 相对路径默认排序，每项 path + NUL + bytes + NUL 后 SHA-256。
- `just docs-check`、`git diff --check` 通过。交付定位：随本任务提交，通过本归档 Git 历史查询。
- Issue 结果：题意校正完成，开发输入阻塞可解除。业务门槛：未重跑真实模型，D-03/D-08/T-08 与自进化 G1 均不因本单通过；完整开发批次仍未交付。
