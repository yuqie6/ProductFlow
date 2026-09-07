# 任务：实现事实来源分层闸（CF-B0）

状态：完成
类型：实现
认领者：sub-image/compete-facts-layer-gate
认领于：2026-09-07T12:16:30+08:00
完成于：2026-09-07T12:31:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：CF-B1 图位文字追溯；不改 IMG 42/32 完成条件

按 [Issue 协议](../README.md) 认领。承接 [compete-facts-layout-contract](compete-facts-layout-contract.md) **CF-B0** 与父章程 IQ-CF-01。协调者审核：2026-09-07 CTO 通过——product 与 drafts 复测通过；冲掉后重写已核实。

## 问题来源

事实已有 `source_type`/`status` 闭集，但缺少业务闸：未确认推断不得升为已确认性能事实；营销口吻不得写入规格级事实；冲突须可见可裁定。

## 做成什么样

扩展/收紧 facts 写入与展示：确认门、冲突可见、营销文案与性能事实分栏；**不新建第二事实仓库**。正例：用户确认容量 → `confirmed`+`user`；图观材质 → `image_observation` 待确认。反例：`agent_inference`「保温 24h」未确认即 `confirmed`；「明星同款」写入规格事实 —— 均须拒绝或保持未确认并可见。

## 前置与并行

- 前置：compete-facts-layout-contract 已归档；复用 `normalizeFactPayload`。
- 冻结输入：认领时 HEAD；不改 IMG 旧闸门。
- 运行资源：聚焦 Go 测试；无需真实 provider。
- 排他写入：`go/internal/product/` facts 相关、必要 schema 校验、工作台资料面板最小展示（若需）、测试、父章程 CF-B0/IQ-CF-01 行、本文件。
- **并行注意**：勿改 `go/internal/auth/`、identity schema、workbench 成果视图文件（其他认领任务占用）。

## 只改这些文件

- `go/internal/product/facts.go`：`layer` 闭集、确认门、营销口吻闸、冲突须带说明
- `go/internal/product/dto.go`：Fact.Layer
- `go/internal/product/http.go`：parseFactItems 允许 `layer`
- `go/internal/product/serialize.go`：metadataFacts 写 `layer=performance`
- `go/internal/product/facts_test.go`：正反夹具与 HTTP 闸
- `web/src/lib/types.ts`：`ProductFactLayer`
- `web/src/lib/nodeDetailMessages.ts`：性能/营销/冲突分区文案
- `web/src/pages/workbench/canvas/graphNodeEditorDrafts.ts`：layer/确认升级为 user
- `web/src/pages/workbench/canvas/graphNodeEditorDrafts.test.ts`
- `web/src/pages/workbench/canvas/GraphNodeInspector.tsx`：资料面板分栏与冲突可见
- `docs/audits/image-quality.md`：IQ-CF-01 / CF-B0 状态
- 本文件

## 不要碰

CF-B1+、排版 SDK、Skill/grader、金标池、商家身份表、采用快照模型。

## 合同

- IQ-CF-01；MP 无关的事实闸在单商家下可验。
- 完成 ≠ R3；≠ CF-B1。

## 怎么验收

`product/facts*` 夹具正反测；必要面板可见冲突/未确认；`just docs-check`；相关包测试。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：sub-image/compete-facts-layer-gate。
- 交接：实现与验证已就绪，等待协调者审核归档；未提交 git、未改看板 README。停下等分配。

## 证据

- 写入闸：`normalizeFactPayload` + `applyFactLayerGate`；`layer`=`performance|marketing`；`agent_inference`/`image_observation` 默认 `observed`+`requires_confirmation`，显式 `confirmed` 拒绝；营销口吻入性能层拒绝；`conflicted` 须非空 `conflicts`
- 正夹具：用户 `capacity`→`confirmed`+`user`+`performance`；图观 `material`→`image_observation` 待确认；`selling_point` 默认 marketing
- 反夹具：`agent_inference`「保温 24h」+`confirmed`；「明星同款」写入性能事实
- HTTP：`TestFactsHTTPLayerGateFixtures`
- UI：资料面板性能/营销分栏；冲突区展示说明；确认按钮升为 `user`+`confirmed`
- 验证：`go test ./internal/product/` 通过；`vitest graphNodeEditorDrafts.test.ts` 4 passed；`just docs-check` 通过（看板仍为认领待审）
- 自审：未改 auth/identity、成果视图、Skill/grader/金标/provider；未新建事实仓库；未提交；未改看板 README

- 审核者：CTO（本会话）；结论：通过。可拆 CF-B1。
