# 任务：交付采用硬闸消费 text/route_qualified

状态：认领
类型：实现
认领者：sub-iq/compete-adoption-hard-gate
认领于：2026-09-07T16:14:58+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：主体提取链 / OCR 闸（另发）；≠R3

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。按 [所有权前置规则](README.md#认领与并行) 已确认认领。

## 问题来源

[IQ-CF-08](../image-quality.md#compete-facts-layout) / CF-B1·B3：`text_qualified` / `route_qualified` 判据已在 graph 产物侧就绪；[delivery-adoption-snapshot](archive/delivery-adoption-snapshot.md) 仅按客户端 `quality_status=fail` 拒绝采用。客户端可把不合格图标为 `pass` 绕过。总纲方向 3 要求不合格图不得进入已采用交付合格集。

## 做成什么样

1. `CreateAdoption`（服务端）在持久化前，对每个 slot 若提供 `source_node_id`（或可解析到 graph 节点产物），读取该节点/产物上的 `text_trace.text_qualified` 与 `produce_route`/`route_qualified`（字段以现有 graph JSON 为准）。
2. 若 `text_qualified === false` 或 `route_qualified === false`（或等价 `RouteAllowsDeliveryPass` 为假），整次创建拒绝（validation），**不得**仅依赖客户端 `quality_status`。
3. 无追溯元数据时：保持可配置策略——默认拒绝进入合格采用，或显式 `unchecked` 且不允许标为合格导出；须在证据中钉死选定策略并加测试（正：qualified true 可采用；反：false 拒；客户端谎报 pass 仍拒）。
4. 前端成果采用路径展示拒绝原因；不改 Skill/grader；≠R3 通过。

## 前置与并行

- 前置：CF-B1/B3 与 delivery-adoption-snapshot 已归档。
- 冻结输入：无。
- 排他写入：`go/internal/delivery/` 采用路径、必要 `go/internal/graph` 只读辅助导出、成果采用相关 web 接线与测试、本文件、父章程 IQ-CF-08 行。

## 只改这些文件

- `go/internal/delivery/adoption*.go`、`adoption_test.go`（及确需的 dto）
- 必要时 `go/internal/graph` 导出只读解析辅助（不改评分门槛）
- `web/src/pages/workbench/canvas/deliveryAdoption.ts(+test)`、`GraphResultsView` 相关拒绝展示
- `docs/audits/image-quality.md`（IQ-CF-08 状态）
- 本文件

## 不要碰

- Skill/grader；IMG 42/32 门槛；开放第二商；共享 `productflow` down。

## 现在代码在哪

- 判据：`go/internal/graph/text_trace.go`、`produce_route.go`（`RouteAllowsDeliveryPass`）
- 采用：`go/internal/delivery/adoption.go` `prepareAdoptionSlots` 仅查 `quality_status`/`text_overflow`
- UI：`web/src/pages/workbench/canvas/deliveryAdoption.ts`

## 合同

- IQ-CF-08：不合格不得进已采用合格集；服务端强制。
- 完成 ≠ R3。

## 怎么验收

- Go：假 pass + `text_qualified=false` / `route_qualified=false` → CreateAdoption 失败；true → 成功。
- 相关 Vitest；`just docs-check`。
- 父章程 IQ-CF-08 更新为完成或仍列 OCR 残余。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
- 交付定位：随本任务提交
- 审核者 / 结论：
- Issue 结果 / 业务门槛结果 / 剩余缺口：
