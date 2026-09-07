# 任务：持久化交付采用与版本快照

状态：完成
类型：实现
认领者：sub-delivery/delivery-adoption-snapshot
认领于：2026-09-07T12:26:00+08:00
审核材料提交于：2026-09-07T12:40:00+08:00
完成于：2026-09-07T12:42:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：品牌视觉复用交互；默认入口裁定若仍缺操作证据则另发走查单

任务合同以本文件为准，按 [Issue 协议](../README.md) 认领并确认所有权后调查。协调者审核：2026-09-07 CTO 通过——delivery 与 Vitest 14 复测通过；schema 采用表已在工作树基线中；未宣称 R2。

## 问题来源

[总纲 §5.5 / §6.4](../../../ROADMAP.md#55-采用与交付)：生成成功仅产生资产；采用表示用户选定本次交付。重跑或局部编辑不得偷偷改变已采用版本。成果视图任务交付后，需要可持久化的交付选择与版本快照，引用现有资产 ID、图位用途、排序、DeliverySpec 与必要来源版本。质量合格判据见 [IQ-CF-08](../../image-quality.md#compete-facts-layout)。

## 做成什么样

用户可对图位明确采用候选；形成可读取的交付快照（不复制图片字节）。重新生成/局部编辑不改已采用快照；局部替换产生新版本。导出预览与下载一致；缺图、重复图位、裁切超界、格式不支持、文字溢出在导出前可定位。确定性尺寸转换不调用图片模型。

## 前置与并行

- 前置：`delivery-workbench-projection` 已交付并归档；成果投影覆盖全部生图节点；本任务可依赖其 UI 入口接线。
- 冻结输入：认领时 HEAD、现有 Graph/资产/DeliverySpec 合同；不改 O1–O7 文稿采用语义为交付采用。
- 运行资源：优先 mock/组件与隔离 PG；真实 provider 非验收必需。
- 排他写入：交付快照模型与相关 workbench/delivery 路径；与商家身份 B0 并行时未改 `auth/` / identity / httpx session；共享 schema 仅追加 delivery 表与 `products.current_delivery_adoption_version_id`。

## 只改这些文件

实际写入：

- `go/internal/platform/db/schema/models.go` / `constraints.go`：`delivery_adoption_versions`、`delivery_adoption_slots`、商品当前指针列
- `go/internal/delivery/adoption*.go`、`http.go`：创建/列表/读取/预览/确保派生/导出
- `go/internal/delivery/adoption_test.go`：采用、重跑不覆盖、并发版本、预览=导出
- `go/cmd/productflow-api/routes_contract_test.go`：新路由列入 `goOpsExtras`
- `web/src/lib/types.ts` / `api.ts` / `i18n.ts`：采用合同与四语文案
- `web/src/pages/workbench/canvas/deliveryAdoption.ts(+test)`、`GraphResultsView.tsx(+test)`、`workbenchResults.tsx`、`GraphCanvasPanel.tsx`：成果视图采用/导出接线
- `docs/ARCHITECTURE.md` / `.en.md`、`docs/USER_GUIDE.md`、父章程与本文件

## 不要碰

第二执行器、Shot 表、商家额度账本、品牌版本存储（留给品牌复用任务）、Skill/grader、共享 provider 设置、`go/internal/auth/`、identity schema、httpx session、Skill/grader/金标。

## 现在代码在哪

`go/internal/delivery/` 拥有采用快照与 ZIP 导出；成果视图 `workbenchResults.tsx` → `GraphResultsView` 接线采用/导出；文稿候选采用仍在 graph/localedit，与交付采用分离。

## 合同

- 交付采用引用资产 ID，不批量作废历史图。
- 已采用快照在节点重跑后仍可读且导出不变；新采用显式产生新版本。
- 与现有 DeliverySpec / ZIP 谱系一致；预览字节与下载一致。
- 角色与商家范围消费商家平台已固定合同；隔离未就绪时单商家验收，不伪造多租户通过。
- 消费图片质量 IQ-CF-08 合格判据：`quality_status=fail` / 文字溢出不得进入已采用交付合格集。

## 怎么验收

Go 持久化与并发回归：采用、重跑不覆盖、新版本、导出一致性。浏览器：成果视图采用 → 重跑节点 → 导出仍为采用版。`just docs-check` 与受影响包测试。完成不宣称 R2 全部通过。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者审核。
- 交接：审核材料已提交；**未提交 git**；停下等分配。

## 证据

### 实际修改范围

见上文「只改这些文件」。未改 auth/identity/httpx session、Skill/grader、第二执行器。共享 schema 与商家 B0 并存：仅追加 delivery 专用表与 products 指针列。

### 验证命令与结果

| 命令 | 结果 |
|---|---|
| `go test -C go ./internal/delivery/ -count=1` | pass（含采用不可变版本、并发 bump、预览=导出） |
| `go test -C go ./cmd/productflow-api/ -run 'SealedHTTPRoutes\|RouteContract'` | pass |
| `go test -C go ./internal/platform/db/schema/ -run 'TestApplyExistingHeadKeepsSchema\|TestApplyTwice'` | pass |
| `pnpm --dir web exec vitest run …/deliveryAdoption.test.ts …/GraphResultsView.test.tsx …/resultProjection.test.ts` | 3 files / 14 passed |
| `just docs-check` | pass |

### 自审与剩余差距

- 浏览器整链（成果采用 → 重跑 → 导出）未跑 opt-in E2E；Go HTTP 已覆盖不可变与导出一致。
- 质量「pass」仍可由客户端声明；图片质量组 CF 闸未齐时默认 `unchecked` 可进快照但不计入 `qualified`。
- 导出采用包需先 `renditions` 并等派生成功；未在成果视图内嵌异步轮询完整态机。
- 未宣称 R2 全部通过。

### 是否需要协调者同步

活文档已写 ARCHITECTURE / USER_GUIDE / 父章程；看板 README 未改。可归档或继续品牌复用拆单。

- 审核者：CTO（本会话）；结论：通过。可拆品牌复用。
