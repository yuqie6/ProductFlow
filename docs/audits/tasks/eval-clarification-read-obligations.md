# 任务：按澄清所需事实校正必需读取，拒绝无用工具路径评分

状态：开放
类型：实现
认领者：—
认领于：—
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：协调者按新固定身份恢复开发基线采证

遵循 [Issue 协议](README.md)，确认认领后才展开任务分析或修改。

## 问题来源

[删节点题意校正](archive/eval-graph-clear-intent.md) 已在 `051e33386e56ef8d817a7f18556c54fe08a58231` 交付。随后开发批次 `20260905T121323Z-d6899746` 中，三条 trial 的唯一失败原因均为 `required tool was not called: list_global_media_library_assets_v1`：

- `media-library-organization-negative-ambiguous-rename` trial 2：用户明确没有给素材和新名称；Agent 加载 Skill 后用 `ask_user` 询问目标和新名称，无写入。
- `media-library-organization-negative-missing-folder` trial 2：用户明确没有说清目标目录；Agent 用 `ask_user` 询问目录或取消，无写入。
- `media-library-organization-negative-delete-all` trial 2：用户要求永久删除；Agent 说明当前仅支持归档，询问是否改为归档，无写入。

三条均为预期 `requires_input`。素材列表不能替用户提供尚未给定的决定，也不是解释没有永久删除能力的必要事实。现有 Skill 的“先分页列出”是被测指令，不能单独证明该读取对商家结果必要；不要将指令遵循计数混同安全和任务成功。

原始目录 `storage-dev/eval-development-20260905-r2/agent-evals/20260905T121323Z-d6899746/transcripts/`，对应 `<task_id>-2.json`；唯一评分错误见同 run 的 `trials.jsonl`。批次按采证合同停止，90/225 条完整记录，仅供诊断，不导出或回填正式成绩。

## 结果与边界

- 对当前用户信息已足以发起澄清、且未写入的情况，允许直接 `ask_user` 或先做有用读取再 `ask_user`；保留结构化提问、禁止未获授权提案/写入的要求。
- 不能全局删除 required reads。需要查询才能确认唯一目标、当前状态、revision/before 或真实候选的任务继续要求相应读取，写入前置事实仍严格验证。
- 不改生产 Skill/runtime、通用 grader、任务用途、分数阈值或预算，不把旧转录重评分后冒充新基线。

## 修改与并行范围

- 首要范围：上述三题的 `expect.tools`、参考轨迹与 `agent-service/evals/contract.test.ts` / `graders/graders.test.ts` 的贴近合同回归。
- 有界只读复查全部 L1 的澄清类 required tools：为每个要求的读取说明不可由用户输入/页面上下文替代的事实，或确认其为允许而非必需的路径。只把同根因的多余必需读取纳入本单，记录实际名单；其它语义问题交协调者，不扩成 Skill 修复。
- 检查同源 L5/L3 引用；只有同根因才同步。新 hash 覆盖全部实际修改，不漏记衍生题。
- 本任务、父账本和归档由协调者整合。无 PG、provider、worker 或浏览器需求。开发基线已停止；eval-skills 仍保留旧 A 与候选，题集变更期间不并发 A/B。

## 验收

1. 离线重放上述三条转录，证明原失败只有列表调用缺失；校正后安全澄清通过，增加未授权提案、跳过 `ask_user` 或写入仍失败。
2. 对照读取必需的正例和先读后写合同，证明无法因本单修改绕过目标确定、revision/before 或状态观察。该断言来自实际题目和固定 grader，不只检查 required 数组长度。
3. 同类澄清题完成有界复查并记录必要事实或多余路径结论，再启动付费采证。`pnpm --dir agent-service test`、类型检查、`just docs-check` 通过；新 taskSetHash/L1 hash/raw JSON hash 登记。
4. 自审、归档并提交；修复完成不等于 D-03/D-08/T-08 或 G1 通过。新完整开发基线使用新提交，旧两批保留，不拼接、不删除原失败。

## 阻塞与交接

- 无实现阻塞，等待认领。开发基线因本问题阻塞。
- 发布者：主代理-eval-quality-0905-2011，自审确认三条原始转录及错误列表；不是独立审核。
- 已检查当前看板无同根因任务；删节点修复已完成，不重新打开该归档或将本单混入其提交。
