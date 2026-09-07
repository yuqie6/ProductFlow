# 任务：双路线声明（CF-B3）

状态：完成
类型：实现
认领者：sub-compete/compete-facts-produce-route
认领于：2026-09-07T14:05:00+08:00
完成于：2026-09-07T14:16:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：CF-B4 受控排版；不改 IMG 42/32

按 [Issue 协议](../README.md) 认领。承接 [CF-B2](compete-facts-impact-preview.md) 与父章程 IQ-CF-05 / CF-B3。协调者审核：2026-09-07 CTO 通过——graph 聚焦测与 Vitest 复测；完整 graph 包测因并行 B7 agent WIP 未跑；未宣称主体提取链/采用硬闸/R3/CF-B4。

## 问题来源

主图保留主体与场景生成式未在图位/运行记录上声明；生成式提示词可能宣称像素保真；保留主体失败仍可能标合格。

## 做成什么样

图位/运行记录 `produce_route=subject_preserve|generative`；生成式禁像素保真文案；保留主体路线失败→未解决项。正例：主图 `subject_preserve`；场景 `generative` 且 UI 显示「可能改变外观」。反例：生成式含「像素级一致」；保留主体失败仍标已交付合格。

## 前置与并行

- 前置：CF-B2 已归档（合同允许与 CF-B1 并行，现串行于 B2 后）。
- 排他写入：路线枚举持久化、文案审计测试、相关 UI、父章程 CF-B3、本文件。勿与 `merchant-delivery-localedit` 同写 delivery 采用合格集除非必要。
- 不改 Skill/grader/金标；无需真实 provider。

## 只改这些文件

- `go/internal/graph/produce_route.go`：路线闭集、默认、记录、禁令审计、`RouteAllowsDeliveryPass`
- `go/internal/graph/produce_route_test.go`：正反夹具与文案审计
- `go/internal/graph/catalog.go`：`produce_route` 配置字段与按图种默认
- `go/internal/graph/listing_prompt.go`：生成式声明「可能改变外观」并剥离像素保真宣称
- `go/internal/graph/providers.go`：`ImageRequest.ProduceRoute`
- `go/internal/graph/execute_node.go`：prompt/image 产物挂载 `produce_route` 记录
- `go/internal/graph/compiler.go`：`promptStrippedKeys` 含 `produce_route`
- `go/internal/graph/text_trace_test.go`：剥离回归
- `web/src/pages/workbench/canvas/CatalogConfigFields.tsx`：选项文案键
- `web/src/lib/i18n.ts`：产图路线四语文案（含「可能改变外观」）
- `docs/audits/image-quality.md`：IQ-CF-04/05 / CF-B3 状态
- 本文件

## 合同

- IQ-CF-05 / CF-B3；完成 ≠ R3 / ≠ CF-B4。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：无。
- 交接：已归档；交付随本任务提交。

## 证据

- 配置：`image_prompt` / `image_generation` Catalog 字段 `produce_route` 闭集 `subject_preserve|generative`；`FillDefaultNodeConfig` 主图→`subject_preserve`、场景→`generative`
- 产物：prompt/image artifact 挂 `produce_route` 记录（`route` / `appearance_may_change` / `route_qualified` / `unresolved_items`）；hydrate 经 `promptStrippedKeys` 剥离
- 编译：生成式 listing prompt 声明「可能改变外观」；用户侧「像素级一致/还原」从规则与 variation 剥离；禁令审计命中→`route_qualified=false`
- 合格判据：`RouteAllowsDeliveryPass`；保留主体失败/缺本体参考→未解决项，不得当交付合格（未改 delivery 采用包，判据侧就绪；运行时 `SubjectPreserveFailed` 待主体提取链接线）
- UI：选项「生成式（可能改变外观）」
- 正夹具：hero 默认 `subject_preserve`；scene `generative` + 外观可变；干净路线 `route_qualified=true`
- 反夹具：生成式含「像素级一致」不合格；保留主体失败仍 `RouteAllowsDeliveryPass=false`
- 验证（协调者复测 2026-09-07）：
  - `bash scripts/with_dev_env.sh bash -lc 'cd go/internal/graph && go test -count=1 -p 1 -run "ProduceRoute|CompileImage|FillDefault|StripV3|TextTrace|BuildTextTrace|CheckSelling|TemplateSpec|generationSpecForShot" $(ls *.go | grep -v run_contract_consumers_test.go)'` 通过
  - `pnpm --dir web exec vitest run src/pages/workbench/canvas/catalogConfig.test.ts src/pages/workbench/canvas/generationOptions.test.ts` 通过（19）
  - 缺口：完整 `go test ./internal/graph/` 被并行 B7 agent WIP（`loadScopedConversation` 签名，经 `run_contract_consumers_test.go`）挡住包编译；未改 agent/delivery/localedit
- 自审：未改 Skill/grader/金标、delivery 采用合格集、ProductWorkbenchSurface/resultProjection/GraphCanvasPanel、看板 README；未 commit
- 未宣称：主体提取实现链；交付采用强制消费 `route_qualified`；R3 / IMG 42/32 / CF-B4
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/compete-facts-produce-route.md` 查询）

- 审核者：CTO（本会话）；结论：通过。可拆 CF-B4 受控排版。
