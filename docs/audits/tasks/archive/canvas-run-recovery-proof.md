# 任务：场景运行与失败恢复的浏览器业务验收

状态：完成
类型：实现
认领者：主代理-canvas-0905-1647
认领于：2026-09-05T16:56:26+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无

任务合同以本文件为准；认领、资源窗口和关闭遵循 [Issue 协议](../README.md)。

## 问题来源

[完整操作链核查](canvas-workflow-coverage.md) 发现：场景运行仅有 request builder 与后端 selection 测试；`workbench-v3-actions.spec.ts` 的 can retry 用例只检查按钮可见。用户要求本组完整落地，需要证明点击之后的真实业务结果。

## 做成什么样

- 关闭 Agent，从镜头/场景入口点运行，一次 POST scope=selection，node_ids 精确等于该组生图节点；运行成功且非目标节点/已手填文稿不被改写。
- 通过真实业务路径制造可重试失败，在检查器修正输入后点击重试；请求指向原失败 run，产生新的 run，使用当前保存值完成，保留原失败记录。
- 运行记录中仅重试失败节点，提交一次 selection，不包含成功/unknown/取消节点；保存失败阻止提交。
- 不把断言限制为按钮可见、HTTP 2xx 或 run 数量。

## 前置与并行

- 前置：canvas-workflow-coverage 已核查当前调用链，C0-C6 不变。
- 冻结输入：当前 Graph/文稿权威合同；不改 Agent/图片/平台组的输入。
- 运行资源：优先独立 API/worker/DB/storage 的 mock 栈。使用共享 dev 前须确认 mock/provider/worker 独占窗口；图片池仍持有共享 provider 资源，未经协调不得切换。现有无成本失败用例可在专用浏览器中执行，不占图片组页面。
- 证据目录：`/tmp/productflow-canvas-run-recovery/`，不得使用图片或 Agent 产物目录。
- 认领确认：本组主代理在操作链核查提交 `2292af39` 后复读本包、看板与目标 diff；其他在途工作仍限 Agent eval、Skill 和图片池，平台 imagesession 已移归档。用户已授权本组交付，本协调工作树登记并复核；本单独占上述 Web 运行边界，不创建认领提交。

## 只改这些文件

- `web/e2e/workbench-v3-actions.spec.ts`、必要的专项 browser spec 与本组专属 e2e helper。
- `web/src/pages/workbench/canvas/GraphCanvasPanel.tsx`、`GraphRunsPanel.tsx`、`GraphNodeInspector.tsx` 及相邻测试：仅有复现支持的运行/恢复接线问题，先记录因果。
- 本任务与父账本/任务索引。共享 `justfile` 如需命令须与平台组串行整合，不整文件暂存。
- 本次确认为测试补齐：新增 `web/e2e/canvas-run-recovery.spec.ts`、复用型夹具 `canvasWorkflow.ts`，补 `graphRunPreview.test.ts` 的 unknown/cancelled/skipped/live 排除断言；`justfile` 新增独立入口。未改生产实现。

## 不要碰

Agent、图片池、imagesession、provider 全局设置和 C0-C6 既有语义。不得改 scope、自动重放文稿 409 或用 HTTP 写入代替被测检查器输入。

## 现在代码在哪

`GraphCanvasPanel.runShot/submitRun` → `shotChangeSet.shotRunRequest`；`GraphNodeInspector.retryRun` 和 `GraphRunsPanel.retryRun/retryFailedNodes` → `api.retryGraphRun/submitGraphRun`；Go `graph/runs.go:retryGraphRun` 用当前图重新提交原 scope/target；`graphRunPreview.failedNodesRunInput` 只选 failed。

## 合同

O2/O3/O7 与 AR-01；当前图决定重试输入，原 run 不被覆写；场景只运行对应组的图；保存失败不发 run。

## 怎么验收

新增浏览器用例有效执行并核对持久化数据；贴近修改边界的 Vitest 通过，Web 变更执行 test:run/lint/build。新增 UI 变化才补视觉矩阵。`just docs-check`、diff 自审、单次交付提交；无有效运行证据不得关闭。

## 阻塞与交接

- 原因：无实现前置阻塞；浏览器运行前仍需落实隔离环境或资源窗口。
- 解除条件：使用隔离栈或由协调者确认共享窗口。
- 跟进者：工作流体验组主代理。
- 交接：浏览器测试已结束；隔离 API 29392、Web 29393、Redis 16489 与独立新建 PG/storage 由本组主代理保留供后续交付图任务使用，未修改共享进程/配置。启动脚本 `/tmp/productflow-canvas-run-stack.mjs`；本任务不占共享 provider/worker。

## 证据

- 日期 / 命令 / 基线 / 结果：2026-09-05，当前隔离栈从 `2292af39` 后共享源码构建，生产 Graph/Web 未修改；本单仅补测试和命令。`WEB_BASE_URL=http://127.0.0.1:29393 just web-e2e-canvas-run-recovery --output=/tmp/productflow-canvas-run-recovery/final --trace=on`（进程另传隔离环境专用 ADMIN_ACCESS_KEY / SETTINGS_ACCESS_TOKEN），4 passed，34.9s。
- 断言：场景只运行两张 detail，不运行 hero，全部节点 config 不变、authored 保持；检查器通过选图修复 missing reference 后 POST 原 run retry，新 run 成功且旧 run failed；混合成功/失败场景只重试失败 image，成功兄弟资产不变；保存 409 一次且不重放，scene submit=0、草稿保留。
- 原始证据：上述 final 目录四份 Playwright trace；失败夹具的早期运行只作调试，不混入通过结论。fixture 的 connect_nodes 改为生产要求的 client_ref/推导角色后重跑，未放宽业务断言。
- Web gate：`pnpm --dir web test:run` 91 files / 647 tests passed，lint/build passed（已有 bundle size warning）；补全非 failed 排除断言后 `graphRunPreview.test.ts` 6 passed；`tsconfig.e2e.json` 通过。
- 交付定位：随本任务提交。
- 审核者 / 结论：主代理-canvas-0905-1647 自审通过，非独立审核；无生产代码修改。
- Issue 结果 / 业务门槛结果：完成，场景运行与恢复合同有效验收；不代表交付图、配方与资产复用通过。未复跑 C4 会暂停全机 worker 的旧夹具。
