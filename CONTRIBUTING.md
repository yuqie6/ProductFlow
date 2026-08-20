# Contributing to ProductFlow

[中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

感谢你考虑为 ProductFlow 贡献代码、文档或问题反馈。ProductFlow 当前定位为开源自托管项目，优先保证本地可运行、文档真实、数据和密钥边界清晰。

## 开始前

1. 阅读 `README.md`，确认项目定位和本地启动方式。
2. 阅读 `docs/README.md`、`docs/PRD.md` 和 `docs/ARCHITECTURE.md`，理解文档职责与当前功能边界。
3. 如果要改后端，读取 `backend/AGENTS.md`。
4. 如果要改前端，读取 `web/AGENTS.md`。
5. 跨层或产品语义变更先核对 `CONTEXT.md` 与 `docs/adr/`；发布状态和未完成证据记录在 `docs/rollout/`。
6. 不要提交 `.env`、`web/.env`、storage、缓存、构建产物、日志或本地数据库 dump。

## 本地开发

```bash
cp .env.example .env
cp .env.dev.example .env.dev
cp web/.env.example web/.env
docker compose up -d productflow-postgres productflow-redis
just backend-install
just agent-service-install
just web-install
just backend-migrate
just backend-run
just backend-worker
just backend-async-dispatcher
just agent-service-run
just web-dev
```

或使用 `just dev` 一次启动 PostgreSQL、Redis、迁移、API、worker、dispatcher、Pi Agent 和 Web。默认 `mock` provider 不需要真实 API key。

## 常用检查

后端变更建议运行：

```bash
uv run --directory backend ruff check .
just backend-test
```

前端变更建议运行：

```bash
just web-build
```

文档或开源治理文件变更至少应确认引用的命令、路径和配置文件存在。

```bash
just docs-check
```

## 文档风格

正式文档、发布说明、PR 描述和贡献说明应保持具体、可验证，避免模板化交付腔：

- 不使用“这不是……而是……”“不是……而是……”这类空泛对比句。
- 不使用“先把……打通”或宣传式“先……再……”脚手架来包装进度。
- 英文文档不使用 “This is not ..., but ...”“not ..., but ...”“establishes the main loop” 或宣传式 “first ..., then ...”。
- 可以保留真实技术顺序，例如命令执行顺序、迁移步骤、自动保存后运行、故障排查步骤。
- 写当前事实和已验证结果；未来方向要明确标为未实现或计划。

## 代码约定

- Python 目标版本为 3.12，Ruff 行宽 120，lint 规则见 `backend/pyproject.toml`。
- 后端保持 `presentation` / `application` / `domain` / `infrastructure` 分层。
- Provider 具体 SDK 调用应留在 `infrastructure/prompt` 或 `infrastructure/image`，不要从路由直接调用。
- 前端 API 请求集中在 `web/src/lib/api.ts`，DTO 类型集中在 `web/src/lib/types.ts`。
- 数据库 schema 变更需要 Alembic migration，并尽量补回归测试。
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

`AGENTS.md` 保存仓库和分层工程约束，`CONTEXT.md` 保存当前领域边界，`docs/adr/` 保存需要长期解释的架构决策，
`docs/rollout/` 保存具体迁移或发布的已完成项、证据和停止条件。任务状态由 GitHub Issues 与实际 Git 状态承担。
