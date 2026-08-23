# Schema-v3 工作台侧栏修葺北极星

## 1. 状态

- 文档状态：Draft
- 批准意图：2026-08-22 会话要求先完成画布对象命令缺口，再把侧栏升级写成后续切片的默认阅读，避免把「页签在」当成产品完成。
- 目标合同：`docs/adr/0008-free-canvas-agent-graph-authority.md`、`docs/adr/0005-agent-workbench-ui.md`
- 画布北极星：`docs/specs/v3-canvas-restoration.md`（图合同、ChangeSet、390px 浏览器 gate 仍以画布规格为准）
- 审计证据：2026-08-21/22 对照 `origin/main` V1、`e6cf9e66` V2、当前 `workbench/` 侧栏
- 未完成项入口：`docs/ROADMAP.md` §2；本文是该节侧栏部分的实施北极星

在线图权威已经是 schema-v3。本文管的是：**人在侧栏把当前对象改完、跑完、绑完、看完失败时，完成度必须达到 V1 对象反馈 + V2 工作台打磨，语义必须是 v3，文案必须是结果语言。**

## 2. 一句话目标

侧栏是当前对象的操作面，不是架构说明书。用户打开详情就能改、跑、看到失败并重试；空选时能添加、打开图库、运行整张图；运行页只讲这次跑的结果；图库只帮人选图绑定。Agent 是同一壳里的协作者，不挡住这些动作。

「页签在」不算完成。完成定义是：**一条连续动作能做完，失败写在打开的对象上，忙时侧栏锁住会改图的操作，桌面与 390px 都通，常规 chrome 不出现内部名。**

## 3. 已锁定的方向（后续切片不得改）

1. **图合同只许 v3。** 侧栏持久写入仍走 `WorkflowChangeSet` + Node Catalog。禁止为侧栏恢复 V1 文案编辑器、V2 mutation、plan key、浏览器 undo 权威。
2. **壳只许复用。** 唯一 inspector 所有者是 `ProductWorkbenchInspector`。禁止为侧栏新写一套轨、抽屉或详情卡皮肤。
3. **人是主控。** 详情、运行、图库、添加都必须能由用户单独完成。Agent、Draft、GraphProposal 不能成为打开详情或运行的闸门。
4. **绑定 ≠ 连边 ≠ 子图库。** 图库绑定 `image_asset` 不自动连 `reference`。未使用必须在详情可见。
5. **检查器的目的地是 Catalog。** `GET /api/v3/node-catalog` 的 `config_fields` 是表单、校验、Agent tool schema 的同一份文档。当前 `GraphNodeInspector` 类型化编辑器是过渡适配。语言、失败、空选、flush、busy 可以先修；禁止再加深私有字段模型。
6. **配方来自用户主动保存。** 应用可以先落到待确认 Draft，但必须能预览将产生的图变更。禁止写出 V2 recipe payload，也禁止把 Draft id 画在成功条上。配方写路径仍属画布切片 E。
7. **文案是结果语言。** 用户看见的是下一步和结果。`ChangeSet`、`revision`、digest、asset id、`schema-v3`、`ProductImageAsset` 不出常规 chrome。完整句子只留给空态、确认、不可恢复错误。
8. **完成证据是浏览器。** 单元测试证明投影和命令形状。切片完成还要桌面、窄桌面、390px、亮/暗色、reduced-motion。
9. **390px 底抽屉属于工作台 gate。** 规格要检查器不挡节点。底抽屉已接到 `ProductWorkbenchInspector`（窄屏不再整页盖住画布）。与画布切片 G 同一刀验收，不在语言切片里新写一套移动壳。
10. **Go 后端重写不得插入本程序。**

## 4. 质量上限

### 4.1 六个工具的职责

| 工具 | 必须完成的工作 | 不合格形态 |
|---|---|---|
| 添加 | 建六类节点；选区可复制、分组、解散；忙时锁定 | 只罗列类型；配方入口用词和页签不一致 |
| 详情 | 改当前节点；处理节点可运行/到此运行；失败写在打开的检查器上并可重试；输入/消费者可跳转；空选是下一步 | 失败只在卡片或运行页；空选只剩类型计数和版本号 |
| 运行 | 看这次跑的结果、取消、重试、跳到节点 | 把 compiled_context 键、digest、id、JSON 铺在第一屏；缺失节点显示 `node_id` 切片 |
| 图库 | 选一张商品图绑到 `image_asset`；未使用可见 | 架构说明当 hint；桌面文件夹改名只靠 hover |
| 配方 | 预览将产生的图变更后应用 | 保存继续 410 却宣称完成；成功条画出 Draft id |
| Agent | 对话、附图、回答、确认 Agent 发起的运行 | 短标签只写英文 Agent；图-only 页用空壳挡住添加/详情 |

### 4.2 连续动作（必须整条存在）

1. **打开详情 → 看见上次失败 → 重试或改配置后再跑。** 失败不得只存在于卡片或运行页。
2. **空选详情 → 添加节点 / 打开图库 / 运行整张图。** 禁止把 graph revision 当作仪表盘正文。
3. **改检查器 → 保存徽章 → 切工具或运行前 flush。** 画布 ChangeSet 进行中，添加、详情、运行重试、图库绑定、配方应用都锁定。
4. **运行页打开一次失败 → 看见原因和节点名 → 取消或重试 → 跳回节点。** 技术键进折叠或不出现。
5. **绑定素材 → 详情显示已绑；未连边则显示未使用。** 换绑不改边。
6. **应用已保存配方 → 预览节点/边变更 → 确认后一次写入。** 第一刀可以先落到 Draft，但成功反馈说结果，不说 id。

### 4.3 语言

- 工具条默认 icon + `aria-label` / `title`。
- 空态、确认、失败：一句结果 + 下一步。
- 禁止把 schema、revision、asset id、digest、payload 校验值画在常规 chrome 上。
- 节点类型、状态、关系用色/图标/预览；字只补画面说不清的。

### 4.4 视口

- 桌面：侧栏贴画布，不新做第二套轨。
- 390px：主操作不是 hover-only；无水平溢出；检查器用底抽屉或等价不挡节点的容器（切片 S4 / 画布 G）。

## 5. 明确不做

- 加深 `GraphNodeInspector` 私有字段模型，推迟 Catalog。
- 恢复 V1 文案编辑器、`PromptPreviewDialog`、顶部流程统计条。
- 为侧栏新写一套玻璃检查器或第二视觉身份。
- 把 Redo 映射成再调一次 undo（历史权威在画布切片 B）。
- 配方保存继续走 V2 payload，或保持 410 却宣称侧栏完成。
- 用 GraphProposal 顶替用户直接改详情。
- 在语言切片里重做 390px 整页壳。

## 6. 切片顺序（可调工期，不可调依赖）

后一切片可以提前做只读勘察，但不得在前一刀的停止条件未满足时把后一刀的半成品合进主路径。画布切片 C/E/G 与侧栏 S2/S3/S4 是同一刀的两端，不要平行发明第二套合同。

| 顺序 | 切片 | 用户可见结果 | 停止条件 |
|---|---|---|---|
| S1 | 结果语言与对象反馈 | 详情能看见失败并重试；空选是下一步；revision/digest/id 退出常规 chrome；忙时运行/图库/配方锁定 | 打开失败节点的详情能读到原因；空选无版本号；无新 v2 写入 |
| S2 | Catalog 检查器 | 详情表单按 `config_fields` 渲染；保存仍 `update_node_config` | 与画布切片 C 同一刀；前端不再为新字段加一份私有 schema |
| S3 | 配方预览 | 应用前能看见将创建的节点/边；成功条说结果 | 与画布切片 E 同一刀；410 消失；无 Draft id 展示 |
| S4 | 390px 底抽屉 | 打开详情时画布仍可见一截；主操作可点 | 与画布切片 G 同一刀；390px 无水平溢出、无 hover-only |

S1 不改 Inspector 字段模型。S2 之前只修语言、失败、空选、flush、busy、输入/消费者跳转。

## 7. 每个侧栏 PR 的完成定义

1. 指出本 PR 服务切片 S1–S4 中的哪一条，以及覆盖了 §4 哪几条链路。
2. 持久操作产生一个 ChangeSet / 运行请求，或明确属于纯客户端（选工具、滚列表、折叠证据）。
3. 不新增 V1/V2 图写入，不加深类型化 Inspector。
4. 不新增平行壳；缺口优先喂给现有 inspector / 运行 / 图库槽位。
5. 有 focused 测试：空选、失败投影、证据过滤或 busy 锁定。
6. 触及可见侧栏时，写明桌面与 390px 的验证结果或明确「未做浏览器、不得宣称完成」。

## 8. 文档同步

- 行为落地后更新 `docs/USER_GUIDE.md` 与 `web/src/pages/HelpPage.tsx` 同一提交。
- `ARCHITECTURE.md` 写保存提取与配方应用到 live graph 的当前事实。浏览器证据仍待切片 G。
- 交互表状态列落后于代码时改交互表，不要为了表格绿灯降低 §4。

## 9. 代码锚点

- 壳：`web/src/pages/workbench/agent/AgentWorkbenchShell.tsx`、`chrome/ProductWorkbenchInspector.tsx`
- 详情/运行/图库/配方：`GraphNodeInspector.tsx`、`GraphRunsPanel.tsx`、`GraphLibraryPanel.tsx`、`RecipeLibraryPanel.tsx`
- 添加：`GraphAddNodePanel.tsx`
- 图库目录：`chrome/image-explorer/`
- 目录：`backend/.../domain/graph_catalog.py`
- 配方：`backend/.../workflow_recipes/service.py`、`extract.py`
