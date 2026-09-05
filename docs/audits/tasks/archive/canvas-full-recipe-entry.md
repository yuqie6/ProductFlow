# 任务：让商家能从当前创建流程使用完整配方

状态：完成
类型：实现
认领者：主代理-canvas-0905-1808
认领于：2026-09-05T18:08:50+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无

认领、验收和关闭遵循 [Issue 协议](../README.md)。交付定位：随本任务提交。

## 问题与验收合同

[资产与配方验收](canvas-asset-recipe-proof.md) 发现：原名称-only 和直接创建都会建立 live graph，完整配方只允许目标无图；受支持的无图 API 商品又被缺 Agent 工作区挡在工作台外。后端 create 回归通过没有证明商家能触达入口。

本单要求从当前可见创建入口选择完整配方，预览取消无图写入，明确确认后得到新商品的第一张图；不要求 Agent，不允许覆盖已有图，不以建图后删除的补偿绕过约束。新节点、边和商品资料使用目标身份，来源绑定资产与生成结果不泄漏。创建事务、重复确认和父章程 AR-01 / O1-O7 保持。

## 实现与边界

- 创建页增加「从配方创建」模式，独立填写新商品名称、参考图和可选商品说明；只列完整配方。预览无需先创建商品，取消连商品也不落库。确认成功打开现有画布，不创建 Agent 会话。
- `recipe.PreviewCreation` 使用既有计划器计算无目标身份的结构及摘要，校验配方版本、归档状态和完整/片段类型。新增 `POST /api/v3/workflow-recipes/{recipe_id}/creation-preview`。
- `product.CreateFromRecipe` / `POST /api/v3/products/from-recipe` 在原创建事务回调中校验摘要，并用同一 GORM 事务的 Recipe Preview/Apply 写首张图和应用记录；图写入仍由 Graph Command 负责，原目标专属 digest 与不可覆盖规则保留。
- `products.creation_idempotency_key` / `creation_request_hash` 可空且成对，key 唯一。同键同输入返回同一商品的当前投影，不同输入冲突；并发竞争在回滚后读取已提交结果。首次 HTTP 201，回放 200。页面对结果不明的重试保留原请求变量。
- 创建媒体的补偿释放移到外层事务提交成功之后；事务提交阶段失败也清理新文件。
- 素材占位保持未绑定，上传图片在新商品图库可选；没有自动替用户匹配参考图，也没有自动改写可复用文稿。用户仍需检查文稿和重新选图。没有修改无图 API 商品的工作台路由、Agent、imagesession、图片池或运行模型。
- 中英文 PRD / ARCHITECTURE / USER_GUIDE 和帮助页四语言同步；删除使用指南里“无图工作台可套配方”的失真声明。部署需要 `just go-migrate`，本轮只迁移隔离库。

## 修改所有权

主代理在协调工作树认领并复读确认后调查，登记因果范围：`go/internal/product/` 的创建用例、HTTP、测试与媒体补偿；`go/internal/recipe/{service.go,http.go,http_test.go}`；schema 的商品字段、约束和迁移测试；创建页、新表单、API/types/i18n 和邻近测试；本单 E2E、必要活文档、父章程和归档索引。未修改其它组的生产实现或评测输入，未暂存其它组的状态行。

## 验证证据

日期：2026-09-05。使用当时共享工作树的本单专属实现；API 二进制和 Web 固定构建位于 `/tmp/productflow-canvas-full-recipe-entry/`，运行期间不使用共享 HMR。原始证据保留在该目录，测试用例随本单提交。未使用干净固定 commit 宣称整树验收。

| 验证 | 命令与结果 | 原始记录 |
|---|---|---|
| 商品 / 配方 / 迁移 PG | `bash scripts/with_dev_env.sh go test -C go ./internal/product ./internal/recipe ./internal/platform/db/schema -count=1 -p 1 -v`；43 + 13 + 3 = 59 passed，无 skipped | `go-final.log` |
| 创建关键故障 | `TestRecipeCreationPreviewAndAtomicConfirmation`、`TestRecipeCreationConcurrentConfirmation`、`TestRecipeCreationHTTPContract`、`TestRecipeCreationCommitFailureRollsBackFiles`；无写入预览、版本/digest 拒绝、回滚、并发、不同输入冲突、无 Agent 会话、提交失败清理文件 | 同上 |
| 创建预览 HTTP | `TestRecipeCreationPreviewHTTP`；鉴权、未知字段、无效/过期版本、片段和归档拒绝 | 同上 |
| schema | 旧形状商品表补列后保留已有行；key/hash 成对和 key 唯一；迁移重复应用稳定 | 同上 |
| Web 全量 | `pnpm --dir web test:run`；92 files / 650 passed；`pnpm --dir web lint`、`pnpm --dir web build` 通过，含 E2E 类型检查；保留既有大 chunk 警告 | `web-unit-accepted.log`、`web-lint-accepted.log`、`web-build-accepted.log` |
| 全仓 Go 编译 | `go test -C go ./... -run '^$'` 通过；没有执行全仓测试，不作全仓业务回归结论 | 本次命令输出 |
| 浏览器业务与布局 | `WEB_BASE_URL=http://127.0.0.1:29395` 下 `just web-e2e-canvas-asset-recipe --output /tmp/productflow-canvas-full-recipe-entry/accepted`；7 passed，1.3m，无跳过 | `accepted.log`、`accepted/` 的截图和 trace |
| 文档与 diff | `just docs-check`、`git diff --check` 通过；主代理自审 | 本次提交检查 |

浏览器前置为隔离 mock prompt/image 绑定与管理员 session。原有三项固定资产、拖入参考、片段确认及完整配方拒绝覆盖保留通过。新增三项分别在 1440/1024/390 宽度通过：真实创建表单 → 取消无商品/图写入 → 确认后目标身份与新节点/边 → 刷新持久化。来源图先生成结果，新图不含其资产和产物。1440 用浏览器 fetch 边界注入“后端提交成功但应用未收到响应”，界面重试返回同一商品，列表只有一条。

布局项覆盖三宽度 × 明暗 × 中/英/日/越四语言，共 24 组合、48 张表单/预览截图；含长配方名、真实缩略图、reduced motion、视口/内容宽度、确认按钮边界及文字裁切检查，未见 console/page/network error。测试不等同于所有图规模、全部浏览器或商家效率观察。

## 无效尝试与剩余未知

- `browser/` / `browser-retry/` 的重复提交注入丢失了浏览器 multipart 文件正文，400 不代表业务路径失败；最终改为应用 fetch 的响应边界注入，文件上传照常经过浏览器。
- `browser-entry/` 已有六项业务通过，布局初次把滚动条预留宽度小于视口误判；最终检查内容不超出实际视口。`layout/` 为中间构建，最终以 `accepted/` 为准。
- 帮助页新增段落初次缺日/越翻译导致全量测试失败，四语言已补齐；一次格式化与构建同时读写造成无效构建，之后冻结文件重新构建通过。这些失败不计最终通过。
- 原 `@known-gap` 和 probe 开关已从可执行测试移除；前单 FAIL 保留为历史证据。未运行真实 provider C5，未复验完整局部编辑和商家任务成本；不据此宣称业务组整体完成。

## 审核与运行交接

审核者：主代理-canvas-0905-1808，自审。结论：本单完整配方创建合同通过，相关确定性与浏览器回归通过；父章程的入口 FAIL 更新为上述限定范围 PASS。实现、测试、活文档、证据摘要与本归档合并一次交付提交，不 push。

全部采证命令已结束，原工具会话中的栈已正常停止。另保留独立后台预览供用户查看：Web `127.0.0.1:29395`、API `29392`、Redis `16489`，数据库 `productflow_canvas_proof_1788603919339`，固定 `web-dist-accepted` 与本轮 API 二进制；supervisor PID `45713`，日志 `preview-session.log`。停止命令 `kill -TERM 45713` 会回收该预览的子进程。它不占用共享开发栈、provider、worker 或其它组目录；后续测试不得直接接管这份用户预览数据。
