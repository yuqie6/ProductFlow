# 任务：批准开发集合并采集可供 Miner 消费的冻结 L1 批次

状态：阻塞
类型：证据
认领者：—
认领于：—
业务组：评测
父账本：agent-eval-system.md
完成后可拆：Agent 维护者审核开发包合同后发布 P3；不自动解锁 P4

遵循 [Issue 协议](README.md)。前置解除并获确认认领后才开展任务调查或采证。

## 问题来源

[集合隔离机制](archive/eval-collection-isolation.md) 已在 `2ab85674` 交付，旧 run 没有其冻结身份，不能回填后冒充隔离采证。[eval-contract-alignment](archive/eval-contract-alignment.md) 已独立校正并冻结 L1 `task_hash=406dc178b7908384db08a836038c7b8809f0c05b0821b76d8345bd8a9a8db7fb`。Agent P3 仍需评测维护者批准场景分组并采集带身份的新开发批次，不能将旧题集误判直接归因为行为机制。

## 做成什么样

评测维护者批准场景分组与暴露情况，冻结校正后的题集，运行一次完整 `k=3` 的开发用途 L1 批次，导出仅包含开发材料的输入包。记录有效失败和不确定结论，保留全部试验；通过率不达标可以完成采证，但缺项、无效运行或缺身份不能关闭。

## 前置与并行

- [eval-contract-alignment](archive/eval-contract-alignment.md) 已独立校正、审核并提交，逐题产品依据和新 task hash 齐全；生产 Skill/runtime/harness 不随校正改变。
- 评测维护者审核清单：当前公开题与已读轨迹按 exposed development 处理，同源 scene/source/origin 不跨用途。隐藏与独立验收材料未就绪时记录空集与缺口，不另贴标签宣称独立。
- 真实 provider 凭据可用；固定候选代码 checkout、任务/world、Skill、壳、provider/model、推理参数、并发/预算与完整 trial 分母。协议已声明 dirty 工作树不能承担同 commit 比较时，使用独立固定 checkout，协调认领留在共享工作树。
- 运行使用独立 `STORAGE_ROOT`，不得修改共享 provider 设置、DB、worker 或图片池/浏览器资源。记录预期 task/trial 数后再开跑，不事后降分母。

## 修改与占用范围

- 本任务、父章程 T-07 与本次 L1 证据、Agent P3 依赖状态、看板/归档由维护者整合。
- 原始 plan、manifest、run、transcript 与 development-inputs 只进约定 `STORAGE_ROOT/agent-evals/`，不提交 Git。现有工具实现为 `evals/collections.ts`、`live-runner.ts`、`cli.ts`。
- 不改题目、world、Skill、harness、grader、集合实现、试验预算或分数阈值；发现新合同错误时暂停，独立交回评测实现任务。

## 执行与验收

1. 批准分组依据和暴露清单，用 `just agent-evals-freeze-collection <绝对 plan 路径>` 生成 manifest，登记 hash、代码基线、各用途 task 数、预期 L1 trial 数。
2. 用 `just agent-evals-run-collection <绝对 manifest 路径> development` 跑一次 k=3。原始失败不得删掉，基础设施故障保留原批次及明确原因，不重复采样直到绿。
3. `just agent-evals-export-development <run_id> <绝对 manifest 路径>` 通过，记录导出位置与完整 task/trial 数、harness/Skill/task/collection hash、provider/model/推理配置与 pass 指标。旧 run 不回填 collection。
4. 维护者检查开发包没有隐藏题/逐题结果/历史摘要回流，确认来源和身份可用于 P3。代码不自动证明分组的语义等价性，人工审批需署名和结论。
5. `just docs-check` 通过；有效采证完成与 D-03/D-08、T-08、G1/G2 分别记录。P4 拟送提案器的簇仍须每簇人工抽读至少 3 条，此任务不代办该审核。

## 阻塞与交接

- 原因：首轮考题合同已冻结，但 `eval-skills` 的固定新批次发现仍有不可见目标值、非等价释义与必要读取事实缺口；尚无获批准的冻结开发清单与新批次。
- 解除条件：[可见输入合同](eval-observable-input-contract.md) 独立校正并冻结后，评测维护者接手分组审核并确认固定执行窗口。`skill-ab-20260905` 的公开诊断批次不替代本单审批或 Miner 开发包。
- 跟进者：Agent 能力组主代理协调评测维护者。
- 交接：无执行者、源码 diff 或运行资源占用；不预研 Miner、不重用旧转录伪造完成证据。eval-contract-alignment 已归档。

## 发布依据

2026-09-05：主代理-agent-0905-0458 核对现有开放任务。L2 state、L3 user-sim、L6 production mine 与图片池均不承担本单的冻结 L1 开发输入，无重复采证单。仅发布为阻塞，不记为采证完成。
