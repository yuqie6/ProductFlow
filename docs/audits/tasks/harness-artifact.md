# 任务：可哈希的领域壳工件（Self-Harness P1）

状态：开放
认领者：—
认领于：—
父账本：agent-self-harness.md
完成后可拆：harness-attribution.md（P2 归因；合同从父账本 P2 抄齐）

读完本文件就可以改代码。认领前不要改「只改这些文件」。认领步骤见 [README.md](README.md)。不要去读 Self-Harness 总账本开工。本阶段做完之前不要开 P2–P7。

## 做成什么样

出现一份可哈希的 `h_t`：

- 新目录 `agent-service/harness/` 装壳工件（指令段、以后 overlay 的位置、runtime-control 占位）
- `go/prompts/agent/runtime-policy.md` 拆成权限/确认（冻结）与行为段（可编）
- `agent-service/src/pi-runtime.ts` 改为读入该工件，而不是只拼散落 Skill + 整份 policy
- 测试钉住规范哈希：改可编辑段则 hash 变，改冻结段的测试应失败或拒绝

生产 loop 仍是 Pi。不要复活 Go `agent-harness`，不要第二套执行器，不要提案器、Steer、playbook、热切。

## 只改这些文件

- 新建 `agent-service/harness/`（及该目录测试）
- `go/prompts/agent/runtime-policy.md`
- 打包/生成它的现有脚本（agent-service 里引用 runtime-policy 的 generate 路径）
- `agent-service/src/pi-runtime.ts` 的**读入壳**部分；不要借机拆其它职责
- `agent-service/src/pi-runtime.test.ts` 里与读入/hash 相关的用例
- `docs/ARCHITECTURE.md` 里「壳是版本化对象、pi-runtime 读 harness 工件」那一两句
- 本文件

## 不要碰

- `agent-service/.pi/skills/` 正文（本阶段不写 overlay）
- `evals/` grader、任务、runner
- Go `agent_model_invocations` 新列（那是 P2）
- journal / lease / SSE / 确认协议
- Steer 钩子、playbook、Promoter

## 合同

- 三层：Pi 仍是 L1 loop；本任务只把 L2 壳收成可哈希对象。
- 执行时 `h_t` 冻结。本阶段还没有热切。
- 权限与确认段保持「禁止代点确认 / 物化；page snapshot 不是授权」。
- 工具 JSON schema 与 Graph Command 不动。
- hash 算法写进测试；规范序列化，不要随 JSON 键序漂移。

## 怎么验收

```bash
just agent-service-test
just docs-check
```

断言：同一工件两次 hash 相同；改行为段 hash 变；生产路径加载的是 harness 目录而不是只读旧整文件。

## 证据

- harness 目录路径：
- hash 测试名：
- ARCHITECTURE 是否已写「壳是版本化对象」：
- 日期 / commit：
