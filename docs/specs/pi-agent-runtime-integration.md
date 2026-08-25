# Pi Agent Runtime 集成与 ProductFlow Skills 实施规范

## 1. 状态

- 文档状态：Approved
- 批准依据：`docs/adr/0007-pi-agent-runtime-boundary.md`
- 当前交付：main adapter 已实现；真实 provider、浏览器和后台 durable gate 仍待补，见 `docs/rollout/pi-agent-durability.md`
- 当前实现：Node.js 22 + `@earendil-works/pi-coding-agent` ProductFlow adapter，入口为 `agent-service/src/main.ts`
- 实验实现：`exp` 分支保留 Go Agent service + `agent-harness` snapshot

本文负责 Pi runtime、Skill、动态 Context、ProductFlow Tool 和迁移验收的实现规则。全局 Session/Task 产品语义仍由 `docs/specs/global-agent-human-workflow-design.md` 负责；Draft、素材和 WorkflowRun 的业务权威仍由现有 ADR 与 application use case 负责。

## 2. 目标结构

```text
Web
  -> FastAPI Agent API
  -> ProductFlow Agent service adapter
       -> Pi SDK agent session
            -> ProductFlow Skills
            -> per-turn Context injection
            -> ProductFlow custom tools
       -> Pi event translator
  -> existing ProductFlow SSE / Web projection

ProductFlow custom tools
  -> FastAPI internal API
  -> application/domain validation
  -> PostgreSQL / Redis / storage / provider
```

Pi 只位于 Agent service adapter 内。Web、FastAPI 业务层和 worker 不依赖 Pi 的内部类型；它们只依赖当前 ProductFlow 的 HTTP、SSE、Draft 和业务 DTO。

## 3. 四个词的准确含义

| 名称 | 解决的问题 | 例子 | 是否能阻止错误操作 |
|---|---|---|---|
| Skill | Agent 应该怎样理解和处理一类工作 | “整理素材需要列出有界资产，随后提交整理 Draft” | 不能单独阻止 |
| Context | Agent 这一轮面对的真实背景是什么 | 当前商品、Task 目标、选中资产、页面 revision | 不能授予权限 |
| Tool | Agent 能请求哪个业务能力 | `get_product_workflow_context_v1`、`propose_workflow_draft` | 只能把请求交给后端 |
| Backend | 这次请求是否有效以及如何产生副作用 | scope、revision、幂等、事务、队列、确认 | 可以并且必须阻止 |

把 ProductFlow 操作封装给 Pi 时，业务知识放进 Skill，动态事实放进 Context，机器可执行的入口放进 Tool。Tool 不能只是一段 Skill 文本，Skill 也不能承担后端校验。

## 4. Runtime adapter 规则

### 4.1 外部合同保持稳定

当前 FastAPI 通过 `infrastructure/agent_service.py` 调用 Agent service，Web 通过 FastAPI 读取 Turn、Question、Draft 和 SSE。Pi 迁移期间保持这些外部语义：

- Turn 创建、状态查询、取消、恢复和 SSE 继续由 ProductFlow-facing adapter 提供。
- `text.delta`、`question.required`、`tool.step`、artifact、completed、failed、cancelled 和 unknown 继续映射到现有 projection。
- Pi 原始事件、模型 request、完整工具参数和内部 session file 不直接返回浏览器。
- adapter 在 health/status 中报告 runtime 名称、版本、Skill catalog hash 和 Tool contract version，便于排查混用。

### 4.2 使用 Pi SDK

主实现使用 Pi SDK 的 `createAgentSession` 和隔离配置的 `DefaultResourceLoader`：

- `DefaultResourceLoader` 关闭默认 Skill、Extension、prompt template、context file 和操作系统工具发现。main 不加载用户目录或工作区里的资源，只保留一个 adapter-owned 的隐藏 request hook 映射后端 provider 选项，该 hook 不注册业务工具、不访问外部资源。
- `agent-service/src/skills.ts` 使用 Pi 的 Agent Skills parser 做 metadata discovery 和标准校验；完整 `SKILL.md` 不在启动时注入，而是由 adapter-owned 的 `load_productflow_skill` custom tool 按名称按需加载。
- `references/` 里的静态文本可以通过同一个受控 loader 按相对路径读取；`scripts/` 不执行，`assets/` 不通过 Agent tool 暴露。这样保留 Skill 的可移植目录格式，同时不把 `read`、`bash` 或宿主文件系统权限带入 ProductFlow Agent。
- `customTools` 在 `agent-service/src/tools.ts` 注册 Skill loader 和 ProductFlow business tools。
- session event subscription 负责把 Pi 的文本、工具调用、工具结果、错误和结束事件翻译成 ProductFlow wire events。
- Pi 的 `abort`、session switch 和 compaction 通过 adapter 映射到现有 Turn 控制语义。
- Pi RPC 仅在 SDK 直接嵌入无法满足隔离要求时评估，不能作为第一版默认实现。

### 4.3 关闭操作系统工具

ProductFlow 业务 Agent 不需要代码编辑能力。创建 Pi session 时必须使用显式 allowlist：

- 不启用 `read`、`write`、`edit`、`bash`、`grep`、`find`、`ls`。
- 不把 ProductFlow data root、数据库 socket、provider secret 或宿主机工作目录暴露给 Pi。
- 不依赖 Pi 默认用户权限来完成业务安全；运行进程仍使用最小系统账户和必要的容器/沙箱边界。
- 每次 CI 测试检查实际注册的 tool names，防止升级 Pi 或 Extension 后默认工具悄悄出现。

Pi 官方说明默认运行时没有文件、进程、网络和 credential 的内置权限系统；这条约束属于 ProductFlow adapter 的强制配置，不依赖模型是否“听话”。

### 4.4 连接、长任务与重启恢复

浏览器只观察 FastAPI 的 Web projection 和 SSE。SSE 关闭不调用 Agent cancel；重连使用 sequence cursor 从 PostgreSQL `agent_turn_events` 继续读取。

分层：

- Pi 模型 Turn 可以在浏览器关闭后继续，直到当前 Agent service 进程完成、取消或失败。
- WorkflowRun、图片生成和 delivery rendition 由 PostgreSQL/Redis/Dramatiq 承担。
- Pi session/event 文件只服务对话与 Turn 投影恢复，不构成业务执行队列，也不证明工具副作用已经完成。

启动恢复只重放尚未开始、且没有 execution attempt/fencing 记录的 `queued` Turn。已进入模型或工具、问题事实不完整、或无法证明结果的 Turn 结束为 `unknown`。完整问题 checkpoint 的 `waiting_input` 通过 continuation Turn 回答，不重放原模型轮次。lease、fencing、handoff 和 effect reconciliation 的判定表以 `agent-service/src/pi-runtime.ts`、`application/agent/sync.py`、`test_workflow_agent_service.py` 为准。进程重启后的原地模型请求恢复、全量副作用对账和跨实例调度仍见 `docs/rollout/pi-agent-durability.md`。

## 5. Skills 设计

### 5.1 初始 Skill 集合

Skill 先按用户任务组织，不按后端模块拆分：

| Skill | 负责的用户目标 | 允许使用的能力 |
|---|---|---|
| `productflow-core` | 统一理解 ProductFlow 对象、事实来源和安全边界 | 有界读取、追问、live graph 写入规则 |
| `product-intake` | 收集商品事实、真实参考图和图片需求 | 商品上下文读取、选中图片 inspect、intake、ChangeSet |
| `media-library-organization` | 整理全局素材、文件夹、标签和归档状态 | 素材列表、选中素材 inspect、整理 Draft |
| `workflow-run-request` | 针对明确工作流请求一次待确认执行 | Workflow/Run 读取、revision 校验、Run request |

第一版不为每个 CRUD endpoint 建一个 Skill。Skill 应该描述用户任务的完成路径；Tool 才负责提供具体的机器接口。

### 5.2 Skill 内容准则

每个 `SKILL.md` 至少说明：

1. 触发条件和不适用条件。
2. 本任务需要的事实和读取顺序。
3. 可用 Tool 以及每个 Tool 的用途。
4. 信息缺失时应该追问什么，什么情况下停止追问。
5. 何时产生 Draft，Draft 中必须包含哪些事实和 revision。
6. 何时必须等待用户确认。
7. 明确禁止的行为，例如臆造商品特征、读取整个图库、跳过 revision 或直接执行。
8. 一个成功样例和一个冲突/过期样例。

目录和 frontmatter 遵循 Agent Skills 格式：每个 Skill 是包含 `SKILL.md` 的目录，`name` 与父目录相同，使用 1--64 个小写字母、数字和单连字符；`description` 必须同时说明能力和适用任务，长度不超过 1024 个字符。`license`、`compatibility` 和 `metadata` 可按需要增加，但不能承载当前业务事实或权限。标准中的实验性 `allowed-tools` 不授予 ProductFlow 权限，实际可用 Tool 仍由 adapter 的显式 allowlist 和后端校验决定。

`SKILL.md` 正文是激活时加载的工作指引，建议控制在 500 行以内。较长的稳定参考资料放入 `references/` 并在任务需要时读取；不要把每个 backend endpoint 或完整 JSON Schema 原样复制进 Skill。

Skill 不应包含以下内容：

- 当前商品 ID、资产列表、Workflow revision、文件路径或 provider secret。
- 需要经常变化的业务枚举、完整 JSON Schema 或数据库字段清单。
- “调用某个接口即可成功”这类绕过后端校验的指令。
- 把自然语言输出当作已经完成业务动作的描述。

### 5.3 Skill 版本和测试

- Skill 文件随代码提交，使用小而清晰的目录和稳定名称。
- 每次 Skill 变化都进入 catalog hash；启动提示只包含 Skill metadata，实际正文通过 `load_productflow_skill` 进入当前 Pi Turn。
- Skill 变更至少补一个离线场景测试：工具顺序、追问条件、禁止动作或停止条件中至少有一项可以被断言。
- Skill 只提供行为指导；工具 schema、后端校验和用户确认仍是强制边界。

main 当前目录：

```text
agent-service/
  .pi/skills/
    productflow-core/SKILL.md
    product-intake/SKILL.md
    media-library-organization/SKILL.md
    workflow-run-request/SKILL.md
  src/skills.ts
  src/tools.ts
  src/pi-runtime.ts
```

实际部署可以把 Skills 打包进 Agent service 镜像；部署目录变化不能改变 Skill 的版本、hash 和测试方式。

## 6. Context 设计

### 6.1 三层 Context

#### 固定规则

固定规则进入 ProductFlow core Skill 或稳定 system prompt：

- ProductFlow 是单管理员、单商家 workspace。
- 真实商品参考图优先于 Agent 猜测。
- Agent 只能提出待确认 Draft，不能假装已经物化。
- WorkflowRun、媒体 bytes、Draft revision 和确认状态由 ProductFlow 持有。

#### 每轮动态 Context

每次 Turn 启动前由 FastAPI/application 生成有界结构化 Context，至少包括：

```json
{
  "schema_version": 1,
  "scope": {
    "scope_type": "global|product_workflow",
    "session_id": "...",
    "conversation_id": "...",
    "task_id": "...",
    "product_id": "...",
    "workflow_draft_id": "..."
  },
  "task": {
    "goal": "immutable task goal",
    "status": "running",
    "waiting_reason": null
  },
  "session_summary": "bounded summary",
  "page": {
    "route": "/products/p1/workflows/w1/library",
    "page_type": "workflow_sub_library",
    "selected_asset_ids": ["asset-1"],
    "visible_asset_ids": ["asset-1", "asset-2"],
    "filters": {"folder": "scene", "tag": "hero"},
    "workflow_revision": 42,
    "library_revision": 108
  },
  "business_summary": {
    "current_draft_version": 3,
    "selected_image_types": ["hero"],
    "planned_quantities": {"hero": 2}
  }
}
```

Task goal 是业务目标，不能被 route 或页面选区覆盖。Page context 只帮助解释“当前页面上的这些对象”，不授予权限，也不改变 Task scope。

#### 副作用前 Fresh Observation

页面快照和上一轮摘要不能作为写操作的最终事实。提交 Draft、组织素材 Draft 或 Run request 前，Tool 和 FastAPI application 必须重新读取：

- 对象是否仍属于当前 scope。
- 当前 revision 是否仍等于 expected revision。
- 引用的资产、工作流、Draft 是否仍存在并处于可用状态。
- 用户选项、当前业务状态和队列约束是否发生变化。

过期时返回结构化 conflict，让 Agent 解释并重新计算；不能用旧 Context 强行覆盖新状态。

### 6.2 Context 注入位置

- 固定 ProductFlow 规则进入稳定 system prompt/Skill。
- Task goal 进入当前 Agent session 的固定 scope context。
- Session summary、PageContextSnapshot 和动态 business summary 在每轮启动前注入，优先使用 Pi Extension 的 per-turn system prompt hook。
- 用户原始消息保持独立，不把 Context 和用户原话拼成无法区分的一段自然语言。
- 如果使用的 Pi 版本无法安全修改 per-turn system prompt，adapter 才使用带 schema/version 标记的独立 context message，并在测试中确认该消息不会覆盖 Task goal。

### 6.3 Context 限制

- Context 只传 ID、名称、状态、revision、摘要和用户明确选择的有限对象。
- 不把整个图库、全部历史 Turn、storage path、data URL、provider request、credential 或媒体 bytes 塞进 Context。
- 图片内容通过明确的 inspect Tool 按需获取，单次数量沿用 ProductFlow 现有上限。
- 动态 Context 建议以 64 KiB 作为初始软上限；超限时压缩摘要或要求 Tool 分页，不静默截断 ID、revision 或确认状态。
- 所有字段有 JSON Schema、长度上限和未知值语义；新增字段必须兼容旧 adapter，Skill 行为在兼容性验证通过后才可改变。

## 7. ProductFlow Tool 设计

### 7.1 Tool 分类

| 类别 | 作用 | 例子 | 是否产生业务副作用 |
|---|---|---|---|
| Read | 查询有界事实 | `get_product_workflow_context_v1`、`list_global_media_library_assets_v1` | 否 |
| Inspect | 检查明确选择的图片或对象 | `inspect_product_image_assets_v1`、`inspect_global_workflow_runs_v1` | 否 |
| Propose | 保存待审核 Draft revision | `propose_workflow_draft`、`propose_global_draft` | 只写 Draft 记录，不改正式状态 |
| Request | 创建待确认业务请求 | `request_workflow_run_v1` | 只写 pending request，不启动执行 |
| Confirm | 用户点击确认后的业务命令 | FastAPI confirmation route | 是；不注册给 Agent |

### 7.2 Tool 参数合同

涉及业务对象的 Tool 统一考虑以下字段：

- `scope`: conversation/task/product/workflow 的明确范围。
- `expected_revision`: 读取时观察到的 revision。
- `idempotency_key`: 同一次意图的稳定幂等键。
- `payload`: 版本化、canonical JSON，禁止额外未知字段或明确保留 unknown。
- `request_metadata`: Agent turn、skill hash、runtime version 等审计信息，不含 secret。

Tool 不接受 storage path、任意 URL、未验证的内部数据库 ID 集合或“帮我把所有东西都改掉”这类无界参数。列表和 inspect 必须分页或限制数量；批量 Draft 必须有对象数量、operation 数量和 payload 大小上限。

### 7.3 Tool 返回值

Tool 返回值分成三部分：

1. 给模型看的短文本：当前结果、下一步和冲突原因。
2. 给 adapter/审计看的结构化 details：对象 ID、revision、Draft revision、状态和 bounded counts。
3. 给 Web projection 的安全摘要：`kind`、`summary`、`status`、实际 `tool_name` 和白名单 `details`。details 只允许 Skill 名称、最多 12 KiB 的 Skill 正文摘要及截断标记、上下文区段与大小、问题选项、结果摘要、错误码、校验路径和可重试标记。

涉及外部副作用的 Tool 还必须写 `tool_effect_intent` 和 `tool_effect_result` checkpoint。`tool_effect_result.result` 只能是 `applied`、`failed` 或 `unknown`；reconciliation 的来源状态可以通过额外的 `reconciliation_state` 字段记录，但不能替代这三个正式结果类别。

完整原始响应、storage path、data URL、secret、provider payload 和未清洗的异常堆栈不能进入模型历史或 Web projection。

### 7.4 高层 Tool 优先

Tool 按用户意图设计，避免把数据库 CRUD 原样暴露给模型：

- 使用 `propose_global_draft` 表达一次有范围的整理方案，后端负责重复操作、引用保护、revision 和原子应用。
- 使用 `propose_workflow_draft` 表达完整 WorkflowDraft，后端负责 schema、真实资产 ID、商品事实和当前 Draft revision。
- 使用 `request_workflow_run_v1` 表达一次明确工作流执行请求，后端负责 workflow revision、队列、幂等和用户确认。
- 只有确实需要模型分步探索时才增加低层 Read/Inspect Tool；不要为了复刻所有页面按钮而创建同等数量的 Tool。

## 8. Question、Draft 与事件翻译

### Question

- Agent 需要用户补充信息时调用 ProductFlow `ask_user` 能力或等价的 adapter 机制。
- FastAPI 持久化 Question，Web 显示选项/文本输入。
- 用户回答后继续同一个 ProductFlow Conversation 和 Pi session；答案只能作为新 continuation Turn 的输入，不能修改原始 Task goal 或历史事实。
- ProductFlow 在原始 Turn projection 中保存问题答案和 continuation Turn ID。重复回答复用稳定 idempotency key；Agent service 可用时取消旧 waiter，进程不可用时由 queued continuation 和 lease recovery 接管。

### Draft

- `propose_*_draft_v1` 的 JSON Schema 是最终结构化边界，不能依赖助手 prose 解析。
- adapter 只有在 ProductFlow 后端验证通过后才把 artifact 标记为可审阅。
- 验证失败时把有限、可修复的错误返回给 Agent，允许同一 Turn 修订；不得保存无效 Draft 作为正式 revision。
- 用户确认针对明确 Draft revision；确认成功后仍由现有 ProductFlow materialization/application use case 完成。

### Event

Pi event -> ProductFlow event 的映射必须集中在 adapter：

| Pi 事件类别 | ProductFlow 投影 |
|---|---|
| assistant text delta | `text.delta` |
| custom tool start/update/end | `tool.step`，只保留有界类别、摘要和状态 |
| ask user | `question.required` |
| validated proposal | artifact + Draft revision reference |
| abort/cancel/error | Turn terminal status + bounded error |
| session/runtime metadata | server-side diagnostics，不直接给 Web |

前端继续消费 ProductFlow 自有事件，不对 Pi event type 建立长期依赖。

## 9. Durability 和 Task 范围

### 9.1 交互式 Turn

第一阶段主线必须支持：

- Turn 创建和幂等。
- 文本流、工具步骤、Question、Draft artifact 和终态。
- 用户取消、断线重连和当前 session 恢复。
- ProductFlow 侧的 scope、Task、PageContextSnapshot 和 projection 一致性。

### 9.2 后台 Task

Pi 的 session file、compaction 和 resume API 不能直接证明以下语义：

- 进程在工具副作用之后崩溃，重启后不会重复副作用。
- 多个 Agent service 实例不会重复 claim 同一个 Turn。
- 工具调用和业务事务可以进行效果对账。
- 未知 provider 结果能保持 `unknown`，而不是被误标为失败或成功。

因此，Pi 主线把长期后台 Task 作为独立阶段。实现该阶段前需要额外的 coordinator、Turn claim、effect journal 或等价的可审计机制；具体实现可来自 ProductFlow application 或后续自研 runtime，但不能把 Pi 默认 session persistence 当作替代品。

`exp` 分支继续验证现有 harness 的 durable runtime。实验结果通过共享 contract/eval 反馈给 main，不把实验 runtime 内部 API 直接带回 ProductFlow 业务层。

## 10. 已交付阶段与剩余 gate

阶段 0 至 4 与阶段 6 已在 main 落地：HTTP/SSE 合同、Pi SDK adapter、只读 Tool、Question/Draft、待确认 WorkflowRun request，以及 `exp` 实验线隔离。当前结果以 `agent-service/`、`application/agent/` 和对应测试为准，不在本文重复阶段日志。

阶段 5（后台 Task 和恢复）以及真实 provider、PostgreSQL/Redis、浏览器、SSE 断线恢复和全量效果对账仍待补，见 `docs/rollout/pi-agent-durability.md` 与 `docs/ROADMAP.md`。通过 live recovery gate 前，不扩大后台 Task 的默认能力，也不把 Pi session persistence 当作 durable 证明。

## 11. 实施准则

1. **业务合同优先。** 沿 `input -> wire schema -> application -> persistence/effect -> response -> Web projection` 追踪后，决定 Tool 或 Context 的形状。
2. **Skill 不放权限。** 权限、scope、revision、幂等和确认必须在 Tool/backend 重复验证。
3. **动态事实不进 Skill。** 商品、资产、页面和 revision 随 Turn 生成；Skill 文件只写长期稳定的流程规则。
4. **Tool 面向意图。** 高层业务 Tool 优先，避免把内部 CRUD 和数据库字段直接暴露给模型。
5. **读取有界。** 列表分页，inspect 指定对象，媒体内容按需，完整历史和图库不一次注入。
6. **写操作可审阅。** Draft proposal 和 pending request 可以由 Agent 创建，正式 materialization、组织应用和执行由用户确认/API 完成。
7. **Fresh Observation 必须在后端完成。** 页面快照只提供意图线索，不能成为写入依据。
8. **事件由 adapter 翻译。** Web 只依赖 ProductFlow wire contract，不依赖 Pi 内部 event type。
9. **运行时版本可观测。** 每个 Turn 记录 runtime、Pi version、Skill hash、Tool contract version、model/provider 和 Context schema version，不记录 secret。
10. **共享测试，隔离实现。** main 和 exp 使用相同 golden input/expected business result，runtime 内部存储和恢复测试分开。
11. **不提前承诺 durable。** 交互式恢复、后台恢复、效果重放和多实例调度分别验收。
12. **先保证用户可接管。** Agent 失败时，用户仍能在工作流画布、素材库和运行面板完成同一业务目标。

## 12. 质量与验收

### 必须为零的安全/一致性问题

- 未确认 Draft 产生正式 Workflow、图库组织或 WorkflowRun 副作用。
- Agent 读取或修改超出 Conversation/Task scope 的对象。
- 过期 revision 被静默覆盖。
- 同一个 idempotency key 产生重复业务副作用。
- Pi 默认 filesystem/process tool 出现在 ProductFlow tool allowlist。
- Web 通过 Agent runtime 事件重建完整 transcript 或敏感原始 payload。

### 需要持续比较的质量指标

使用同一个 provider、model、输入、参考图和 Context，对 Pi 与 exp harness 运行同一组样本，记录：

- 追问是否只覆盖会影响结果的缺失事实。
- 商品事实和用户确认选择是否被正确保留。
- 参考图绑定、图片类型、数量和 WorkflowDraft schema 是否正确。
- Skill/tool 选择路径、工具调用次数和无效调用比例。
- 用户修改 Draft 的次数、确认前往返轮数和完成耗时。
- provider 错误、Tool 错误、取消、断线和恢复后的终态是否可解释。
- token、延迟和 provider 成本。

安全指标以零错误为硬门槛；质量指标使用当前 harness 基线做相对比较，不因为 Pi 底座更成熟就自动判定 ProductFlow 质量达标。

### 最小测试矩阵

| 场景 | 必须验证 |
|---|---|
| 商品创建 | 参考图、图片类型、数量、缺失事实、WorkflowDraft proposal |
| 全局素材整理 | 有界列表、选中图片 inspect、整理 Draft、确认前无副作用 |
| 工作流修改 | 明确 product/workflow scope、Draft revision、旧 revision 冲突 |
| WorkflowRun 请求 | pending request、用户确认、同一 application use case、取消/队列拒绝 |
| Session/Task | Task goal 不随路由变化、Context 隔离、多个 Task 不串话 |
| runtime failure | provider timeout、tool error、abort、SSE reconnect、unknown |
| security | 无 OS tool、无 secret 泄漏、无 storage path/data URL、无跨 scope 读取 |
| restart/recovery | interactive resume；后台 Task 只有在阶段 5 通过后才标记支持 |

## 13. 完成定义

Pi 迁移完成必须同时满足：

- `main` 使用 Pi adapter，且没有对 `third_party/agent-harness` 的运行时依赖。
- FastAPI/Web 外部合同保持兼容，或有明确版本迁移和回滚证据。
- ProductFlow 业务 authority、Draft confirmation、WorkflowRun、媒体 owner 和人工接管路径保持成立。
- Skills、Context、Tools 有目录、版本、schema、hash、测试和运行时观测。
- 最小测试矩阵通过，真实 provider 和真实依赖 gate 有结果。
- 对后台 Task 的支持范围写成已验证能力；未验证部分保持关闭或明确标记，不使用模糊的“支持恢复”描述。

## 14. 官方资料

- [Pi SDK](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/sdk.md)
- [Pi RPC mode](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/rpc.md)
- [Pi Extensions and custom tools](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/extensions.md)
- [Pi repository and permission/containerization notes](https://github.com/earendil-works/pi)
