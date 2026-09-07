# 任务：品牌视觉方案复用交互

状态：完成
类型：实现
认领者：sub-delivery/brand-visual-reuse
认领于：2026-09-07T13:15:00+08:00
审核材料提交于：2026-09-07T14:05:00+08:00
完成于：2026-09-07T13:32:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：默认入口走查；与图片质量 CF-B5 衔接

按 [Issue 协议](../README.md) 认领。承接 [采用快照](delivery-adoption-snapshot.md) 与总纲 §6.3。协调者审核：2026-09-07 CTO 通过——visualsystem/recipe/Vitest 复测通过；Brand 占位诚实；未宣称跨商分享/浏览器整链。

## 问题来源

下一商品应复用品牌/视觉方案，同时清除旧商品身份。配方已清身份；缺商家可理解的选择、预览与显式采用新版本交互。

## 做成什么样

单商家内保存/选择/预览视觉方案版本；继承优先级可见；更新品牌不静默改在做任务与旧交付；第二商品预览列出继承与待填项。消费 IQ-CF-07；Brand 表若商家平台未就绪则用显式占位合同。

## 前置与并行

- 前置：delivery-adoption-snapshot 已归档。
- 与 `merchant-root-ownership` / CF-B5 写集冲突时串行。
- 不新建第二执行器。
- 未改 `go/internal/product/` 商品链隔离路径（并行 `merchant-product-chain`）。

## 只改这些文件

实际写入：

- `go/internal/platform/db/schema/models.go` / `constraints.go`：`product_visual_selections`
- `go/internal/visualsystem/`：CRUD、继承解析、impact、显式选择、HTTP、测试
- `go/cmd/productflow-api/main.go` / `register.go` / `routes_contract_test.go`：挂载路由
- `go/internal/recipe/plan.go` / `dto.go` / `service.go`：`reuse_preview`；提取时偏好版本；Apply 写选择
- `web/src/lib/types.ts` / `api.ts` / `i18n.ts`：合同与四语文案
- `web/src/pages/workbench/canvas/visualReuse.ts(+test)`、`VisualReuseControls.tsx`、`GraphNodeInspector.tsx`、`RecipeLibraryPanel.tsx`
- `docs/ARCHITECTURE.md`、`docs/USER_GUIDE.md`、父章程与本文件

## 不要碰

第二执行器、Skill/grader、跨商家分享/公共市场、看板 README、git commit/push/reset、`go/internal/product/` 商品链隔离。

## 合同

- IQ-CF-07；配方清身份保持。
- Brand 占位：`status=unavailable` / `reason=brand_table_not_ready`。
- 完成 ≠ 跨商家分享/公共市场。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者审核。
- 交接：审核材料已提交；**未提交 git**；状态保持认领，不自行归档。

## 证据

### 实际修改范围

见上文「只改这些文件」。未改 product 商品链隔离、Skill/grader、第二执行器。schema 仅追加 `product_visual_selections`（未改 products 子链 B2 文件）。

### 验证命令与结果

| 命令 | 结果 |
|---|---|
| `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/visualsystem/ ./internal/recipe/ ./cmd/productflow-api/ -count=1 -p 1'` | pass |
| `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/platform/db/schema/ -run "TestApplyExistingHeadKeepsSchema\|TestApplyTwice" -count=1'` | pass |
| `pnpm --dir web exec vitest run …/visualReuse.test.ts …/RecipeLibraryPanel.test.ts` | 2 files / 9 passed |
| `pnpm --dir web exec tsc -p tsconfig.app.json --noEmit` | pass |
| `just docs-check` | pass |
| 浏览器整链（保存→选定→追加→显式采用→第二商品预览） | **未跑** |

### 自审与剩余差距

- Brand 表仍为显式占位，不宣称多品牌实体。
- 浏览器整链与真实商家效率未验证。
- 从配方创建商品后的选择写入走 recipe Apply；from-recipe 原子创建若未走 Apply 需依赖后续工作台选定（product from-recipe 包未改）。
- 未宣称跨商家复用或公共市场。

### 是否需要协调者同步

活文档已写 ARCHITECTURE / USER_GUIDE / 父章程；看板 README 未改。保持认领等审核。

- 审核者：CTO（本会话）；结论：通过。可拆默认入口走查；与 CF-B5 衔接。
