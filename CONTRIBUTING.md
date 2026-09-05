# Contributing to ProductFlow

[中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

感谢你考虑为 ProductFlow 贡献代码、文档或问题反馈。ProductFlow 当前定位为开源自托管项目，优先保证本地可运行、文档真实、数据和密钥边界清晰。

## 开始前

首次了解或配置项目时，参考 `README.md` 的定位和启动说明。日常修改遵循 [AGENTS.md](AGENTS.md)，按任务读取材料，已读且未变化的上下文可复用：

- 后端修改读取 `go/AGENTS.md`；前端修改读取 `web/AGENTS.md`，并检查相关实现与测试。
- 涉及领域或产品语义时查 `CONTEXT.md`、`docs/PRD.md` 的相关定义；不清楚代码归属时查 `docs/ARCHITECTURE.md` 对应章节。
- 编辑文档时查 `docs/README.md` 的职责；未完成方向写在 `docs/ROADMAP.md`。`docs/adr/` 只用于历史追溯。
- 退休的 FastAPI 树在 `retired/python`，不要合并回主线。不要提交 `.env`、`web/.env`、storage、缓存、构建产物、日志或本地数据库 dump。

## 本地开发

```bash
cp .env.example .env
cp .env.dev.example .env.dev
cp web/.env.example web/.env
docker compose up -d productflow-postgres productflow-redis
just agent-service-install
just web-install
just go-migrate
just go-api
just go-worker
just go-dispatcher
just agent-service-run
just web-dev
```

或使用 `just dev` 一次启动 PostgreSQL、Redis、迁移、API、worker、dispatcher、Pi Agent 和 Web。该命令会先停止已有开发进程并应用迁移；共享环境中先检查其它任务占用，已有可用服务可直接复用。默认 `mock` provider 不需要真实 API key。

## 常用检查

按 [根规则的验证矩阵](AGENTS.md#testing-guidelines) 选择检查。局部逻辑改动运行贴近触发点的回归；共享合同、持久化和跨模块改动扩大到相关读写方。必需验证与可选扩大检查分别记录。

| 范围 | 验证入口 |
|---|---|
| Go 局部修复 / 完整后端门禁 | [go/AGENTS.md](go/AGENTS.md#tests)；完整门禁为 `just go-test` |
| Node.js/Pi 局部修复 / 完整服务门禁 | 根规则中的定向测试与合同检查；完整门禁为 `just agent-service-test` |
| 前端局部修复 / 完整前端门禁 | [web/AGENTS.md](web/AGENTS.md#verification)；完整门禁为 test、lint 和 `just web-build`（含包体预算） |
| 纯文档和工程指令 | `just docs-check`、相关引用与 diff；规则变更检查正反触发场景，不默认运行业务构建 |

真实 provider、容量和固定模型评测按任务合同或所作声明触发，需具备相应环境、授权和资源隔离。报告实际命令、结果及未验证项；通过局部检查不代表完整发布门禁通过。完成标准见 [AGENTS.md](AGENTS.md#completion)。

## 文档风格

正式文档、发布说明、PR 描述和贡献说明应保持具体、可验证，避免模板化交付腔：

- 不使用“这不是……而是……”“不是……而是……”这类空泛对比句。
- 不使用“先把……打通”或宣传式“先……再……”脚手架来包装进度。
- 英文文档不使用 “This is not ..., but ...”“not ..., but ...”“establishes the main loop” 或宣传式 “first ..., then ...”。
- 可以保留真实技术顺序，例如命令执行顺序、迁移步骤、自动保存后运行、故障排查步骤。
- 写当前事实和已验证结果；未来方向要明确标为未实现或计划。

## 代码约定

- 业务后端按 `go/internal/` 竖切，约定见 `go/AGENTS.md`。
- Provider 具体 SDK 调用留在 `go/internal/providers`，不要从路由直接调用。
- 前端 API 请求集中在 `web/src/lib/api.ts`，DTO 类型集中在 `web/src/lib/types.ts`。
- 数据库 schema 变更走 GORM models 与 `go/internal/platform/db/schema` 约束补钉，并尽量补回归测试。
- 涉及上传、storage、secret、provider key 的改动要优先考虑安全边界。

## 提交和 PR

建议一个 PR 聚焦一个主题。PR 描述请包含：

- 用户可见变化。
- 关键实现说明。
- 是否包含迁移或配置变更。
- 已运行的验证命令和结果。
- UI 变更截图或录屏（如适用）。

正式版本 tag 使用 annotated tag，并写中英双语说明。tag message 应包含版本定位、主要包含内容、已验证命令和明确边界；不要把一次性的发布准备清单写进仓库文档。建议格式：

```text
ProductFlow vX.Y.Z

中文：
<一句话版本定位>

包含：
- ...

已验证：
- ...

边界：
- ...

English:
<One-sentence release positioning>

Includes:
- ...

Verified:
- ...

Boundaries:
- ...
```

## 工程知识位置

`AGENTS.md` 保存仓库和分层工程约束，`CONTEXT.md` 保存当前领域边界。`docs/adr/` 是历史档案。产品需求与 PRD 使用 GitHub Issues；委派执行任务的合同和状态由 [本地公共任务池](docs/audits/tasks/README.md) 中的任务文件维护，看板是同步索引。直接修复不强制建单，Git 状态用于检查实际改动和提交归属。
