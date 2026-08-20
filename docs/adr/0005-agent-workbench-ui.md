# ADR 0005: Agent 工作台 UI 设计系统与工具步骤投影

## 状态

Accepted

## 背景

Agent 商品工作台（`pages/workbench/agent/`）目前是"能用的功能拼接"，缺少统一的设计意图。对照 DeepSeek Harness 的 Web UI，问题可以落到七个具体维度：布局、工具调用降噪、状态节点化、输入框、详情面板、turn 尾结构、设计 token 体系。其中六个可以在 frontend 内解决，但"工具调用降噪"受到一个架构约束：当前 wire 契约只投影 Agent 的最终 prose、Question 和 WorkflowDraft artifact，中间工具步骤没有暴露。

本 ADR 记录 UI 设计系统的决策和工具步骤投影的契约边界，为分阶段落地提供稳定依据。

## 决策

### 1. UI 设计系统七维度

#### 1.1 布局：从绝对定位叠加改为三层 grid 结构

当前 `AgentWorkbenchShell.tsx` 用 `absolute inset-0` 叠加 canvas 与 inspector，靠 `invisible/opacity-0` 切换，padding 由 JS 计算。改为语义化的三层结构：

- **Canvas 层**：V2 workflow 画布，占主导区域。
- **对话层**：Agent 对话，在窄视口下作为独立视图（沿用现有 mobileView tab），在宽视口下作为 inspector 的一个 tool。
- **Inspector 层**：复用现有 `ProductWorkbenchInspector`（已具备 resize、collapse、storage 持久化）。

保留 `ProductWorkbenchInspector` 为唯一 inspector 所有者，不新建并行的布局壳。核心变化是把 canvas padding 的 JS 计算收敛为 CSS grid track，让列宽由单一 owner 决定，响应式断点退化为"是否渲染对话为独立视图"这一个决策，避免 `canvasPaddingRight` 这类派生值在两个地方漂移。

#### 1.2 工具调用降噪：新增有界工具步骤投影（跨层）

这是唯一跨层的一项。main 的 `agent-service/src/contracts.ts`、`store.ts` 和 `pi-runtime.ts` 产生 `tool.step` 事件与 `ToolStep` 状态；前端 `agentEventReducer.ts` 严格解析该事件并按 `step_id` 合并快照与 live 步骤。Agent 的中间动作（加载 Skill、注入上下文、提出问题、读资产、读取历史、提出 Draft、创建待确认请求）通过有界投影对用户可见。

决策：在 Agent service 侧维护**有界工具步骤投影事件**，作为 web projection 的一部分，与 ADR 0001 的"ProductFlow 存 web projection，不重建 transcript"边界一致。投影使用四个必填字段和两个可选字段：

- `step_id`（幂等、可审计的步骤标识）
- `kind`（ProductFlow 自有工具类别，见下）
- `summary`（单行、有界长度的人类可读摘要）
- `status`（`running` / `succeeded` / `failed` / `unknown`）
- `tool_name`（实际 ProductFlow tool 名称，单行且有界）
- `details`（白名单详情：Skill 名称、有限长度的 Skill 正文摘要及截断标记、上下文区段与大小、问题选项、结果摘要、错误码、校验路径和可重试标记）

不允许把工具原始参数、完整输出、storage path、图片 bytes、私密 transport 内容或未定义的结果引用放入投影。校验错误只保存结构化的 path/message，避免把完整草案复制到浏览器。这与 CONTEXT.md 的"Agent lists bounded metadata and inspects only selected images"对齐。

ProductFlow 自有工具类别（不包含文件系统、进程、搜索或网络 coding tools）：

| kind | 摘要语义 |
|---|---|
| `inspect_image` | 查看商品图库资产 |
| `inspect_context` | 查看商品上下文与资产元数据 |
| `read_history` | 读取商品历史 |
| `organize_assets` | 整理商品图片资产 |
| `request_workflow_run` | 创建待用户确认的 WorkflowRun 请求 |
| `create_product` | 创建用户明确要求的空商品工作区 |
| `propose_draft` | 提出/修订 WorkflowDraft |
| `load_skill` | 加载版本化 Skill 指令 |
| `inject_context` | 注入本轮 Agent contract、Skill catalog 和页面摘要 |
| `ask_question` | 提出结构化问题并等待回答 |

当前没有真实 `generate_image` Agent tool，不加入投影。`ask_question` 不复制完整 Question owner；完整问题仍由 `question.required` 事件和 Question 状态提供，tool step 只展示动作、选项摘要和状态。

工具步骤投影是**可选能力**：Turn 快照缺失 `tool_steps` 时保留现有快照并兼容旧服务，显式 `[]` 才清空。前端在投影事件缺失时优雅降级为纯 prose 渲染。

#### 1.3 状态节点化：收敛散弹枪式错误横幅

当前 `AgentConversationPanel.tsx` 有六种独立 error 状态（list/initial/control/question/composer/preview），各自堆叠成 `PanelError` 横幅。收敛为两类：

- **Turn 级错误**：作为消息流里带位置的节点（持久、可定位），对应 Agent service 里该 turn 的失败事实。
- **局部操作错误**：composer 输入校验、预览失败等，留在操作发生处的局部 surface。

不在 header 下堆叠多个全局横幅。每条错误有单一 owner 和单一 render 位置。

#### 1.4 输入框：状态机 + Send/Stop 切换

当前 `AgentComposer.tsx` 是朴素 textarea + 图片选择按钮 + 发送按钮，cancel 在 header 里。改为：

- 输入框承载 Send/Stop 状态切换（running 时主按钮变 Stop），不再依赖 header 里的独立 cancel。
- 保留 enter 提交、shift+enter 换行、IME composition 保护（现有 `onKeyDown` 已有的行为）。
- 图片附件 rail 已有（`composerAssets`），保持，但收起布局收敛进 composer 卡内，与 input 形成单一交互面。

chip token（`/name`、`@subagent` 这类在文本流里按"单个实体"渲染、原子删除、可命中的引用 chip）暂缓引入：当前 Agent 只接收自然语言 + 图库 asset rail，没有需要文本内引用的语义对象，照搬 Harness 的 `/skill`、`@subagent` 只会得到没有语义对象的空机制。

但这不是永久取消。当 Agent 升级到需要在一条消息里引用多个对象（某张图、某段草稿 revision、某段历史 turn）时，chip token 就有明确用途。届时语义对象应是 ProductFlow 自有实体（`ProductImageAsset`、`WorkflowDraftRevision`、历史 turn），chip 的删除/undo/命中语义要与现有 asset rail 和 draft 引用对齐，而非照搬 Harness 的 `@subagent`。引入时机由 agent 能力升级触发，不在当前阶段实现。

#### 1.5 详情面板：第二阅读面

`AgentToolStepList` 提供行内可展开的详情阅读面。默认保持紧凑；失败的结构化校验步骤自动展开，成功步骤保留 Skill、上下文、实际 tool 名称和结果摘要。详情继续遵守 web projection 边界，不从 Agent service 重建完整 transcript。Skill 正文摘要只用于解释“加载了什么指令”，完整正文仍只进入模型上下文；原始工具参数、业务草案和内部资源路径不进入 Web。

#### 1.6 Turn 尾结构

每个 Agent turn 在消息流里拥有一个明确的"turn 尾"，承载：状态（`STATUS_KEYS` 已存在）、retry、失败摘要、artifact 引用（`workflow_draft_revision_id` → 审阅草稿入口）。当前状态以 10px 小字散落在消息下方，收敛为结构化的 turn 尾节点。

#### 1.7 设计 token 体系

当前 `web/src/index.css` 只有 `--font-sans`，组件里散落 `bg-slate-50`、`dark:bg-[#0b1220]`、`text-indigo-700` 硬编码，dark 靠零散 `dark:` 前缀。决策：

- 在 `@theme` 中建立最小 token 层，覆盖：语义色（`--color-surface-base`、`--color-surface-raised`、`--color-surface-inverse`、`--color-border-l1/l2/l3`、`--color-text-primary/secondary`、`--color-accent`、`--color-accent-fg`、`--color-danger`）、状态色（`--color-state-success/warning/error`）、缓动（`--ease-in-out`、`--duration-slow`）。
- 沿用 Tailwind v4 的 `@custom-variant dark`，token 由 variant 切换，不再在组件里写 `dark:` 前缀。
- 主题 tokens 参考 DeepSeek Harness 的 `--dsw-*` 命名思路，但命名收窄到 ProductFlow 需要的语义，不照搬其全部 token。

## 后果

- 工具步骤投影是 wire 契约的新增，需要 Agent service 与 ProductFlow 双向同步，且前端要对缺失事件降级。
- `tool.step` 的新增详情字段需要 Agent service、ProductFlow 和 Web 同步升级；旧四字段步骤仍可读取，未知详情字段在后端和前端都被拒绝或过滤。
- 布局改造有回归风险（画布拖拽/缩放/选择/edge 编辑/inspector/run history 必须保留，见 `web/AGENTS.md` 的 Canvas And Image Workflows）。
- token 体系改造面大（现有组件散落硬编码），需分阶段，先建 token 再逐组件迁移，避免一次大爆炸。
- 这些决策不改变 Agent 的权威边界（ADR 0001）、canonical 图片身份（ADR 0002）、schema-v2 工作流（ADR 0003）、V1 cutover（ADR 0004）。

## 排除方案

- 直接复刻 DeepSeek Harness 的 terminal/read/search/web 工具卡组件——ProductFlow 的 Agent 工具是图片生成和草稿修订，不是文件系统操作。
- 在 Agent 能力升级前提前引入 chip token 输入语法或命令面板；chip token 的引入应由真实的多对象引用需求触发（见 1.4）。
- 让 ProductFlow 从 Agent service 重建完整 transcript 或工具原始参数（违反 ADR 0001 的 web projection 边界）。
- 新建一套并行的工作台布局壳，而不复用 `ProductWorkbenchInspector`。
- 一次性替换全部图标/颜色/间距，而不建立 token 层。

## 落地顺序（分阶段）

1. **Token 层**（纯前端，最低风险）：建立 `@theme` token，不迁移组件，先让新写组件可用。
2. **布局收敛**（纯前端）：`AgentWorkbenchShell` 的 padding/列宽由 grid track 收敛。
3. **状态节点化 + turn 尾**（纯前端）：错误横幅收敛、turn 尾结构化。
4. **输入框状态机**（纯前端）：Send/Stop 切换。
5. **工具步骤投影**（跨层，最后）：Agent service 契约 + ProductFlow 消费 + 降级路径；当前已包含可展开的安全详情和问题/上下文/Skill 状态。

每阶段独立可验证、独立可回滚，不互相阻塞。
