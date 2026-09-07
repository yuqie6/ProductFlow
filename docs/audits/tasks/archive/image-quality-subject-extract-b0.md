# 任务：主体保留路线提取链 B0

状态：完成
类型：实现
认领者：sub-iq/image-quality-subject-extract-b0
认领于：2026-09-07T17:57:00+08:00
完成于：2026-09-07T18:10:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：质检失败→未解决项加深；≠R3 / ≠像素保真宣称；生成路径自动 Apply；背景/阴影/比例

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

IQ-CF-04 / CF-B3 已声明 `produce_route=subject_preserve`，但**主体提取→背景/阴影/比例实现链未建**；localedit 供应商修补不能标成像素保真。方向 3 要求摄影生成与保留主体路线可分。

## 做成什么样

1. 最小可测主体提取管线（选型写入证据：本地算法或 provider；禁止假空实现）：输入参考图 → 主体蒙版/抠图产物 → 可接现有资产 lineage。
2. 当图位 `produce_route=subject_preserve` 时，走该管线或明确失败→`route_qualified=false` / 未解决项（与 CF-B3 合同一致）。
3. 自动化：至少成功路径留下可核验蒙版/抠图元数据；失败路径不得标路线合格。
4. **不得**宣称绝对像素保真。≠R3。更新父章程 IQ-CF-04。

## 前置与并行

- 前置：CF-B3、采用硬闸已归档。
- 排他：新包或 `localedit`/`graph` 最窄、本任务、父章程；勿改导出叠层 UI、Agent 额度接线。

## 只改这些文件

- 主体提取实现与测试
- 父章程、本文件

## 不要碰

- 评委/金标；假标 R3；支付/额度无关包。

## 合同

- IQ-CF-04 子集；完成 ≠ R3。

## 怎么验收

- 包测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：实现与验证已就绪，等待协调者审核；**未 commit / 未 push**。

## 证据

- 选型：仓库无现成抠图 provider。B0 默认引擎 = **角点采样背景色差分割** `corner_chroma_local`（四角中位色 + RGB 欧氏阈值 + 最大连通域）；产出 mask PNG / cutout PNG 字节与 SHA-256，`lineage.kind=subject_extract` 供接 ProductImageAsset parent 链。禁止空 stub 恒 pass；**不宣称**绝对像素保真。
- 包：`go/internal/subjectextract`（`ExtractPNG` / `ApplySubjectPreserveGate` / `CheckAsMap`）；窄接线 `graph.ApplySubjectPreserveExtract` 写回 `produce_route.subject_extract`，失败强制 `route_qualified=false` + 未解决项；生成式路线跳过；**未**改 `execute_node` 自动 Apply、采用 HTTP、localedit、web 导出叠层、`go/internal/agent`。
- 正测：白底中央色块 → pass；`mask_sha256`/`cutout_sha256` 与字节一致；蒙版含主体与背景；抠图含不透明主体与透明底；`route_qualified` 保持 true。
- 反测：纯色图 / 空字节 → pass=false → `route_qualified=false` + unresolved；不得 `RouteAllowsDeliveryPass`。
- 命令（2026-09-07）：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/subjectextract/ ./internal/graph/ -count=1 -p 1 -run "Extract|SubjectPreserve|ApplySubject|ProduceRoute|DeliveryQual"'` → PASS
  - `just docs-check` → Documentation contract check passed
- 父章程：IQ-CF-04 仍 `部分完成`（B0 提取闸已交；背景/阴影/比例/生成路径自动 Apply/R3 未宣称）。
- 未宣称：R3；像素保真；复杂背景/透明反光全覆盖；生成路径自动 Apply；背景重绘与阴影。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/image-quality-subject-extract-b0.md` 查询）
- 审核者 / 结论：主代理自审通过（2026-09-07）。复测 subjectextract/graph 相关 PASS；失败强制 `route_qualified=false`；≠R3 / ≠像素保真。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：**完成**（主体提取 B0）。
  - 业务门槛：IQ-CF-04 仍 **部分完成**；R3 **未通过**。
  - 剩余缺口：生成路径自动 Apply；背景/阴影/位置比例；复杂场景质检。
