# 任务：核定 journal 发布与确认的职责收拢收益

状态：开放
类型：证据
认领者：—
认领于：—
业务组：架构重构
父账本：architecture-refactoring.md
完成后可拆：维护者接受收益、权限矩阵和独占范围后发布 journal 实现 issue；允许裁定保留现状而不发单

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。执行前读取适用仓库规则、当前实现、调用链、测试和 diff。

## 做成什么样

回答一个有界问题：让 journal module 吸收在线发布、回执校验、ACK 和重启前缀确认，能否实质减少 TurnRuntime 的协议知识与测试私有侵入，同时保持现有业务权威？

交付在本 issue 的证据节：当前职责/调用方清单、保留现状与最小收拢方案的比较、权限与失败矩阵、拟迁移测试和实现范围建议。明确选择“建议实施”或“保留现状”并说明证据。不得只按文件长度决定，也不得仅复述来源报告。

用户可见行为在本任务中保持不变：对话仍由 Go/PostgreSQL journal 投影，不改变重启恢复或事件发布能力。任务完成只表示调查证据足够，不表示重构已落地或可靠性门槛通过。

## 前置与并行

- 前置：安装锁定的 Agent service 依赖，Node 版本满足 `agent-service/package.json`；无需真实模型凭据或共享 dev DB。
- 冻结输入：每次测试固定所读的 `agent-service/src/` 和对应 Go journal 合同源码；记录 HEAD、相关路径 diff 与测试前后变化。并行 writer 改到输入时重跑受影响验证，不沿用旧结果。
- 运行资源：使用现有 fake HTTP/provider adapter、真实临时 WAL 目录和测试自行分配的本地端口；不得启动/重启共享 dev、暂停 worker、改 provider 配置或向共享数据库写业务数据。
- 可与画布任务并行调查；不得在画布 live 窗口改其代码或配置。共享章程、索引及 Git 写入由维护者串行整合。

## 只改这些文件

- 本文件：填写调查、设计比较、证据与交接。
- 父章程的 AR-02 结论由维护者验收后更新，执行者不直接修改。

如必须加测试/仪器化才能取证，先交回维护者扩展合同；本次发布不授权改 runtime 或测试实现。

## 不要碰

- `agent-service/`、`go/internal/agent/` 的实现、测试、wire schema、生成产物和持久化形状。
- lease 所有权、Go scanner 终态权限、模型请求恢复、Skill、grader、工具协议和能力。
- 画布 autosave、Graph Command、性能任务及其它业务组的活跃文件。
- 新建通用 journal/recovery 框架，或把接口草案写进稳定产品文档。

## 现在代码在哪

- `agent-service/src/turn-runtime.ts`：`appendJournalEvent` 写 WAL；`publishUnpublishedEvents` 入队；`appendPublishedBatch` 处理 5xx、回执与 ACK；`confirmPublishedPrefix` / `confirmMatchingUnpublishedPrefix` 负责恢复比对；`adoptConfirmedTerminal` 处理已确认终态。
- `agent-service/src/journal-publisher.ts`：batcher 的排队、批量、定时 flush、barrier 与 drain；`runtime-journal.ts`：wire 转换、回执匹配、重试分类和审批辅助函数。不要把审批编排顺带搬进发布 module。
- `agent-service/src/store.ts`：`unpublishedEvents` / `publishedThrough` / `markEventsPublished`；`productflow.ts`：`appendTurnEvents` / `confirmTurnEvents` 现有 HTTP adapter。
- `go/internal/agent/http_internal.go` -> `execution.go:AppendEvents` / `event_confirm.go:ConfirmEvents`：追加与确认的不同权限；确认只比对已提交事件，不取得执行权。
- `agent-service/src/pi-runtime.test.ts`：错误回执与 5xx 测试通过强转取得 runtime 并调用私有发布方法；另查该文件的恢复、分叉和 fencing 用例。
- `agent-service/src/journal-publisher.test.ts`、`store.test.ts`、`process-restart.e2e.test.ts`：队列、实际 WAL 与进程重启的已有测试入口。进程测试使用 fake 远端，不冒充真实 Go/PostgreSQL。

完整调用链与候选背景见 [父章程 AR-02](../architecture-refactoring.md#ar-02journal-发布与确认)。

## 合同

- AR-02-A：PostgreSQL 是 journal 权威，ACK 只推进已证明前缀。错误回执不 ACK；重试原事件、原序列，不生成替代事件。
- AR-02-B：在线 append 受 execution/owner/lease 约束；confirmation 不 claim、不续租、不生成业务终态。方案必须分清“确认已存在”和“获准追加”。
- AR-02-C：矩阵至少覆盖错误回执、5xx、已提交但响应丢失、部分前缀缺失、内容分叉、旧 lease fencing、barrier drain，以及已有终态的确认。Node 重启不重放丢失的模型执行，Go scanner 保留丢失执行的终态裁定。
- AR-02-D：TurnRuntime 保留生命周期、等待问题/确认及终态编排。方案应列出哪些调用方必知状态可以删除、哪些仍必须保留；只搬文件或多一层转发不足以支持实施。
- 保留真实临时目录的 WAL 测试与进程重启回归，复用已有 HTTP adapter/fake；不得以更多文件操作 mock 替代持久化证据。

## 怎么验收

```bash
pnpm --dir agent-service exec vitest run src/journal-publisher.test.ts src/pi-runtime.test.ts src/store.test.ts src/process-restart.e2e.test.ts
just docs-check
```

完成条件：

1. 逐一对应当前函数、调用者、状态和副作用，画清在线 append 与重启 confirmation 两条链；标注真实缺陷、维护摩擦、尚未验证风险。
2. 给出至少“保留现状”和“最小职责收拢”两种比较，说明被删除的重复知识与保留的生命周期职责；接口草案仅保留在本任务中，不预先规定新类或方法数量。
3. 权限/失败矩阵每项对应已有测试名、有效执行结果，或明确的缺测及未来回归断言；不把 skipped、没跑或 fake HTTP 结果当作真实 Go gate。
4. 执行上面的无筛选测试，记录通过/失败/跳过数及基线。有效 FAIL 可以构成调查证据，但必须分析它对实施建议的限制；环境缺失或未执行不能关闭。
5. 维护者审核“建议实施/保留现状”的理由。建议实施时列出独占路径、依赖、必须删除的旧调用与测试入口、未来验证门；执行者停止等分配。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：本 issue 尚未认领，无归属本任务的未提交实现、运行进程或资源占用。

## 证据

- 命令 / 日期 / 结果：待认领后填写；父章程的 4 条筛选基线不能替代本任务无筛选验证。
- 基线 commit / 相关路径 diff / 测试输入变化：待填写。
- 职责清单 / 方案比较 / 权限矩阵 / 测试映射：待填写。
- 交付 commit：待填写。
- 审核者 / 结论：待填写，自审须注明。
- Issue 结果 / 业务门槛结果 / 剩余缺口：未执行，未验收。
