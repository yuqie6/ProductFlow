# 任务：按原始附件补齐六类节点表单合同

状态：完成
类型：实现
认领者：主代理-node-handoff-0906-0014
认领于：2026-09-06T00:14:28+08:00
完成后可拆：无

## 问题来源

用户持续目标指定附件 `5f9ff0a0-5c48-40da-8ce6-d92144477ccf/pasted-text-1.txt`。逐项验收发现 [上一轮交付](node-detail-redesign.md) 的局部验证未覆盖附件全部要求。本任务继续原始目标，不改写历史交付证据。本主代理确认认领及节点模块独占；不创建认领提交。

## 做成什么样

- 共用名称/保存状态，输入、编辑与结果分区；未设置、禁止与继承不混淆。
- 商品事实只在确认后用于生成，跨商品编辑明确来源；待确认项独立。
- 参考图片支持预览、绑定、用途、简短多行说明、可跳转下游；说明进入执行，不虚构参考强度。
- 创作要求默认四项，重点/必需/禁止条目可增删；fact_gaps 退出编辑表单，只读问题与商品资料入口分离。
- 系列风格提供连接参考缩略图、风格文本及带用途的可选配色，不承载共享禁忌。
- 画面方案四章节；视角/占比、装饰/氛围按需展开；保真独立摘要及具体保留要求。共享要求/系列风格显示来源摘要，不复制编辑。AI 候选可按章节生成及采用。
- 图片生成结果、历史、预览下载优先；逐项覆盖与整组文字覆盖均可恢复。补充要求不能取消上游约束；本图风格明确显示被替代值。
- 生成与高级设置按实际模型能力出现；导出单独操作。每个开放字段均有执行和影响范围证据。

## 前置与范围

- 前置 `44078831`、`60c8095e`、`66411957`。原始附件及用户侧栏/prompt 要求是验收依据。
- 独占 `go/internal/graph/`、必要 `providers/` 和 `product/` 接线、`go/prompts/`，Web canvas、必要 lib/共用生成设置与创建表单，相关测试和活文档。
- 本轮必要接线：`go/cmd/productflow-api/main.go` 注入只读生成选项查询；`product/gallery_query.go` 与 `product/http.go` 给既有图库分页增加节点筛选，游标绑定该筛选。它们与其它在途任务写入及冻结资源无交集。
- 不碰 Agent 性能/recovery、评测池与冻结题库，不改 Skill/harness。共享文档只提交本任务 hunk。
- 复用本会话隔离栈 `29402/29403`、Redis `16509`、数据库 `productflow_node_detail_0905`；不暂停共享进程，不修改共享 provider，不进行收费真实模型运行。

## 怎么验收

- 对上述每项记录实现锚点和当前证据，未验证不视为完成；保留附件全部范围。
- 按实际改动执行 Go 带 PG 回归、Web 全量测试/lint/build、合同及 docs-check。
- 隔离浏览器检查变化后的真实控件、候选/继承/导出行为，桌面/窄桌面/移动端及相关语言主题。
- 自审、选择性提交、任务归档完成后才声明交付；真实模型质量与合同正确性分别报告。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：主代理-node-handoff-0906-0014。
- 2026-09-06 用户指定接手：承接原认领者的节点模块未提交 diff、现有截图与隔离栈（29402/29403、Redis 16509、productflow_node_detail_0905）。图片质量任务使用独立固定 checkout 和资源；Agent Skill 占用继续保留，不进入其范围。既有测试记录保留为历史证据，后续按当前相关输入决定复用或重验。
- 交接：下方保留接手前证据，本轮实现与最终验收单列。图观察评测快照由后续任务使用固定提交刷新。

## 用户追加：合同变更跨消费者回归

2026-09-06 用户在合同审查后要求继续跟进。以下保留合同回归会话的原始诊断；本认领者已接收，运行拒绝及当前创建入口回归见本轮验收。

- 工作流 `d623dc70-1f87-4ade-ab8a-2fbb0bb9f8bc`（商品 `6adf2ccc-b4c8-43fc-865e-0af1ddd7d94a`）真实浏览器点击「运行此场景」，请求 `scope=selection`，图片节点为 `de49bb89-29d3-4ba2-bee6-d9c8ffe84f9c`、`a910c82e-d698-4c79-9330-7655d60d0356`，返回 400：`目标节点不可运行: generation_spec包含未登记字段: text_language, text_policy`。创作要求还存有旧 `design_goals`；旧数据来源尚未追溯，不据此断言当前创建入口仍写旧格式。
- 同图「运行整张图」返回 201，运行 `ef7be868-d205-402b-af3e-f82d2f975b06` 最终 succeeded，但仅有两个 frozen 文稿节点且均 skipped，没有图片生成。`select.go` 全图选点过滤配置无效节点；preview 将配置无效也标成「缺少必连输入」。需明确部分节点不可运行时的提交/反馈合同，覆盖 graph、selection、node、to_node 及 Agent 运行消费者，不能用空出图的成功掩盖配置错误。
- 交付前验证所有当前写入入口使用新合同：直接创建、Agent intake、Graph Command、画布添加场景及配方；验证文字设置继承/覆盖、provider 输入、digest 与响应投影。按仓库规则不增加旧数据兼容读取或迁移命令，不擅自清空用户数据库。
- 固定合同提交后交接 [图观察权威任务](eval-graph-observation-authority.md)，通过本归档的 Git 历史定位交付版本，并提供下方变更字段和回归结果，由该任务刷新当前评测观察并验证拒绝语义；不在本任务改冻结题库、grader 或 Skill。`agent-service-check-contracts` 通过不证明 Catalog/intake 评测快照同步。
- 已有本次诊断证据：`TestEvalObservationFixtures` 在 Catalog 比较阶段失败；文字默认值、旧文字字段拒绝、文字设置 digest、有效覆盖 digest 四个无数据库 Go 回归通过。浏览器截图 `/tmp/productflow-run-scene-400.png` 为本机临时证据；未执行收费模型评测。上述结果不替代本任务最终固定合同的验收。

## 接手前证据

- 上一目标轮分类：进展，交付提交及浏览器证据成立，但附件全项验收缺失。
- 当前已确认差距：fact_gaps 仍可编辑、共享约束重复编辑、视角/占比默认展开。
- 模型能力、单图风格覆盖展示、按章节生成候选、结果历史入口待核。
- 表单补齐：创作要求短条目增删、只读问题与来源跳转；方案视角/占比折叠、保真独立、共享要求只读。参考说明改为多行。
- 本图风格逐字段覆盖/恢复，投影复用执行侧系列内容；明确清空配色不再被保存层压为继承。Go graph 全包带 PG 通过（80.489s）；Web 92 文件/656 测试通过；候选与节点详情组合浏览器 12/12 通过。
- 用户追加指出多个节点内部样式不协调，当前集中调整六类详情的分组、间距、空继承行、配色排布、商品短字段和参考图操作。仅改呈现，不改存储/运行语义；最新截图与回归仍在核验，不据此关闭整个附件任务。
- 原始附件仍缺模型能力过滤、按章节生成候选、结果历史等全项验收；本任务保持认领。
- 最新样式回归：六类详情四语、1440 亮色/390 暗色截图，1024 交互；`node-detail-redesign.spec.ts` 4/4 通过（1.7m）。最终 Web 656 单测、lint、`just web-build`（含预算）通过。截图已审看并保留于 `/tmp/productflow-node-detail-redesign/browser-style-2356`。一次外部 SIGTERM 终止 lint/build 和隔离服务，核实进程退出后已恢复自己的栈并重新验证。
- 审核：主代理自审；交付定位：随本任务提交。

## 本轮实现与验收

| 附件合同 | 实现与证据 |
|---|---|
| 六类详情名称、自动保存、分组 | `GraphNodeInspector.tsx`、`NodeDetailFields.tsx`、`nodeDetailForm.css`。六类详情在中英日越、1440 亮色和 390 暗色检查，1024 交互；新章节操作使用既有 flush 后提交路径。 |
| 商品事实确认与来源 | 商品资料仍由 `ProductSourceEditor` 和 product facts 版本拥有；浏览器验证确认后落库、引用其他商品时的来源身份，不把未确认事实写成可生成事实。 |
| 参考图用途、说明和连接 | Catalog 的 label 使用多行输入，详情保留预览、绑定/解绑和下游跳转；`compileReference` 将 role/label 送进 provider/digest，graph 和 adapter 回归通过。 |
| 创作要求四项与待确认问题 | goal、key_messages、required_elements、prohibitions 保留编辑，短条目增删；fact_gaps 保留数据与 provider 所有权，表单只读并跳转商品资料。`TestNodeDetailReadOnlyQuestionsAndReferenceNote` 与浏览器通过。 |
| 系列风格与角色配色 | 风格文本、角色/值/标签配色，连接参考缩略图；不在系列表单重复编辑共享约束。配色浏览器编辑、持久化及四语布局通过。 |
| 画面方案四章节 | 场景/构图、重点内容、文字与目标分组；视角/占比、装饰/氛围折叠；商品保真独立，共享要求和风格显示来源摘要。 |
| 按章生成和明确采用 | `document_section` 贯穿提交、预览、队列去重、重试及 PromptRequest。章节按嵌套叶字段划分；落候选前限制修改范围，防止供应商越界输出。`TestSectionCandidateReachesProviderAndApplyAllPreservesOtherSections` 与浏览器证明候选未采用不改正文、整份采用章节候选也保留其他章节。 |
| 单图继承、明确清空及范围 | 本图内容逐项、文字整组、风格/配色分别覆盖和恢复；`InheritedVisual` 使用执行侧同一系列输入。空配色保留为空。Go digest/输入投影与浏览器刷新、上游修改、独立恢复通过。 |
| 补充要求约束 | `CompileImageModelPrompt` 明确补充要求不得取消事实、必需内容、禁忌及保真要求；测试检查冲突补充与上游约束一起送入模型并标明优先级。未将此测试表述为真实模型服从率。 |
| 实际生成能力 | 新只读 `/api/v3/image-generation-options` 根据当前 ImageClient 的真实适配器与白名单返回可消费选项；无效控制隐藏，存储配置不静默改写。OpenAI Images/Responses、Gemini、mock 映射回归通过；浏览器校验受限比例、质量高级项、无效参考保真隐藏。没有调用供应商探测接口。 |
| 结果历史、下载、独立导出 | 既有图库分页增加 node_id 筛选并绑定游标，只列节点原始生图。`NodeImageHistory` 提供预览、下载、分页。导出规格独立折叠，沿既有交付图操作执行；浏览器断言生成交付图前后生图运行列表不变。 |
| 非法配置拒绝运行 | `select.go` 对范围内节点及供给输入的处理祖先校验配置，预览显示真实原因。保留整图略过缺边草稿的既有合同。HTTP 四范围遇退役文字字段均返回 400 且不写 run；Agent prepare 返回既有 409，确认提交复用同一 graph 边界。冻结祖先非法、无关草稿不阻塞局部运行均有回归。 |
| 当前写入入口 | 直接创建经 `BuildDirectCreateTemplate`，Agent intake 经 `ExpandBirth`，画布 `shotChangeSet.ts` 明确写方案 text_settings，Graph Command/配方经 `NormalizeNodeConfig` 拒绝旧字段。新增真实 Agent intake 展开后逐节点校验及文字默认值回归；graph、product、recipe 与 Agent 相关回归通过。旧数据库未被清空或迁移。 |

最终验证：

- `bash scripts/with_dev_env.sh go test -C go ./internal/graph ./internal/providers ./internal/providers/adapt ./internal/product ./internal/recipe -p 1` 通过；graph 77.650s，providers 0.105s，adapt 0.012s，product 2.629s，recipe 2.606s。使用 testdb 派生的包隔离 PostgreSQL。
- `bash scripts/with_dev_env.sh go test -C go ./internal/agent -run 'Test(AgentFinalizeIntake|BrowserWorkspaceIntake|ConfirmWorkflowRunRequest|CreateWorkflowRunRequest|CreateGlobalWorkflowRunRequest|PrepareWorkflow|WorkflowRunRequest)' -count=1 -p 1` 通过（2.302s）；未修改 Agent 生产代码、Skill 或评测输入。
- Web 全量 93 文件 / 663 测试、全量 lint、`just web-build`（应用/Node/E2E 类型检查、Vite 与包体预算）通过。运行预览 TypeScript 补齐字段后另跑 app 类型检查和 6 条 preview 单测通过。
- `just agent-service-check-contracts` 通过，仅证明其生成物一致性；不包含 Catalog/intake 评测快照。
- 隔离 Playwright `node-detail-redesign.spec.ts` + `canvas-document-mock.spec.ts` 14/14 通过（3.1m）。命令启用 `PRODUCTFLOW_RUN_NODE_DETAIL=1`、`PRODUCTFLOW_RUN_CANVAS_DOCUMENT=1`，Web `29403`，API `29402`，Redis `16509`，数据库 `productflow_node_detail_0905`；mock 绑定在测试后恢复。
- 运行拒绝修复后重建自己的 API/worker/dispatcher，章节生成/历史/导出与稀疏继承两条浏览器复验通过（38.8s），目录 `/tmp/productflow-node-detail-redesign/browser-run-contract-0906`。最终选点及章节校验聚焦回归通过（1.146s）。
- 截图目录 `/tmp/productflow-node-detail-redesign/browser-final-0906`；已检查桌面/移动端章节表单、配色、适配器选项与历史结果，图片 naturalWidth 非零。mock 灰色图片只证明资产链路。
- `git diff --check`、`git diff --cached --check` 与归档后的 `just docs-check` 通过。隔离预览栈留供用户查看，不停止其他任务进程；本任务临时构建和截图均在 `/tmp`，不提交。

审核者：主代理-node-handoff-0906-0014（自审）。结论：附件节点交互与执行合同完成，跨消费者拒绝回归通过。未执行收费真实模型出图或质量评分，未更新旧评测批次。交付定位：随本任务提交。

## 固定合同交接

图观察任务需从本归档所属提交重新采集当前 Catalog/intake，重点核对 `creative_brief.goal`、`image_prompt.text_settings`、`image_generation.text_override`、生成参数退役文字字段的拒绝、章节候选嵌套字段、运行预览 `document_section` 及配置错误反馈。本任务的接口/确定性验证不解除评测快照漂移，也不授权在新固定观察完成前启动付费全量采样。
