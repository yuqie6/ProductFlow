# ProductFlow 文档地图

本文定义仓库文档的职责。遇到重复或冲突时，按下表找到唯一所有者，并删除其他文档中的重复实现细节。

| 文档 | 唯一职责 | 不应包含 |
|---|---|---|
| `../CONTEXT.md` | 领域词汇、权威边界、跨版本稳定不变量 | 页面操作说明、文件清单、未来计划 |
| `PRD.md` | 当前用户能力、产品合同、非目标、成功标准 | Python/TypeScript 模块所有权、部署命令 |
| `ARCHITECTURE.md` | 当前运行单元、代码所有权、数据流、实现和测试锚点 | 未实现设想、逐步用户教程、lease/SSE 运行时小说 |
| `USER_GUIDE.md` | 用户可以执行的页面操作和故障提示；产品内 `/help` 的权威正文 | 内部事务、模型表、未来计划 |
| `ROADMAP.md` | 尚未实现或尚未验证的产品与工程方向；schema-v3 目标合同的唯一默认入口 | 已交付能力清单、当前架构复述 |
| `specs/` | scoped PRD 与实现设计；状态必须标注 `文档状态：Draft` 或 `文档状态：Approved`；Approved 必须给出批准 issue、decision 或任务 evidence | 冒充当前运行事实、部署证据、会频繁变化的任务状态 |
| `adr/` | 已接受决策的背景、选择、后果 | 会频繁随重构变化的文件列表 |
| `rollout/` | 某次迁移或发布的已完成项、缺失证据和停止条件 | 长期工程规则、已完成的一次性审计 |
| `operations/` | 操作者命令、前置条件、回滚和证据处理 | 产品愿景、普通开发流程 |
| `archive/` | 已退出默认阅读路径的历史设计、过程记录和本地 method-pack 残留 | 当前实现、当前决策、部署操作依据 |
| `audits/` | 某次治理、回退或知识迁移的执行记录，不是长期规范 | 当前产品合同、当前架构复述 |
| `agents/` | 仓库协作元数据：issue tracker、triage、domain 阅读约定 | 产品能力或运行时实现 |
| `assets/` | 品牌图和静态文档资源 | 产品说明或架构正文 |
| `../AGENTS.md` | 全仓开发方法和验证要求 | 领域需求复述 |
| `../backend/AGENTS.md` | 后端可执行工程约束 | 产品路线图 |
| `../web/AGENTS.md` | 前端可执行工程约束 | 后端内部实现细节 |

英文 `*.en.md` 是对应中文产品/用户文档的翻译，不是独立规范。修改当前能力、架构或路线图时，同一提交更新对应翻译。

`web/src/pages/HelpPage.tsx` 是 `USER_GUIDE.md` 的产品内投影，不是第二份用户操作规范。用户操作变化必须同一提交更新指南和 HelpPage。

## 阅读路径

开发前按改动类型读取最小集合。

### 默认阅读

普通功能开发只需要：`CONTEXT.md`、本文件、对应 package `AGENTS.md`，以及相关的 `PRD.md` / `ARCHITECTURE.md` 段落。

只有改动触及对应边界时，才继续读取 ADR、spec、rollout 或 operations。`archive/` 和 `audits/` 默认跳过。

- 当前产品语义：`CONTEXT.md`、`PRD.md`、相关已落地 ADR（0001–0007）。
- 后端：`backend/AGENTS.md`、`ARCHITECTURE.md` 中对应所有权、相关代码和测试。
- 前端：`web/AGENTS.md`、`ARCHITECTURE.md` 中对应所有权、相关代码和测试。
- 跨层合同：以上两份 package `AGENTS.md`，沿 wire DTO、应用用例、持久化、API client 和 UI projection 验证完整链路。
- 用户操作：`USER_GUIDE.md`；产品内文案同步 `web/src/pages/HelpPage.tsx`。

### 仅在触及对应边界时

- Agent service / Pi adapter：`adr/0007-pi-agent-runtime-boundary.md`、`specs/pi-agent-runtime-integration.md`，并回看 `ARCHITECTURE.md` 的当前实现段落。
- 全局素材未完成迁移证据：`rollout/media-library-transition.md`；产品剩余项见 `specs/media-library-prd.md`。
- 全局 Agent 产品边界：`specs/global-agent-human-workflow-design.md`。
- V1 切换：`rollout/workflow-v2-cutover.md` 与 `operations/legacy-v1-cutover.md`。

### 只从 ROADMAP 进入

schema-v3、自由画布和 Agent 图协作不是当前实现。不要把下列文档写进 CONTEXT、PRD 或 ARCHITECTURE 的当前事实段落：

- `ROADMAP.md` §2
- `adr/0008-free-canvas-agent-graph-authority.md`（Accepted，implementation pending）
- `specs/schema-v3-admission-slice.md`（Draft，批准前不进代码）
- `rollout/free-canvas-v3-interaction-parity.md`

已批准但未交付的其他变更同样先出现在 `ROADMAP.md` 和对应 spec，不得提前写成当前事实。

代码、测试、迁移和真实运行行为始终是当前实现证据。稳定文档中的实现敏感声明应指向当前代码所有者或测试；路径变化时同步更新文档。

运行 `just docs-check` 校验当前前端路由、核心 code owner 路径、spec 状态标注和仓库内 Markdown 链接。该检查覆盖可机械验证的漂移；产品语义仍需沿真实调用链和测试审阅。
