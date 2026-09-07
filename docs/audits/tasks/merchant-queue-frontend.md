# 任务：队列与前端商家边界（B8）

状态：认领
类型：实现
认领者：sub-merchant/merchant-queue-frontend
认领于：2026-09-07T14:36:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B9 运营面或 B10 双商门；完整隔离门未过不开放第二商家

按 [Issue 协议](README.md) 认领。承接 [B7](archive/merchant-agent-tools.md) 与覆盖矩阵 **B8 / J\***。

## 问题来源

矩阵 J\*：async_dispatches / 五 Actor 重试与 Restage 可能丢商家快照；前端 queryKey / EventSource 缺 merchant 边界，切换或迟到响应可能串商。

## 做成什么样

五 Actor 受理快照含 `merchant_id`；Stage/Restage/recovery 保持原商家；改信封商家无效。前端 queryKey（或切换时整表清空）与 SSE 取消含商家边界；迟到响应不进新商 UI。正例：混合队列下本商 UI 正确。反例：改信封商家；旧订阅；切换后迟到。仍不开放第二商；不宣称 MP-B。

## 前置与并行

- 前置：B7 已归档。
- 排他写入：`go/internal/platform/queue` 及相关 recovery、notify 必要接线、前端 merchant/SSE/queryKey 边界、父章程 B8、本文件。
- 勿改 Skill/grader；勿开放第二商注册。

## 只改这些文件

- 认领后按现场补齐（预期：queue/actors 快照与 Restage 回归、各域 recovery 必要处、web App/docks queryKey+EventSource、父章程、本文件）。确认根因后更新本清单。
- `docs/audits/merchant-platform.md`：B8 / J\* 状态
- 本文件

## 不要碰

- Skill/grader；第二商开放；MP-B 宣称；重做 B0–B7 已交付隔离除非接线必需。

## 现在代码在哪

- 合同：[merchant-platform.md](../merchant-platform.md) 矩阵 J\* / 批次 B8
- 队列：`go/internal/platform/queue`；Actor 列表见父章程
- 各域 recovery：graph / imagesession / delivery / localedit / agent
- 前端：`web/src` queryKey、EventSource（agent / image-session / graph run）

## 合同

- 矩阵 J\*；完成 ≠ MP-B / ≠ B10。

## 怎么验收

- 正测：混合队列本商 UI/任务正确；Restage 商家不变
- 反测：改信封商家无效；切换后旧 SSE/迟到响应丢弃
- 命令：认领后钉相关 `go test` 与 Vitest；`just docs-check` 若改链接

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
