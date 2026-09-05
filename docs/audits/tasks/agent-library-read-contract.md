# 任务：补齐 Agent 素材整理的生产可见事实

状态：开放
类型：实现
认领者：—
认领于：—
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：维护者独立更新观察夹具并冻结完整测评，再安排 eval-skills A/B

遵循 [Issue 协议](README.md)。取得确认认领后调查和实现；本单发布未授予执行所有权。

## 问题来源

[测量合同审计](archive/eval-observable-input-contract.md) 发现生产 `ListLibraryAssets` / `InspectLibraryAssets` 返回 `AssetMetadata`，没有素材 revision、tag_names、is_archived。目录名称无法发现目标目录 ID；已归档素材被读取接口排除。`propose_global_draft` 的整理操作要求 expected_revision、before 及目标身份，模型不能从实际可见事实构造可靠请求。旧桩返回内部字段掩盖了问题。

## 做成什么样

商家请求改名、移动、标签、归档、恢复或关联时，Agent 能通过受权限约束的生产读取取得必要身份、当前值和 revision，再提交草案；不会猜 revision、默认 before 或伪造目录 ID。未授权或不存在的资源仍明确失败。

## 前置与并行

- 前置：本单使用已确认的生产字段缺口，可独立修复；上游测量归档给出回归及观察边界。不得在测量任务仍占用夹具时修改评测输入。
- 冻结输入：`.pi/skills/`、`agent-service/evals/` 的题目、grader、world、fixtures、split 与旧 A 产物全部只读。行为修复与评分修订分版本审核；本单不能清掉 observability_blocker 让自身过题。
- 并行：与 eval-skills 涉及同一生产面时串行；当前该任务仍阻塞并保留认领，维护者须确认允许的生产路径。PG 使用独立测试库，不调整共享 provider/worker/浏览器或图片池。

## 只改这些文件

- `go/internal/agent/tools_assets.go`、`dto.go` 及对应 Agent 内部读取路由/DTO/聚焦测试。修改共享 AssetMetadata 前扫描商品图库和全局图库所有读写者。
- 沿真实调用链确需的 `go/internal/library/` 现有查询，不新增平行整理状态或绕开现有权限。
- 若读取 wire 必须扩展，维护者核对后允许 `agent-service/src/productflow.ts`、对应 tool schema/工具适配及生成合同工件；冻结 Skill 与评测不动。
- 本文件、父账本和必要活文档；若根因超出范围，交维护者调整任务，不绕过当前所有权。

## 现在代码在哪

- `go/internal/agent/tools_assets.go`：`libraryMeta`、`ListLibraryAssets`、`InspectLibraryAssets`；`go/internal/agent/dto.go`：`AssetMetadata`。
- `agent-service/.pi/skills/media-library-organization/SKILL.md`：读取 before/revision/目录要求；`go/internal/agent/global_draft_schema.json`：草案格式。
- `go/internal/library/`：资产查询、目录与组织草案应用；`go/internal/agent/eval_user_sim_host_test.go`、`agent-service/evals/go-world.test.ts` 已证实 Agent 读缺字段与独立合法草案确认可持久化，两者不等价。

## 合同

全局 scope、资源身份、乐观并发、确认后写入及已归档素材权限沿用生产合同。读模型不得泄漏 bytes、内部密钥或无关资源；不要用 expected_revision=0、空 before、直接 Service 绕过 wire 验证。发现资源与用户授权修改资源分开。

## 怎么验收

- 真实 Go HTTP + 隔离 PG：从 Agent 实际读返回构造 schema 合法草案，分别覆盖改名、移入现有目录、标签、归档后读取并恢复、关联工作流；确认前不写，确认后复读值和 revision 正确。
- 错 revision、错误 before、错误 scope、不存在目录、非目标资产与未确认的配对失败；商品图库投影回归。
- `just agent-service-test`、`pnpm --dir agent-service build`、受影响 Go packages 聚焦与合同测试、`just docs-check`、`git diff --check`。
- 完成生产读取回归不等于 A/B 能力通过。独立评测 owner 再决定观测阻塞解除、更新 Go 生成夹具及冻结输入；禁止本单改题自证。

## 阻塞与交接

- 跟进者：协调主代理。发布前已检索现有 board，无独立素材读字段修复单；eval-skills 仍保留占用，认领本单前由维护者明确串行窗口与写入路径。
- 当前未认领、未实现，不占用进程或数据库。结果和审核随实际执行补证。
