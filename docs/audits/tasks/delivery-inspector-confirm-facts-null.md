# 任务：检查器确认事实 null.length 修复

状态：认领
类型：实现
认领者：sub-ux/delivery-inspector-confirm-facts-null
认领于：2026-09-07T19:35:00+08:00
审核材料提交于：2026-09-07T19:25:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无；≠假标 R2

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。按 [所有权前置规则](README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

[商家成图闭环](archive/delivery-merchant-image-loop-gate.md) 本门 PASS，但检查器「确认事实」UI 触发 `Cannot read properties of null (reading 'length')`；采证改走 `PUT /facts`。商家日常改稿应能点确认，不能只靠 API 绕过。

## 做成什么样

1. 定位并修复确认事实路径的 null 解引用（最窄 causal）。
2. 自动化：组件/ e2e 覆盖确认事实成功且无控制台该错误。
3. 更新父章程一句；`just docs-check`；**≠宣称 R2 通过**。

## 前置与并行

- 前置：闭环门已归档。
- 排他：`web` 检查器/事实确认最窄 + 本任务 + 父章程。
- 勿改：quota、评委、graph compose。

## 只改这些文件

- 相关 web 组件/钩子/测试；本文件；父章程一句

## 不要碰

- 假标 R2；批跑产品化。

## 合同

- §5 改稿可达；完成 ≠ R2 关闭。

## 怎么验收

- 复现失败→修复→测过；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：审核材料已提交；**未 commit / 未 push**；状态保持认领待维护者审核。≠R2。

## 证据

### 根因

「确认事实」仅改 `status`/`requires_confirmation`/`source_type`，键值不变。保存时 `POST .../facts/impact-preview` 经 Go `append([]string(nil), empty...)` 把空 `changed_fact_keys` 编成 JSON `null`。检查器 `shouldShowFactsImpactPreview` 裸读 `.length` → `Cannot read properties of null (reading 'length')`。

### 修复

- `factImpactPreview.ts`：对 `changed_fact_keys` / `nodes` / `default_update_node_ids` 做数组归一后再读 length。
- `GraphNodeInspector.tsx`：影响预览列表对 `nodes` 空值兜底。

### 命令 / 日期 / 结果

| 命令 | 日期 | 结果 |
|---|---|---|
| `pnpm --dir web exec vitest run src/pages/workbench/canvas/factImpactPreview.test.ts src/pages/workbench/canvas/graphNodeEditorDrafts.test.ts` | 2026-09-07T19:23+08:00 | **2 files / 10 passed** |
| `just docs-check` | 2026-09-07T19:24+08:00 | Documentation contract check passed |

- 交付定位：随本任务提交（执行者未 commit）
- 审核者 / 结论：
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：修复已交审；状态仍 **认领**
  - 业务门槛：≠R2；仅闭合检查器确认事实 null.length
  - 剩余缺口：Go 侧空切片 JSON `null` 未改（前端已兜底）；批跑/真实 provider 仍缺
