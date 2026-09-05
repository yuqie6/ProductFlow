# 任务：资产复用与配方确认的浏览器业务验收

状态：完成
类型：实现
认领者：主代理-canvas-0905-1736
认领于：2026-09-05T17:36:32+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：canvas-full-recipe-entry

认领、验收和关闭遵循 [Issue 协议](../README.md)。

## 问题来源

[完整操作链核查](canvas-workflow-coverage.md) 确认图编辑 actions 已有持久化断言；配方浏览器到预览/取消为止，资产固定与复用主要是操作计划测试。本单补用户确认后的身份和结构验收。

## 做成什么样

- 在图库选择明确资产并绑定参考节点；拖入画布/参考端口后核节点绑定、边角色与资产身份。
- 固定当前生图结果创建独立 image_asset，绑定当前结果，不自动连边；后续图运行/编辑不改变该资产身份。
- 保存片段配方，在另一商品预览并确认后核新增节点/内部边及目标已有节点保留；不带原商品身份、绑定图和生成结果。
- 完整配方仅无图商品可创建；已有图明确冲突，取消预览不写图。
- 复跑当前无成本图编辑 actions，保留关闭 Agent 可操作合同。

2026-09-05 维护者现场拆分：固定资产、拖放、片段确认与完整配方拒绝覆盖由本单交付。无图商品的完整配方界面入口实际被工作台 bootstrap 拒绝，属于创建/路由合同缺口，独立交给 [canvas-full-recipe-entry](canvas-full-recipe-entry.md)；本单记录有效失败与后端 create 回归，不以跳过或错误断言代替该业务链通过。原完整配方用户验收目标继续由后续任务承担。

## 前置与并行

- 前置：现有 Graph/Recipe/资产合同及操作链核查，默认在本组运行恢复和交付图之后串行，不构成代码依赖。
- 冻结输入：schema-v3 图、资产身份、配方脱敏合同，不改 Agent 行为。
- 运行资源：专属 browser context 与测试商品，固定已验证图片；不调用真实 provider、不占图片组 browser/provider/storage、不重建共享 DB。优先隔离栈，产物 `/tmp/productflow-canvas-asset-recipe-proof/`。

## 只改这些文件

- `web/e2e/workbench-v3-actions.spec.ts`、`workbench-v3-proof.spec.ts` 或本组专属 spec/helper。
- `GraphCanvasPanel.tsx`、`graphAssetDrop.ts`、`RecipeLibraryPanel.tsx`、`recipeSave.ts` 及相邻测试：仅确证缺陷所需修改。
- Graph/Product/Recipe 后端只读调查；跨后端根因由维护者确认范围后修复。
- 本任务、父账本、任务索引。
- `justfile`：只增加本单浏览器命令入口。

## 不要碰

图片池、Agent、imagesession；不新建图库、配方格式、运行模型或兼容旧数据。

## 现在代码在哪

Canvas `buildPinImageAssetOperations` / asset drop → ChangeSet；Explorer → 绑定回调；`recipeSave` / `RecipeLibraryPanel` → Recipe API → `go/internal/recipe` 的 Graph Command 应用。`recipe/http_test.go` 已验保存/应用及 full 冲突；浏览器 proof/actions 只到预览。

## 合同

明确资产 id，不复制媒体；配方无商品身份和生成结果；确认一次写入、取消不写入；保持 O1-O7 与既有撤销合同。

## 怎么验收

真实点击、持久化图和资产身份断言；复跑无成本 actions 的桌面与窄屏用例；相关 Web test:run/lint/build，后端如有修改补隔离 PG 回归。`just docs-check`、自审与单次交付提交。截图/预览可见不能代替实际确认结果。

## 阻塞与交接

- 原因：本单约定切片已验证；完整配方入口由后续任务承担。
- 解除条件：无。
- 跟进者：工作流体验组主代理。
- 交接：临时 API/worker/dispatcher/Web/Redis 已停止，数据库与证据文件保留；未停止共享服务或切共享 provider。提交完成后释放本单文件占用。

## 证据

- 2026-09-05 本单 Web 生产代码无修改；新增 `canvas-asset-recipe.spec.ts`，复用 `canvasWorkflow.ts` 与实际 API/worker。`workbench-v3-actions.spec.ts` 仅修正连线点击：沿 SVG 曲线寻找 `elementFromPoint` 命中自身的点，用真实鼠标点击；原外接矩形中心可能落在另一条交叉边上，不用 force 或 dispatch 绕过。
- `just web-e2e-canvas-asset-recipe --output=/tmp/productflow-canvas-asset-recipe-proof/verified`：3 passed / 1 skipped，30.4s。通过固定结果后再次生成不换绑定、真实图库拖到 reference 端口、片段保存/取消/确认后的节点与内部边、目标共享输入接入、原配置和边保留、身份与结果清除、完整配方拒绝覆盖。绑定选图另复用前单及 actions 的真实用例；未宣称所有拖放组合通过。
- `PRODUCTFLOW_PROBE_FULL_RECIPE_ENTRY=1 just web-e2e-canvas-asset-recipe --grep @known-gap --output=/tmp/productflow-canvas-asset-recipe-proof/verified-entry-fail`：1 failed，2.9s，用例先确认无图 API 返回 404、页面明确显示缺 Agent 工作区，再断言可见配方入口为 0 而非 1。此为有效业务 FAIL；默认跳过该诊断不计为通过。
- 上述命令均设置 `WEB_BASE_URL=http://127.0.0.1:29394`，使用本次隔离栈专用管理员及 settings token；无真实供应商调用。固定静态 Web、独立 API 29392、Redis 16489、数据库 `productflow_canvas_proof_1788602054894`；API/worker/Web 的 cwd 为 `/tmp/productflow-canvas-asset-recipe-proof`，不占共享开发服务。
- 固定产物：Web `index.html` SHA256 `ddf9c34001768ca188b3e04615a4f479c834c88187b7c62f94c5211b8a826e78`；`ProductWorkbenchSurface-BsmyqE7A.js` SHA256 `8450eed831754a958d80c589139429ada6e5cabf64440857c68bf18a885b0717`；API binary SHA256 `585d9622626a967c1d4ba56dfc3ff7ee1ae05b020a6a7feea2b27b82245a960b`。共享树存在其他会话改动，不宣称整树固定 commit 验收。
- 过程边界：首次配置位置检查未启动用例；配方内部 key 可沿用原节点 ID，应用时重映射，修正了错误测试假设；共享 HMR 干扰后切固定构建。临时服务曾被外部 SIGTERM 停止，发送者未确认，连接拒绝轮次无效；仓库开发清理脚本的 cwd/命令匹配能覆盖原临时进程，因此重启到仓库外 cwd。旧 actions 第一轮 76 passed / 2 failed，均因 SVG 点击位置，最终复验另记。
- 截图与 trace：`verified/` 为通过场景，`verified-entry-fail/` 为缺入口失败；已人工核对片段应用后画布与失败页。早期加载动画截图不作可用性证据。Go/Web 原始回归输出留在同一根目录的 `recipe-go.log`、`web-unit.log`、`web-lint.log`、`web-build.log`。
- 交付定位：随本任务提交。
- 最终 actions：`PRODUCTFLOW_RUN_LIVE_BROWSER_GRAPH=1 pnpm --dir web exec playwright test e2e/workbench-v3-actions.spec.ts --output=/tmp/productflow-canvas-asset-recipe-proof/actions-final`，同一隔离 Web 29394，78 passed，5.9m；1440/1024/390 各 light/dark，包含绑定、连线重连、复制、分组、撤销与预览取消。该文件虽使用 live 开关，本轮不调用真实 provider。
- 最终确定性回归：`bash scripts/with_dev_env.sh go test -C go ./internal/recipe -count=1 -v`，12 passed、无 skip，1.527s；`pnpm --dir web test:run` 91 files / 647 passed；lint、build（含 e2e TypeScript）通过，保留既有大 chunk 警告。
- 审核者：主代理-canvas-0905-1736，自审；检查完整新增 spec、连线 helper、命令和任务/章程 diff，未修改生产业务代码，未纳入其他会话 diff。
- Issue 结果：拆分后的资产身份、片段确认与冲突验收交付；完整配方入口有效 FAIL 已交独立任务。业务门槛：上述已测切片通过，完整配方创建操作链未通过，不宣称本组全部完成。
