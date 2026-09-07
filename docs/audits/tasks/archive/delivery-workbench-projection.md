# 任务：提供覆盖全部图片产出的成果工作视图

状态：完成
类型：实现
认领者：sub-delivery/delivery-workbench-projection
认领于：2026-09-07T11:55:25+08:00
审核材料提交于：2026-09-07T12:20:00+08:00
完成于：2026-09-07T12:25:00+08:00
冻结 HEAD：`d6709c4aacb2e26bb30ab70a99d08b1dca05f487`
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：根据任务走查结果裁定默认入口，再发布采用与交付快照；不重复配方创建和基础修图

按 [Issue 协议](../README.md) 认领并确认所有权后调查和实现。协调者审核：2026-09-07 CTO 通过——全量投影与默认流程视图符合合同；i18n 键已接受；已同步 PRD/USER_GUIDE/HelpPage。opt-in E2E 未跑不阻塞本任务。

## 问题来源

2026-09-07 只读观察现有开发站已有商品工作台，桌面和手机主区均为复杂节点图；结果集中在底部胶片条。源码 `shotProjection.ts` 按 group 投影，无法完整表达未分组生图节点与每个图位的候选。已有胶片条、运行和图库保留，新增 [总纲第 5 节](../../../ROADMAP.md#5-正常体验的最低要求) 的成果视图，降低检查全部产出的导航负担。该样本不证明新视图效率必然更高。

## 做成什么样

工作台可在成果与流程视图间切换，原画布默认保持。成果视图完整展示每个生图节点的当前图片、名称/用途、状态与现有操作入口，分组仅组织展示；未分组项和真实证据图片可达。一组多图分开呈现，能定位流程节点、查看历史、运行选中范围和继续既有局部编辑/图库路径。切换视图不建图、不触发 Agent Turn、不运行模型。

## 前置与并行

- 前置：现有 schema-v3 Graph、资产与运行合同已实现；无需新租户 API 或品牌模型。当前单商家内可独立验收。
- 冻结输入：认领时 HEAD、相关 Graph/资产/运行 wire；复用固定测试夹具，禁止改服务端状态语义迎合 UI。
- 运行资源：优先组件与 mock 浏览器测试，必要 PG 使用隔离库及独立端口；真实 provider 不在本任务验收内。
- 写入独占：workbench Surface、canvas 投影/新视图与其测试；任何涉及这些文件的其它任务串行。

## 只改这些文件

实际写入：

- `web/src/pages/workbench/agent/ProductWorkbenchSurface.tsx`：默认 `mainView=flow`、选择同步、预览/局部编辑接线
- `web/src/pages/workbench/canvas/GraphCanvasPanel.tsx`：视图切换、成果层懒加载、定位/运行桥接
- `web/src/pages/workbench/canvas/resultProjection.ts` + `resultProjection.test.ts`：按生图节点全量投影
- `web/src/pages/workbench/canvas/GraphResultsView.tsx` + `GraphResultsView.test.tsx`：成果网格与操作入口
- `web/src/pages/workbench/canvas/workbenchResults.tsx`：懒加载切换器与成果层（控制 workbench shell 体积）
- `web/e2e/delivery-workbench-projection.spec.ts`：opt-in mock 栈浏览器门
- `web/src/lib/i18n.ts`：成果/视图切换四语文案（共享文案，请协调者确认）
- 本文件与父章程验收记录

扩大共享：`web/src/lib/i18n.ts` 新增 `graph.results.*` / `graph.canvas.view*` 键，交协调者裁定是否同步 PRD/USER_GUIDE/HelpPage。

## 不要碰

Graph schema、Go/Node 运行时、Skill、评测合同、provider 配置、全站主题、商品创建事务。不得新增 Shot 表、第二执行器、采用状态或商业费用字段；采用/交付快照另有完整合同。

## 现在代码在哪

`ProductWorkbenchSurface.tsx` → `GraphCanvasPanel.tsx` → 默认 `GraphWorkflowCanvas` + `GraphShotFilmstrip`；成果态懒加载 `workbenchResults.tsx` → `GraphResultsView`；投影 `resultProjection.ts`。图片查看/编辑沿 Surface 预览对话框与 `LocalImageEditController`；运行复用 Surface `onBeforeRun` flush 与 `scope=node`。

## 合同

- 父章程 AR-01 和 O1–O7、同节点冲突、运行前 flush、候选/撤销继续成立；关闭 Agent 后可手动操作。
- 每个生图节点恰好呈现一次；组内多图、未分组、无当前图、有旧结果、失败但有可用旧图分别展示，不把历史图误称当前成功。
- 当前产物不等于交付采用；不在本任务偷偷引入第二套采用状态。
- 运行选择复用现有 `selection`/`node` scope；缺输入、保存失败和 409 不提交旧稿。查看历史、局部改图保留来源与采用边界。
- 空图、只含参考图、长标题、30 张计划图、部分失败和 unknown 均有稳定布局和可达操作，不能只做成功截图。

## 怎么验收

按 `web/AGENTS.md` 执行受影响组件回归、静态检查和 UI 验证。投影回归覆盖上述不同数据形状，浏览器点击真实切换、定位和运行入口，断言不重复项、不触发隐式创建/生成、提交 scope/targets 正确、保存冲突不发 run。

在 `1440×960`、`1280×800`、`390×844` 下检查截图、文字/图片加载和操作遮挡。用同一现有资产夹具完成“找缺图、检查一图、去改图、选择失败项、回到流程”，分别记录现有/新视图的操作和无法完成项；该内部走查不冒充真实商户效率或竞品领先。完成必要 `just web-build`、相关 E2E 与 `just docs-check`，记录真实执行命令。

完成条件是视图可用、全量投影和原操作合同不退化。默认入口维持原值，后续依据观察裁定；本任务不需要调用真实模型证明已有生成逻辑。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者审核后决定是否改默认入口与文档同步。
- 交接：实现已写入共享工作树并提交审核材料；**未提交 git**；保持认领等待协调者验收；停下等分配。

## 证据

### 实际修改范围

见上文“只改这些文件”。未改 Graph schema / Go / Agent / provider。

### 成果投影数据形状

`projectGraphResults` 输出 `GraphResultSection[]`：

- `kind: "group" | "ungrouped" | "evidence"`
- 每项 `GraphResultItem`：`nodeId`（唯一）、`kind`、`title`、`imageTypeKey`、`groupId/groupTitle`、`status`、`failureReason`、`currentAssetId`（当前产物≠采用）、`showingStaleCurrent`（失败/unknown 但仍有旧图）、`runnable`

覆盖用例（单测）：组内多图、未分组、无当前图、失败保留旧图、skipped 输出、unknown+旧图、30 节点无重复、证据空位/已绑定。

### 验证命令与结果

| 命令 | 结果 |
|---|---|
| `pnpm --dir web exec vitest run …/resultProjection.test.ts …/GraphResultsView.test.tsx …/shotProjection.test.ts` | 3 files / 16 passed |
| eslint（上述 TSX + Surface/Panel） | 通过 |
| `just web-build` | 通过；workbench shell raw≈549.99 KiB / 550 KiB；成果块拆为 `workbenchResults-*.js` |
| `just docs-check` | 通过 |
| `web/e2e/delivery-workbench-projection.spec.ts` | 已添加；需 `PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1` 隔离 mock 栈，本任务未跑该 opt-in 门 |

### 浏览器走查（开发站 `29283`，商品「国风旗袍」）

默认流程视图；切换「图片成果」后胶片条隐藏，10 个生图节点各呈现一次、无重复。

| 任务 | 流程视图 | 成果视图 |
|---|---|---|
| 找缺图 | 需在画布/胶片中扫 | 直接点无图卡片 → 打开详情 |
| 检查一图 | 点节点进检查器/历史 | 历史按钮打开检查器；有图项露出局部编辑 |
| 去改图 | 检查器局部编辑 | 卡片铅笔入口 → 既有 LocalEdit |
| 选失败/unknown | 依状态色找节点 | 直接选 `unknown`/`failed` 卡片 |
| 回流程 | n/a | 定位 → `flow` + 聚焦节点 |

视口：`1440×960` / `1280×800` / `390×844` 无横向溢出，运行入口可见。切换视图未发商品创建或 Agent Turn；运行提交 body 为 `{"scope":"node","node_id":…}`。

走查中曾对一个 unknown 节点触发一次真实 run（验证 scope）；未改 provider 设置。

### 自审与剩余差距

- 默认仍为流程编辑；是否改为成果默认需后续走查裁定。
- opt-in E2E 未在本机 mock 栈执行。
- 手机窄屏下抽屉仍占主区，与既有工作台一致；成果网格可达但不等于手机体验最优。
- 未引入采用状态；交付快照另任务。

### 是否需要协调者同步 PRD/USER_GUIDE/HelpPage

**需要。** 新增工作台「图片成果 / 流程编辑」切换与操作入口，活文档与帮助页应由协调者同步；本任务已写入 i18n 四语键。

- 审核者：CTO（本会话）；结论：通过。可开放 delivery-adoption-snapshot。
