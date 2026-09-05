# 任务：让商家能从当前创建流程使用完整配方

状态：开放
类型：实现
认领者：—
认领于：—
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无

认领、验收和关闭遵循 [Issue 协议](README.md)。

## 问题来源

[资产与配方验收](archive/canvas-asset-recipe-proof.md) 发现：当前名称-only 和直接创建都会建立 live graph，完整配方只允许目标无图。用受支持的 `POST /api/v2/products` 建无图商品后访问工作台，又显示“商品还没有 Agent 工作区”，没有配方入口。后端 `TestRecipeApplyCreateModeAndFragmentNeedsGraph` 通过，不能证明用户可达。商家目前可保存完整配方，但未找到从当前创建界面把它用于新商品的路径。

## 做成什么样

- 从当前可见创建入口选择已保存完整配方，预览后一次确认得到属于新商品的 live 图，不要求调用 API 或先启动 Agent。
- 取消预览不写目标图；再次打开仍可选择配方。现有有图商品继续明确拒绝完整覆盖。
- 新图采用新节点/边身份，商品资料绑定目标商品，原商品绑定资产及生成结果不泄漏；维持创建事务与幂等合同。
- 先确定创建与套用的真实因果边界，不预设允许完整配方覆盖 birth graph，也不以创建后删除图的补偿方式绕过约束。

## 前置与并行

- 前置：复现及现有 Recipe create/merge 合同已具备；`canvas-asset-recipe-proof` 交付后释放本组 spec 与章程。
- 冻结输入：schema-v3、完整配方不可覆盖已有图、Agent 可选；不改 Agent 模型、工具与测评。
- 运行资源：独立 API/Web/PostgreSQL/Redis、mock 绑定；固定 Web 构建防止共享 HMR。产物 `/tmp/productflow-canvas-full-recipe-entry/`。不修改共享 provider 或暂停其他 worker。

## 只改这些文件

- 有界调查 `web/src/pages/AgentProductCreatePage.tsx`、`pages/product-create/`、`pages/workbench/ProductWorkbenchPage.tsx`、`pages/workbench/agent/productWorkbenchRoute.ts` / `ProductWorkbenchSurface.tsx`、Recipe 调用及 `go/internal/product/` / `go/internal/recipe/`。
- 维护者确认创建和应用合同后登记实际写入范围及最小跨层方案；不要仅改前端错误提示掩盖缺入口。
- `web/e2e/canvas-asset-recipe.spec.ts` 的 `@known-gap` 复现转为正常业务回归，补从真实创建入口的完整确认与取消用例。
- 相关聚焦测试、必要 API/types 与当前产品文档、本任务、父章程及索引。涉及使用步骤时同步 USER_GUIDE 与 HelpPage。

## 不要碰

Agent runtime/工具/评测、图片池、imagesession；不恢复旧工作台、旧数据兼容或新运行模型。

## 现在代码在哪

创建入口 → Product create / workspace create → birth/direct graph；`productWorkbenchRoute.resolveProductWorkbenchSurface` 在 graph 404 + Agent 409 时返回 error。`RecipeLibraryPanel` → preview / apply；`go/internal/recipe/plan.go` 在目标有图且 kind 非 fragment 时拒绝合并。应联合判断这些边界，而非只看 route 存在。

## 合同

一次明确确认、一致事务与幂等、来源身份清除、当前图不被覆盖；保持父章程 AR-01 与 O1-O7。

## 怎么验收

- 真实创建入口选择完整配方 → 预览取消无写入 → 再次确认 → 新商品图持久化和目标身份正确；有图目标拒绝覆盖。桌面与窄屏均可操作，不能用 API 夹具代替用户入口。
- 当前复现：隔离栈设置 `PRODUCTFLOW_PROBE_FULL_RECIPE_ENTRY=1 just web-e2e-canvas-asset-recipe --grep @known-gap`，预期当前 FAIL；实现后移除临时 probe 开关并纳入常规门禁。
- 相关 Web test:run/lint/build 与 Product/Recipe 隔离 PG 回归；验证创建事务、重复确认和取消边界。`just docs-check`、自审、单次交付。

## 阻塞与交接

- 原因：无未交付业务依赖；认领前确认前单已释放文件及运行资源。
- 解除条件：按当前看板与归档复核。
- 跟进者：工作流体验组主代理。
- 交接：未认领，本任务无实现 diff 或运行进程。

## 证据

- 复现：2026-09-05，固定 Web 构建下无图商品展示 Agent 工作区错误；原始截图/trace 在 `/tmp/productflow-canvas-asset-recipe-proof/`，最终证据位置见前单。
- 修复结果：未实现。当前 backend create 通过与 browser entry 失败分别记录。
- 交付定位：随本任务提交。
- 审核者 / 业务门槛：待认领和验收。
