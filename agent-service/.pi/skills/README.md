# ProductFlow Skill 编写标准

技能是带 frontmatter 的 Markdown。目录提示词只暴露 name、description、triggers、owns_tools、scope；正文按需用 `load_productflow_skill` 加载。

语言中立 JSON 任务集在 `agent-service/evals/tasks/`，世界描述在 `evals/worlds/`。改技能必须同步对应任务的 `expect` 与 `reference.scripted_calls`。`just agent-service-test` 跑 L0 合同（schema、owns_tools、禁令、参数路径、非法参数两次内修复、工具/op 覆盖）。真实模型档是 opt-in：`just agent-evals-live`（需要 `AGENT_PROVIDER_API_KEY`，默认 k=3）；`just agent-evals-smoke <skill>` 按技能 k=1 冒烟。分层验收见 [`../../../docs/audits/agent-eval-system.md`](../../../docs/audits/agent-eval-system.md)。为追 L1 分数改 Skill 前，对 [`../../../docs/audits/agent-self-harness.md`](../../../docs/audits/agent-self-harness.md) 的阶段门：当前进行中阶段之外，手改单题刷绿不算进化完成。`just docs-check` 校验技能引用的工具名在清单内。

`load_productflow_skill` 返回原始 Markdown 指令，不用 `{ schema_version, data, guidance }` 信封。

## Frontmatter

| 字段 | 必填 | 说明 |
|---|---|---|
| `name` | 是 | 与目录名一致，`kebab-case` |
| `description` | 是 | 一句话，进目录提示词 |
| `triggers` | 是 | 何时加载，进目录提示词 |
| `owns_tools` | 是 | 本技能负责其前置条件与调用顺序的工具名，必须存在于 `tool-manifest.ts` |
| `scope` | 是 | `global`、`product_workflow` 或 `any` |
| `version` | 是 | 正整数 |

## 正文五段

固定使用这些标题，祈使句，工具名写清单里的精确名称：

1. **何时使用**
2. **前置事实**
3. **工作循环**
4. **禁止行为**
5. **完成判据**

规则只允许出现在 system prompt / runtime-policy、技能、工具 description 三者之一。工具 description 只说明这个工具是什么；技能说明何时怎么组合。
