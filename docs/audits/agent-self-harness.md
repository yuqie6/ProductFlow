# Agent 能力组与 Self-Harness 验收账本

本账本管理 ProductFlow 领域壳的版本化、失败挖掘、同模型提案、评测接受与生产热切。它衡量「壳会不会按声明面进化」，不替代 [`agent-eval-system.md`](agent-eval-system.md) 的行为分数，也不替代 [`performance-governance.md#production-gates`](performance-governance.md#production-gates) 的可靠性 Gate。lease、journal、SSE、确认协议仍走生产就绪账本。

**Agent 能力组章程。执行以已发布 issue 为界。** 本组接收原壳进化方向与普通 Skill 缺陷修复；当前任务与认领见 [Issue 看板](tasks/README.md)。P2 及以后由维护者按 P1 交付和对应阶段门发布。

## 组职责与交接

- 对 Agent 能否遵守商品事实、用户意图、确认权限并正确使用既有工具负责。普通 Skill 修复与 Self-Harness 进化分别验收；修好产品缺陷不等于 P1–P7 或 G1/G2 完成。
- [eval-skills](tasks/eval-skills.md) 从评测组转入，尚未交付。2026-09-05 复核发现 expect 与生产路由/提问合同冲突，暂停比较，交 [eval-contract-alignment](tasks/eval-contract-alignment.md) 独立校正并冻结新题集。只修生产 Skill，不改题目、world 或 grader；能力改善由评测组按冻结输入复核。普通修复无需等待 P1；当前无 Skill diff 或 live 占用，P1 可按自身合同推进。
- 本组拥有壳版本与候选行为，不拥有评分规则。题目过时交评测组独立处理；共享 runtime 或 policy 的 live 输入按看板冻结，不能边修改边采信旧结果。
- lease、journal、恢复与 SSE 基础交平台可靠性；Graph Command 与人工编辑合同交工作流体验。工具使用错误由本组修，工具业务实现错误按根因移交，不用 Skill 绕开。
- Skill 修复交付后，本组记录修复结果，评测账本记录分数与门槛；同一 run_id 通过链接复用，不复制第二套评分结论。Self-Harness 后续门槛保持下文合同。

Cursor Canvas 与会话纪要不是合同。合同只在本账本；未完成方向由 [`../ROADMAP.md`](../ROADMAP.md) 索引。阶段完成后，已接线事实写回 `docs/ARCHITECTURE.md`（Turn 带 `harness_hash` 等）；商家可见操作（清空 playbook）进 P6 才写 `docs/USER_GUIDE.md` 与 HelpPage。CONTEXT 词汇等代码落地再迁。

## 来源与使用规则

- 来源：2026-09-05 当前会话将 Self-Harness 设计冻成仓库合同。原始消息没有可在仓库中复核的 thread ID 或独立附件，因此本账本不登记伪造的来源 ID 或文本哈希。
- 适用范围：生产 `agent-service/.pi/skills/` 的普通修复，以及未来的 `agent-service/harness/`、Skill overlay、`runtime-policy` 行为段、Pi 钩子 Steer、商家 playbook、进化控制器（Miner / Proposer / Validator / Promoter）、演化轨迹与 `harness_hash` 归因。当前生产 loop 仍是 `PiRuntimeManager`。
- 本账本允许同时写目标合同、当前代码事实和缺口。当前能力只写入 `docs/ARCHITECTURE.md`；未完成方向由 ROADMAP 索引。
- 状态变更必须引用当前代码、自动化测试或真实运行结果。真实模型结果还要登记 `run_id`、模型、`harness_hash`、`task_hash`、`skill_hash`、试验次数和结果目录；没有这些字段不得补写「已通过」。
- 评测原始转录仍只进入 `STORAGE_ROOT/agent-evals/`，不提交仓库。演化轨迹（P2b）同样只落 `STORAGE_ROOT`，不进 git，不进 PostgreSQL 对话 journal。
- 后继实现必须先把目标阶段标「进行中」，再动该阶段代码。G1 / G2 是出口聚合，不是另开的实现切片。
- 一次变更只打一个可编辑面。上一阶段未标 `完成` 不得开下一阶段；P2b 可与 P3 并行。

### 状态四值

| 状态 | 判定规则 |
|---|---|
| `完成` | 当前实现、贴近合同的自动化测试和条款要求的真实运行证据都存在。 |
| `部分完成` | 已有可执行实现或既有证据，但数据规模、观测边界、统计口径、真实基础设施或复跑证据不全。 |
| `缺失` | 实现不存在，或当前证据不足以判断。 |
| `违背` | 当前实现明确采用冻结决策禁止的合同；迁移完成前保留该标记。 |

## 当前基线

2026-09-05 P1 交付盘点（代码与默认测试；进化控制器不存在）：

- 模型 loop 由 `@earendil-works/pi-coding-agent` 承担：`agent-service/src/pi-runtime.ts` 装配 Skill catalog、`runtime-policy.md`、动态上下文和 ProductFlow 工具。Pi `InlineExtension` 目前只用 `before_provider_request`。
- `agent-service/harness/` 是可哈希工件，`src/harness.ts` 读取并冻结；四段原有行为指导已从 policy 拆入 instructions。`runtime-policy.md` 保留冻结权限/确认/问题协议，manifest 钉住其摘要。Skill 在 `.pi/skills/`（独立 `skills.hash`）；工具清单保留 `TOOL_MANIFEST_VERSION`。
- 评测 L0–L6 在 [`agent-eval-system.md`](agent-eval-system.md)。L1 有 live `run_id`，regression 门槛未过，D-08 分数不可采信。本账本不得把那些 pass^k 写成 Self-Harness 成果。
- P1 工件 hash=`13e8e19ae0ba1cadc732f5f4ba6d0ffba0da08d5a859671ccb67d72bf52dae21`。生产 invocation/checkpoint 与 eval 尚无 `harness_hash` 归因（P2）；Miner / Proposer / Promoter、Steer、playbook 与演化轨迹仍缺失。P1 [交付证据](tasks/archive/harness-artifact.md) 不证明能力分数提高。

### 目标拓扑（G2 全开时）

```mermaid
flowchart LR
  live["生产 Turn 冻结 h_t"] --> traces["L1 trials 与可选演化轨迹"]
  traces --> mine["P3 失败签名 phi"]
  mine --> propose["P4 同模型最小 diff"]
  propose --> gate["held-in 且 held-out 且变异且 L5 且成本且体积"]
  gate --> merge["相容候选合并后再评"]
  merge --> rerun["基线与最终候选独立复跑"]
  rerun --> human["G1 人审后切版本"]
  rerun --> auto["G2 Promoter 热切"]
  human --> prod["生产 h_tplus1"]
  auto --> prod
  prod --> live
  prod --> rollback["kill switch 或 parent_hash 回滚"]
```

## 两档生产出口

Self-Harness「生产可用」分两档。它不等于生产就绪账本的 G-06 / G-07。

| ID | 出口 | 阶段 | 商家可见 | 评测依赖 |
|---|---|---|---|---|
| G1 | 辅助进化：生产 Turn 带着 `harness_hash` 执行冻结的 `h_t`；候选初筛、独立复跑后**人**审查并切版本（hash 指针或仓库引用） | P1–P4 均 `完成` | 无新 UI | 阶段验收登记独立验收集结果；允许评测 D-08 仍为分数不可采信，但必须标 `uncalibrated`，仅作辅助实验；分数不足以证明能力提高，禁止用 pass^k 对外宣称 Agent 质量 |
| G2 | 自动进化：Steer、playbook、自动热切、一键回滚、kill switch | G1 + P5 + P6 + P7 均 `完成` | playbook 可清空（P6 起写 USER_GUIDE） | 开 P7 前须 G1、P5、P6 完成，评测账本已登记通过 L5 的 `run_id` 且 D-08「分数可采信」为 `完成`；G2 阶段验收另登记独立验收集结果。D-03 通过率达标不得替代 D-08 |

G1 / G2 当前均为 `缺失`。

## 冻结决策

写入下表后，不得用会话摘要缩小范围。变更必须先改本表再改代码。

| ID | 决策 | 状态 | 当前证据与验收缺口 |
|---|---|---|---|
| D-01 | 自研对象是 ProductFlow 领域壳 + 进化控制器。Pi 继续当模型 loop。不复活 Go `agent-harness`，不并行第二套生产 loop | `完成`（决策） | P1 工件由现有 Pi adapter 加载，无旁路执行器；控制器缺失 |
| D-02 | 三层：L1 loop（Pi）/ L2 壳（Skill、policy、工具、上下文、确认）/ L3 控制器（挖失败 → 提案 → 评测门 → 热切） | `完成`（决策） | P1 收拢可编辑指令工件，原有 Skill/工具/上下文业务权威不变；L3 缺失 |
| D-03 | 三时钟、分写权：回合内只 Steer；商家 playbook 只进该商家 context；全局 Skill / 指令只从过门 diff 升。执行时 `h_t` 冻结 | `完成`（决策） | P1 加载时冻结工件，当前无运行时重载；Steer/playbook/晋升尚未实现 |
| D-04 | 同模型提案。工具 JSON schema、Graph Command、确认/物化、任务划分、评分与汇总、接受规则、结果记录、版本谱系、控制器提示与预算均禁止提案器修改 | `完成`（决策） | 评测任务与 grader 所有权在评测账本；控制器按固定规则写记录与谱系，不授予候选写权 |
| D-05 | 用户句子只进矿，不直接写全局 Skill | `完成`（决策） | 当前也没有运行时写 Skill 的路径；P6/P7 不得打开这条路径 |
| D-06 | 第一块可证明工作是 P4：只动 failure-recovery 与一条 Skill overlay；Steer 与 playbook 不得与 P4 同切片 | `完成`（决策） | P4 实现缺失 |
| D-07 | G1 = P1–P4 人切版本；G2 = 再加 P5–P7。开 P7 须 G1、P5、P6 完成，L5 通过且登记 `run_id`，评测 D-08 `完成`；D-03 不替代可信性要求 | `完成`（决策） | 评测 D-08 / L5 未完成；D-03 保留原产品可靠性指标与门槛，不承担测量可信性证明 |
| D-08 | 阶段串行；P2b 可与 P3 并行。一次 PR 只打一个可编辑面 | `完成`（决策） | P0/P1 已交付；P2 及以后仍按前置发布 |
| D-09 | 标 `完成` 必须有代码 owner + 测试或 `run_id`（`harness_hash` / `task_hash` / `skill_hash` / 模型）。分数可信性由评测账本 D-08 裁定，D-03 单独报告通过率达标情况 | `完成`（决策） | 进化代码尚未产生 `run_id` |
| D-10 | 进化不得改 grader 刷分，不得只加会过的新题。开发集、隐藏回归集、独立验收集及其划分、访问与退役规则由评测账本拥有 | `完成`（决策） | 见评测账本「Self-Harness 评测用途」；现有双集合不构成独立验收证据 |
| D-11 | 能力范围外、不进必做项：换 Pi、进化工具 schema、元进化提案器提示、subagent、SaaS 多租户、训练 LLM。以后要做必须先改本表 | `完成`（决策） | — |
| D-12 | 生产归因：`harness_hash` 写入 eval `run.json` 与生产 `agent_model_invocations` / checkpoint（新列不回填）。浏览器 journal 不因此改成存完整工具参数 | `完成`（决策） | P2 实现缺失 |

## 可编辑面与冻结面

提案器只能碰可编辑面。diff 碰到冻结面直接拒绝。

| 面 | 类别 | 说明 |
|---|---|---|
| bootstrap / execution / verification / failure-recovery 指令 | 可编辑 | P1 从 runtime-policy 行为段拆出；各段字节上限见 P4 接受门 |
| Skill overlay（追加，不改 frontmatter） | 可编辑 | P4 第一环只允许一条 overlay |
| runtime-control 执行行为阈值与 Steer 文案 | 可编辑 | 仅限声明的执行行为字段；钩子实现由人写、由测试钉死。控制器预算不在此面 |
| 商家 playbook | 可编辑 | P6 按反馈解释规则修订，按商家隔离，不进全局 `harness_hash`；每轮另记实际使用版本 |
| runtime-policy 权限与确认段 | 冻结 | 禁止代点确认 / 物化；page snapshot 不是授权 |
| 工具 JSON schema、Graph Command、幂等键、effect 对账 | 冻结 | 走普通产品开发 |
| ask_user 协议、lease、journal UI 合同 | 冻结 | 生产就绪账本 |
| 任务集及划分、graders、评分汇总 | 冻结 | 评测账本拥有；候选不得删除任务或试验、改变分母、改写分数 |
| 接受规则、结果记录、版本谱系 | 冻结 | 由控制器按固定协议维护；候选不得篡改结果、父版本、晋升决定或隐藏拒绝记录 |
| Miner / Proposer / Validator / Promoter 提示与控制器预算 | 冻结 | 禁止元进化；预算在壳工件外由人工配置并记录版本 |

## 防局部打钻

每个阶段用「本阶段做 / 本阶段不做」约束。下列走偏一旦出现，该切片不得标 `完成`：

- 手改一条 `SKILL.md` 把单题刷绿，跳过 held-out 与 lineage，并把它当作进化完成
- 为了让进化好看去改 grader，或只加会过的新题
- 同一变更里混 Steer、playbook、全局 Skill
- 没有 `harness_hash` 就开始热切生产
- 把再实现一遍 Pi 或复活 Go harness 当成 Self-Harness
- 用未校准 pass^k 对外宣称 Agent 质量
- 在 G1 / P5 / P6、评测 D-08 或 L5 未满足时开 P7，或用 D-03 达标豁免 D-08
- 复用候选筛选成绩当作独立复跑，或反复重跑直到通过

产品缺陷仍可按普通 Skill 修补推进；若评测 `expect` 已过时，交评测组独立修订并冻结后再比较。普通修补的提交说明写「产品合同 / Skill 缺陷」，不得写入本账本的 lineage 当进化成果。

## 阶段门

P0/P1 已按各自合同验收；后续阶段保留未完成状态。

| ID | 阶段 | 状态 | 本阶段做 | 本阶段不做 | Owner / 证据 / 缺口 |
|---|---|---|---|---|---|
| P0 | 转向文档 | `完成` | 本账本、ROADMAP / README 索引、AGENTS.md 与 Skill README 指针、`.cursor/rules/self-harness.mdc` | 任何进化代码 | 2026-09-05：本文件与索引、规则。`just docs-check` 为证据。CONTEXT / PRD / ARCHITECTURE 不得把 Self-Harness 写成已交付 |
| P1 | Harness 工件 | `完成` | `agent-service/harness/` 可哈希的 `h_t`；policy 权限段冻结、行为段可编；`pi-runtime` 读入；测试钉 hash | 提案器、热切 | [harness-artifact](tasks/archive/harness-artifact.md)：默认测试 218 passed / 2 skipped，build 与容器工件加载通过；完整镜像构建受 npm 网络超时阻塞，不宣称发布镜像已验收 |
| P2 | 归因 | `缺失` | eval `run.json` 与生产 `agent_model_invocations` / checkpoint 写 `harness_hash`；`/health` 可观察 | 为挖矿改浏览器 journal / UI 协议去存完整工具参数 | 列与字段不存在 |
| P2b | 演化轨迹 | `缺失` | 可选、默认关的有界轨迹，落 `STORAGE_ROOT`，不进 git、不进 PG 对话协议，供 P3 / P6 挖生产失败 | 把用户原文写入 Skill | 可与 P3 并行。P6 / G2 依赖本阶段或评测 L6 脱敏样本；不得只靠 L1 桩世界宣称「线上在学」 |
| P3 | Miner | `缺失` | 封闭失败签名 φ；仅开发集 L1 trials → 证据包；簇抽读合格才给提案器 | 自动改文件；读取隐藏回归或独立验收轨迹 | φ 词表见下节，实现缺失 |
| P4 | 第一环 | `缺失` | 同模型（生产 Agent 模型）对 overlay + failure-recovery 出最小 diff；初筛、合并再评、独立复跑、人审；谱系按 provider/model 分叉；落 lineage；G1 独立验收 | Steer、playbook、P7、subagent | 第一块可证明工作；依赖评测账本的集合隔离与复跑合同，缺证据不得标完成 |
| P5 | Steer | `缺失` | Pi `tool_result` 钩子拦截同参重放 / 缺 `load_productflow_skill` / 非法 op | 换 loop | 现有 extension 未注册这些钩子 |
| P6 | 商家 playbook | `缺失` | 按下方反馈规则形成该商家后续 context；可清空；Turn 及 eval 记录实际 playbook 版本；商家可见时同步 USER_GUIDE / HelpPage | 升全局 Skill；仅凭确认 / 丢弃 / undo 写长期规则 | 依赖 P2b 或 L6 脱敏样本 |
| P7 | 生产热切 | `缺失` | 影子评测与独立复跑通过后切 `harness_revision`；G2 独立验收；`parent_hash` 回滚；kill switch 冻结晋升；独立控制器配置限制战役次数 / token。单商家用「只影子不切」或「下 N 个 Turn」限制影响范围 | 无门热改；候选提高控制器预算 | 前置：G1、P5、P6 已完成，且评测 L5 已通过并登记 `run_id`、D-08 `完成` |

### P4 接受门

候选初筛必须同时满足，缺一项即拒绝。初筛通过只获得复跑资格，不直接切版本；以下条款均为待实现合同：

| 规则 | 内容 |
|---|---|
| 涨分 | k=3 聚合：Δᵢₙ ≥ 0 且 Δₒᵤₜ ≥ 0，且至少一项 > 0 |
| 最小改进 | 净增合计至少 2 个通过的任务·试验；属于筛选门槛，不构成统计显著性保证 |
| 成本 | 中位 tokens 增幅 ≤ 15% |
| 体积 | 可编辑指令合计 ≤ 16 KiB；overlay 合计 ≤ 10 KiB |
| 边界 | 由候选校验器核对声明的文件与字段；触及未声明面 / 冻结面、或没有审计记录 → 拒绝 |
| 合并再评 | 同轮多个通过的候选合并后作为新候选再过一次门 |
| 安全（G1） | 负例任务不得劣化；任何越权或未确认副作用直接拒绝，不得由其他涨分抵消。L5 未登记时安全证据不足，标 `uncalibrated`，不得据此宣称安全通过 |
| 安全（G2 / P7） | 引用评测账本已登记且通过的 L5 `run_id`，满足其 D-07 的 ASR 与效用门槛，且 ASR 不劣于当前 `h_t` |

最终候选（有合并时使用合并版本）与父版本各做一批新的 `k=3` 独立复跑，再应用上述门槛。复跑输入、完整性、失败处置及证据字段由评测账本「Self-Harness 独立复跑」定义。复跑失败不得晋升；G1 由人审查完整结果后决定切版本，G2 才由 Promoter 按固定规则执行。G1 / G2 阶段验收另使用独立验收集，不能用隐藏回归集或复跑结果替代。

### P6 反馈与归因

本节为待实现合同。playbook 的修订范围限定在当前商家：

| 反馈 | 写入规则 |
|---|---|
| 用户明确表达长期偏好，如「以后都这样」 | 可写入长期条目，保留来源与适用范围 |
| 当前任务要求，如「这次不要文字」 | 只约束当前任务，不自动写入长期偏好 |
| confirm / discard / undo | 作为带上下文的待分析证据，单独出现不能推出长期规则 |
| 与已有偏好冲突 | 区分临时例外与长期修订；记录修订来源，禁止静默叠加矛盾指令 |

playbook 不进入全局 `harness_hash`。P6 起每个 Turn 固定其实际使用的 playbook 版本，并在生产归因与 eval 记录中关联；无 playbook 显式记录为空。清空后后续 Turn 使用空版本，已开始的 Turn 不变。记录不得阻止清空内容。候选比较固定同一 playbook 版本；不得把记忆变化带来的效果差异计作壳进化。

## 失败签名 φ

P3 使用封闭词表。新词必须先加入本表再给 Miner 使用。三元组 φ = (c, q, m) 精确一致才进同一簇。

| 维 | 封闭值 |
|---|---|
| c 验证器级原因 | `wrong_terminal`、`forbidden_write`、`missing_required_write`、`wrong_write_params`、`unrepaired_conflict`、`budget_exceeded`、`state_mismatch`、`injection_followed` |
| q 因果地位 | `primary`、`contributing`、`incidental` |
| m 抽象机制 | `skipped_precondition_read`、`hallucinated_op_name`、`stale_revision`、`identical_payload_retry`、`premature_completion`、`over_asking`、`under_asking`、`scope_leak`、`pagination_incomplete`、`ignored_issues_path`、`tool_result_misparse`、`exploration_loop`、`skipped_skill_load`、`wrong_enum_alias` |

簇抽读：每个拟送提案器的簇至少人工抽 3 条轨迹，确认 c/q/m 与 grader 错误一致，才进入 P4。

## 评测账本依赖

- pass^1 / pass^k / Wilson / 变异 / kappa / L2 / L5 / L6 的条款与 `run_id` 只登记在 [`agent-eval-system.md`](agent-eval-system.md)。
- G1 可以用未校准 L1 辅助实验，结果记 `uncalibrated`；仍需独立复跑、人审和阶段独立验收记录，不得据分数宣称能力提升。
- G2 晋升必须引用评测账本已登记且通过的 L5 `run_id` 与 D-08 `完成` 的证据；D-03 仍单独报告，不得豁免 D-08。
- 三类任务集、分组隔离、独立验收与复跑证据只由评测账本维护；P3 / P4 / P7 读取对应结果，不得自行改题、改分组或改变试验分母。
- 评测剩余工作（同 commit 复跑、kappa、L2 live）走评测切片。Self-Harness 任务包只声明依赖，不顺手改评测冻结决策或任务合同。

## 运维合同

命令在对应阶段落地后写入 `justfile` 并回填本表 Owner。未落地前不得把下表写成已有命令。

| 操作 | 目标 | 状态 |
|---|---|---|
| 观察当前壳 | 生产 `/health` 与 eval `run.json` 暴露 `harness_hash`、`parent_hash`、`lineage_id`、模型 | `缺失`（P2） |
| 冻结晋升 | kill switch：Promoter 停止切版本，执行继续用当前 `h_t` | `缺失`（P7） |
| 回滚 | 切回 `parent_hash` 对应的 harness 工件 | `缺失`（P7） |
| 审计 | 列出最近被接受 / 被拒绝的 diff 与评测 `run_id` | `缺失`（P4） |
| 清空 playbook | 商家侧清除该商家记忆（P6 起进 USER_GUIDE） | `缺失`（P6） |

战役预算（P7）：单次进化战役的候选评测次数与 token 上限由壳工件外的控制器配置持有，人工设定并记录版本。候选无写权；超限停止晋升并留审计记录。`runtime-control` 仅持有声明的执行行为配置，不含控制器自己的预算或接受规则。

## 词汇表

下列术语在代码落地前只在本账本使用，不写入 CONTEXT。

| 术语 | 含义 |
|---|---|
| harness / 壳 | 版本化的 ProductFlow 领域配置：指令、Skill overlay、runtime-control、Steer 文案。不含 Pi loop、不含 Go 业务权威 |
| `h_t` | 某一时刻的冻结 harness 工件 |
| `harness_hash` | 对该工件的规范哈希，用于归因 |
| `lineage_id` / `parent_hash` | 谱系与回滚指针；按 provider/model 分叉 |
| overlay | 追加到某条 Skill 正文末尾的有界补丁，不改 frontmatter |
| Steer | 回合内、对 `tool_result` 的确定性注入或拦截 |
| playbook | 单商家记忆，只进该商家后续 Turn 的 context |
| Miner / Proposer / Validator / Promoter | L3 控制器四件：挖失败、出 diff、评测门、热切 |
| φ | 失败签名 (c, q, m) |
| G1 / G2 | 辅助进化 / 自动进化两档生产出口 |
| uncalibrated | G1 在评测 D-08 未完成时使用的 verifier 标记 |

## 活文档迁回

| 条件 | 写入 |
|---|---|
| P1 `完成` | `docs/ARCHITECTURE.md`：壳是版本化对象；`pi-runtime` 读 harness 工件 |
| P2 `完成` | `docs/ARCHITECTURE.md`：Turn / 模型调用带 `harness_hash` |
| P6 商家可清空 playbook | `docs/USER_GUIDE.md` 与 HelpPage 同一提交 |
| G1 或 G2 `完成` | 从 ROADMAP 本节移除已完成出口，稳定事实留在 ARCHITECTURE；本账本保留证据 |
| 术语进入代码标识符并被多处使用 | 迁入 `CONTEXT.md` |
