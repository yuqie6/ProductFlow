# 任务：事实变更影响预览（CF-B2）

状态：完成
类型：实现
认领者：sub-compete/compete-facts-impact-preview
认领于：2026-09-07T13:45:00+08:00
完成于：2026-09-07T14:05:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：CF-B3 双路线（可并行若写集不冲突）；不改 IMG 42/32

按 [Issue 协议](../README.md) 认领。承接 [CF-B1](compete-facts-text-trace.md) 与父章程 IQ-CF 变更影响合同。协调者审核：2026-09-07 CTO 通过——graph/product/routes 与 Vitest 复测；未宣称 OCR/自动跑图/R3。

## 问题来源

改商品事实后缺「哪些图位受影响」预览；用户无法多选更新范围，易导致全图重跑或静默跳过。

## 做成什么样

事实保存前预览依赖图位；用户多选更新；未选中已完成节点保持 artifact；解释旧 `fact_set_version_id`。正例：改 500→600ml 列出规格/卖点图；场景图无容量字默认不入更新集。反例：改容量后全图自动重跑；预览注入未连边节点资料。

## 前置与并行

- 前置：CF-B1 已归档。
- 排他写入：事实影响预览 API/UI、相关 graph digest/`skipUnchanged` 回归、父章程 CF-B2、本文件。勿与 `merchant-graph-recipe` 同写冲突的 graph HTTP/SSE 商家过滤文件——认领前核占用。
- 不改 Skill/grader/金标；无需真实 provider。

## 只改这些文件

- `go/internal/graph/fact_impact.go`：`PreviewFactImpact` / `DiffFactKeys` / 采用钉版本 / 未选中 digest 重盖
- `go/internal/graph/fact_impact_test.go`：正反夹具与 `incomingFactSetVersions`/`skipUnchanged` 回归
- `go/internal/product/fact_impact.go`：`PreviewFactsImpact` / `UpdateFactsAndAdopt`
- `go/internal/product/facts.go`：`UpdateFactsInput.update_node_ids`
- `go/internal/product/http.go`：`POST .../facts/impact-preview`；PUT 扩展
- `go/internal/product/facts_test.go`：impact-preview HTTP 夹具
- `go/cmd/productflow-api/routes_contract_test.go`：登记新路由
- `web/src/lib/types.ts` / `api.ts` / `i18n.ts`：合同与文案
- `web/src/pages/workbench/canvas/factImpactPreview.ts` + `.test.ts`：默认选中逻辑
- `web/src/pages/workbench/canvas/GraphNodeInspector.tsx`：保存前预览多选
- `docs/audits/image-quality.md`：IQ-CF-03 / CF-B2 状态
- 本文件

## 合同

- 父章程 CF-B2；完成 ≠ R3 / ≠ OCR 闸。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：实现与验证已就绪，等待协调者审核；未提交 git、未改看板 README。停下等分配。

## 证据

- 预览：`POST /api/v3/products/{id}/facts/impact-preview`；只经 RoleFacts 可达 prompt/generation；`text_trace.fact_keys` / BuildTextTrace 定默认更新集
- 采用：PUT facts 带 `update_node_ids` → 钉 `product_source.fact_set_version_id`；未选中已完成节点重盖 `input_digest` 保 artifact；**不入队全图运行**
- 解释：预览节点带 `bound_fact_set_version_id` / `artifact_fact_set_version_id`
- 正夹具：改 capacity → 规格/卖点默认选中；场景无容量字默认不选；未连边 orphan 不出现
- 反夹具：`incomingFactSetVersions` 不注入未连边源；force 目标不 skip；digest 不匹配不 skip
- 验证：
  - `bash scripts/with_dev_env.sh bash -lc 'cd go && go test ./internal/graph/ ./internal/product/ ./cmd/productflow-api/ -count=1 -p 1'`（graph/product/routes 通过）
  - `pnpm --dir web exec vitest run src/pages/workbench/canvas/factImpactPreview.test.ts` 通过
- 自审：未改 Skill/grader/金标、`imagesession/`、library 绑定核心、看板 README；未 commit
- 未宣称：OCR；自动入队选区 force 跑图；R3 / IMG 42/32

- 审核者：CTO（本会话）；结论：通过。可拆 CF-B3 双路线。
