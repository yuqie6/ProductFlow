# ProductFlow 商品视觉生产工作台 · 产品设计与开发需求书

## 0. 状态

- 文档状态：Draft
- 文档版本：v0.3
- 产品类型：单商家商品视觉生产工作台
- 运行形态：自托管 Web，单管理员、单商家
- 目标技术：React / Vite、FastAPI、PostgreSQL、Redis、Dramatiq、Node.js 22 + Pi Agent
- 开发方式：在现有仓库上演进，不另起产品、不另起运行时
- 未完成项入口：`docs/ROADMAP.md` §8；本文是该节的实施北极星

> **和现有文档的关系**
>
> - `CONTEXT.md`、`PRD.md`、`ARCHITECTURE.md`、`USER_GUIDE.md` 仍是**当前已交付合同**的唯一所有者。
> - 本文是工作室竞争力第一版的产品与开发总基线：复述已交付规则，并给出必须补齐的能力、验收和明确不做。
> - 标注「待交付」的条目在落地前不得写进 `PRD.md` / `CONTEXT.md` / `ARCHITECTURE.md` 的当前事实段。
> - 画布与侧栏未完成交互以 `v3-canvas-restoration.md`、`v3-sidebar-restoration.md` 为质量上限。
> - 工程切片、ADR、路由所有权以 `ARCHITECTURE.md`、`adr/0008` 和 `adr/0009` 为准。本文不取代那些文件。
- 创建路径改为现图出生、去掉 onboarding Task 和自动 Turn：以 `adr/0009` 与 `specs/agent-canvas-sandbox.md` 为准。本文 §4.2「live graph 出现之前只产出 WorkflowDraft」描述当前代码，不是创建入口的目标合同。

> **修订记录**
>
> - v0.1：吸收爱创、美图、稿定、ComfyUI、PixPix、Photoroom、Claid 的可复用形态，保留 ProductFlow 的商品级 DAG 与确认边界。
> - v0.2：按《我不是炮神》需求书的结构方法重整竞争策略，补充竞品官方证据、能力分层、核心护城河、北极星指标和竞争力验收；删除无来源的功能拼盘式判断。
> - v0.3：补齐对象生命周期、模块输入输出、阶段门禁、异常路径、端到端验收脚本、测试证据与需求追踪，达到可直接拆解研发任务的详细度。

---

## 1. 项目概述

ProductFlow 是给一个商家用的商品视觉生产工作台。

用户上传真实商品参考图，选定要出哪些图、出几张。Agent 通过对话把商品事实、视觉体系和单图提示词整理成可确认的草案。用户确认后，系统把草案一次写入可编辑、可执行、可复用的 schema-v3 工作流图。之后同一件商品的出图、改图、重跑、存配方、导出交付，都在这张图和这件商品的图片库里完成。

当前仓库服务个人 live demo 与自托管。多租户、计费、团队角色、自动上架电商平台不属于本版本。

---

## 2. 核心设计目标

- 让用户感觉自己在经营**一件商品的视觉生产线**，而不是在技能货架上点一次按钮。
- 让真实货品外形成为不可绕过的输入：没有身份参考，摄影/信息图不能跑生图。
- 让 Agent 把缺口问清楚、把草案写完整；把图定下来的人永远是用户。
- 让视觉体系、提示词、参考绑定、生成规格在节点上可见、可改、可重跑。
- 让同一套结构可以存成配方，迁到下一件商品时不带走像素、不带走商品身份。
- 让确认之后的第一套可用图按**镜头**推进，失败落到具体镜头，不必先懂 DAG。
- 让「八成对、改那 20%」走局部修，写回同一资产谱系，不必重跑整张图。
- 六类节点够用。不把产品做成 ComfyUI，也不做成 18 个互不相通的技能页。
- 模型由用户自备（prompt / agent / image 三用途）。产品绑定的是商品、图、配方，不是某一家出图模型。

### 2.1 北极星结果

第一版只追一个结果：**商家能把一件真实商品稳定地做成一套可修改、可复用、可交付的商品图。**

验收时拆成五个指标，避免用「生成成功」掩盖产品没有完成：

| 指标 | 第一版定义 | 测量边界 |
|---|---|---|
| 首套图时间 | 从参考图和图种就绪，到第一套计划镜头都有成功结果 | 分开记录 ProductFlow 编排耗时与 provider 排队/生成耗时 |
| 商品保真 | 人工核对外形、颜色/材质、logo/关键文字、文字策略 | 不用自动分数替代人工结论；失败图仍保留用于复盘 |
| 修改闭环 | 用户能重跑单镜头或局部修，不必重建商品和整张图 | 记录原图、修后图、节点当前结果和 lineage 是否一致 |
| 配方复用 | 第二件商品预览并应用同一结构时，不携带上一件商品的身份和像素 | 以 recipe payload、预览和落图后的引用审计为准 |
| 可恢复性 | 页面刷新、SSE 重连或单节点失败后，用户能从数据库事实继续 | 不把 session 文件存在或测试替身成功当作业务恢复证据 |

首轮质量样本建立前不写虚假的百分比目标。阶段六必须产出固定样本集、计时口径和基线数值；后续版本只能在同一口径上提高目标。

---

## 3. 核心生产循环

1. 在设置页配置 prompt、agent、image 三个用途。
2. 创建商品：填名称即可进对话，或在创建页填类型、参考图后直接建图。
3. 上传 1 至 6 张通过校验的真实参考图，其中至少一张能识别商品本身。
4. 选择图片类型与每类数量，形成套图计划。
5. Agent 追问会改变结果的事实，产出版本化草案；或跳过 Agent。
6. 用户确认明确 revision，系统在一个事务内写入 live schema-v3 图。
7. 在镜头列表或画布上生成套图：先内容节点，后各镜头生图。
8. 检查外形、颜色、文字；不满意则局部修、重跑该镜头或改提示词后再跑。
9. 按交付预设导出，不重新烧模型。
10. 把满意的全图或片段存成自己的配方。
11. 下一件商品预览并应用配方，替换参考与商品资料后继续出图。
12. 需要探索时进入连续生图；满意结果保存回该商品图片库或全局素材库。

循环必须能在第 7 步交出第一套图。第 8–11 步是工作室留存下来的原因。

---

## 4. 产品原则

### 4.1 权威

| 对象 | 权威 | 不允许 |
|---|---|---|
| 商品、事实、资产、Draft、图、Run、配方、设置 | ProductFlow PostgreSQL | 用 Agent session 文件或浏览器本地状态充当业务权威 |
| 交互式 Turn transcript | Agent service 的 session/event 文件 | 用网页投影拼第二份模型 transcript |
| 在线工作流 | 唯一 schema-v3 `workflow_graphs` | 运行时再读 V1/V2 编辑器或执行器 |
| 媒体字节 | `MediaObject` | 节点、封面、边引用存储路径或数组下标 |
| 商品内图片身份 | `ProductImageAsset` | 同一张图在节点和图库里用两套 id |
| 工作流执行 | `WorkflowGraphRun` | 必须先开 Agent Session 才能跑图 |

### 4.2 确认边界

- Agent 在 live graph 出现之前只产出 `WorkflowDraft` revision，不能直接写正式图。创建入口的目标是商品出生即 live 图（`adr/0009`）；该条在沙箱规格落地前仍描述当前 collecting Draft 路径。
- 用户确认针对一个明确 revision。确认与落图在同一事务。
- live graph 出现之后，Agent 可以解释、检查、请求运行、提交单次 Graph Command 或未应用 GraphProposal。
- live graph 出现之后，Agent 不能再提交一份会覆盖现图的 WorkflowDraft。
- 冲突事实不能通过最终 Draft 确认。
- 事实优先级：用户确认的结构化事实 > 未确认输入 > Agent 图片观察。

### 4.3 图即生产工具

- 工作流生成后仍是用户可以直接编辑和执行的生产工具。
- Agent 不能取代画布、运行按钮、节点重试和人工选择。
- Compiler 只读取目标节点的入边。断开边即失去该输入，禁止全图扫描补参考。
- 画布分组只整理视觉布局，不是 DAG 节点，没有端口、运行、取消、重试。

### 4.4 配置化

- 节点可接受的边、可编辑 config 键由 Node Catalog 拥有。
- ChangeSet 不得写入未登记键或已退休 plan key。
- 图片类型、默认画幅、GenerationSpec / DeliverySpec 默认值放在目录或创建模板中，不写死在渲染逻辑。
- 供应商模型名、尺寸、高级字段以 provider 能力为准，前端不发明一份平行模型表。

---

## 5. 目标用户与使用场景

### 5.1 用户

- 需要持续生产电商商品图的独立商家。
- 要对提示词、参考图、比例、画质和交付尺寸保留人工控制的设计者。
- 希望 Agent 澄清需求，同时保留可视化工作流的用户。

第一版只服务一个人操作的一个商家工作区。

### 5.2 主路径场景

| 场景 | 用户做什么 | 系统必须做到 |
|---|---|---|
| 新品第一次出图 | 上传实拍，选套图，确认，生成 | 几分钟内按镜头交出摄影/信息图 |
| 改一版主图 | 改提示词或局部修，重跑该镜头 | 不影响其他镜头的当前结果 |
| 下一件同类商品 | 应用配方，换参考和资料 | 预览将出现的节点和边，一次确认写入 |
| 详情卖点图 | 选信息图类型，要文案和语种 | 提示词和生图都受文字策略约束 |
| 证据图 | 上传资质或工厂图 | 只占位绑定，不调用生图模型 |

### 5.3 第一版不服务的场景

- 女装平铺一键上身、虚拟模特库、姿势裂变。
- 粘竞品详情链接做整页复刻。
- 一次批处理整个店铺几百 SKU。
- 图生视频、视频翻译、直播切片。
- 多人同时改同一张图。

### 5.4 用户任务，而不是功能入口

需求优先级按用户要完成的任务排序，不按竞品菜单排序：

| 用户任务 | 用户真正关心的结果 | ProductFlow 必须隐藏的复杂度 |
|---|---|---|
| 给新品做一套上架图 | 每种图有结果、整体视觉一致、商品没变形 | 节点拓扑、provider 参数差异、运行队列 |
| 改掉一张图的问题 | 只改错的区域或镜头，其他结果不丢 | mask、lineage、current asset 切换、幂等 |
| 复用上次的做法 | 换商品后仍得到同类结构，不串图 | recipe 去身份、重新绑定、冲突预览 |
| 按平台交付 | 一次得到正确尺寸/格式的文件 | rendition job、格式、裁切、文件命名 |
| 追查为什么失败 | 知道哪一步失败、能做什么 | provider error、run snapshot、重试边界 |
| 换模型继续做 | 历史商品、结构和图片不受影响 | provider adapter、effective config、能力差异 |

用户不会为了“体验 DAG”而使用 ProductFlow。DAG 的价值是让结果可解释、可局部重跑、可复用；镜头视图负责把这些价值翻译成商家对象。

### 5.5 关键阻力与产品响应

| 阻力 | 典型症状 | 产品响应 |
|---|---|---|
| 不知道该做哪些图 | 只说“帮我做电商图” | 推荐套图 + Agent 追问 + 可修改计划 |
| 害怕商品变形 | 不敢把真实商品交给生成模型 | 身份参考入边、保真意图、运行后人工清单、历史保留 |
| 不会写提示词 | 只会说风格和用途 | creative brief / visual system / prompt 节点分工，Agent 可建议 |
| 看不懂节点图 | 画布一打开就退出 | 默认镜头视图；画布作为高级控制面 |
| 生成后还要去别的软件 | 文字错、杂物、裁切不对 | 结果上下文中的局部修与交付预设 |
| 每件商品从头来 | 同类 SKU 反复配置 | 无像素配方、官方配方、视觉体系复用 |
| 模型错误不透明 | 一直重试但不知道原因 | 结构化失败、provider note、可重试判断和下一步 |

---

## 6. 竞品取舍

本节是 2026-08-23 的研究快照，只引用竞品公开页面能证实的产品形态。竞品会变化；实施某项能力前应重新核页面，不把下表当永久事实。

### 6.1 可验证的竞品能力

| 来源 | 公开能力 | ProductFlow 借鉴 | 明确不复制 |
|---|---|---|---|
| [爱创 AI](https://www.51aic.com/) | Agent 模式、批量生成、主图套图、18 种作图场景、详情页规划、多语言和平台适配 | 套图计划、少步骤生成、详情内容规划、平台交付视角 | 18 个彼此独立的技能入口；以工具数量代替一件商品的连续生产 |
| [美图设计室电商 Agent](https://www.designkit.cn/help/130) | 一句话生成电商套图、多角度商品图、爆款主图复刻和多场景氛围图；[公开版本记录](https://apps.apple.com/cn/app/id1113276760)还包含自定义套图类型与 AI 消除 | 商家语言的镜头列表、整套生成、结果后局部修 | 把每次一句话当成不可审阅的正式业务写入；把旁路修图货架放到主导航 |
| [稿定 AI](https://www.gaoding.art/zh-tw) | Agent、商品精修、主图套图、详情页/A+、场景图、AI 画布、改图、改字和 Skill Hub | 官方场景配方、套图入口、改图/改字回到结果上下文 | 用海量 Skill Hub 或模板商城替代商品、图、运行和资产谱系 |
| [ComfyUI App Mode](https://docs.comfy.org/interface/app-mode) | 同一 workflow 可设置 App 或 Node Graph 默认视图，App 只暴露选定输入/输出并保留运行、取消和结果 | 镜头视图与画布共用同一张 canonical graph；商家默认看镜头，专业用户进入画布 | 把 ProductFlow 变成通用节点开发器 |
| [ComfyUI Templates](https://docs.comfy.org/interface/features/template) | 内置 workflow templates，加载时检查模型与输入要求 | 官方配方与用户配方共用一套预览、依赖检查和应用合同 | 模板暗中下载模型、复制媒体或绕过当前 provider 配置 |
| [ComfyUI Partial Execution](https://docs.comfy.org/interface/features/partial-execution) | 可只运行到选定输出所需的工作流分支 | 运行节点、运行到此节点、运行镜头和整图共享同一个 DAG 执行语义 | 为每个入口写一套执行器 |
| [ComfyUI Subgraph](https://docs.comfy.org/interface/features/subgraph) | 选中节点可收敛为可复用子图，并保留输入输出类型校验 | 用 group/selection 保存 recipe fragment，允许预览后复用 | 把 group 变成第七类运行节点或引入嵌套运行语义 |
| [Photoroom Batch AI Backgrounds](https://www.photoroom.com/batch/ai-backgrounds) | 批量应用一致的商品背景、场景和品牌视觉，并强调商品边缘、形状、纹理与光线 | 同一视觉体系驱动多个镜头；交付前能批量保持位置、背景和尺寸一致 | 第一版直接进入数百 SKU 批处理；把批处理结果与商品资产身份脱钩 |
| [Claid AI Photoshoot](https://claid.ai/blog/article/ai-photoshoot) | Precise/Creative 两种模式、商品位置和比例控制、从单图生成多角度与多场景、API 规模化 | 把「保真优先」与「创意优先」变成明确生成意图；输入质量在运行前可见 | 声称模型能够自动证明商品真实性；在没有样本 gate 时承诺目录级规模 |
| [Adobe Firefly Style Reference](https://developer.adobe.com/firefly-services/docs/firefly-api/guides/concepts/style-image-reference/) | 风格参考与强度控制，用同一风格保持多资产一致 | 视觉体系中的 style reference、来源、强度和适用镜头必须显式 | 把风格图误当商品身份图；让 style reference 越过 edge 输入合同 |
| [Adobe Firefly Generative Fill](https://helpx.adobe.com/uk/firefly/web/work-with-images/edit-images/generative-fill.html/content/help/en/firefly/web/work-with-images/edit-images/generative-remove.html) | 用户刷选区域后用提示词添加、替换或移除局部内容 | 局部修必须保存 mask、指令、来源资产和新资产 lineage | 没有区域选择就把整图重生成伪装成局部修 |

### 6.2 市场基线与竞争力分层

主图套图、场景图、详情页、局部修和 Agent 已经是公开市场能力。ProductFlow 只做到这些功能时没有核心竞争力，只是达到入场线。

| 层级 | 必须解决什么 | ProductFlow 对应能力 |
|---|---|---|
| 入场线 | 上传商品图后快速得到一套可用结果 | 推荐套图、镜头列表、生成套图、官方场景配方 |
| 留存线 | 不满意时能只改一张、保留历史并继续生产 | 单镜头重跑、局部修、资产 lineage、当前结果切换 |
| 复用线 | 下一件商品能复用结构和视觉方法 | recipe preview/apply、视觉体系节点、商品身份重新绑定 |
| 信任线 | 用户知道 Agent 做了什么，正式图不会被静默覆盖 | WorkflowDraft revision、GraphProposal、确认、冲突和运行快照 |
| 控制线 | 简单用户不看 DAG，专业用户仍能看见并修改真实关系 | 镜头/画布同图、六类领域节点、typed edge、partial run |
| 所有权线 | 模型、数据和部署由用户掌控 | 自托管、用户自备 provider、PostgreSQL 业务权威、稳定媒体身份 |

### 6.3 ProductFlow 的核心护城河

第一版必须同时建立以下六点。缺少任一点，产品都会退化为套图按钮、Agent 聊天框或节点编辑器中的一种。

1. **商品长期记忆。** 商品事实、身份参考、视觉体系、图、运行结果和历史修改持续存在，下一次操作从同一业务对象继续。
2. **一图两种界面。** 镜头视图负责让商家快速产出，画布负责让用户检查和改变真实关系；两者不能拥有两份状态。
3. **确认式 Agent。** Agent 负责问清楚、组织和提案，用户确认明确 revision 或 ChangeSet 后才产生正式副作用。
4. **可追溯资产链。** 上传、生成、局部修、节点当前结果、图库和交付 rendition 通过稳定身份与 lineage 串在一起。
5. **无像素配方。** 配方保存生产方法，不保存上一件商品、绑定资产和生成结果；复用前必须预览结构变化。
6. **供应商中立。** ProductFlow 管商品生产合同，prompt/agent/image provider 管模型能力；更换供应商不能改变商品、图、运行和配方身份。

竞争策略不建立在模型效果永久领先上。模型质量会快速收敛，ProductFlow 要守住的是商家从首套图、修改、复用到交付的连续业务状态。

### 6.4 取舍原则

- 竞品已有的高频能力，只有能进入「商品 → 镜头 → 结果 → 修改 → 复用 → 交付」循环时才实现。
- 新入口默认投影现有业务对象和执行器。不能证明 owner 时，不新增 route、表、节点类型或状态库。
- 模板、Skill、Agent 和配方都不能绕过商品身份、用户确认和资产 lineage。
- 服装试衣、视频、爆款复刻和大批量店铺作业有独立数据模型与质量门，第一版不借功能名硬塞进现有图。
- 稿定的版权素材规模、美图和爱创的专用模型效果、ComfyUI 的通用生态不是第一版可复制资产；需求书只吸收交互与系统方法。

---

## 7. 操作系统与页面

### 7.1 页面

| 路由 | 职责 |
|---|---|
| `/login` | `ADMIN_ACCESS_KEY` 登录 |
| `/settings` | 供应商档案、三用途绑定、运行时限制 |
| `/products` | 商品列表与自动封面 |
| `/products/new` | 创建：Agent 对话或直接建图 |
| `/products/:productId` | 商品工作台：Agent、镜头/画布、详情、运行、配方、图片库 |
| `/image-chat` | 连续生图会话 |
| `/media-library` | 全局素材库 |
| `/help` | 用户指南投影 |
| `/history` | V1 只读归档 |

`/gallery` 只做兼容重定向。`/products/new/agent` 重定向到 `/products/new`。

### 7.2 工作台主区（待交付：镜头列表为默认）

商品工作台主区提供两种视图，读写**同一张** live graph：

| 视图 | 给谁 | 显示 |
|---|---|---|
| 镜头 | 默认。商家出套图 | 每个会生图的分组一行：图种、张数、状态、最新缩略图、运行此镜头 |
| 画布 | 要改结构的人 | schema-v3 自由画布，六类节点、连线、一层分组 |

规则：

- 切换视图不保存第二份图，不改变执行语义。
- 「添加场景」在两种视图都能用，一次落下分组 + 提示词 + 一张生图。
- 未选中节点时，侧栏提供添加、图库、运行套图。
- 窄屏检查器是底抽屉，画布仍露出一截节点。
- 最大化收起顶部导航，主区占满。

已交付：画布、分组进入/面包屑、节点旁工具条、撤销重做接线。
待交付：镜头列表作为默认主区；「生成套图」作为商家文案（内部仍是按 DAG 跑内容节点和生图节点）。

### 7.3 工作台信息架构

宽屏工作台由三块组成：

1. **生产主区**：镜头或画布；永远占最大空间。
2. **对象详情**：当前镜头、节点、运行、图片或配方的检查器。
3. **Agent 协作区**：对话、问题、Draft/Proposal、工具步骤和待确认动作。

约束：

- 生产主区不能因为 Agent 打开而变成不可操作的背景。
- 详情和 Agent 可以共享右侧壳，但不能共享含义不清的选中状态。
- 从运行错误、图片结果或 Agent 影响对象跳转时，必须定位到同一个 canonical 对象。
- 关闭检查器只改变布局，不清除选择；清除选择是单独动作。
- URL 负责商品和必要的可分享定位，不把临时拖拽、hover、未保存表单全部编码进 query。

### 7.4 页面级状态

每个主页面必须显式处理：

| 状态 | 用户看到什么 | 允许动作 |
|---|---|---|
| 首次加载 | 稳定骨架，不用假数据闪烁 | 取消导航或等待 |
| 空数据 | 当前对象为什么为空、最直接的创建动作 | 创建、上传、应用配方 |
| 部分加载 | 已有内容继续可读，局部显示加载 | 不冻结无关区域 |
| 保存中 | 具体对象 busy；布局不跳动 | 取消未开始的动作；禁止重复提交 |
| 冲突 | 哪个 revision 已变化、用户输入是否保留 | 重新读取、比较、重新应用 |
| 可恢复失败 | 安全错误摘要和下一步 | 重试、改配置、查看详情 |
| 不可恢复失败 | 停止原因，不伪造成功 | 返回稳定入口、导出诊断标识 |
| 权限/登录失效 | 明确需要重新登录 | 登录后回到原路由 |

任何 toast 都不能成为唯一结果记录。创建、运行、生成、局部修和导出完成后，页面中的持久对象必须可见。

---

## 8. 商品系统

### 8.1 商品身份

一件商品是生产单元。它拥有：

- 名称与说明。
- 结构化事实（来源、状态、冲突）。
- 1 至 6 张创建期参考图（可在工作台继续增加图片库资产）。
- 至多一张 active schema-v3 图。
- 商品图片库。
- 至多一段与该商品绑定的 Agent 对话（工作台右侧继续）。

封面只是列表展示，改封面不改事实、不改参考绑定。

### 8.2 创建入口

`/products/new` 是唯一创建入口。

Agent 路径：

1. 名称即可进入工作台对话。
2. 系统写入 draft Product、collecting `WorkflowDraft`、商品 Conversation、AgentSession。
3. 用户在对话里上传参考图并说明图种；Agent 写入不可变 intake。
4. 创建页上的类型和文件可作为进对话前的快捷填写。
5. 缺类型或参考不阻塞 Agent Turn，但确认落图前必须满足校验。

直接创建路径：

1. 选择图种、上传参考、填写说明和出图设定。
2. 一次写入商品、参考、可运行图，不经 Draft 确认面板。
3. 进入工作台后立刻可以对话和运行。Agent 不能再提交覆盖现图的 Draft。

无图时允许「从空白建图」，写入 revision 1 的空图，再经 Graph Command 加六类节点。重复创建 active graph 返回冲突。

### 8.3 数量上限

- 图种初始不选中。选中后数量默认 2，范围 1–6。
- 计划生成总数不能超过 30。
- 参考图 1–6 张；PNG / JPEG / WebP。
- 后端校验数量、所有权、字节、格式。语义是否像真货由 Agent 和用户审阅，后端不声称能自动证明真实性。

### 8.4 商品生命周期

商品不是一次表单提交。第一版使用以下业务阶段投影；是否落成单独枚举由现有模型决定，不为 UI 文案新造数据库状态：

```text
draft / collecting
  -> graph_ready
  -> producing
  -> has_results
  -> reusable / deliverable
```

- `draft / collecting`：Agent 创建已建立商品，但参考、图种或 Draft 仍可不完整。
- `graph_ready`：存在 active schema-v3 graph，用户可直接编辑。
- `producing`：存在 queued/running WorkflowGraphRun；商品本身仍可查看。
- `has_results`：至少一个生图节点存在成功当前结果。
- `reusable / deliverable`：用户可保存配方或生成 rendition；不是商品终态。

商品删除、归档或永久清理不在第一版需求内。实现删除前必须单独定义资产、运行、配方、全局图库引用和 V1 archive 的处理合同。

### 8.5 商品事实与冲突

- 每条事实保存 key、value、来源、状态和版本；同一 key 的冲突不能靠覆盖最后一条消失。
- 用户确认的结构化事实优先于未确认文本和 Agent 图片观察。
- Agent 可以指出冲突和建议值，不能替用户确认价格、材质、规格、认证或禁用声明。
- 事实修改影响后续运行，不回写历史 run snapshot。
- 已经生成的图片保留其使用过的事实版本；详情页能追溯，不要求第一屏展示全部内部 id。

---

## 9. 参考图与身份

### 9.1 角色

`image_asset` 节点一对一绑定一个 `ProductImageAsset`。角色字符串进入 prompt 与 image provider：

| role | 含义 | 创建时 |
|---|---|---|
| `product_identity` | 商品本身 | 创建上传默认为此 |
| `environment` | 场景/环境参考 | 用户后加 |
| `style` | 风格/构图参考 | 用户后加 |
| `evidence` | 资质、工厂等证据 | 证据图种占位 |

身份参考接到视觉规范、创作要求、以及每个会生图镜头的提示词和生图节点。不接到证据占位。

### 9.2 运行规则

- 会生图的 `image_generation` 缺少 reference 入边时，运行结构化失败。
- 绑定不是下游 `reference` 边。先绑定资产，再把该节点连到需要它的处理节点。
- 需要新参考时：上传或把连续生图/局部修结果保存到商品，再绑定。
- 断开边后，compiler 不得继续使用那张图。

### 9.3 参考输入质量与失败

上传校验分三层：

1. **确定性校验**：数量、字节、MIME、解码、尺寸下限、所有权，由后端拒绝不合法输入。
2. **可用性提示**：主体被裁断、严重模糊、强反光、logo 不清等，由 Agent/用户判断并可继续补图。
3. **语义确认**：是否真是该商品、是否具有使用权，只能由用户负责，系统不得自动声称已证明。

运行时：

- 参考资产不存在、不可读或不属于商品时，在 provider 调用前失败。
- provider 不支持多参考或保真参数时，必须在运行前给出能力差异，不能静默丢参考。
- 一张图同时承担 identity/style/environment 时，用户必须显式选择多个角色或多条边；系统不从文件名猜角色。
- style reference 影响风格，不能满足 image generation 的商品 identity 前置条件。
- 证据图默认不进入生成上下文，除非用户把它重新作为合法参考连接到目标节点。

---

## 10. 图片类型与套图计划

### 10.1 三个家族

| 家族 | keys | 落图 |
|---|---|---|
| 摄影 | `hero` `scene` `detail` `sku` `packaging` | 一组：1 个提示词 + N 张生图 |
| 信息图 | `selling_point` `dimensions` `specifications` `after_sales` `precautions` `faq` `shipping` `brand_story` | 同上；未指定文案策略时默认 `required` |
| 证据 | `certification` `factory` | 不建提示词/生图。1 个未绑定 `image_asset`，`role=evidence` |

`image_type_key` 只作分类标签和图库目录，不表达拓扑。Shot 不是第七种节点，用户单元是一层 Group。

### 10.2 默认画幅

| key | 默认 |
|---|---|
| `hero` `selling_point` 及多数信息图 | 3:4 |
| `scene` | 4:3 |
| `detail` `sku` `packaging` `certification` | 1:1 |
| `brand_story` `factory` | 16:9 |

创建时可改。画幅写入对应生图节点的 GenerationSpec。改 DeliverySpec 不改 GenerationSpec。

### 10.3 推荐套图（待交付：作为创建快捷项）

创建页提供「推荐套图」，一键勾选：

```text
hero × 2
detail × 2
scene × 1
selling_point × 2
```

用户仍可增减。这只是选择快捷项，不绕过数量上限，不自动跑图。

类型中文名以 `USER_GUIDE.md` / i18n 为准，例如首屏海报图、细节展示图、场景展示图、核心卖点图。

### 10.4 镜头投影合同

镜头是一个一层 Group 的商家投影，不是持久化新对象。一个可生成镜头必须满足：

- 一个 group，带稳定 group id、名称、图种和顺序。
- 恰好一个主 `prompt_generation` 节点；多个提示词属于多个镜头，不塞进数组。
- 一个或多个 `image_generation` 节点；每个节点代表一张计划输出图。
- group 外的 product/identity/visual/brief 通过真实 edge 进入组内节点。
- 镜头状态由组内节点和关联 run 投影得到，不写入平行 `shot_status`。

镜头列表一行至少显示：名称、图种、计划张数、成功/运行/失败计数、最新结果、缺失输入、运行与打开详情动作。

投影异常必须显式：

- group 没有 prompt：显示「缺提示词步骤」，允许进入画布修复。
- group 没有 image node：显示「没有计划图片」，不显示假进度 0/0 成功。
- 多个 prompt：显示结构异常，不能随便选择第一个运行。
- 节点跨多个 group：拒绝持久化或在读取时标成非法，不复制节点修复。

### 10.5 套图计划变更

- 增加数量：在同一镜头 group 中新增 image node，并复制可复用生成意图，不复制当前结果。
- 减少数量：删除尚未运行的计划节点；已有结果的节点删除前必须确认，历史资产仍留在图库。
- 改图种：改变分类和默认建议，不自动重写用户已经编辑的提示词和结果。
- 改顺序：只改 group/order 投影，不改 DAG 依赖。
- 删除镜头：预览将删除的 prompt/image 节点和边；历史 run/result 不物理删除。
- 推荐套图再次点击：只填充缺失类型，不能静默覆盖现有镜头配置。

---

## 11. Agent 与 Draft

### 11.1 Agent 允许做的事

- 追问价格、品类、规格、卖点、风格、语种、文案密度、禁用内容、画幅、保真、背景。
- 把参考图和用户选择写入 intake。
- 产出商品事实、视觉体系、图片计划、每张图提示词、参考绑定、生成规格、将创建的分组/节点/边。
- 建议计划变更，不能静默改已确认选择。
- live graph 之后：解释画布、检查缺口、请求运行、单次 Graph Command、GraphProposal 幽灵预览。
- 有界读取商品图库元数据；需要看图时只拉选中的图。
- 发布可确认的全局素材整理 Draft。

### 11.2 Agent 不允许做的事

- 一次把整个图库放进模型上下文。
- 把自然语言终答解析成正式 DAG。
- 循环 create-node 来物化正式图。
- 未确认就写 live graph。
- live graph 之后再提交覆盖现图的 WorkflowDraft。
- 直接提交 WorkflowGraphRun；只能创建待确认运行请求，确认后走同一套 graph run 约束。

### 11.3 Draft 状态

`collecting → awaiting_confirmation → confirmed → materializing → ready`，以及 `failed` / `cancelled`。

每次 Agent artifact 追加 revision。确认针对明确 revision。

确认面板必须汇总：事实与冲突、图种与数量、视觉体系、每类/每张目标与提示词、参考绑定、比例/分辨率/文字策略/交付规格、将创建的文件夹与连线。

待交付：确认面板信息密度、冲突修改反馈、Agent 追问轮数与事实准确度的评估样本（见 ROADMAP §1）。第一版竞争力验收仍要求：用户能看懂草案并一次确认落图。

### 11.4 Agent Turn 与问题状态

一次 Turn 的用户可见状态：

```text
queued -> running -> waiting_input -> running -> succeeded
                    \-> cancelled
          \-> failed / unknown
```

- `queued`：尚未进入模型或工具，可在有完整 handoff 证据时恢复。
- `running`：可能已经调用模型或工具，重启后不能无条件重放。
- `waiting_input`：必须有完整问题 checkpoint；回答创建 continuation Turn，不重跑原 Turn。
- `unknown`：无法证明 provider/tool 是否产生结果。UI 必须提示检查对象，不自动假定失败后再做一次。
- `cancelled`：用户取消本轮，不等于取消已有 WorkflowGraphRun 或后台图片任务。

### 11.5 Agent 上下文预算

Agent 每轮按任务读取最小事实：

- 商品摘要、冲突事实和明确 revision。
- 当前图的节点/边摘要，不默认附全部 config 和历史 artifact。
- 当前页面与选择的有界快照，只作为意图线索。
- 图库只列分页元数据；图片 bytes 只读取用户选择或工具明确选择的有限集合。
- 历史对话依赖 Pi session/compaction；ProductFlow web projection 不重建另一份 transcript。

上下文截断必须可观察：记录省略了哪些区段和数量，不让 Agent 把“没看到”说成“不存在”。

### 11.6 提案和副作用矩阵

| 动作 | Agent 可直接执行 | 是否需用户确认 | 最终 owner |
|---|---|---|---|
| 读取商品、图、运行摘要 | 是 | 否 | application query |
| 提问、解释、给建议 | 是 | 否 | conversation projection |
| 更新未确认 intake | 受合同限制 | 视字段而定 | WorkflowDraft/intake |
| 创建/修订 WorkflowDraft | 是 | 是，确认 revision | workflow_drafts |
| live graph 单一低风险命令 | 仅允许白名单单 operation | 依命令合同 | Graph Command |
| 多节点 GraphProposal | 只能发布未应用提案 | 是 | Graph Command apply |
| 请求运行 | 只能创建待确认请求 | 是 | WorkflowGraphRun use case |
| 整理全局素材 | 只能发布 organization Draft | 是 | media-library application |
| 删除媒体、历史、source table | 否 | 独立运维批准 | owning cleanup transaction |

---

## 12. 工作流图

### 12.1 六类节点

| 类型 | 种类 | 运行时 |
|---|---|---|
| `product_source` | source | 不调用模型。提供确认过的商品事实 |
| `image_asset` | source | 不调用模型。一对一绑定图片 |
| `creative_brief` | processing | 调 prompt provider，结果写入节点 config，可再编辑。不生图 |
| `visual_system` | processing | 同上，写风格、背景、限制 |
| `prompt_generation` | processing | 根据上游生成提示词并写回。不生图 |
| `image_generation` | processing | 调 image provider。不回头填空的视觉/创作/提示词 |

空的内容节点需要先单独运行，或对生图使用「运行到此节点」。

### 12.2 生图节点字段

GenerationSpec（会烧模型）：

- 比例、分辨率档位、质量意图。
- 参考保真度。
- 背景意图。
- 文字策略：`none` / 允许 / 必须；语种。
- 直接创建默认要文案、简体中文；画布上新加的生图节点默认不要文案。

DeliverySpec（不烧模型）：

- 宽、高、格式、fit、背景色、裁切锚点、可选体积上限。
- 改变交付规格只产生 rendition，不替换生成源图。

### 12.3 画布编辑（已交付合同，浏览器证据待补）

- 从输出点拖到输入点连线。合法绿、非法红，连不上写出原因。
- 多选后可复制、编组、存配方、删除。
- 删除节点或选区前确认；删线可立刻撤销。
- Undo / Redo 走 ChangeSet，Redo 不得映射成再调 Undo。
- 自动布局只整理排线。
- 分组可进入局部视图，组内与全图分记视口。跨组边在全图可见。

### 12.4 添加场景

一次 ChangeSet：分组 + 提示词 + 一张生图，接到已有商品资料、视觉、创作要求。当前选中身份参考时也连上。六类原语仍可单独添加。

### 12.5 Graph revision 与写入

- 每次成功 ChangeSet 产生新 revision；客户端提交 expected revision。
- expected revision 落后时整次拒绝，返回当前 revision 和结构化冲突，不做部分应用。
- 一次 ChangeSet 内的 node/edge/group 操作必须在一个事务中验证并写入。
- Undo 保存 inverse ChangeSet 所需信息；Redo 对已撤销的原变更重新应用其 forward 语义。
- 重放同一 idempotency key 只有 payload hash 一致时才返回原结果；hash 不同视为冲突。
- GraphProposal 保留 base revision。确认时 base 已变则重新预览，不能强行覆盖。

### 12.6 连接与配置验证

验证所有权按最窄层分配：

| 规则 | owner |
|---|---|
| node type、port、edge data type、config key | Node Catalog/domain |
| node/edge/group 属于同一 graph | Graph Command application |
| asset 属于商品且可读 | product image application |
| expected revision、idempotency、事务原子性 | Graph Command application/persistence |
| provider 是否支持生成字段 | provider capability adapter |
| 表单显示和错误定位 | Web projection，不重复业务判定 |

非法连接必须返回稳定错误 code、相关 node/handle 和安全 message。前端预判只改善交互，后端必须再次验证。

### 12.7 删除与历史

- 删除 node 会删除当前图中的相关 edge/group membership，不删除历史 run snapshot 和已生成资产。
- 删除 image_asset node 不删除其绑定 ProductImageAsset。
- 删除 group 默认只解散视觉组织；明确“删除分组及内容”才删除成员节点，并要求确认影响数量。
- 已被 recipe、cover 或 library 使用的资产不因 graph 删除而丢失。
- V1 archive 是只读历史，任何 schema-v3 Graph Command 都不能写入 archive。

---

## 13. 运行

### 13.1 三种运行粒度

| 用户操作 | 实际 |
|---|---|
| 运行该节点 | 只跑当前处理节点 |
| 运行到此节点 | 先跑上游处理节点，再跑当前节点 |
| 运行此镜头 | 组内第一张生图 `to_node`，其余 `node` |
| 生成套图（待交付文案；语义=运行整张图） | 按 DAG：视觉规范、创作要求、各镜头提示词、各生图。跳过证据占位 |

工作流页面可以直接创建 `WorkflowGraphRun`，不需要 Agent Session。

### 13.2 执行快照

- 执行读 run snapshot，不读 live graph 的后续编辑。
- 一张计划输出图对应一个可跑的生图节点。重跑更新当前资产，旧结果留在图库和 run history。
- 运行中可取消。可重试的失败提供重试。
- 安全错误信息给用户；内部键、版本号、digest 不进第一屏。

### 13.3 套图进度（待交付）

进度按商家对象显示：

```text
视觉规范 → 创作要求 → 首屏海报图 → 细节展示图 → 场景展示图 → 核心卖点图
```

单镜头失败不自动取消后续镜头，除非用户取消整次 Run。失败行显示原因和下一步（改提示词 / 检查参考 / 重试）。

### 13.4 Run 状态机

```text
queued -> running -> succeeded
                  -> partially_succeeded
                  -> failed
                  -> cancelling -> cancelled
                  -> unknown
```

节点状态至少区分 queued、running、succeeded、failed、cancelled、skipped、unknown。整图状态由节点结果归约，不允许浏览器自行猜终态。

- `partially_succeeded`：至少一个目标结果成功，至少一个目标失败/取消/unknown。
- `skipped`：节点不在本次 target scope，或上游失败使其不可运行；必须记录原因。
- `unknown`：请求越过不可安全重放的 provider/effect 边界后失去确认。
- 已取消请求不保证 provider 侧立即停止；迟到结果必须由 attempt fencing 判断能否采用。

### 13.5 调度、并发和幂等

- 同一 graph 的结构修改和 run admission 使用明确锁/revision 边界。
- 同一节点同一 revision 的重复运行必须有独立 Run/attempt 身份，不能互相覆盖状态。
- provider 并发上限来自运行时设置；前端显示排队，不另建本地并发器。
- dispatcher/worker 重投只重投可证明尚未产生不可逆副作用的任务。
- prompt/image provider 调用前后记录 attempt token、request fingerprint 和有限 provider 证据。
- `unknown` 默认不自动重试；用户检查 provider/结果后显式重试会创建新 attempt。

### 13.6 取消、重试和结果采用

- 取消整图：停止未开始节点，并向运行中节点发取消；已成功结果保留。
- 取消单节点：只影响该 node run，不改变其他独立分支。
- 重试失败节点：复用原 run snapshot 输入，除非用户选择“按当前图重新运行”。
- 按当前图重新运行：创建新 WorkflowGraphRun，使用新 graph revision。
- 迟到成功结果与当前 revision/attempt 不一致时进入历史，不静默替换新 current output。
- 用户可以显式采用历史结果为节点当前资产；该动作记录来源 run/node/attempt。

### 13.7 错误分类

| 类别 | 示例 | 默认下一步 |
|---|---|---|
| 输入错误 | 缺参考边、空 prompt、非法尺寸 | 回到节点/镜头修复 |
| 配置错误 | 未绑定 provider、模型不支持字段 | 跳设置或降级配置，由用户确认 |
| 暂时错误 | 限流、网络、服务不可用 | 有界重试或稍后重试 |
| 内容拒绝 | safety/policy rejection | 修改 prompt/reference，不自动绕过 |
| 结果不明 | response loss、worker 中断 | 标记 unknown，检查后显式处理 |
| 系统缺陷 | invariant、反序列化、数据库约束 | 停止写入，保留诊断 id，不暴露 secret |

---

## 14. 视觉体系、创作要求、提示词

视觉体系是工作流级共享约束，记录风格、色彩、字体、光线、真实感、形态锁定。同类图可共用目标和构图；不同角度、内容或文字必须有单独提示词。单图例外必须明确记录。

内容节点跑完把结果写进检查器，用户可改后再跑。提示词节点维护版本记录。

待交付：视觉体系的用户级保存、跨商品复用和版本比较（ROADMAP 中期）。第一版用配方携带 visual_system 节点配置，不另做「品牌套装」对象。

### 14.1 三类内容的职责

| 内容 | 回答的问题 | 不应包含 |
|---|---|---|
| creative brief | 这套图要向谁表达什么、哪些卖点和限制 | 具体 provider 模型名、图片 bytes |
| visual system | 整套图怎样保持一致：色彩、光线、背景、字体、真实感 | 单张图独有构图、商品事实替代值 |
| prompt | 这一张图具体画什么、怎样构图、需要哪些文字 | 与其他镜头共享但未通过上游边输入的隐藏约束 |

内容节点的 provider 输出先写入该节点 config/artifact，用户可审阅和编辑。下游只读取运行快照中的明确版本。

### 14.2 版本和人工编辑

- provider 重新生成内容时创建新 artifact/version，不删除人工编辑前的版本。
- 用户手动编辑后标记来源，不让下一次自动运行静默覆盖；重新运行前提示影响。
- graph revision 和内容 artifact version 分开：移动节点不产生新 prompt 版本。
- 历史图片能追溯当时使用的 brief/visual/prompt artifact。
- 配方保存当前可复用配置，不保存商品事实和历史 artifact 全量。

### 14.3 风格参考

第一版如果 provider 支持 style reference：

- 参考必须是用户有权使用的资产。
- 保存资产 id、角色、强度和适用节点，不把临时 URL 写进图配置。
- strength 使用 ProductFlow 归一化意图，再由 adapter 映射 provider 范围。
- 不支持 style reference 的 provider 明确显示能力缺失，用户可选择仅用文字风格描述。
- 同一视觉体系可有多张参考，但进入单次 provider request 的数量受 provider 能力和上下文上限约束。

---

## 15. 图片库与素材

### 15.1 商品图片库

保存上传图、工作流生成图、连续生图转入图、局部修结果。不定义废稿/交付清单状态。成功结果全部保留。

目录：系统分类、图种、来源、一层用户文件夹。删文件夹只解除组织，不删资产、不打断节点/封面/lineage。

### 15.2 全局素材库

`/media-library` 是跨商品长期入口。工作流子图库只保存关联，不复制 bytes。节点、封面、参考、交付 lineage 使用工作流侧稳定图片身份，可追溯到全局素材身份。

Agent 对全局库只发布整理 Draft，用户确认后应用。

### 15.3 身份不变量

- `MediaObject`：不可变字节。
- `MediaLibraryAsset`：全局逻辑身份。
- `ProductImageAsset`：商品命名空间中的一张图。
- `WorkflowMediaLibraryAsset`：使用关联。

### 15.4 资产来源与谱系

每个 ProductImageAsset 至少可追溯一种来源：

| 来源 | 关键证据 |
|---|---|
| upload | uploader、原文件名、media object、校验结果 |
| workflow result | graph run、node run、attempt、generation spec、provider effective config |
| image session | session、turn/task、候选序号、基图/上下文 |
| local edit | source asset、mask、instruction、edit operation、provider attempt |
| delivery rendition | source asset、delivery spec、rendition job；通常不成为新的生成源身份 |
| legacy import | source profile、manifest/hash、迁移记录 |

谱系是审计和选择依据，不等于自动质量评分。

### 15.5 组织、归档和删除

- 系统目录是查询投影，不能被用户重命名或删除。
- 用户文件夹只是一层组织；移动不复制媒体。
- 标签、文件夹、归档是独立维度，归档不破坏节点、cover、lineage 和 rendition 引用。
- 同一媒体 bytes 可被多个逻辑资产引用；清理必须先做引用审计。
- 第一版不提供普通用户 hard delete。需要释放存储时另立 retention/cleanup 合同。
- 下载失败、缩略图失败不改变资产业务状态；原文件可用时允许重建衍生图。

### 15.6 检索与选择

商品库和全局库至少支持按名称、来源、图种、创建时间、文件夹、标签、归档状态筛选。工作流绑定器返回稳定 asset id，选择完成后重新校验商品/工作流范围，不能相信浏览器旧列表。

---

## 16. 配方

### 16.1 用户配方（已交付合同）

只由用户从 live graph 主动保存：全图、分组或选区。

配方含节点/边/分组和可复用配置，不含：

- 商品身份
- 绑定资产
- 生成结果
- 媒体字节

应用前预览将出现的节点和边。预览失败不能确认。

- 完整配方：目标商品还没有 live graph 时创建；已有图则冲突。
- 片段配方：合并进已有图；无法合并则明确冲突，不写成 Draft，不写成退休模型。

无图工作台可以预览和应用配方。

### 16.2 官方场景配方（不交付）

配方库不预置官方画布模板。用户从 live graph 保存完整工作流、分组或选区后，配方才出现在库中。历史 0086 官方 seed 行保留为归档审计，不进入在线列表、预览或应用。

### 16.3 Recipe payload 合同

Recipe version 至少包含：schema version、kind（full/fragment）、nodes、edges、groups、可复用 config、catalog compatibility 和创建来源。必须剥离：

- Product/ProductImageAsset/MediaObject/WorkflowGraph id。
- 当前结果、run、artifact、attempt、provider secret。
- 坐标以外的浏览器临时状态。
- 退休的 plan key 和未登记 config。

提取时运行 sanitizer；应用时再次运行 catalog/edge/config validation，不能因为 payload 来自本库就信任。

### 16.4 预览和冲突

预览返回用户可理解的差异：新增镜头/节点/边、将修改的配置、需要重新绑定的输入、无法应用的原因。确认携带 preview digest、目标 graph revision 和 recipe version。

以下情况整次拒绝：

- full recipe 应用到已有 live graph。
- fragment 找不到合法连接点且不能作为独立 group 落下。
- payload 含未注册 node/config/edge。
- 目标 revision 在预览后变化。
- 应用会引入上一商品的身份或媒体引用。

### 16.5 官方配方治理

官方画布模板不进入在线配方库。已落图的 graph 与 recipe application 审计记录继续可读。

---

## 17. 出图后局部修（待交付）

### 17.1 目的

商家真实循环是「八成对，改那 20%」。局部修是对已有 `ProductImageAsset` 的操作，不是第七类节点。

### 17.2 入口

- 镜头列表缩略图
- 生图节点结果
- 商品图片库预览

### 17.3 第一版操作

| 操作 | 说明 |
|---|---|
| 消除 | 去掉指定元素（道具、杂物、多余文字） |
| 换字 | 替换画面中的指定文字 |
| 局部重绘 | 圈定区域按文字说明重绘 |
| 裁切 | 确定性裁切，可只走 DeliverySpec；用户明确要重绘构图时才烧模型 |

### 17.4 结果

1. 生成新的 `ProductImageAsset`，谱系指向来源资产。
2. 来源保留。
3. 用户可选择「用作该生图节点的当前结果」（更新节点当前资产，不删历史）。
4. 用户可只存进图库，不改节点。
5. 局部修失败时节点当前结果不变。

实现优先复用连续生图会话的 image provider 能力（基图 + 指令）。没有 edit 能力的 provider 必须给出可见失败，不得假装已改。

第一版不做：去水印独立产品、批量 200 张、超清作为独立货架、视频。

### 17.5 局部修请求合同

一次编辑请求至少包含：

```text
source_asset_id
operation
instruction
mask_asset_id / mask geometry（操作需要时）
reference_asset_ids（可选、有界）
expected_source_revision
provider capability intent
idempotency_key
```

- mask 与源图使用同一像素坐标空间，保存原始宽高和变换矩阵。
- 浏览器缩放、旋转、裁切后的笔刷坐标必须反算到源图，不直接发送 viewport 坐标。
- 消除/局部重绘要求非空 mask；换字还需原文字或区域与目标文字；纯裁切不走 image provider。
- 请求提交后 mask 不再变化；再次编辑创建新请求。

### 17.6 编辑状态与失败

```text
draft -> queued -> running -> succeeded
                          -> failed / cancelled / unknown
```

- draft 可反复改 mask 和 instruction，不产生业务图片。
- queued/running 时源资产不可被替换成另一个 id。
- succeeded 先持久化 media + ProductImageAsset + lineage，再允许采用为 current result。
- unknown 不自动重放，避免产生两张用户分不清的修改结果。
- provider 返回整图但声称局部修时仍保存真实 effective mode，不伪造 mask 保真。

### 17.7 编辑界面

- 默认展示源图与编辑预览的可切换/并排比较。
- 笔刷具备大小、硬度、擦除选区、清空选区；布局稳定，不遮挡确认动作。
- 提交前显示会消耗模型的操作、源图和目标节点影响。
- 完成后提供“仅存图库”“采用为当前结果”“继续编辑”三个明确动作。
- 采用 current result 可撤销为上一资产引用，但不删除新旧资产。

---

## 18. 平台交付预设（待交付）

交付预设是 DeliverySpec 模板，不是新节点、不烧模型。

第一版预置：

| 预设 | 画幅 | 说明 |
|---|---|---|
| 淘宝/天猫首屏 | 3:4 | 列表首图 |
| 京东主图 | 1:1 | |
| Amazon 主图 | 1:1 | 正方形主图 |
| 详情竖图 | 3:4 | 信息图默认 |
| 场景横图 | 4:3 | |
| 自定义 | 用户宽高 | |

应用预设只改选中生图节点或导出任务的 DeliverySpec。GenerationSpec 保持原样。已有生成源图时，导出走 rendition job。

语种仍在生图节点的文字策略里，不跟交付预设绑死。创建时可同时选「目标平台」作为默认交付预设。

第一版不实现：自动发布到店铺、按平台审核规则拒图、多语言图片内文字翻译工具。

平台名称只是方便选择的内置模板，不构成平台合规保证。各平台规则可能变化；实现内置值时记录 reviewed_at/source，用户始终可以检查并覆盖宽高、格式和体积。

### 18.1 DeliverySpec

```text
width / height
format
fit: contain | cover | crop
background_color / alpha policy
crop_anchor
quality / compression
max_bytes（可选）
filename_template
```

- 不允许宽高为零、超出处理上限或使用 provider 不支持的编码。
- `contain` 需要明确留白/背景；`cover/crop` 在提交前展示裁切预览。
- PNG/JPEG/WebP 的透明度和背景处理不同，不能只改扩展名。
- max_bytes 是交付约束；压缩后仍超限则失败并说明，不循环无限降质。

### 18.2 Rendition job

- rendition 输入是不可变 source media + DeliverySpec snapshot。
- 相同 source hash + spec hash 可以幂等复用结果。
- 失败不改变 source asset 或 image node current output。
- 结果记录实测宽高、格式、字节数、checksum 和 storage object。
- 用户改预设创建新 rendition；旧 rendition 可保留到 retention 清理。

### 18.3 导出包

单图可直接下载。套图导出生成 manifest + 文件集合：

- 文件名包含安全化商品名、图种、镜头序号和规格，不暴露数据库 id。
- 同名冲突稳定追加序号，不覆盖。
- manifest 记录商品、graph/run revision、源资产、rendition、规格和生成时间；不包含 secret。
- 某张 rendition 失败时默认阻止“完整套图”宣称成功，允许用户下载已完成项并看到缺失清单。

---

## 19. 连续生图

独立于商品 DAG 的探索面。`/image-chat`：

- 选基图和至多 6 张上下文（基图占一个名额）。
- 每轮候选数量、尺寸、高级字段跟 provider 能力。
- 排队、进度、取消、失败重试、候选分支。
- 结果可下载、存全局库、存某商品库。

存进商品后与上传图、工作流结果同一套 `ProductImageAsset`，可绑定到参考节点。

连续生图不取代镜头运行，也不自动改 live graph。

### 19.1 会话与分支

- ImageSession 是连续探索容器，不等于 AgentSession。
- 每轮保存 prompt、基图、上下文、GenerationSpec、候选和 provider effective config。
- 从候选继续生成形成 lineage 分支；UI 能回到父结果。
- 切换基图不改写历史轮次。
- 保存到商品或全局库是显式动作，重复保存按 idempotency/来源避免意外复制。

### 19.2 候选采用

- 候选生成成功不自动成为商品资产。
- 保存到商品后创建 ProductImageAsset，可作为参考或被用户显式采用到节点。
- 保存到全局库创建/关联 MediaLibraryAsset，不伪造 Product scope。
- 候选删除/会话清理不能删除已经保存且被其他对象引用的 canonical media。

---

## 20. 设置与 Provider

三个用途都直接参与核心循环：

| 用途 | 用于 |
|---|---|
| prompt | 视觉规范、创作要求、提示词节点 |
| agent | 需求澄清、图库整理、工作流创建与解释 |
| image | 工作流生图、连续生图、局部修 |

档案含名称、类型、Base URL、API Key、能力、默认模型。Key 保存后不回显。

未配置完三个用途时，创建商品可以进入，运行生图必须给出「先去设置绑定 image」的下一步，不得静默失败。

### 20.1 Provider profile 与 binding

- Profile 保存 provider type、base URL、secret reference、默认模型、能力和用户可见名称。
- Binding 把 `prompt` / `agent` / `image` 用途指向一个 profile/model；同一 profile 可承担多个用途。
- secret 写入后不回显，更新使用 replace 语义；日志、SSE、错误和导出都不得包含。
- 删除正在绑定的 profile 前必须先解除或替换 binding。
- 数据库 runtime setting 可以覆盖受支持的 provider/model 选择；数据库连接、Redis、session secret 和管理员 key 仍只来自环境。

### 20.2 能力协商

image provider 能力至少描述：

- text-to-image、image-to-image、edit/inpaint。
- 多参考数量、mask、style reference、reference fidelity。
- 支持的尺寸/比例/格式/质量/背景/压缩。
- 每次候选数、partial image/stream、取消和结果查询能力。

前端表单只展示 binding 当前能力支持的字段。已保存 graph 含 provider 不再支持的字段时保留原值并标记不兼容，不静默删除。

### 20.3 Provider 错误归一化

adapter 把上游错误归一为 authentication、rate_limit、invalid_request、unsupported_capability、content_rejected、timeout、upstream_unavailable、unknown_result。保留有限 provider code/request id，清除 secret 和原始大 payload。

用户切换 provider 后重新运行创建新 attempt，历史 run 仍显示原 provider-effective 值。

### 20.4 设置验证

- 保存 profile 时验证 URL 形状和字段，不要求一定发真实模型调用。
- “测试连接”与“保存”分开，显示实际测试用途、模型和耗时。
- 测试成功不证明真实工作流完整能力；image edit、多参考、structured output 分别验收。
- provider schema 不允许的 `oneOf`、高级字段或响应格式必须在 adapter 层适配，不泄漏到业务 DTO。

---

## 21. 工作台界面规则

- 主生产面是镜头/画布，不是落地页。Agent 在右侧协作者位置。
- 节点靠类型色、图标、预览扫描；六类在缩略图尺寸必须能分清。
- 卡片只显示摘要。完整表单在详情。
- 空态、确认、失败：一句结果 + 下一步。
- 常规 chrome 不展示 schema、revision、asset id、digest。技术详情可折叠。
- 重要状态不能只靠颜色，要配合图标或文字。
- 文案、token、无障碍以 `productflow-frontend` 技能和 `adr/0005` 为准。不新开色板。

### 21.1 桌面、窄桌面和移动端

| 视口 | 主要布局 | 必须可完成 |
|---|---|---|
| 1440+ | 主区 + 可调宽检查器/Agent | 完整镜头/画布、详情、运行、配方、图库 |
| 1024 左右 | 主区保持可用，右侧面板受限或覆盖 | 不丢选择；可收起面板和最大化主区 |
| 390 | 主区 + 底抽屉/分视图 | 镜头运行、结果查看、基本详情、Agent 输入、返回画布 |

- 响应式改变布局，不改变业务状态和 API。
- 底抽屉必须留出画布/镜头上下文，不用全屏白板永久遮住主区。
- 触屏的平移、选择、节点拖动必须有明确模式，不能一次手势同时触发两个动作。

### 21.2 键盘与无障碍

- 所有核心命令可通过可聚焦按钮完成；快捷键是加速，不是唯一入口。
- `Escape` 关闭最上层浮层，不连续误关多个层级；不可恢复确认不能只靠 Escape。
- 焦点进入 dialog/drawer 后被约束，关闭后回到触发元素或对应对象。
- 图片有描述性 alt/label；纯装饰图不重复朗读。
- 状态、错误、进度有文字或 aria live 语义，不只用颜色/动画。
- reduced-motion 下关闭非必要平移和缩放动画，不影响进度反馈。

### 21.3 文案层级

- 第一层说结果和下一步：“场景图生成失败，检查商品参考后重试”。
- 第二层说业务原因：“缺少商品参考连线”“当前模型不支持局部重绘”。
- 技术详情折叠展示 error code、run/node、request id；不展示 secret、storage path 和原始 prompt 全量。
- 普通 chrome 不出现 schema-v3、ChangeSet、digest、revision 等内部术语；冲突对话可用“工作流已更新”并在详情提供 revision。

### 21.4 稳定几何

- 工具栏、节点卡、镜头行、计数器和缩略图定义稳定 min/max/aspect ratio。
- loading、hover、错误徽标、长模型名不能改变主区列宽。
- 长商品名和文件名可换行/截断并通过 tooltip/详情查看完整值。
- 主区不得产生横向页面滚动；画布内部平移不计作页面 overflow。

---

## 22. 商品保真

外形保真是产品承诺，不是提示词里的一句形容词。

### 22.1 运行前（已有结构，必须保持）

- 摄影/信息图生图必须有身份参考入边。
- `reference_fidelity` 进入 image provider。
- 身份参考同时进入视觉、创作、提示词。

### 22.2 运行后（待交付：结果检查清单）

生图结果详情提供人工核对清单，不自动删图：

- 外形是否还是这件货
- 颜色/材质是否漂移
- logo / 关键文字是否可读
- 画面文字是否符合文字策略

清单结果只作为用户决策记录，第一版不做自动废稿。评估样本与自动打分见 ROADMAP「图片生产质量」，不阻塞本需求书第一版。

### 22.3 保真检查记录

每张被检查的结果保存检查版本、检查人、时间、四项结论和可选备注。结论为 `pass` / `fail` / `not_applicable`，不能只有一个总分。

- 换成另一张 current result 后，旧检查仍绑定旧资产，不自动继承。
- 局部修后的新资产需要重新检查受影响维度。
- 检查失败不删除资产，不自动阻止下载；导出完整套图前给出缺失/失败提示。
- 第一版不把检查结论发送给模型做自动训练或微调。

### 22.4 固定质量样本

阶段六建立版本化样本集，至少覆盖：

- 有 logo/包装文字的规则盒装商品。
- 透明、反光或金属材质商品。
- 软质或不规则轮廓商品。
- 多颜色 SKU 中颜色容易漂移的商品。
- 信息图文字 required 与摄影图文字 none。
- 一张清晰参考和多张角度参考。

每个样本固定商品事实、参考、图种、期望禁区和人工评分表。记录 provider/model/effective config，不用不同模型结果混成同一基线。

### 22.5 保真失败的产品响应

- 外形/颜色失败：建议提高保真、补参考、减少创意幅度或改 precise 场景，不自动宣称已修复。
- logo/文字失败：允许局部修或改为无文字生成后确定性叠字；第一版不承诺所有模型准确写字。
- 多次失败：保留结果和尝试，允许用户换 provider/模型；不无限自动重试消耗费用。
- 结果与输入完全无关：标记 provider/result anomaly，保留有限诊断信息。

---

## 23. 技术栈与运行单元

当前七个运行单元，第一版不新增：

1. React / Vite Web
2. FastAPI 业务 API
3. Dramatiq worker
4. PostgreSQL async dispatcher
5. Node.js 22 + Pi Agent service
6. PostgreSQL
7. Redis 与媒体 storage

分层：`presentation` → `application` → `domain` / `infrastructure`。路由不组织复杂事务。浏览器只打 Web 和 FastAPI。Agent service 用独立 token 打 internal API。

Python 迁 Go、SaaS 租户计费不在第一版范围，见 ROADMAP。

### 23.1 运行单元故障边界

| 故障 | 允许退化 | 不允许 |
|---|---|---|
| Agent service 不可用 | 商品/画布/图库继续，Turn 保持 queued 或明确失败 | 阻塞用户直接编辑和运行 |
| Redis/worker 不可用 | 读取业务事实；新异步任务明确排队/失败 | API 返回成功但任务未持久化 |
| provider 不可用 | 保存图和配置；显示可重试错误 | 自动换模型导致结果不可解释 |
| storage 读失败 | 显示资产错误和诊断；数据库引用保留 | 删除引用或返回空白成功图 |
| PostgreSQL 不可用 | 停止业务写入 | 用浏览器/Redis/session 文件继续当权威 |
| SSE 断开 | 按 cursor 重连或回源查询 | 把断线当 Turn/Run 失败 |

### 23.2 配置与部署

- `.env`/runtime DB settings 的所有权保持现有合同；需求书不引入第二套配置中心。
- Docker Compose 和本地 `just dev` 使用同一 schema/migration 路径。
- 启动前应用 Alembic；失败时服务不得在半迁移 schema 上继续接受写入。
- media storage 与数据库分别备份，但恢复演练必须验证引用一致。
- Agent session/event 文件可备份用于交互历史，不替代 PostgreSQL 业务恢复。

### 23.3 安全

- 管理员登录、设置解锁和 Agent internal token 职责分离。
- 所有资产/graph/run/recipe 写入重新验证 scope 和 ownership。
- 上传文件按解码后的真实格式处理，文件名不决定 MIME；限制字节、像素和解压尺寸。
- provider base URL 需要 SSRF 边界；不允许通过设置访问未批准的本地/metadata 地址。
- Markdown/Agent 文本按不可信内容渲染，禁止任意 HTML/脚本。
- 下载文件名安全化，storage path 不暴露给浏览器。
- 日志、SSE、错误、测试快照和导出 manifest 不包含 API key、access key、session secret。

### 23.4 数据迁移和兼容

- schema 变化使用 Alembic revision 和 migration test，不使用 ORM AutoMigrate。
- persisted enum/JSON 变化前搜索所有 reader/writer、历史值和导出工具。
- 不为新功能恢复在线 V1/V2 fallback。旧数据只通过有界 archive/backfill/retirement owner 读取。
- additive migration 先落 reader/writer，再经部署证据清理旧列/表；清理需要独立批准。
- API version 号与 workflow schema version 分开，`/api/v2/...` 不代表在线 V2 graph。

---

## 24. 代码所有权（实现时必须落在现有边界）

| 能力 | 主要所有权 |
|---|---|
| Agent 创建与 intake | `application/agent/product_workspaces.py`、`application/product_intake.py` |
| Draft 与落图 | `workflow_drafts/service.py`（GET 与 409 写入） |
| 图命令与执行 | `domain/graph_catalog.py`、`graph_rules.py`、`product_workflow/graph_*.py` |
| 配方 | `workflow_recipes/` |
| 交付图 | `delivery_renditions/` |
| 商品图片 | `product_images/`、`media_objects.py` |
| 连续生图 | `image_sessions/` |
| 局部修（待交付） | 复用 `image_sessions` + `product_images` lineage，不新开平行媒体模型 |
| 官方配方（不交付） | 历史 seed 归档；在线配方库只列出用户保存的配方 |
| 镜头列表（待交付） | `web/src/pages/workbench/` 对已有 group 的投影 |
| 前端画布 | `pages/workbench/canvas/`、`chrome/` |
| 前端 Agent | `pages/workbench/agent/` |
| HTTP | `web/src/lib/api.ts` |

依赖方向：`agent -> canvas, chrome`；`canvas -> chrome`；`chrome` 不得引用 agent 或 canvas。

---

## 25. 核心对象

业务对象保持现有名称，不因本文新造平行模型。

| 对象 | 业务职责 | 身份与生命周期 | 主要关系和硬约束 |
|---|---|---|---|
| `Product` | 一件商品的长期生产单元 | 创建后保持稳定身份；归档不等于删除 | 拥有商品图片、正式图、运行、Agent 会话和导出记录；不能用会话代替商品 |
| `MediaObject` | 媒体 bytes、MIME、尺寸、存储位置等底层事实 | 内容写入后不可原地篡改；派生结果创建新对象 | 不直接表达「这是商品参考」或「这是当前结果」等业务角色 |
| `ProductImageAsset` | 商品范围内的图片资产和角色 | 可归档；局部修、重跑、导出均产生新资产或 rendition | 指向 `MediaObject`；通过 lineage 关联父资产；原图不能被修后图覆盖 |
| `MediaLibraryAsset` | 全局素材库中的可复用资产 | 独立于单商品存在；跨商品使用需显式绑定 | 不能因被某个 workflow 引用而转移所有权 |
| `WorkflowMediaLibraryAsset` | workflow 对图库资产的有界绑定 | 随 workflow revision 读取；解除绑定不删除源媒体 | Graph Compiler 只能得到明确绑定且沿入边可达的参考 |
| `WorkflowDraft` | Agent 提议与正式图之间的可确认容器 | draft、ready、confirmed、rejected/expired；确认后不可继续写原 revision | 未确认不能产生 live graph；确认需带明确 revision 和幂等身份 |
| `WorkflowDraftRevision` | 某一时刻完整、可比较的草案 | 单调递增、内容不可变 | 确认的是 revision，不是「当前最新」这样的漂移指针 |
| `WorkflowGraph` | 商品当前正式生产结构 | 一件商品至多一个在线权威图；结构变化形成 revision | 六类节点、边和 group 均受 catalog/rules 约束；不存在浏览器私有平行图 |
| `Node` | 一个事实、参考、内容、视觉或生图职责 | 稳定 id；config 通过命令修改 | 执行所需语义在节点；不得把关键配置藏到侧栏本地状态 |
| `Edge` | 节点间有方向、有 handle 的数据关系 | 连接/断开均通过 Graph Command | 只有 catalog 允许的端口和类型可连接；断边后事实不得继续泄漏到目标节点 |
| `Group` | 镜头/图片类型的结构分组 | 是 graph 内结构，不是第二套 Shot 实体 | 镜头列表只投影 group；组内运行仍归属同一 graph/run 模型 |
| `WorkflowGraphRun` | 一次整图或有界子图执行 | queued、running、terminal；不可用前端动画推断终态 | 固定读取一个 graph revision；包含多个 `NodeRun` |
| `NodeRun` | 节点在某次 run 中的执行事实 | 支持 attempt；成功、失败、取消和 unknown 可观察 | 重试创建新 attempt，不改写失败历史；effect 身份必须稳定 |
| `Artifact` | 节点执行产生的结构化或媒体输出引用 | 随执行追加，不原地伪装成另一结果 | 成功 artifact 经明确 adoption 才能成为节点当前资产 |
| `WorkflowRecipe` | 可复用结构的稳定身份 | 用户从 live graph 保存；可归档 | 不包含商品 id、媒体 bytes、运行结果和 provider secret |
| `WorkflowRecipeVersion` | 一版不可变 recipe payload | 发布后不可变；修改产生新版本 | apply 必须记录采用版本，以便复现和审计 |
| `AgentSession` / `AgentConversation` | 商品范围内的交互上下文与对话导航 | 可恢复但不是业务权威 | 关联商品、task/turn；不能覆盖正式图或运行状态 |
| `AgentTask` | 一个有边界的 Agent 工作目标 | pending、running、waiting、terminal/unknown | 多 task 可属同一 session；每个 task 独立执行和恢复 |
| `AgentTurnProjection` | 面向 UI 的一次交互投影 | 可由持久事实重建 | 只投影问题、提议、effect 与结果；SSE 丢失不能改变真实状态 |
| `ImageSession` | 围绕某个意图连续探索多个候选结果 | 保留分支和候选，不自动替换正式结果 | 保存回商品库或采用到节点均需显式动作 |
| `DeliveryRenditionJob` | 从已有成功资产生成确定性交付文件 | 可重试、可复现；不调用 image generation provider | 输入为源资产 + `DeliverySpec`；输出不得成为新的创作源事实 |
| `ProviderProfile` | 某个外部或自托管 provider 的连接配置 | secret 不回显；禁用不删除历史执行证据 | 与具体用途分离，不把 provider 名写入业务对象身份 |
| `ProviderBinding` | prompt、agent、image 等用途到 profile/model 的选择 | 可变配置；运行时解析出 effective binding | 历史 run 记录当时的 effective 参数，不随设置页修改而漂移 |

### 25.1 三类规格不得混写

- `GenerationSpec` 描述创作意图：主体、构图、背景、文字策略、参考角色、输出候选数等。它应当可被不同 image provider 解释。
- provider-effective request 描述本次实际调用：provider、model、采样参数、尺寸映射、能力降级和请求身份。它是运行证据，不回写覆盖创作意图。
- `DeliverySpec` 描述确定性衍生：目标尺寸、比例、格式、质量、背景/留白、命名和打包。修改它不重跑生图。
- 三者可相互引用但不能共享一个任意 JSON 大包。字段新增必须明确 owner、schema version、reader/writer 和迁移策略。

### 25.2 权威、投影与缓存

| 信息 | 权威来源 | 允许的投影 | 禁止成为权威的副本 |
|---|---|---|---|
| 商品事实 | `Product` 与正式事实节点 | Agent 摘要、检查器表单 | 会话文件、浏览器草稿文本 |
| 图结构 | `WorkflowGraph` 当前 revision | 镜头列表、React Flow、运行 HUD | shot side-state、localStorage graph |
| 运行终态 | Run / NodeRun 持久记录 | SSE、卡片徽标、详情时间线 | toast、前端定时器、worker 内存 |
| 当前结果 | 节点结果引用和 adoption 记录 | 节点预览、镜头封面、图库角色 | 最后一次收到的 SSE payload |
| 配方内容 | `WorkflowRecipeVersion` | 配方卡和预览 diff | 应用后的临时前端 graph |
| provider 选择 | `ProviderBinding` + run effective snapshot | 设置页、运行详情 | 组件内默认 model 常量 |

缓存只能加速读取。任何缓存丢失后，都必须能从上述权威重建用户可见状态。

---

## 26. 模块职责（产品语义）

### Node Catalog

工作流连接规则和可编辑 config 的单一来源。实际炮弹（执行）和预测（检查器表单、连线高亮）都必须读它。

- 输入：节点类型、端口、config schema、候选连接上下文。
- 输出：允许的字段、默认值、端口兼容性、执行能力声明。
- 失败：未知节点类型、未知字段、非法 handle 或类型不兼容必须给结构化错误。
- 禁止：前端和 backend 分别维护无法比较的连接白名单；用运行失败代替创建时可判定的非法边验证。

### Graph Command / ChangeSet

结构与 config 的唯一写入口。Agent 立即写入只接受一条 operation。多节点重构走未应用 GraphProposal。

- 输入：workflow id、expected revision、operation、actor、idempotency key。
- 输出：新 revision、归一化 change summary、受影响对象和可重放事件。
- 冲突：expected revision 落后时返回当前 revision 与可理解的冲突，不静默覆盖。
- 原子性：一个命令全部成功或全部失败；多 operation proposal 经用户确认后仍在一个服务端事务提交。
- 禁止：route、Agent adapter、recipe service 或浏览器绕过命令层直接改 nodes/edges JSON。

### Graph Compiler

只编译目标节点入边得到的 facts、references、briefs、visual guidance。这是运行输入的单一来源。

- 输入：固定 graph revision、目标节点集合、沿入边可达的已解析上游 artifact。
- 输出：版本化 execution input，包含来源节点/边和 reference role，便于审计。
- 失败：缺必需身份参考、上游未成功、输入 schema 不兼容时，在 effect 前失败并指出责任节点。
- 隔离：不扫描商品全部图库、不读取断开的旧边、不从 Agent 对话补隐式 prompt。
- 确定性：同一 graph revision 与同一上游 artifact 集合应得到相同编译结果；provider 请求时间戳等运行字段不进入编译身份。

### WorkflowGraphRun

业务执行记录的单一来源。页面按钮和 Agent 待确认请求最终都进这里。

- 输入：workflow id、revision、目标 scope（整图/group/node）、触发者和 idempotency key。
- 输出：Run、NodeRun DAG、调度记录、attempt、artifact 与 terminal summary。
- 调度：只运行目标闭包中需要且可运行的节点；已有可复用上游结果时按合同决定复用，不靠前端跳节点。
- 恢复：API/worker/Redis 重启后根据数据库事实识别 pending、running、unknown 和可重试 effect。
- 禁止：创建另一种「镜头任务」执行模型；镜头运行仍是 group scope 的 WorkflowGraphRun。

### WorkflowDraft

Agent 与正式图之间的确认边界。浏览器动画或 SSE 断开不得留下半张图。

- 输入：商品事实、参考绑定、套图计划、用户补充约束和 Agent 结构化输出。
- 输出：不可变 revision、验证结果、面向人的摘要与可确认 payload。
- 确认：验证 revision、商品归属和幂等身份后一次落完整图；重复确认返回同一结果。
- 失败：草案不完整停在可修正状态；确认事务失败时 live graph 不出现任何部分对象。
- 禁止：Agent 在正式图已存在后重新走 draft-confirm 覆盖现图；后续修改必须走 GraphProposal/Command。

### Recipe apply

配方落到 live graph 的唯一写入口，走 Graph Command。禁止配方直接写 storage 或复制媒体。

- 输入：recipe version、目标商品/workflow、apply mode、expected graph revision、显式参数绑定。
- 预览：服务端计算将新增、修改、保留、冲突的节点/边/group，并标识缺失参考或必填变量。
- 输出：确认后的单次 ChangeSet 与采用记录；预览本身没有 effect。
- 失败：完整配方不能合并、节点身份冲突、必填绑定缺失时不产生部分结构。
- 隔离：payload 和 apply 结果审计不得出现来源商品 id、来源资产 id、媒体 bytes 或历史 artifact。

### Media 与资产采用

- 上传先创建底层媒体事实，再在事务内建立业务资产与角色；校验失败不能留下可见半资产。
- provider 输出先作为 attempt artifact 持久化；只有成功且通过 adoption 条件后，才更新节点当前结果。
- 局部修、连续生图、重跑和导入均走同一资产谱系规则，不各自维护 current image 字段。
- 取消或失败后的迟到结果可保留为未采用 artifact，但不能反向覆盖较新的当前结果。
- 删除请求先检查 graph、recipe apply 记录、lineage 和 rendition 引用；不允许制造悬空引用。

### Agent adapter

- 将 ProductFlow 的有界上下文、工具合同和确认请求交给 Pi Agent；不把整个数据库或图库塞入上下文。
- tool effect 必须携带 task/turn、actor、expected revision 和幂等身份；读工具与写工具在 UI 中可区分。
- adapter 可以断线和重启；产品 task、turn projection、proposal 与 effect 结果必须可从业务持久层恢复。
- Agent 无权把自然语言当作已确认结构变更。涉及正式图、运行、采用和删除的 effect 均服从本文确认矩阵。

### Delivery rendition

- 只接受已经持久化且用户有权访问的源资产；外链或临时 provider URL 必须先归档为媒体事实。
- 同一源内容哈希与归一化 DeliverySpec 可复用成功 rendition，失败重试不产生语义重复记录。
- 颜色空间、EXIF 方向、透明通道、放大/裁切策略必须显式；不允许静默拉伸商品。
- 导出打包记录清单和 checksum，使用户能确认缺图、重复文件和命名冲突。

---

## 27. 性能与运行质量

- 单次套图 Run 的节点数随计划图走，总数受 30 张计划上限约束。
- 并发生图受设置页限制，不在前端另做一套队列。
- SSE 断线可重连；Turn 未知时保留 `unknown`，不猜终态。
- 同一幂等键只有请求哈希相同时才能复用结果。
- HUD / 检查器不每帧重建大 DOM。画布交互跟现有 chrome 合同。
- 媒体不在浏览器组装 graph。落图在服务端事务完成后再读。

### 27.1 性能预算

第一版先建立测量，目标在固定开发数据集上验收：

| 操作 | 目标 |
|---|---|
| 商品列表/工作台首个可操作内容 | 本地开发环境 warm load 不超过 2 秒；单独记录 API 与渲染 |
| 镜头/画布切换 | 不重新拉取或复制完整 graph；主线程无明显长任务 |
| 普通 Graph Command | 不含网络异常时 API p95 建立基线并持续回归；前端 optimistic 状态可回滚 |
| 100 节点级画布交互 | 拖动/平移不因状态投影持续触发全树重渲染 |
| 图库分页 | 不一次加载全库元数据或原图 bytes |
| 上传/下载 | 流式或有界内存处理，不把多张大图全部复制进进程内存 |

具体毫秒阈值由阶段二实测写入性能 gate；没有测量前不编造硬指标。provider 模型耗时与 ProductFlow 自身耗时分开。

### 27.2 可靠性

- 所有外部 effect 有稳定 attempt/idempotency/fencing 身份。
- 数据库 commit 成功但消息 publish 失败时，async dispatcher 能从数据库事实恢复投递。
- consumer 重复投递不会重复采用结果或破坏 current asset。
- 失败状态保留安全错误、可重试判断和最后更新时间。
- worker/Agent/service restart 的验收必须使用真实 PostgreSQL/Redis 进程，不只用内存替身。

### 27.3 可观测性

日志和指标至少能按以下 id 关联：product、conversation/turn、workflow/revision、run/node run/attempt、asset/rendition、dispatch、provider request。日志使用结构化字段，不在 message 中拼完整 payload。

关键指标：

- Turn/Run/NodeRun 排队、运行、成功、失败、unknown、取消数量和时长。
- provider 按用途/模型的请求数、耗时、限流、拒绝和 unknown。
- async dispatch pending/stale/retry。
- 上传/解码/storage/rendition 失败。
- Graph Command 冲突、非法边和 recipe apply 冲突。

### 27.4 兼容性与浏览器

- 第一版验证当前 Chrome/Chromium 桌面与 390px mobile emulation；其他浏览器不做无证据承诺。
- reduced-motion、亮/暗色、键盘路径与触屏模式纳入前端 gate。
- Vitest 的 node environment 不能证明滚动、portal、drag、viewport 和 canvas 几何；这些必须用真实浏览器。
- build 通过不证明 CSS/asset 渲染正确；关键页面保留截图、尺寸和 console/network 证据。

### 27.5 隐私与数据保留

- 用户参考图、生成图和 prompt 可能是商业敏感数据；provider 调用前明确将发送哪些输入。
- 不向未绑定 provider 或第三方分析服务发送媒体。
- 第一版不自动训练用户数据，不跨商品把未授权资产当参考。
- retention/hard delete 尚未设计前不提供“永久删除”承诺。
- 部署证据、数据库备份和 provider 日志不进入 Git。

---

## 28. 开发阶段

阶段可以并行到文件所有权不冲突为止。每个阶段结束时仓库必须可运行。

### 28.1 总体顺序和门禁

| 阶段 | 依赖 | 主要交付 | 不得夹带 | 退出证据 |
|---|---|---|---|---|
| 一：基线 | 当前 live truth | 文档所有权与路线图 | 提前声明未实现能力 | docs-check、索引和当前/目标用词审查 |
| 二：套图与镜头 | schema-v3 graph/group/run | 默认生产视图与有界运行 | 新 Shot 表、新执行器 | API/test/build/真实浏览器镜头与画布一致性 |
| 三：官方配方 | 不交付 | 配方库只保留用户保存内容 | 画布内预置模板 | 在线列表不含 official seed |
| 四：局部修 | asset lineage/current adoption | 结果级修图闭环 | 原地覆盖媒体、隐式采用 | provider capability、失败、迟到结果和谱系测试 |
| 五：交付预设 | delivery rendition | 确定性导出 | 重新生图、平台合规承诺 | 像素尺寸/格式/checksum/不调用模型证据 |
| 六：质量与保真 | 前述生产闭环 | 固定样本与可比较基线 | 无口径分数、营销结论 | 样本记录、人工清单、耗时拆分和失败复盘 |
| 七：画布证据 | 工作台共享投影 | 浏览器级几何与交互验收 | 第二套节点卡/状态源 | desktop/mobile 截图、像素/console/network 证据 |

跨阶段规则：

- 数据库迁移、共享 DTO、route 或 graph schema 的前置改动，只随第一个真实使用者交付，不提前建设空壳。
- 每阶段先记录变更前基线，再做实现和 focused test，最后跑受影响包的集成 gate。没有基线时无法证明没有回归。
- 一个阶段未通过退出门禁，后续阶段可做不共享合同的探索，但不得合并依赖该阶段的产品声明。
- 任何阶段如果发现现有权威模型不足，先写清故障模式、reader/writer 和迁移边界；不能以「未来扩展」为理由加表或状态。
- 功能落地时同步更新当前事实所有者。本文继续保留目标与验收，不复制 route 清单或实现细节。

### 28.2 每阶段统一交付包

每个阶段至少留下以下证据，文件位置服从仓库现有测试和 rollout 约定：

1. 用户可见行为和明确不做的范围。
2. 真实调用链：入口、wire schema、application use case、持久化/外部 effect、响应和前端投影。
3. 受影响的所有 reader/writer 搜索结果，尤其是 enum、JSON、route、provider 和共享 UI projection。
4. focused regression tests；跨层改动另有 API/DB/浏览器证据。
5. migration 的 upgrade、历史数据读取和必要时 downgrade/forward-fix 说明。
6. 配置、secret、部署或 provider 能力变化。
7. 当前限制和下一阶段不能假定已经存在的能力。
8. PRD、CONTEXT、ARCHITECTURE、USER_GUIDE/HelpPage 的同步结果。

### 阶段一：总基线对齐

进入条件：当前 checkout、文档地图、ROADMAP 和已有实现已核对；Go 重写规格作为下一步资料保留，不混入本轮 Python 产品实现。

完成：

- 本文作为 Draft spec 进入 `docs/specs/`。
- 已交付能力继续以 PRD / CONTEXT / ARCHITECTURE 为准。
- 待交付项列入 ROADMAP 指向本文。

验收：

- 文档地图能找到本文。
- PRD 没有提前写入未实现的镜头列表、官方配方、局部修、平台预设。
- 删除的旧稿在 active docs 无悬空链接；需要保留的独立历史审计进入 archive 并从默认导航移除。
- `docs/specs/go-backend-rewrite-prd.md` 与 `docs/specs/go-backend-rewrite-design.md` 保持可索引且内容未因本轮整理被改写。

### 阶段二：生成套图 + 镜头列表

进入条件：group、run scope、NodeRun 状态和当前结果的后端合同已有真实测试；镜头行能完全由 graph projection 得出。

完成：

- 工作台默认镜头列表，可切画布。
- 「生成套图」跑现有整图 DAG，进度按镜头展示。
- 运行此镜头保持现有 group 语义。
- 创建页「推荐套图」快捷勾选。

验收：

- 直接创建推荐套图后，不打开画布也能看到四个镜头行。
- 点生成套图后，视觉/创作/各镜头按依赖推进。
- 单镜头失败，其他镜头仍可重试。
- 切到画布看到的分组与镜头行一一对应。
- 1440 与 390 宽度下镜头列表可完成：运行、打开详情、切画布。
- 页面刷新或 SSE 断线后，镜头状态从 Run/NodeRun 恢复，不重置为待运行。
- 两个浏览器标签同时修改 graph 时，落后 revision 的操作得到冲突，不覆盖新结构。
- 退出门禁：focused backend tests、前端 test/lint/build、desktop/mobile Playwright；真实 provider live gate 仍保持 opt-in。

### 阶段三：官方场景配方

不交付。配方库只列出用户主动保存的配方。

### 阶段四：出图后局部修

进入条件：provider capability 能区分生成与编辑；资产 lineage、attempt 和 current adoption 已有单一 owner。

完成：

- 四类操作入口。
- 新资产谱系。
- 「用作当前结果」更新节点当前资产。

验收：

- 原图仍在图库。
- 节点当前预览换成修后图。
- 无 edit 能力的 provider 给出可见失败。
- 修失败不改节点当前资产。
- 用户取消后到达的迟到成功结果不会覆盖当前资产，可在运行详情中审计。
- 对同一编辑请求重复提交只产生一个语义 effect；请求内容不同却复用幂等键时明确拒绝。
- 退出门禁：至少一个真实 edit-capable provider 的 opt-in 证据，以及 mock 下的能力缺失、超时、失败、取消、迟到和重复投递测试。

### 阶段五：平台交付预设

进入条件：源资产方向/尺寸/MIME 可验证；rendition job 已与创作 run 分开。

完成：

- 预置 DeliverySpec 模板。
- 创建时可带默认预设。
- 导出走 rendition，不重跑生图。

验收：

- 改预设后源图不变。
- 导出尺寸与预设一致。
- 自定义宽高仍可用。
- 透明图、EXIF 旋转图、超长边图和小于目标尺寸的图按明确策略处理，无静默拉伸。
- 同名文件在导出包中按稳定规则去重，manifest 能对应回商品、镜头、源资产和 DeliverySpec。
- 退出门禁：像素级尺寸/格式测试、内存边界测试、重复导出幂等测试，并证明 image provider 未被调用。

### 阶段六：Agent 创建质量与保真清单

进入条件：固定代表性商品样本已获授权，可长期用于回归；三类 provider binding 可记录 effective 参数。

完成：

- 结果页人工保真清单。
- 确认面板密度与冲突反馈改进（与 ROADMAP §1 同一验收）。
- Agent 对话创建路径至少有一条可重复的质量样本。

验收：

- 用户能在确认面板改冲突后再确认。
- 生图结果上能勾外形/颜色/文字核对。
- 跳过 Agent 的 live graph 路径保持 `just web-e2e-live-graph` 可跑。
- 每个样本保留输入版本、graph revision、provider/model、结果资产、人工判定和修改次数，避免只留精选截图。
- 同一失败可区分 ProductFlow 编排、provider 排队/生成、输入不充分和人工审美不通过。
- 退出门禁：至少覆盖高反光、带文字包装、透明/半透明、软质形变和普通硬质商品中的代表样本；数量以真实可维护性为准，不编造统计显著性。

### 阶段七：画布/侧栏浏览器证据

进入条件：阶段二共享 chrome 和投影合同稳定；待验收交互在对应 restoration 文档中有明确 owner。

完成：

- `v3-canvas-restoration` 切片 G 与侧栏 S1–S4 的浏览器证据。
- 与阶段二的镜头列表共用同一 chrome，不长第二套节点卡。

验收以那两份北极星为准，不在本文另定一套画布质量。

补充退出门禁：

- 1440px、窄桌面和 390px 视口均检查固定 toolbar、dock、inspector、modal/portal、滚动和文本溢出。
- 画布非空、节点可见且 fit-view 正确；不能只证明 DOM 存在。
- 捕获 console error、failed network、截图和关键元素 bounding box。hover、drag、outside-click 和键盘路径由真实事件验证。
- 阶段二镜头列表与阶段七画布使用同一运行和当前结果事实；切换视图不会触发结构复制或丢失未提交表单警告。

---

## 29. 验收标准

以下验收同时覆盖正向结果、故障行为和可追溯证据。「按钮可点」「接口 200」「模型返回图片」都不能单独判定完成。

### 创建验收

- 名称可进入 Agent 创建。
- 直接创建在提交参考和图种后得到可运行图。
- 参考图 1–6，格式限 PNG/JPEG/WebP。
- 图种数量 1–6，总计划 ≤ 30。
- 证据图不创建生图节点。

### 确认与权威验收

- 未确认 Draft 不会出现 live graph。
- 确认明确 revision 后一次出现完整图。
- live graph 之后 Agent 不能覆盖现图。
- 断参考边后该生图节点不能再拿到那张图。

### 套图验收

- 推荐套图可一键勾选。
- 生成套图按镜头给出进度。
- 运行此镜头只影响该组生图。
- 重跑更新当前资产，旧图留在库里。

### 配方验收

- 用户可从全图/分组/选区保存配方。
- 官方四个配方可预览和应用。
- 完整配方不能合并进已有 live graph。
- 应用后的图不含上一件商品的像素。

### 局部修验收

- 四种操作可从结果进入。
- 谱系可追溯到源资产。
- 可选替换节点当前结果。

### 交付验收

- 改交付预设不调用 image 模型。
- 导出文件尺寸符合预设。

### 工作台验收

- 镜头视图与画布视图是同一张图。
- 失败原因在卡片和详情可见，可重试。
- 常规界面不出现内部 id。

### 竞争力验收

- 固定样本中，一件商品从参考和计划就绪到首套图完成，能够分别导出 ProductFlow 编排耗时、provider 排队耗时和 provider 生成耗时。
- 用户不打开画布也能完成推荐套图、生成、查看失败、重试单镜头、局部修和按预设导出。
- 用户切到画布后看到的 group、节点、边、运行状态和当前结果与镜头视图一一对应；任一视图修改后另一视图立即读取同一事实。
- 第二件商品应用第一件商品保存的配方后，引用审计证明没有复制上一件商品的 id、绑定资产、媒体字节或生成结果。
- 局部修产生新资产并保留源资产；修复失败、页面刷新或 SSE 重连不会改变节点原有当前结果。
- Agent 对同一任务的 Draft/GraphProposal 有明确 revision、差异和确认动作；拒绝或冲突不会留下部分图变更。
- 替换 prompt、agent 或 image provider 后，商品、workflow、recipe、run 和资产身份保持不变，provider-effective 参数仍可观察。
- 第一版固定样本完成后记录首套图时间、人工保真结果、修改次数、第二商品复用步骤数和失败恢复结果；没有这些基线，不宣称优于竞品。

### 29.1 端到端脚本 A：新商品首套图

前置：三用途 provider 已通过设置验证；测试账号没有同名商品；准备 2–4 张合法参考，其中一张清楚显示商品身份。

1. 创建商品，输入名称、类型和参考图，选择首屏、细节、场景、卖点各一张。
2. 走 Agent 路径，回答一个会影响结果的问题；检查 Draft revision、参考角色和四个镜头摘要。
3. 修改一个镜头提示词并确认明确 revision。
4. 验证 live graph 一次出现，包含共享事实/参考/内容节点和四个 group；没有部分草案残留。
5. 在镜头列表运行套图，观察内容依赖完成后四个镜头独立推进。
6. 人为让一个镜头 provider 失败，确认其他成功结果仍可查看；只重试失败镜头。
7. 刷新页面并断开/恢复 SSE，确认状态、attempt、当前结果和错误仍一致。
8. 对一个成功结果做局部修，比较源图、修后图、lineage 和当前采用记录。
9. 选择一个 DeliverySpec 导出，核对 manifest、文件尺寸、格式和命名。
10. 将完整结构保存为用户配方，记录 recipe version。

通过条件：用户全程可以停在镜头视图；画布打开后展示同一 group、状态和结果；所有 effect 可由 product/run/node run/asset id 串联。

### 29.2 端到端脚本 B：第二商品复用

前置：脚本 A 的 recipe 已存在；创建不同商品并上传完全不同的身份参考。

1. 选择脚本 A 的 recipe version，预览完整应用。
2. 检查预览明确列出将创建的结构、待绑定参数和冲突；预览不创建 graph 对象。
3. 确认后检查新 graph 的节点类型、group 和通用 config 与 recipe 一致。
4. 搜索 payload、数据库关系、编译输入和 provider 请求，确认没有第一件商品 id、资产 id、媒体 URL/bytes 或 artifact。
5. 运行一个镜头，确认 Graph Compiler 只使用第二商品沿入边绑定的参考。
6. 修改第二商品的节点 config，确认第一商品 graph 和 recipe version 均不变化。
7. 将第二商品结构另存为 recipe 新版本或新配方，确认版本归属清晰。

通过条件：复用减少结构搭建步骤，同时保持商品身份、像素、运行历史和当前结果完全隔离。

### 29.3 端到端脚本 C：恢复与并发冲突

1. 创建一个包含至少两个可并行生图节点的 run，在一个节点执行中重启 worker。
2. 验证 dispatcher/worker 根据数据库事实恢复；无法确认外部 effect 结果时状态为 `unknown`，不自动宣称失败或成功。
3. 用相同幂等键和相同请求重放，确认返回相同语义结果；用相同键和不同请求哈希，确认拒绝。
4. 在两个浏览器标签读取同一 graph revision。标签 A 改 config 成功后，标签 B 提交旧 revision。
5. 标签 B 收到冲突和当前 revision；用户可重新加载/重新应用自己的修改，标签 A 的结果不被覆盖。
6. 在局部修运行中取消请求并模拟迟到成功；确认迟到 artifact 可审计但不采用。
7. 重启 API 并刷新页面，确认 Run、NodeRun attempt、Agent proposal、当前资产和冲突结果可恢复。

通过条件：没有重复 effect、部分 graph、幽灵运行或旧结果覆盖；所有未知状态都对用户诚实可见并有下一步动作。

### 29.4 负向与边界验收

| 场景 | 预期行为 | 必须证明的不变量 |
|---|---|---|
| 0 张身份参考运行摄影节点 | effect 前阻止并指向缺失输入 | provider 未调用、Run/NodeRun 状态可解释 |
| 7 张参考或非法 MIME | wire/application 边界拒绝 | storage 无可见孤儿、错误不泄漏内部路径 |
| 总计划超过 30 | 创建或修改计划时拒绝 | graph 没有部分新增 group/node |
| 非法 edge handle | Graph Command 返回 catalog 错误 | revision 不增加、浏览器回滚 optimistic edge |
| 确认过期 Draft revision | 返回 revision 冲突 | live graph 不出现或保持原图 |
| recipe 缺必填绑定 | preview 标红且禁止 apply | graph、媒体和采用记录均不变化 |
| provider 无 edit capability | 局部修入口禁用或提交时结构化失败 | 原 current asset 不变化 |
| provider 限流/超时 | 归一化为可理解错误并按策略重试 | attempt 有界、不会无限重放 |
| 上传解码炸弹/超大图 | 有界读取并拒绝 | 进程内存受控、临时文件被清理 |
| rendition 目标小于主体安全区 | 按显式 fit/crop 策略预览或拒绝 | 不静默拉伸、源资产不变 |
| 用户归档被引用资产 | 明确提示引用或仅隐藏 | graph/lineage 不悬空 |
| SSE 消息乱序/重复 | projection 按版本/序号归并 | terminal 状态不倒退、结果不重复采用 |
| Agent 输出未知字段 | schema 验证并请求修正 | 未验证内容不进入 Draft/live graph |
| secret/API key 读取 | 仅显示掩码和验证状态 | API、日志、前端 bundle 不含明文 |

### 29.5 测试层级与证据

| 风险 | 最低测试层级 | 证据 |
|---|---|---|
| 纯 domain 规则 | 单元/参数化测试 | 节点连接、计划上限、状态转移、规格归一化 |
| wire 与 application 合同 | route/use-case 集成测试 | 错误码、revision、幂等、事务回滚 |
| PostgreSQL 持久化与迁移 | 真实数据库测试 | upgrade、历史值读取、约束、并发冲突 |
| Redis/dispatcher/worker 恢复 | 真实进程或专用 live gate | commit/publish 间隙、重复投递、restart/unknown |
| provider adapter | contract test + opt-in live test | capability、effective request、错误归一化、真实响应 |
| 前端状态投影 | Vitest/组件测试 | reducer、API mapping、冲突和错误状态 |
| portal/scroll/drag/canvas/viewport | Playwright + Chromium | 截图、bounding box、console/network、canvas pixels |
| 商品保真与竞争基线 | 固定人工样本 | 输入、输出、模型、时间拆分、清单、修改次数 |

默认测试不应依赖付费 provider。真实 provider gate 必须 opt-in、明确费用/secret，并保留可核对的运行记录。mock 成功只能证明 ProductFlow 合同，不能证明某模型当前支持能力或输出质量。

### 29.6 需求追踪矩阵

实现阶段为每项填写真实 owner、测试文件和证据链接；尚未实现时保持 `待交付`，不得填计划路径冒充完成。

| 需求族 | 本文条款 | 当前/目标权威 | 完成证据 |
|---|---|---|---|
| 商品与参考 | §8–9、§22 | `CONTEXT.md` / `PRD.md` | backend workflow tests + 固定保真样本 |
| Draft 确认 | §11、§25–26 | ADR 0008 / workflow drafts | revision/transaction/idempotency tests |
| graph 与镜头 | §10、§12–13 | schema-v3 graph；镜头待交付 | graph tests + desktop/mobile browser evidence |
| Agent 协作 | §11、§19、§26 | product Agent application + Pi adapter | turn/proposal/effect/recovery tests |
| 资产与图库 | §14–15、§25–26 | media/product images | lineage/adoption/delete/reference audit |
| 配方 | §16 | workflow recipes | payload isolation + preview/apply conflict tests |
| 局部修 | §17 | 待交付，复用 image session/lineage | capability/attempt/迟到结果/live provider evidence |
| 交付 | §18 | delivery renditions | deterministic pixel/format/manifest tests |
| Provider | §20 | settings/provider infrastructure | binding/effective request/secret/error contract tests |
| 运行质量 | §23、§27 | runtime/operations | restart/dispatch/metrics/security/browser gates |

### 29.7 发布判定

满足以下条件才能把对应能力从本文「待交付」迁入当前事实文档：

- user-visible path、API、持久化和恢复路径都已实现，不依赖开发者手工改库。
- 正向与对应负向验收通过；共享合同有迁移和历史 reader 证据。
- 文档、HelpPage/USER_GUIDE、配置示例和错误文案与当前行为一致。
- 默认 gate 全绿；需要真实 provider/浏览器的 opt-in 证据已记录，未运行项明确标注。
- 没有用 fallback 复活 V1/V2，没有遗留临时双写、隐藏 feature switch 或第二权威。
- 性能、安全、可访问性或质量基线未达目标时，发布说明直接列限制，不用「基本完成」覆盖缺口。

---

## 30. AI 编码规则

交给编码 Agent 时必须遵守：

- 每次只实现一个因果切片。共享 DTO、路由、迁移、工作台编排器时串行。
- 改之前读 live 实现、测试和当前 diff。文档与代码冲突时以代码为准并修正文档。
- 已交付合同写在 PRD/CONTEXT/ARCHITECTURE；本文待交付项落地的同一提交更新那些文档和 `USER_GUIDE.md` / HelpPage。
- 不新造第七类节点来实现镜头、局部修、官方配方、平台预设。
- 不新开技能货架首页。
- 不把 GenerationSpec 与 DeliverySpec 混写。
- 不让 Agent session 文件成为业务权威。
- 不复活 V1 运行时作为 fallback。
- 搜索全部读写方后再改枚举、JSON 形状、路由、provider 字段、共享 UI 投影。
- 前端遵循 `productflow-frontend`：画面先于文字，不新开色板。
- 每阶段结束必须可运行。不为后续阶段预先加复杂兼容层。
- 新增功能说明修改了哪些文件、如何测试、已知限制。
- 修复走最小范围。

### 每次开发任务的输出格式

```text
本次任务：
修改文件：
新增功能：
关键实现：
测试方式：
已知限制：
```

---

## 31. 第一版明确不实现的内容

- 多租户、团队角色、计费、用量结算。
- 自动发布到淘宝/京东/Amazon/Shopify。
- 视频生成、视频复刻、视频翻译。
- AI 试衣、万物穿戴、姿势裂变、虚拟模特库。
- 粘竞品链接做详情整页复刻。
- 去水印、证件照、PPT、拼图等旁路工具作为一级入口。
- 店铺级批量改图（数百 SKU 一次）。
- 社区配方市场、积分、插件商店。
- 自动判定并删除用户不满意的图。
- 在 Agent 上下文一次加载整个图库。
- 独立水平「技能页」与 DAG 平行的第二套生产系统。
- Python 业务后端迁 Go。
- 移动端原生 App。
- 长期保留旧运行时兼容读取器作为在线路径。

服装试衣、视频、爆款复刻、目录批量，只有在本文第一版验收之后才能单独立项。立项时必须说明如何长在商品 + 图上，而不是再开一个工具页。

---

## 32. 最终产品定义

工作室竞争力第一版完成后，用户应该能够：

> 在自托管的 ProductFlow 里，为一件真实商品建立生产单元。上传参考图、选定套图（或让 Agent 问清楚后确认草案），得到一张可编辑的 schema-v3 图。默认在镜头列表上一键生成套图，按首屏、细节、场景、卖点推进；需要时切到画布改节点和连线。出图后可以局部修改并写回同一商品图库，按平台交付预设导出而不重新生图。满意的结构存成配方，下一件商品预览后复用。模型由用户自己的 prompt / agent / image 供应商提供。Agent 始终在旁边澄清和检查，不能绕过确认去覆盖正式图。

这份需求书是后续实现工作室竞争力第一版的总基线。新增功能必须先判断是否属于第一版范围，并遵守本文已经确定的商品单元、确认边界、六类节点、配方和权威规则。
