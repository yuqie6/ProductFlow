# 任务：冻结场景集合并提供只含开发材料的 Miner 输入边界

状态：完成
类型：实现
认领者：主代理-agent-0905-0458
认领于：2026-09-05T06:08:43+08:00
业务组：评测
父账本：agent-eval-system.md
完成后可拆：维护者检查开发集输入合同后可发布 P3；隐藏集与独立验收实证不因本任务自动完成

任务合同以本文件为准，遵循 [Issue 协议](../README.md)，确认认领后才能调查与实现。

## 问题来源

评测 T-07 已登记当前双集合只按任务 ID 分配，缺场景级隔离和可审计的提案器输入边界。Agent P1/P2/P2b 已交付，P3 不得消费未经隔离的失败材料。现有任务、旧结果或经人工修补暴露过的题目不能改标签冒充隐藏或独立验收材料。

## 做成什么样

评测维护者能冻结可哈希的场景分组与集合用途，检查同源任务不跨集合；未来 Miner 只能取得获准开发集的任务与对应结果。错误身份、未登记任务、场景串集、隐藏/验收材料混入应在输出前被拒绝。当前已暴露材料如保留作开发输入须明确登记来源，不制造新的隐藏题。

本任务实现隔离机制及确定性验证，不制造独立性实证，不跑候选搜索，不宣称 T-08、G1 或 G2 完成。具体数据表示认领后依据现有 loader/split/provenance 调用链确定，不先指定第二套 runner。

## 前置与并行

- P2/P2b 已提交；本任务不要求生产凭据、人工标签或真实模型调用。
- 固定生产 Skill、壳、题目、world 与 grader。不得与修改 `split`、runner、run metadata 的任务并行。当前 eval-contract-alignment/user-sim 未认领；集合基线如遇题目修订必须失效并重新冻结，不能静默接受。
- 只使用临时本地目录和确定性 fixture；不使用共享 DB、浏览器或 provider，不占图片池会话的 storage。
- 父评测账本与图片组共享，由主代理只改 T-07 相关条款并选择性暂存，不能纳入图片采证修改。

## 修改边界

- `agent-service/evals/` 中现有 schema、loader、split、provenance、run-storage 及直接测试；新增集合模块/冻结清单只在现有 owner 无法承载时使用。
- 必要的现有 CLI/justfile 接线、评测 README 与活架构文档，只为提供确定的隔离输入入口。
- 调查所有集合身份、读取与写入者后，在本任务登记实际修改文件；如果触及 Go L2 公共 wire，扩充对应合同测试，不留双形状兼容。
- 实际 owner：新增 `evals/collections.ts` 与直接测试承载分组清单/开发投影；`live-runner.ts` 只接单集合 L1 选择与归因，`run-storage.ts` 增加可选运行集合身份，`cli.ts` / `cli.test.ts`、`justfile` 提供冻结/运行/导出入口，ARCHITECTURE 中英文记录边界。不改 Task/Trial wire 或 Go L2；既有 split 只保留为旧报告标签，不能作为集合权限。没有集合身份的旧 run 被开发导出拒绝。
- 本任务、父章程与 Agent 依赖链接、索引与归档。

## 不要碰

- 不改题目意图/expect/reference/world、grader、分数门槛、生产 Skill/harness/runtime。
- 不实现 Miner 分类、模型提案、候选比较、独立验收或热切。
- 不读取真实隐藏内容来人工修补 Agent，不导出隐藏逐题分数、摘要或轨迹给 Miner。

## 当前锚点

父章程 T-07/T-08 与「Self-Harness 评测用途」。已登记 owner 为 `evals/schema.ts`、`loader.ts`、`split.ts`、`provenance.ts`、`run-storage.ts`；认领后验证实际读写链和现有 CLI。旧 run 不回写、不伪造冻结身份。

## 合同与验收

- 同源场景和全部释义/派生任务同集合；冻结清单 hash 和任务内容身份可审计，改题后旧冻结身份不能仍被接受。
- 开发、隐藏回归、独立验收三用途与 suite/layer 正交。空的隐藏/验收集合允许用于机制测试，但必须显式报告缺失，不作为 G1/G2 通过证据。
- 输入投影须在读取或导出转录前完成身份与集合检查；不能先读全体敏感轨迹再丢弃。对未知 task、缺失身份、错配 run/task/manifest 和越界文件路径 fail closed。
- 当前机制若仅为应用层投影，应明确边界，不冒充同 OS 用户下的文件权限隔离；P4 提案器只能接投影材料，不能获得评测文件系统工具。
- 测试覆盖串集、内容变化、未知任务、混合结果、隐藏摘要泄漏、合法开发材料和路径越界；保留旧评分语义。`just agent-service-test`、`just agent-evals-coverage`、`pnpm --dir agent-service build`、`just docs-check` 通过。
- 自审记录实际采信的输入、完整 diff、证据及剩余缺口。实现完成与真实隐藏/独立集验收分开记录。

## 证据

- 发布：2026-09-05，主代理-agent-0905-0458 核对现有看板，无集合隔离实现任务；图片任务仅占图片采证路径，不重叠。本单不代替 eval-contract-alignment 的独立考题校正。
- 认领确认：主代理在协调工作树登记并复核；无其他 Agent 评测源码认领或残留实现 diff，图片会话保留其资源与三份文档修改；本任务自行执行、自审。
- 实现：`evals/collections.ts` 的 TypeBox plan/manifest/run identity 与现有 canonical JSON hash；清单完整覆盖 task set，显式 scene/source/origin 分组不跨用途，exposed 只能 development。任务、world 或分组变化使旧清单失效。全部来源/未暴露声明仍须维护者审核，机器校验不等于语义独立性证明。
- 运行：现有 L1 runner 接 collection path/purpose，禁止 filter/suite/task replacement/其它 layer；写实际单用途 task IDs 与 manifest hash。旧 split 只作原有报告标签，不作为 Miner 权限，不改旧 run 身份。
- 导出：先检查集合、run 身份和 summary 已落盘，再验证全批 trial 身份/数量/表述/路径，最后读对应转录。拒绝混合用途、无集合身份、缺项/重复/未知任务、路径穿越与符号链接。保留失败项；不读 history 或 summary 正文；输出只含开发 task/world/record 与所选转录字段，去掉转录顶层 thinking，不宣称匿名化。当前是可信评测进程的应用边界，未来提案器不得获得原始评测文件系统工具。
- 接线：`just agent-evals-freeze-collection`、`just agent-evals-run-collection`、`just agent-evals-export-development`；内容输出仅落 `STORAGE_ROOT/agent-evals/`，manifest 和开发包以 0600 / wx 写入，不覆盖历史。无生产 Agent、Task/Trial schema、grader 或 Go L2 wire 修改。
- 2026-09-05：定向 `pnpm --dir agent-service exec vitest run evals/collections.test.ts evals/cli.test.ts`：26 passed；`just agent-service-test`：35 文件、253 passed / 2 skipped。实际公共 83 题通过 CLI 在临时目录冻结为全开发 fixture（隐藏=0、验收=0），未登记成真实独立集或生产采证。
- `pnpm --dir agent-service build` 通过。因默认 tsconfig 只覆盖 src，另跑 `pnpm --dir agent-service exec tsc --noEmit --target ES2022 --module NodeNext --moduleResolution NodeNext --strict --skipLibCheck --esModuleInterop evals/collections.ts evals/collections.test.ts evals/cli.ts` 通过。该检查发现 HEAD 的 live-runner 已导出但未导入并发常量；在同文件补齐现有常量 import，不改变并发规则。
- `just agent-evals-coverage`：83 tasks，24/24 tools，12/12 ops；`just docs-check` 与 `git diff --check` 通过。未调用真实模型、未读旧转录、未修改共享服务/provider/DB 或图片采证目录。共享看板曾被并发更新带回旧开放状态，按仍有效的 issue 认领字段重新同步；未改变图片占用。
- 自审：主代理-agent-0905-0458 检查全部实现、未跟踪文件、测试与接线，确认未知身份失败发生在转录读取之前、失败项没有被剔除、无原始材料入 Git；自审不是独立评测审核。结果满足本单机制实现合同，随任务提交；T-07 仍部分完成，T-08/G1/G2 不通过。P3 消费真实数据还需考题合同独立校正、冻结清单及新开发批次，旧批次不回填身份；P4 另需簇人工抽读与未暴露验证材料。
