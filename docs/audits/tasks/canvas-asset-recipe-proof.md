# 任务：资产复用与配方确认的浏览器业务验收

状态：开放
类型：实现
认领者：—
认领于：—
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无

认领、验收和关闭遵循 [Issue 协议](README.md)。

## 问题来源

[完整操作链核查](archive/canvas-workflow-coverage.md) 确认图编辑 actions 已有持久化断言；配方浏览器到预览/取消为止，资产固定与复用主要是操作计划测试。本单补用户确认后的身份和结构验收。

## 做成什么样

- 在图库选择明确资产并绑定参考节点；拖入画布/参考端口后核节点绑定、边角色与资产身份。
- 固定当前生图结果创建独立 image_asset，绑定当前结果，不自动连边；后续图运行/编辑不改变该资产身份。
- 保存片段配方，在另一商品预览并确认后核新增节点/内部边及目标已有节点保留；不带原商品身份、绑定图和生成结果。
- 完整配方仅无图商品可创建；已有图明确冲突，取消预览不写图。
- 复跑当前无成本图编辑 actions，保留关闭 Agent 可操作合同。

## 前置与并行

- 前置：现有 Graph/Recipe/资产合同及操作链核查，默认在本组运行恢复和交付图之后串行，不构成代码依赖。
- 冻结输入：schema-v3 图、资产身份、配方脱敏合同，不改 Agent 行为。
- 运行资源：专属 browser context 与测试商品，固定已验证图片；不调用真实 provider、不占图片组 browser/provider/storage、不重建共享 DB。优先隔离栈，产物 `/tmp/productflow-canvas-asset-recipe-proof/`。

## 只改这些文件

- `web/e2e/workbench-v3-actions.spec.ts`、`workbench-v3-proof.spec.ts` 或本组专属 spec/helper。
- `GraphCanvasPanel.tsx`、`graphAssetDrop.ts`、`RecipeLibraryPanel.tsx`、`recipeSave.ts` 及相邻测试：仅确证缺陷所需修改。
- Graph/Product/Recipe 后端只读调查；跨后端根因由维护者确认范围后修复。
- 本任务、父账本、任务索引。

## 不要碰

图片池、Agent、imagesession；不新建图库、配方格式、运行模型或兼容旧数据。

## 现在代码在哪

Canvas `buildPinImageAssetOperations` / asset drop → ChangeSet；Explorer → 绑定回调；`recipeSave` / `RecipeLibraryPanel` → Recipe API → `go/internal/recipe` 的 Graph Command 应用。`recipe/http_test.go` 已验保存/应用及 full 冲突；浏览器 proof/actions 只到预览。

## 合同

明确资产 id，不复制媒体；配方无商品身份和生成结果；确认一次写入、取消不写入；保持 O1-O7 与既有撤销合同。

## 怎么验收

真实点击、持久化图和资产身份断言；复跑无成本 actions 的桌面与窄屏用例；相关 Web test:run/lint/build，后端如有修改补隔离 PG 回归。`just docs-check`、自审与单次交付提交。截图/预览可见不能代替实际确认结果。

## 阻塞与交接

- 原因：无实现前置阻塞；本组默认串行执行。
- 解除条件：前一任务释放共享文件与运行资源。
- 跟进者：工作流体验组主代理。
- 交接：未认领，无运行进程。

## 证据

- 命令 / 基线 / 结果：待执行。
- 交付定位：随本任务提交。
- 审核者 / Issue 结果 / 业务门槛结果：未验收。
