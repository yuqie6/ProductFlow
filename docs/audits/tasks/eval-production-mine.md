# 任务：生产 Turn 回流采证

状态：阻塞
类型：证据
认领者：—
认领于：—
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：生产 mine 已有 Turn 时，由维护者发布 eval-production-tasks（只新增 production origin 任务）

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](README.md)。执行前读适用仓库规则、当前 mine 查询、测试和 diff。

## 做成什么样

对明确授权的生产库执行一次有界、只读 mine，登记时间窗口、库的环境标识、Turn 数、脱敏产物路径和后续是否具备出题条件。本地 dev 结果不能代替生产；有效零样本报告可以完成采证，但不能发布 production-tasks。

## 前置与并行

- 前置：用户提供或指定可用生产只读连接与允许查询窗口，确认生产采证授权。
- 冻结输入：固定 `go/cmd/productflow-agent-evals/` 与 `go/internal/agent/evalmine.go` 版本，固定查询截止时间或记录实际窗口。
- 运行资源：只读生产访问，不迁移、不 seed、不跑 L2 world；产物脱敏且使用独立文件。

## 只改这些文件

- 本文件

## 不要碰

- 生产业务行、schema、Skill、任务 JSON、grader；凭据和未脱敏文本不进入 Git。

## 现在代码在哪

`go/cmd/productflow-agent-evals` 的 mine 入口调用现有 `go/internal/agent/evalmine.go`，命令 `just agent-evals-mine 7`。

## 合同

- 父章程 L6：只读 PostgreSQL，查询有时间范围与行数上限。
- 原始产物只落 `STORAGE_ROOT/agent-evals/`；文档只记录脱敏环境标识，不记录 DATABASE_URL。

## 怎么验收

确认命令使用已授权的生产只读连接后执行：

```bash
just agent-evals-mine 7
```

核对报告窗口、环境、Turn 数和脱敏情况。完成条件是有效生产报告及后续出题判读，缺生产连接不算完成。

## 阻塞与交接

- 原因：2026-09-07 用户明确项目尚无正式商用版和真实商户用户；原缺少生产连接的记录保留，当前不能采集不存在的生产商户材料。开发库不代替生产。
- 解除条件：真实商用环境存在，并由用户指定生产只读入口及授权范围，维护者确认后恢复开放。此任务安排到上线后，不作为当前正式版研发或自进化启动的生产材料前置。
- 跟进者：CTO/协调者在正式商用环境建立后核对条件；当前不反复索取不存在的生产连接。
- 交接：尚未认领，无本 issue 启动的进程或实现 diff。

## 证据

- [历史 dev 记录](archive/eval-live-layers.md)：7 日窗口、115 turns，`agent-evals/mine/mine-2026-09-04T174527Z.json`，不能作为生产证据。
- 待补：生产环境标识、日期、窗口、commit、Turn 数、artifact、审核者和出题判读。
