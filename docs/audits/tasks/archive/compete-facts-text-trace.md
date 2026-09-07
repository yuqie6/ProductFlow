# 任务：图位文字追溯到商品事实（CF-B1）

状态：完成
类型：实现
认领者：sub-compete/compete-facts-text-trace
认领于：2026-09-07T13:22:00+08:00
完成于：2026-09-07T13:35:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：CF-B2 变更影响预览；不改 IMG 42/32

按 [Issue 协议](../README.md) 认领。承接 [CF-B0](compete-facts-layer-gate.md) 与父章程 IQ-CF-02。协调者审核：2026-09-07 CTO 通过——graph 包测复跑；text_trace 元数据+卖点检查器成立；未宣称 OCR/采用硬闸/R3。

## 问题来源

信息图成稿文字须能追到本商品 fact key 或显式本图覆盖；卖点图默认一图一主要购买理由。

## 做成什么样

prompt/generation 产物记录所用 `fact_keys` 或本图覆盖标记；卖点一图一理由检查器（可先非 live）。正例：规格「600ml」← `capacity`；卖点只强调有依据的一句。反例：成片「24h 保温」无 fact；卖点堆多句无关口号。

## 前置与并行

- 前置：CF-B0 已归档。
- 排他写入：graph/product 产物元数据与相关测试；勿与 `merchant-graph-recipe` / `brand-visual-reuse` 同写 `recipe/`、`visualsystem/`、共享 schema；本批优先 graph 产物元数据与检查器，product store 仅必要最小改动。
- 不改 Skill/grader/金标；无需真实 provider 完成本批。

## 只改这些文件

- `go/internal/graph/text_trace.go`：`BuildTextTrace` / `CheckSellingPointOneReason` / 产物 map
- `go/internal/graph/text_trace_test.go`：正反夹具与 strip 回归
- `go/internal/graph/compiler.go`：`promptStrippedKeys` 含 `text_trace`
- `go/internal/graph/execute_node.go`：prompt/image 产物挂载 `text_trace`；hydrate 剥离子文档
- `docs/audits/image-quality.md`：IQ-CF-02 / CF-B1 状态
- 本文件

## 合同

- IQ-CF-02；OCR 闸非本批必备（元数据+抽检可先行）。
- 完成 ≠ R3。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：实现与验证已就绪，等待协调者审核；未提交 git、未改看板 README。停下等分配。

## 证据

- 元数据：`TextTrace`（`fact_keys`、`user_image_override`、`entries`、`text_qualified`）；prompt artifact 校验后并入；image artifact `payload.text_trace`
- 剥离：`text_trace` 进 `promptStrippedKeys`；hydrate `PromptDocument` 用 `stripV3PromptPayload`
- 正夹具：规格「600ml」← `capacity`；卖点「轻量杯身」一句且有 key；本图 `text_override` 合格
- 反夹具：「24h 保温」无 fact；卖点堆三句；单句无依据
- 检查器：`CheckSellingPointOneReason`（非 live）
- 验证：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/graph/ -count=1 -p 1'` 通过
- 自审：未改 `visualsystem/`、`recipe/`、Skill/grader/金标、delivery 采用快照核心、看板 README；未碰 brand-visual-reuse WIP；未 commit
- 未宣称：OCR 成片对照；交付采用强制消费 `text_qualified`；R3 / IMG 42/32

- 审核者：CTO（本会话）；结论：通过。可拆 CF-B2；OCR/采用硬闸另发。
