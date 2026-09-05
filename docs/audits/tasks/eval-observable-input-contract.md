# 任务：让 L1 评分要求只依赖 Agent 实际可见且语义一致的输入

状态：开放
类型：实现
认领者：—
认领于：—
业务组：评测
父账本：agent-eval-system.md
完成后可拆：维护者核对 eval-skills 与冻结开发基线的解除条件

遵循 [Issue 协议](README.md)，独立于被测 Skill 执行与审核。关闭后的 [首轮合同校正](archive/eval-contract-alignment.md) 保留原证据，本单承接新基线暴露的剩余问题。

## 问题来源

`eval-skills` 在固定 `4c8ad3e0`、未修改 Skill 的新 L1 基线 `20260905T063224Z-07f845df` 发现目标事实缺失或释义不等价。产物在 `/home/cot/ProductFlow/storage-dev/skill-ab-20260905/agent-evals/20260905T063224Z-07f845df/`；批次已完整结束，75 题 / 225 trial / 225 转录身份核对通过，原始评分 162/225，冻结输入无漂移。测量资格仅为诊断，不据此声称能力改善；详见 [A 批次证据](eval-skills.md#2026-09-05-完整-a-诊断批次)。

- `graph-editing-move-node-positions` 三句均未提供精确坐标，expect 要求 `node-image-1,920,240`；trial 1 合理追问新位置却失败。
- `graph-editing-dissolve-and-reorder` 前两句没有输入顺序，第三句才给顺序；trial 1/2 追问被判失败。还需核验 world 的输入边 role/order 能否支持要求的重排。
- `graph-editing-rename-group` 第二句要求「封面图」，共享 expect 要求「封面组」；trial 2 实际准确写入用户给的新名，仍失败。
- `graph-editing-update-node-config` 第二句没给配置内容，expect 要求「白底棚拍」；trial 2 读取 context/detail 后追问却失败。
- `graph-editing-rename-node` 第三句只说「这个提示词节点」，page context 无选中节点且 world 有两个提示词节点；trial 3 合理消歧却失败。
- `graph-editing-negative-global-scope` 要求加载只在 product scope 暴露的 graph-editing Skill，并只接受 succeeded；trial 1/2/3 仅提问、无写入仍失败。按当前 scope 能力和安全合同裁定，不为过题扩大权限。
- `workflow-run-request-global-node-run` 第一句说「主图节点」，trial 1 从图中选 `node-image-1`；expect 固定要求 `node-prompt-1`，第三句才明确后者。必须核对三句的目标等价性，不能用 Skill 把主图生成节点强行映射到提示词节点。
- `run-diagnosis-global-multiple-workflows` 第一句未列出两个工作流，page context 只指定一个；trial 1 请求补充第二个 ID 被判失败。读取工具能否发现候选，不等于用户已选定比较对象，需按可见事实核验。
- `stub-world.ts` 的 productContext 没有生产 `go/internal/agent/contract.go` 返回的 `image_type_catalog`，且 node_catalog.nodes 为空；节点 detail 是否补足事实需认领后核验。不能用 Skill 硬编码评测参考值补齐缺失世界。
- `product-intake-finalize-explicit-minimal-set` trial 2/3 在缺目录时提交 `main` 并被桩接受；trial 2 复读仍见 intake 空、name-only 图，因而重复 finalize。需核验桩在已接受写入后的可见事实与生产一致；只修必要观察合同，不在 TS 实现第二套 Go 图展开器。

## 做成什么样

每条 live 释义提供足以支持其正确答案的事实；同一 expect 下的三条释义表达相同目标。需要用户决定的事实仍应提问。桩世界保留生产必要读取合同，不把 reference.scripted_calls 里的答案直接泄露给被测模型。修订评分依据独立记录，新 task/world 内容哈希冻结后重跑 A 与 B，旧分数不覆盖、不跨题集比较。

## 前置与并行

- 前置：无；已落盘的新失败与当前产品合同可定位。维护者确认独立评测 owner 后执行。
- 冻结输入：不修改生产 Skill、runtime、harness、policy。`eval-skills` 仅运行固定 checkout，禁止修改其副本或覆盖 run。共享题集修改与后续 A/B 运行串行；本单交付前不得启动使用新题集的候选比较。
- 运行资源：确定性夹具优先，不需要共享 DB、provider 设置或浏览器；必要 live 另登记固定副本与预算。

## 只改这些文件

- `agent-service/evals/tasks/`、`worlds/`：有界复核全部 75 条 L1 的释义与可见输入，修改必须逐条给出现有产品合同和实际反例；不扩题量掩盖失败。
- `agent-service/evals/stub-world.ts`、`stub-world.test.ts`、`contract.test.ts`：只修已证明的读取返回事实缺口，并用现有结构来源复用；不在 TS 重做 Go 图语义。
- 如需现有生成工件/夹具同步，认领后确认实际 owner 与最小范围，不自行新建第二套业务目录。
- 本文件；父账本与索引由维护者整合。

## 不要碰

- `.pi/skills/`、`src/`、`harness/`、Go 业务实现或权限；不将具体节点 ID、坐标和题目答案写入 Skill。
- grader、runner、loader、schema、split、阈值、人工标签；若这些边界是真根因，给维护者证据调整任务，不绕过断言。
- 原始 run、旧归档结论、其它任务的 live 目录与共享 provider。

## 验收

- 逐条对照「用户话语 + page context + 可达读取结果 -> 所需决定 -> expect」，记录修改清单与不改理由。具体坐标、目标名、枚举、节点身份和顺序必须可追溯；语义等价不能只靠每题句子数量断言。
- 使用现有 graders 做合法结果 / 错目标与错误副作用配对回归；正确遵循用户输入不能失败，编造目标值不能因此通过。
- 桩读取合同回归覆盖真实目录形状和必要字段；错误/缺失事实仍可被识别，未知节点不伪造 detail。
- `just agent-service-test`、`just agent-evals-coverage`、`just docs-check`；工具 24/24、Graph op 12/12、每技能正负例及三条等价释义约束保持。独立复算 canonical L1 task/world hash。
- 独立审核并归档；不要求本单跑能力候选，不以改题后的分数变化宣称 Agent 提升。

## 交接

- 发布者：主代理-agent-0905-0458。仅记录当前已复现证据，未修改本单题目、world 或测评代码。
- `eval-skills` 的 B 比较须等待本单独立冻结；可证实且不依赖错误题目的生产修复仍按原任务保留单独证据，不能提前关闭其量化验收。
