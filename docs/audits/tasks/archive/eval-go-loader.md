# 任务：Go 评测 loader 拒绝未知字段

状态：完成
类型：实现
认领者：主代理-0905-0431
认领于：2026-09-05T04:31:07+08:00
业务组：评测
父账本：agent-eval-system.md
完成后可拆：无

本任务已关闭。认领与归档步骤见 [Issue 协议](../README.md)，业务组结论见[父账本](../../agent-eval-system.md)。

## 前置与并行

- 前置：无。
- 冻结输入：本任务修改 L2 loader，不得在同一 checkout 的 L2 live 冻结窗口开工；对照测试固定 TS schema 与任务集版本。
- 运行资源：全量 Go 测试需要测试数据库；与其他任务共享数据库时由维护者安排测试窗口，禁止重建对方正在使用的库。

## 做成什么样

Go 加载 `evals/tasks` / `worlds` 时，未知 JSON 字段与 TypeScript TypeBox `extra=forbid` 一样拒绝。非法样例在 TS 与 Go 得出同一结论。

现在 `go/internal/agent/evaltask.go` 的 `readJSON` 用 `json.Unmarshal`，忽略未知字段；只校验必填 id/skill/world 与 world 引用。

## 只改这些文件

- `go/internal/agent/evaltask.go`
- `go/internal/agent/evaltask_test.go`
- 本文件
- `agent-service/evals/fixtures/invalid/` 下对照用坏样例

## 不要碰

- `eval_state_gopg_test.go`、`evalworld*.go`、`evalmine.go`
- `go/internal/agent` 其它生产路径（turns、journal、recovery）
- TypeScript `evals/schema.ts` / `loader.ts`（它们已经 extra=forbid）
- Skill、任务正例

## 合同

- 未知字段、无效枚举、重复 ID、缺失 world、层级不匹配 → 加载失败。
- 不要为了让旧坏 JSON 过测试去加兼容解析。
- 合法任务集加载后，L2 过滤器 `LoadEvalTasks(..., "l2")` 仍要求 ≥15 且每技能 ≥3。

## 怎么验收

```bash
go test -C go ./internal/agent -count=1 -p 1 -run 'EvalTask'
just go-test
```

对照测试至少覆盖：多余字段、未知 enum、重复 id、引用了不存在的 world。TS 侧已有拒绝行为时，用同一份非法 JSON 断言 Go 也失败。

## 证据

- 测试名：`TestLoadEvalTasksIncludesL2Contract`、`TestLoadEvalTasksRejectsInvalidSharedFixtures`（多余字段、page_context 多余字段、world 多余字段、未知 enum、重复 id、未知 world、L2 缺 `expect.state`）。
- 日期：2026-09-05。`go test -C go ./internal/agent -count=1 -p 1 -run 'EvalTask'` 通过；同包 `go test -C go ./internal/agent -p 1` 在 `just go-test` 中为 ok。
- TypeBox 对照（只读 `evals/schema.ts`，未改 TS）：`extra-field-*.json` / `unknown-enum-task.json` 拒绝；`unknown-world-task.json` 与 `duplicate-id-*.json` schema 接受、Go/TS loader 均因引用或重复 id 失败；`layer-mismatch-task.json` TypeBox 接受（`state` 仍可选），Go 按合同拒绝。
- `just go-test` 在共享工作树失败，与本切片无关：`TestSealedHTTPRoutesAreRegistered` 缺 fidelity-checks 路由；`TestApplyEmptyDatabaseMatchesHeadConstraints` 缺 `agent_model_invocations.harness_hash`。未改这些包。
- 自审：未改 `evalworld*.go` / `evalmine.go`；`PageContext` 仍为 `map[string]any`，`Inject.Payload` 仍为 `map[string]string`。合法任务字段补进结构体，没有为坏 JSON 加兼容解析。

## 审核

- 审核者：主代理-0905-0431（自审，非独立审核）
- 结论：通过。issue 完成。T-01 / L2-02 仍为部分完成：TrialRecord 的 Go extra=forbid 与导出 JSON Schema 运行时校验不在本切片。
