# 任务：批准开发集合并采集可供 Miner 消费的冻结 L1 批次

状态：开放
类型：证据
认领者：—
认领于：—
业务组：Agent 自进化
父账本：agent-self-harness.md
完成后可拆：自进化组协调者按父章程发布固定实验与自动验证切片；不直接发布整个进化系统

遵循 [Issue 协议](README.md)。前置解除并获确认认领后才开展任务调查或采证。

2026-09-05 组织协调：本单是自进化组的一次性启动输入交付，承接原 P3 所需开发材料；数据用途审核由协调者完成，不能变成未来每轮人工选题或审簇。现有缺题目校正的阻塞不因改组消失。

## 问题来源

[集合隔离机制](archive/eval-collection-isolation.md) 已在 `2ab85674` 交付，旧 run 没有其冻结身份，不能回填后冒充隔离采证。[eval-contract-alignment](archive/eval-contract-alignment.md) 曾冻结 L1 `task_hash=406dc178b7908384db08a836038c7b8809f0c05b0821b76d8345bd8a9a8db7fb`，后续仍发现输入缺口。自进化组须消费有效校正版本、登记场景分组并采集带身份的新开发批次，不能将旧题集误判直接归因为行为机制。

## 做成什么样

自进化组协调者依据固定集合合同审查场景分组与暴露情况，冻结校正后的题集，运行一次完整 `k=3` 的开发用途 L1 批次，导出仅包含开发材料的输入包。记录有效失败和不确定结论，保留全部试验；通过率不达标可以完成采证，但缺项、无效运行或缺身份不能关闭。

## 前置与并行

- [独立观察刷新](archive/eval-library-observation-refresh.md) 已独立审核并提交，沿用其最新 task/world/fixture 身份；早期 [eval-contract-alignment](archive/eval-contract-alignment.md) 仅作历史依据。生产 Skill/runtime/harness 不随本单采证改变。
- 本组协调者审核清单：当前公开题与已读轨迹按 exposed development 处理，同源 scene/source/origin 不跨用途。隐藏与独立验收材料未就绪时记录空集与缺口，不另贴标签宣称独立。
- 真实 provider 凭据可用；固定候选代码 checkout、任务/world、Skill、壳、provider/model、推理参数、并发/预算与完整 trial 分母。协议已声明 dirty 工作树不能承担同 commit 比较时，使用独立固定 checkout，协调认领留在共享工作树。
- 运行使用独立 `STORAGE_ROOT`，不得修改共享 provider 设置、DB、worker 或图片池/浏览器资源。记录预期 task/trial 数后再开跑，不事后降分母。

## 修改与占用范围

- 本任务、自进化父章程的启动输入状态与本次 L1 证据、看板/归档由协调者整合。T-07 原合同留在 Agent 质量账本，只引用已冻结版本，不在本单修改其评分或集合实现。
- 原始 plan、manifest、run、transcript 与 development-inputs 只进约定 `STORAGE_ROOT/agent-evals/`，不提交 Git。现有工具实现为 `evals/collections.ts`、`live-runner.ts`、`cli.ts`。
- 不改题目、world、Skill、harness、grader、集合实现、试验预算或分数阈值；发现新合同错误时暂停，独立交回评测实现任务。

## 执行与验收

1. 批准分组依据和暴露清单，用 `just agent-evals-freeze-collection <绝对 plan 路径>` 生成 manifest，登记 hash、代码基线、各用途 task 数、预期 L1 trial 数。
2. 用 `just agent-evals-run-collection <绝对 manifest 路径> development` 跑一次 k=3。原始失败不得删掉，基础设施故障保留原批次及明确原因，不重复采样直到绿。
3. `just agent-evals-export-development <run_id> <绝对 manifest 路径>` 通过，记录导出位置与完整 task/trial 数、harness/Skill/task/collection hash、provider/model/推理配置与 pass 指标。旧 run 不回填 collection。
4. 本组协调者检查开发包没有隐藏题/逐题结果/历史摘要回流，登记分组的产品依据和审查结论。代码 hash 不证明语义等价性，证据不足则停止；不要求用户再次审批普通公开开发题的选取，不授权读取未批准数据。
5. `just docs-check` 通过；有效采证完成与质量账本 D-03/D-08、T-08、自进化 G1 分别记录。后续问题选择和归因由控制器自动执行，旧每簇人工抽读前置撤销；本单不提前签收自动诊断质量。

## 阻塞与交接

- 当前实现依赖：[图观察权威](eval-graph-observation-authority.md)。不可测消费边界已交付；图操作及配置反馈需在节点合同稳定后由真实 Go 路径验证。[节点合同补齐](archive/node-detail-contract-completion.md) 已完成实现与验收，固定交付通过该归档的 Git 历史定位。图观察实现与快照更新尚未交付，不据节点归档解除此前置，不启动第四次付费全量采样。

- 输入阻塞已解除：[可见输入合同](archive/eval-observable-input-contract.md)、[生产素材读取合同](archive/agent-library-read-contract.md) 与 [独立观察刷新](archive/eval-library-observation-refresh.md) 已交付，12 条素材阻塞经独立审核移除。最新全量 taskSetHash 为 `1283d72edd0ed6a9ffb7dcd36f63c7652e14ea8eb3a8e1fc1c7e97c57041a258`；以观察归档的交付提交冻结执行代码，不使用旧合同 hash。
- 本单开放待认领。获批准的开发用途清单、新 k=3 批次及有效导出仍未完成，由本单执行者按上述合同采证。`measurementEligible=false` 的诊断分数不得用于候选选择；`skill-ab-20260905` 的公开诊断批次不替代本单有效开发包。
- 跟进者：自进化组协调者消费 Agent 质量组已交付的固定校正版本；输入就绪后本组自行采证。
- 交接：无执行者、源码 diff 或运行资源占用；不预研 Miner、不重用旧转录伪造完成证据。eval-contract-alignment 已归档。

## 发布依据

2026-09-05：主代理-agent-0905-0458 核对现有开放任务。L2 state、L3 user-sim、L6 production mine 与图片池均不承担本单的冻结 L1 开发输入，无重复采证单。仅发布为阻塞，不记为采证完成。
