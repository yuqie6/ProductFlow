# 工作流体验组

本组负责商家把参考素材和出图要求变成可使用商品图片的操作体验：能表达和修改要求，明确控制运行范围，遇到失败可以修复，找到正确结果并交付或复用。关闭 Agent 后，已有手动工作流仍须可独立操作。

文稿不被覆盖是这条操作链的基础合同。它已有较完整的分层测试，但不能单独证明商家能顺畅完成工作，也不能代表本组全部职责已经完成。

本文件是本组唯一章程、合同与证据索引，保留原文件名以维持历史链接。产品能力以 [PRD](../PRD.md)、领域约束以 [CONTEXT](../../CONTEXT.md)、操作入口以 [USER_GUIDE](../USER_GUIDE.md) 为准；未实现方向归 [ROADMAP](../ROADMAP.md)。任务状态由 [Issue 看板](tasks/README.md) 及其任务文件维护。

## 组职责与交接

2026-09-07 正式版职责校准：本组负责 [总纲第 5 节](../ROADMAP.md#5-正常体验的最低要求) 的创建、成果浏览、修图、采用、交付和复用体验。[完整成果投影](tasks/archive/delivery-workbench-projection.md)、[采用/交付快照](tasks/archive/delivery-adoption-snapshot.md) 与 [品牌视觉复用](tasks/archive/brand-visual-reuse.md) 已交付。[默认入口走查](tasks/archive/workbench-default-entry-walkthrough.md) 建议暂不全局切到成果；[有产出时默认成果](tasks/archive/workbench-conditional-results-default.md) **已交付**（条件默认，完成 ≠ 全局一律成果）。基础配方 preview/原子确认、局部编辑和 ZIP 已有实现，不重复发布。

当前没有真实商户用户，内部走查只说明工程可用性。商家平台负责权限语义；本组按其固定合同提供清楚的商家上下文和角色操作入口。二维组合或品牌版本等跨层实现由协调者指定唯一主任务及排他范围，图片质量提供固定质量合同。

本组按商家结果承担必要的前后端完整修复，不按页面或代码目录切断责任。以下是调查与交付边界，不要求每次任务遍历全部模块。前端入口主要位于 `web/src/pages/workbench/`。

| 商家结果 | 本组责任 | 主要实现入口 |
|---|---|---|
| 开始制作 | 创建商品、输入要求、上传并使用参考；手动创建不依赖 Agent 自动执行 | `web/src/pages/product-create/`、`go/internal/product/` |
| 修改方案 | 节点与连接、场景组织、检查器保存、候选审阅、撤销重做；能识别实际生效的要求 | `GraphCanvasPanel.tsx`、`GraphNodeInspector.tsx`、`go/internal/graph/` |
| 控制生成与恢复 | 保存后运行、整图或选定范围、取消、修正后重试；区分旧失败与新结果 | `GraphRunsPanel.tsx`、`graphRunPreview.ts`、`go/internal/graph/runs.go` |
| 使用结果 | 预览、定位与选择资产、局部编辑入口、原图和交付图下载、交付包 | `ProductImageExplorer.tsx`、`DeliveryRenditionPanel.tsx`、`LocalImageEditController.tsx`、`go/internal/delivery/` |
| 复用工作 | 明确资产绑定、固定当前结果、配方预览与确认，视觉方案版本保存/选定/显式采用，避免带入错误来源或覆盖目标商品 | `graphAssetDrop.ts`、`recipeSave.ts`、`RecipeLibraryPanel.tsx`、`VisualReuseControls.tsx`、`go/internal/recipe/`、`go/internal/visualsystem/` |

- 图片质量组负责图片内容与视觉要求及其生成链优化；本组负责选择、修改、运行和取用是否符合用户意图。下载文件正确不等于图片质量合格。
- Agent 质量组负责模型理解、决策与工具行为；本组验证既有 Agent 写入与手动编辑、运行的交错合同，不以模型评分作为确定性操作修复的前置。
- 平台可靠性组负责执行基础设施、故障收敛与资源成本；本组负责相应用户状态与恢复入口。同一根因只指定一张主任务，必要跨层修复由该任务交付。
- 连续图片会话、全局素材组织等相邻能力不因共用资产就全部并入本组；按具体问题协调所有权。路线图中的新界面不自动成为本次实现范围。

## 验收怎样判断

本组同时关心操作正确性与完成工作的负担。前者核实实际写入和文件结果；后者观察是否需要反复找入口、重复输入、无效运行或猜测状态。目前已有证据主要覆盖正确性，尚无足够的商家任务耗时、导航成本和可理解性观察，不能宣称整体体验达标。

记录结论时分开写 **实现事实、证据范围、剩余未知**。缺少浏览器测试不直接判定功能缺失；历史通过不自动代表当前整树通过；测试数不作为组完成度。

| 证据 | 可以回答 | 不能替代 |
|---|---|---|
| 代码与调用链 | 入口是否接线、字段与决策点在哪里 | 实际运行成功、用户能否理解 |
| 确定性单元测试 | 选点、状态过滤、变更计划与局部边界 | HTTP、事务和浏览器完整链路 |
| HTTP / PostgreSQL 回归 | 校验、并发写入、持久化与返回投影 | 用户是否能从界面触达 |
| mock 浏览器与业务状态 | 真实点击、保存与提交、资产身份、下载字节 | 真实供应商质量、全部设备和故障条件 |
| 真实 provider 操作链 | 当前环境下外部调用与实际结果可用 | 全面稳定性、视觉质量合格、商家效率 |
| 明确任务的用户操作观察 | 找入口、理解状态、修复和取用的成本 | 数据一致性和所有并发边界 |

验证强度随风险扩大。纯选点逻辑就近回归；跨层变更追踪输入、wire、用例、持久化或外部效果、响应和前端投影。涉及真实界面的验收须点击真实入口并检查业务结果，截图或成功提示不足以签收。

## 业务合同

### 编辑与运行

- AR-01-A：真实编辑基线沿 autosave → Inspector → Surface → `commitNode` → Graph Command 传递。服务端可按历史安全 rebase 仅兄弟节点的配置变化；客户端不能用最新 revision 掩盖过期编辑。
- AR-01-B：同节点过期保存仍返回 409；保留草稿、显示冲突、停止自动重放，不以当前值相等绕过历史裁定。
- AR-01-C：保存串行，较早响应不能清掉保存期间的新输入；运行前 flush 失败就不提交 run。
- AR-01-D：运行中仍可编辑；验证必须在 run 为 queued 或 running 时落地保存，结束后保持用户稿。
- 运行范围符合按钮含义。“运行此场景”由 `shotRunRequest` 提交一次 selection，选择该组生图节点；分组没有独立运行引擎。检查器节点、运行到这里和工具栏整图保留各自 scope。
- 修正后重试使用当前图创建新 run，保留旧 run 历史；“仅重试失败节点”不得包含成功、unknown、cancelled、skipped 或仍在执行的节点。取消不能回滚用户已保存的文稿。

AR-01 已交付于 [canvas-inspector-midrun](tasks/archive/canvas-inspector-midrun.md)。前端写入链见 `ProductWorkbenchSurface.tsx` 与 `GraphCanvasPanel.tsx`，服务端裁定见 `go/internal/graph/`。

### 不变量

O1-O7 保留历史引用。业务裁判是用户文稿与明确采用意图；数据库字段及搜索预算是当前实现与验证方法，不能反过来定义正确行为。

| ID | 合同 | 当前证据锚点 |
|---|---|---|
| O1 | 无文稿动作、快照 origin 为 seed，且 live 未分叉时，符合发布条件的首次 cook 可采用生成文稿 | `TestGeneratedContentNodeRemainsReadyAfterAdopt`、`execute_node.go` |
| O2 | live 相对运行快照分叉后，生成不得覆盖 live；仍满足发布条件的结果保留为待审候选。已取消或失去发布资格的结果另受运行 fencing 约束 | `TestAdoptSkipsOverwriteWhenUserEditsDuringRun`、`persistContentArtifact` |
| O3 | authored、generated、collaborative 文稿不被普通无 force 整图运行覆盖 | `TestGraphRunAfterAuthoredLayoutEditSkipsPromptProvider`、C2 run_graph |
| O4 | complete / rewrite / replace 使用 node scope 与 force；生成先进入候选，采用前 live 不变；graph + force 被拒绝 | `TestForceRewritePromptDoesNotChangeLiveUntilApply`、`TestSubmitGraphRunRejectsForce` |
| O5 | 文稿基线哈希或上游输入 digest 变化使候选过期；采用请求还须匹配当前 graph revision 和候选 artifact。无关 revision 变化本身不等于候选内容过期，过期请求仍须拒绝 | `document_candidate.go` 的 `loadDocumentCandidate`、`service.go` 的 `ApplyDocumentCandidate`、C2 stale / author → apply |
| O6 | 按 section 采用只改所选业务 section | `TestApplyDocumentSectionsOnlyReplacesSelectedBusinessSection`、C2 apply_objective、C4 |
| O7 | 同节点丢失更新必须 409；仅兄弟节点配置写入的过期保存可按历史安全 rebase | `TestChangeSetStaleRevisionConflict`、`TestWriteTxRebasesStaleNodeConfigWhenSiblingChanged`、`TestWriteTxStaleNodeConfigConflictsWhenSameNodeChanged` |

当前文稿候选使用 `pending_candidate_artifact_id`，采用状态可由 node-run disposition 观察；`persistContentArtifact` 清除文稿节点的 `current_artifact_id`。这些是现场代码事实，历史 ADR 不作为现行存储或采用合同的依据。

### 结果与复用

- 预览、绑定、固定结果和下载必须指向明确资产；后续生成不能悄悄改变已固定资产身份。
- 交付图按保存的 DeliverySpec 从来源资产确定性生成，保留谱系，不替换原图或重新跑图。验收核实际尺寸、格式、字节与 manifest，不能只核预设按钮名字。当前预设按规格匹配，未独立持久化所选 preset key。
- 配方预览与取消不写目标图；确认后的结构和绑定遵守 Recipe 合同。片段复用不携带原商品身份及生成结果；完整配方应用于已有图应明确冲突。
- 局部编辑：检查器对有当前出图的生图节点可画选区并提交；新资产 `origin_type=local_edit` 且 `parent_asset_id` 指向源图；失败或只留图库不改节点当前资产；采用后可撤销。mock 绑定可完成该合同。证据见 [canvas-local-edit-flow](tasks/archive/canvas-local-edit-flow.md)。OpenAI 仍须档案 `image_mask_edit`；Gemini 仍不支持。不是第七类节点。

## 当前证据与未知

2026-09-05 核对当前实现并重述本组结论。初始核查基线与详情留在 [canvas-workflow-coverage](tasks/archive/canvas-workflow-coverage.md)，不把历史基线扩展为当前整树结论。

| 操作链 | 已有实现与证据 | 验收边界与未知 |
|---|---|---|
| 手动创建 → 整图 → 图库 | `product/http_test.go`、C5 `direct-create-full-graph.spec.ts` | 存在既有真实 provider 证据；本次重述未复跑 C5 |
| 图结构编辑与撤销 | `workbench-v3-actions.spec.ts` 核添加、复制、分组、绑定、连线与 undo/redo 的持久结果；[资产与配方验收](tasks/archive/canvas-asset-recipe-proof.md) 固定构建复跑 78 passed | 1440/1024/390 各明暗模式；修正测试的 SVG 外接矩形中心误点，不代表所有图规模和交互组合已验 |
| 保存、候选与并发运行 | AR-01、O1-O7、C0-C4/C6 的已归档交付 | 覆盖具体文稿权威组合，未证明全部工作台可用性 |
| 场景运行与失败修复 | [canvas-run-recovery-proof](tasks/archive/canvas-run-recovery-proof.md)：隔离 mock Chromium 4 passed，34.9s | 核 selection、保存失败不提交、选图修复后新 run 成功、只重试失败节点；保存 409 为浏览器注入，状态排除另有单元回归 |
| 预览、下载和交付包 | [canvas-delivery-proof](tasks/archive/canvas-delivery-proof.md)：隔离 mock Chromium 2 passed，16.8s；Go delivery 24 tests passed | 核两种 PNG 规格、原图不变、实际文件及 ZIP 谱系/hash；503 为浏览器注入，未覆盖所有格式和真实存储故障 |
| 资产与片段配方复用 | [canvas-asset-recipe-proof](tasks/archive/canvas-asset-recipe-proof.md)：固定结果后再生成不换绑定，图库拖到 reference 端口，片段保存/取消/确认及目标配置保留；Recipe PG 12 passed | 专属浏览器门 3 passed / 1 skipped；跳过项是单独启用的未解决入口诊断，不计通过。未覆盖全部拖放组合或商家效率 |
| 完整配方创建入口 | [canvas-full-recipe-entry](tasks/archive/canvas-full-recipe-entry.md)：创建页只读预览、取消无写入、同事务创建首图、丢响应重试不重复；PG 59、Web 650、浏览器 7 passed | 1440/1024/390 业务操作，24 组语言/主题布局；来源身份、资产与结果不继承。素材占位需重新选图，文稿须按新商品审阅；未代替商家效率或真实 provider 验收 |
| 局部编辑 | [canvas-local-edit-flow](tasks/archive/canvas-local-edit-flow.md)：隔离 mock Chromium 2 passed，20.2s；providers/localedit Go 通过。[§5.4 复验](tasks/archive/delivery-r2-local-edit-retest.md)：当前栈隔离 mock **2 passed / 25.3s**；1440/390 结果可达截图；谱系+采用/撤销 | 核检查器入口、谱系、采用/撤销、提交失败不覆盖；未覆盖真实 OpenAI/Gemini 质量、确定性文字层或商家任务耗时。现有 filmstrip 不等于镜头列表主界面；**≠R2 全过** |
| 成果工作视图 | [delivery-workbench-projection](tasks/archive/delivery-workbench-projection.md)：投影/视图 Vitest 16 passed；`just web-build`/`docs-check` 通过；开发站 1440/1280/390 走查切换、定位、`scope=node` 运行 | 投影与切换已交付；打开默认见下条条件默认 |
| 默认入口走查 | [workbench-default-entry-walkthrough](tasks/archive/workbench-default-entry-walkthrough.md)：共享 dev 浏览器路径创建→（代理）出图→成果→定位；Vitest 成果/采用 17 + 视觉/配方 9 passed | **建议暂不全局默认成果**；已跟进 [有产出时默认成果](tasks/archive/workbench-conditional-results-default.md) |
| 有产出时默认成果 | [workbench-conditional-results-default](tasks/archive/workbench-conditional-results-default.md)：`defaultWorkbenchMainView`；有当前图 → results，否则 flow；Vitest 2 files / 17 passed | 非全局一律成果；跨会话记忆见下条 |
| 记忆上次主视图 | [workbench-remember-last-view](tasks/archive/workbench-remember-last-view.md)：商品级显式偏好；无偏好仍条件默认；`clearWorkbenchMainViewPreference` 可关 | 已交付；≠全局一律成果；浏览器整链未宣称 |
| 交付采用快照 | [delivery-adoption-snapshot](tasks/archive/delivery-adoption-snapshot.md)：Go 采用/并发/导出一致；成果视图采用/导出接线；schema `delivery_adoption_*`；Vitest 14 | 浏览器整链见下条本门；质量判据仍依赖图片质量组判定输入；不宣称 R2 全部通过 |
| R2 核心路径门 | [delivery-r2-core-path-gate](tasks/archive/delivery-r2-core-path-gate.md)：**本门 PASS**（2026-09-07）；隔离 mock Chromium 2 passed / 24.7s；路径表含 adopt/export asset+sha256；1440/390 截图 | **≠R2 全过**；导出 UI 叠层已由 [delivery-export-overlay-fix](tasks/archive/delivery-export-overlay-fix.md) 修复；§5.4 已由 [delivery-r2-local-edit-retest](tasks/archive/delivery-r2-local-edit-retest.md) 复验（本门 PASS）；Brand/批跑缺口 |
| 导出叠层可达 | [delivery-export-overlay-fix](tasks/archive/delivery-export-overlay-fix.md)：成果态无浮动工具条；1440/390 UI 点击导出；Vitest 14；隔离 e2e 2 passed | **≠R2 全过**；仅闭合导出可达缺口 |
| §5.4 局部修图复验 | [delivery-r2-local-edit-retest](tasks/archive/delivery-r2-local-edit-retest.md)：**本门 PASS**（2026-09-07）；隔离 mock Chromium 2 passed / 25.3s；检查器→结果/采用/撤销；1440/390 截图 | **≠R2 全过**；确定性文字层与真实 provider 质量仍缺 |
| 品牌视觉复用 | [brand-visual-reuse](tasks/archive/brand-visual-reuse.md)：商家内视觉方案版本 CRUD；商品显式选择；IQ-CF-07 继承预览；追加版本不静默改选择；配方创建预览列继承/待填；Vitest + Go 包测 | Brand 实体 B0 已建（占位改为未选定/已存在未合并）；未宣称跨商家分享 / 品牌色全量合并 / R2 |

两项新浏览器交付未修改生产业务代码，分别提交于 `daa4672c` 与 `f7e70e1e`。当时完整 Web 回归为 91 files / 647 tests passed，lint、build 通过，build 保留既有大 chunk 警告。Go delivery 为带 PostgreSQL 的 24 项实际通过，无跳过；不扩展为全部 Go 包通过。

### 层与验收

C0-C6 是已交付的文稿权威测试体系及外部链路补充，保留编号用于追溯。“已交付”指对应任务证据存在；本次文档重述未重跑这些层，不构成最新全量门禁记录。

| ID | 层与作用 | 测试 / 证据 |
|---|---|---|
| C-00 | C0 单步：origin、merge、section apply、cook 选择 | `document_test.go`、`document_candidate_test.go`、`select_test.go`、`ops_parse_test.go` |
| C-01 | C1 mock HTTP cook：生成与采用分离、运行中编辑 | `cook_contract_test.go` |
| C-02 | C2 有界动作搜索与失败轨迹缩减 | `authority_search_test.go`；默认 8 walks、深度 ≤ 3，另有六类动作深度 2 穷举 |
| C-03 | C3 provider 回调插入写：本节点、兄弟、overlay、撤销、取消和排队 | `authority_inject_test.go`；[整图撤销交付](tasks/archive/canvas-graph-run-undo.md) |
| C-04 | C4 浏览器 mock：检查器输入、文稿动作、运行、撤销、409 和取消 | `web/e2e/canvas-document-mock.spec.ts`；既有 Chromium 8 passed，详见下节归档 |
| C-05 | C5 skip-Agent 真实 provider 整图出图 | `web/e2e/direct-create-full-graph.spec.ts`；不覆盖改写与候选合同 |
| C-06 | C6 Agent 写入与整图运行交错 | `authority_agent_interleave_test.go`、`go/internal/agent/canvas_authority_interleave_test.go` |

### 真实用法矩阵

此标题保留历史链接。重复的“全部完成”组合表收敛为以下证据入口：

- 检查器空闲文稿动作、运行中手填与保存基线：[canvas-inspector-midrun](tasks/archive/canvas-inspector-midrun.md)。
- 整图运行中撤销与文稿 409 停止：[canvas-c4-remainder](tasks/archive/canvas-c4-remainder.md)；HTTP 具名整图撤销另见 [canvas-graph-run-undo](tasks/archive/canvas-graph-run-undo.md)。
- 检查器运行该节点、运行到这里、运行中取消：[canvas-c4-run-controls](tasks/archive/canvas-c4-run-controls.md)。
- 场景选点、修正后重试和仅重试失败节点：[canvas-run-recovery-proof](tasks/archive/canvas-run-recovery-proof.md)。
- 预览、原图和交付图下载、交付包：[canvas-delivery-proof](tasks/archive/canvas-delivery-proof.md)。
- 固定资产、拖入参考、片段确认与完整配方入口失败：[canvas-asset-recipe-proof](tasks/archive/canvas-asset-recipe-proof.md)。

## 测试方法与运行资源

原 D-01 至 D-06 作为文稿测试方法保留：D-01 用 live 文稿、origin、候选及 disposition 观察不变量；D-02 搜索仅含文稿权威动作；D-03 搜索失败 shrink 后写具名回归；D-04 夹具走 HTTP ChangeSet / runs / candidate 与本地 executor，不用 SQL 覆写被测 config；D-05 搜索预算可控；D-06 mock 文稿门与真实 provider 门分离。分组、配方和布局不进文稿搜索器，但仍属于相应业务行为验收范围。

- 浏览器验证手填必须在检查器输入；整图按钮必须提交 graph scope。不得换成 HTTP 写入或 node scope 来回避问题。文稿 409 不循环重放；当前只有纯 `move_nodes` 自动重放。
- mock 快速返回不允许把运行中编辑改成运行后编辑；验收要记录保存时 run 仍在执行的证据。
- mock/live 使用独立环境或明确授权的共享窗口，不改其他组 provider、数据库和 worker。**现有 C4 `findGraphWorkerPids` 扫描全机匹配的 worker 并暂停，独立端口或 browser context 不足以隔离该副作用。运行前必须限定 worker 目标或取得所有受影响资源的独占窗口。**
- 本次运行恢复与交付证据使用独立 API/Web、PostgreSQL、Redis 和 mock 绑定；临时栈已在转入文档重述时停止，证据文件保留，未停止共享开发栈。

```bash
just go-test
just go-test-canvas-search
just web-e2e-canvas-document
just web-e2e-canvas-run-recovery
just web-e2e-canvas-delivery
just web-e2e-canvas-asset-recipe
just web-e2e-canvas-local-edit
just web-e2e-delivery-r2-core-path
just web-e2e-live-graph
just docs-check
```

这些是按风险选择的入口，不要求每项工作全部运行。PostgreSQL 测试须具备数据库环境；浏览器门须具备相应运行栈与资源窗口；交付 ZIP 验收依赖 `unzip`。C2 加深命令把 `PRODUCTFLOW_CANVAS_SEARCH_WALKS` 从默认 8 提高到 80。真实 provider 门为 opt-in，不并入默认确定性回归。R2 核心路径门须挂载 delivery-adoptions / visual-systems 的 **当前** API（过旧共享 just-dev 二进制会 404）；可用隔离端口 + Redis DB 隔离 worker，勿 `docker compose down` 共享栈。

## 后续工作如何选择

优先处理已复现的丢稿、错误运行目标、错误资产身份、结果无法取用与无法恢复等业务故障；其次处理有操作证据的重复劳动与理解成本。证据补强必须说明它阻碍哪项业务判断，不按测试空白数量排优先级。

[完整配方创建入口](tasks/archive/canvas-full-recipe-entry.md) 已修复：新建页直接预览并事务确认，不经过默认 birth graph 或无图工作台。已有图不可覆盖、取消不创建商品/图、目标身份及重试去重保持。前单入口 FAIL 保留为历史；临时 probe 已移除，正向路径纳入常规资产与配方门禁。[局部编辑](tasks/archive/canvas-local-edit-flow.md) 已修复：mock 绑定下检查器可完成消除/换字/重绘、采用或只留图库，失败不改节点当前图。商家任务耗时与镜头列表主界面仍未知，本组当前没有已发布的后续实现单。

其余观察不足保留为未知，不立即制造一批“补齐所有测试”的任务。新交互、局部编辑扩展或主界面改版须有具体商家问题与独立范围；不预设重做编辑器，不引入新运行模型、兼容旧数据或无界动作搜索。[有产出时默认成果](tasks/archive/workbench-conditional-results-default.md) 已按条件默认实现；[记忆上次视图](tasks/archive/workbench-remember-last-view.md) 已交付商品级显式偏好。采用/视觉核心路径浏览器证据见 [delivery-r2-core-path-gate](tasks/archive/delivery-r2-core-path-gate.md)（本门 PASS，≠R2 全过）。导出按钮叠层可达见 [delivery-export-overlay-fix](tasks/archive/delivery-export-overlay-fix.md)（已修，≠R2 全过）。§5.4 局部编辑复验见 [delivery-r2-local-edit-retest](tasks/archive/delivery-r2-local-edit-retest.md)（本门 PASS，≠R2 全过）。Brand 占位与批量无限制批跑仍为缺口 → Brand 表骨架由商家平台 B0 交付后，体验侧残余为商品选定 Brand、品牌色合并与批跑；每次交付更新本文件受影响的结论与证据，详细执行过程留在任务归档。
