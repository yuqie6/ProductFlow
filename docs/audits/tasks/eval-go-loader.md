# 任务：Go 评测 loader 拒绝未知字段

状态：开放
认领者：—
认领于：—
父账本：agent-eval-system.md
完成后可拆：无

读完本文件就可以改代码。认领前不要改「只改这些文件」。认领步骤见 [README.md](README.md)。不要去读其它验收账本开工。

## 做成什么样

Go 加载 `evals/tasks` / `worlds` 时，未知 JSON 字段与 TypeScript TypeBox `extra=forbid` 一样拒绝。非法样例在 TS 与 Go 得出同一结论。

现在 `go/internal/agent/evaltask.go` 的 `readJSON` 用 `json.Unmarshal`，忽略未知字段；只校验必填 id/skill/world 与 world 引用。

## 只改这些文件

- `go/internal/agent/evaltask.go`
- `go/internal/agent/evaltask_test.go`
- 本文件

需要共享非法样例时，只新增 `agent-service/evals/fixtures/invalid/` 下的 JSON（专门给对照测试用的坏样例）。不要改合法 `tasks/*.json`。

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

- 测试名：
- 日期 / commit：
