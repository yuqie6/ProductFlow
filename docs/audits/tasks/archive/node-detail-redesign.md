# 任务：六类节点详情与执行输入合同重构

状态：完成
类型：实现
认领者：主代理-node-design-0905-2024
认领于：2026-09-05T20:24:01+08:00
完成后可拆：无

## 问题来源

用户已确认六类节点及各自详情表单设计，并将全部落地交给本会话，明确本任务优先级最高。用户明确无需旧字段兼容；要求处理自动保存、继承、候选、执行与复用副作用。遵循 [Issue 协议](../README.md)。本会话作为协调者确认上述认领，不创建认领提交。

## 做成什么样

- 保留商品资料、参考图片、创作要求、系列风格、画面方案、图片生成六种节点。
- 详情按各节点职责组织；图片与配色可视化，文稿按章节编辑，事实与待确认问题分区。
- 文字策略由画面方案拥有，图片生成可明确覆盖；删除下游反向决定上游文字策略的路径。
- 单图差异仅保存覆盖项，未覆盖字段跟随方案，可恢复继承；不改共享文稿。
- 字段进入实际执行输入；输入摘要、过期、候选采用与执行使用一致合同。
- 保留添加、连线、独立运行、撤销、配方、局部编辑采用与历史结果；不自动付费重跑。
- 六类侧栏按职责分区，配色使用色块与用途，单图使用逐项编辑/恢复，复杂字段折叠；检查多语言、窄屏与暗色。生成指令参照公开一手实现文档，明确商品身份、参考用途、事实、文字与用户要求的优先关系。

## 前置与并行

- 已交付：参考用途/说明合同 `60c8095e`；局部编辑采用与撤销 `66411957`。
- 用户授权本任务优先；节点合同、检查器与详情表单由本会话独占。其他任务涉及这些范围须交给本会话整合。
- 不改图片评测池、题库、评分、Agent Skill/harness。现有 Agent 采证使用固定 checkout；若改图工具的字段合同，仅同步必要 wire 适配，不改行为策略。
- 不修改共享 provider 设置，不停共享 dev 服务。浏览器验证使用独立 mock 栈、数据库和端口；产物 `/tmp/productflow-node-detail-redesign/`。

## 只改这些文件

- `go/internal/graph/`：Catalog、输入编译、文稿与单图覆盖、模板及回归。
- `go/internal/providers/`、`go/prompts/`：必要的图文稿 DTO 与实际输入适配；不改模型绑定或采证配置。
- `go/internal/product/`、`go/internal/recipe/`：新建与配方字段合同、必要回归。
- `web/src/pages/workbench/canvas/`、`web/src/lib/`、必要的 `workbench/chrome/`、创建表单与测试；复用当前 UI。
- 必要的 Agent 图工具 wire/schema 合同适配，不修改 Skill/harness/评测题。
- `web/e2e/`、必要命令入口、当前产品/架构/用户指南与 Help、任务与索引。
- `scripts/check_web_bundle_budget.py`：将已退休的 shell chunk 名匹配更新为现有 `ProductWorkbenchSurface`；不提高预算。

## 不要碰

- imagesession 性能任务、queue 状态机、评测池、其他会话未提交文件。
- 不做兼容读取、双写或旧数据迁移；不新增节点类型、嵌套运行体系或第二个工作台。

## 现在代码在哪

`graph/catalog.go` 定义字段；`compiler.go` 与 `execute_node.go` 组装执行；`document.go` / 候选模块拥有文稿采用；`template.go` 与 recipe 拥有创建/复用。前端 `GraphNodeInspector.tsx`、`CatalogConfigFields.tsx`、`catalogConfig.ts`、`useNodeDraftAutosave.ts` 拥有表单与保存。

## 合同

- 打开/展开表单不写入配置，不把默认值物化成局部覆盖。
- 未设置、显式空值与继承分开；恢复继承删除局部覆盖。
- 同节点冲突保留草稿，兄弟节点更新不误挡；保存失败阻止运行。
- 正式文稿与 AI 候选分离；应用候选不得修改用户拥有的文字策略或单图覆盖。
- 生成设置与交付设置分离；只改导出不调用模型。
- 所有旧字段退出时扫描读写、模板、配方、测试与文档，不保留退休 reader。

## 怎么验收

- 带 PG 的 graph/providers/product/recipe 回归；必要 Agent 合同回归。
- Web 全量 test:run、lint、build；docs-check、diff-check。
- 隔离 mock 浏览器覆盖六类表单、保存/重载、第二张局部改场景与文字、恢复继承、共享修改、仅运行目标、候选采用、撤销、配方与局部编辑入口。
- 桌面、窄桌面、移动端；检查边界尺寸、溢出、控制台与网络错误。真实 provider 图片质量不以 mock 结果冒充。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：主代理-node-design-0905-2024。
- 交接：新合同、侧栏与提示词已实现并自审。隔离 API/Web 为 `29402/29403`，Redis `16509`，数据库 `productflow_node_detail_0905`；作为本地 mock 预览保留，仅本会话 `/tmp/productflow-node-detail-redesign/start.mjs` 管理这些进程。未修改共享服务或 provider 配置。

## 证据

- 带开发环境的 graph/providers/adapt/product/prompts 回归通过；recipe 使用单独命名测试二进制建立隔离测试库并通过。未新增数据库表或列，不需要字段迁移。
- Web 92 文件 / 655 测试通过；lint、`just web-build`（包含原预算）、`just agent-service-check-contracts`、`just docs-check` 通过。最终 workbench chunk 535,230 bytes / gzip 155,361，预算仍为 550,000 / 170,000。
- 最终隔离 Chromium 19/19 通过（4.4 分钟）：配方与资产复用 7 项、局部编辑采用/撤销与失败保护 2 项、单图覆盖/恢复 1 项、文稿候选/中途编辑/撤销/409/运行/取消 8 项、事实确认/版本冲突/配色/多语言六类表单 1 项。桌面 1440、窄桌面 1024、移动端 390；六类侧栏检查四语言的桌面亮色与移动端暗色，包含表单边界与图片解码检查。
- 浏览器产物 `/tmp/productflow-node-detail-redesign/browser-final/`；此前逐张检查的侧栏截图留于 `browser-forms/`。直接创建无 Agent workspace 时的既有 409 探测单独识别，其余错误响应与 pageerror 均要求为空。未调用真实 Agent。
- 命令：带 `scripts/with_dev_env.sh` 运行 `go test -C go ./internal/graph ./internal/providers/... ./internal/product ./prompts -count=1 -p 1`；recipe 以 `/tmp/node_detail_recipe_0905_final.test` 在独立测试库运行。Web 使用 `pnpm --dir web test:run`、`pnpm --dir web lint`、`just web-build`；Playwright 同批执行 `node-detail-redesign`、`canvas-document-mock`、`canvas-asset-recipe`、`canvas-local-edit`，显式指定隔离 worker PID。
- 公开资料与设计取舍记录在 [prompt 文案说明](../../../../go/prompts/README.md)。冻结的 `agent-service/evals/fixtures/catalog.json` 未改，不能用该旧 catalog 的评测结果签收新合同。没有执行收费的真实模型质量评测；自然语言冲突与最终文字像素保真不作确定性承诺。
- 审核：主代理自审全部本任务 diff 与临时产物，不声称独立审核。旧持久化字段 reader、默认值和候选分区同步替换；不加入旧数据兼容。共享工作树中的 Agent 性能、图片质量与评测材料不纳入本次提交。
- 交付定位：随本任务提交。
