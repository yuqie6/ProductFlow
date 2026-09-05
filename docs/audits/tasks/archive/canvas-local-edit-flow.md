# 任务：让商家能对已出图完成局部消除、换字或重绘

状态：完成
类型：实现
认领者：主代理-canvas-0905-1952
认领于：2026-09-05T19:52:41+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无

任务合同以本文件为准；认领、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始读取当前实现、追踪调用链、调查根因或设计方案。

## 问题来源

父章程「使用结果」要求局部编辑可取用；[完整配方创建入口](canvas-full-recipe-entry.md) 关闭时写明完整局部编辑仍未知。路线图「出图后局部修」仍列为未落地，但 `ProductWorkbenchSurface` → `LocalImageEditController` 与 `go/internal/localedit` 已经接线。

维护者 2026-09-05 核对现场：

- 检查器对有当前出图的生图节点、图库对可读资产都能打开对话框。
- 默认 mock 图片绑定经 `providers.Image()` 返回空 `MockImage{}`，`Capability()` 恒为不支持；商家看到「当前图片供应商不支持局部编辑」，提交被拒。
- 工作流 mock 生图是 1×1 PNG，选区合同要求同时有编辑像素和保护像素，1×1 无法提交。
- `USER_GUIDE` / Help / PRD 未写该操作；无浏览器门。

这是已接线能力无法完成商家任务，不是补测试空白，也不是镜头列表主界面。

## 做成什么样

关闭 Agent 后，商家对已有生图结果做消除 / 换字 / 局部重绘：

1. 从检查器打开（带目标节点）可把结果采用为该节点当前图，也可只留在图库；失败或仅保留都不改节点当前资产身份。
2. 新图 `origin_type=local_edit`，`parent_asset_id` 指向源图；源图字节不变。
3. 采用后可撤销，节点当前图回到采用前资产。
4. mock 图片绑定能完成上述操作合同。OpenAI 仍须档案声明 `image_mask_edit`；Gemini 仍不支持。不引入第七类节点或批量货架。

## 前置与并行

- 前置：localedit HTTP/执行回归、检查器入口、完整配方入口已归档。
- 冻结输入：不改 Agent 题库/Skill、图片池、imagesession 恢复；不切换共享 provider。
- 运行资源：隔离 mock API/Web/worker/PG/Redis；不占图片采集浏览器，不停共享 `just dev`。产物 `/tmp/productflow-canvas-local-edit-flow/`。
- 占用：`perf-imagesession-recovery-visible` 写 imagesession/dispatcher/queue；`image-eval-pool` 占共享 provider 与采集浏览器；`eval-skills` 冻 Skill。本单不与之相交。看板 README 已有其他组未提交行，提交时只暂存本任务 hunk。

## 只改这些文件

- `go/internal/providers/factory.go`、`contract.go` 及 `factory_test.go`：mock 绑定声明并执行 masked local edit；工作流 mock 出图须可画选区。
- `web/e2e/` 本单 spec；必要时复用 `canvasWorkflow.ts`。
- `justfile`：仅新增 `web-e2e-canvas-local-edit`。
- `web/vite.config.ts`：preview 增加 `/api` 代理，隔离预览栈可把页面同源请求转到独立 API。
- `docs/USER_GUIDE.md`、`USER_GUIDE.en.md`、`web/src/pages/HelpPage.tsx`、`docs/PRD.md`、`PRD.en.md`、`docs/ROADMAP.md`、`ROADMAP.en.md`、父账本、本文件与看板/归档。
- 对话框/检查器/图库入口未改；复现根因在 mock 能力声明与出图像素，不在入口组件。

## 不要碰

- imagesession 恢复、Agent 题库/Skill、图片池、graph 文稿权威、delivery 规格。
- 镜头列表默认主区、第七类节点、去水印货架、批量 200。
- 其他任务未提交 diff；不改共享 provider 设置。

## 现在代码在哪

调查时的现场（修复前）：

- 入口：`GraphNodeInspector` `data-graph-node-local-edit`；图库 `ImageAssetActions`（`targetNodeId=null`）。
- 前端：`LocalImageEditController.tsx` → `api.*LocalImageEdit` → `go/internal/localedit`。
- 能力：`LiveImage.Capability` ← `providers.Image()`；mock 分支返回空 `MockImage{}`。
- 生图像素：workflow `MockImage.Generate` 走 `smallestPNG()` 1×1；chat 才按 Size 出灰图。
- 选区：`localEditGeometry.ts` 要求 edit 与 protected 像素都 ≥ 1。
- 采用：`adoptLocalImageEdit` 写节点当前 artifact；失败路径不写 `current_artifact_id`。

修复后：工厂 mock（`store == nil` 或 `kind` 为 `""`/`mock`）返回 `MockImage{Cap: SupportedEditCapability("mock")}`；零值 `MockImage{}` 仍不声明能力。工作流 mock 在未指定 Size 时出 64×64 灰图。

## 合同

- 新资产保留谱系；失败不替换节点当前结果。
- 检查器打开才暴露采用；图库打开默认可只留在图库。
- O1–O7 / AR-01 不变；局部编辑不是 GraphRun，不走文稿搜索器。
- mock 声明能力必须与 `Edit()` 一致；零值 `MockImage{}` / `localedit.MockProvider{}` 仍可注入「不支持」供单测。

## 怎么验收

- `go test -C go ./internal/providers ./internal/localedit -count=1`（localedit 需 PG）。
- 隔离 mock 浏览器：检查器打开 → 画选区 → 提交 → 新资产身份与 parent → 采用改 preview → 撤销恢复；提交失败节点当前图不变。入口必须点检查器按钮，不得改成纯 HTTP 创建任务来回避界面。
- 相关 Web `test:run` / lint / build；`just docs-check`。
- 活文档与路线图同步；真实 OpenAI/Gemini 质量不在本单。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：主代理-canvas-0905-1952
- 交接：隔离栈 API `127.0.0.1:29412` / Web `127.0.0.1:29413` / Redis `16589` / PG `productflow_canvas_local_edit_1788609634265`，验收后停止并删除临时库；未改共享 `just dev`、provider 或采集浏览器。其他组未提交 diff 未纳入本交付。

## 证据

- 命令 / 日期 / 结果：2026-09-05，基线 `fff8a8a7`。隔离 mock：`WEB_BASE_URL=http://127.0.0.1:29413 just web-e2e-canvas-local-edit --output=/tmp/productflow-canvas-local-edit-flow/accepted --trace=on`（进程传隔离专用管理员和设置凭据），2 passed，20.2s。检查器 `data-graph-node-local-edit` 打开后画选区并提交；结果 `origin_type=local_edit`、`parent_asset_id` 为源图；采用改 `preview_asset_id`，撤销恢复；源图字节不变。第二条拦截 `POST **/image-edits/**/submit` 为 503，错误可见且节点当前图不变。
- Go：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/providers ./internal/localedit -count=1'`，providers 0.119s、localedit 1.435s，均 ok。零值 `MockImage{}` 仍不声明能力；工厂 mock 声明 masked local edit。
- Web：`pnpm --dir web test:run` 92 files / 650 tests passed；`pnpm --dir web lint` 通过；`pnpm --dir web build` 通过（保留既有 >500kB chunk 警告）。`just docs-check` 通过。
- 隔离构建说明：协调工作树当时含其他组未提交的 imagesession/dispatcher 改动，隔离二进制按该工作树编译；那些文件不进入本任务暂存。本单生产改动仅 providers mock 绑定与出图像素。
- 交付定位：随本任务提交。
- 审核者 / 结论（自审须注明）：主代理-canvas-0905-1952 自审通过，非独立审核。
- Issue 结果 / 业务门槛结果 / 剩余缺口：mock 绑定下局部编辑操作链可完成。未覆盖真实 OpenAI/Gemini 质量；商家任务耗时与镜头列表主界面仍未知，不在本单。
