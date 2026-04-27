# Contributing to ProductFlow

感谢你考虑为 ProductFlow 贡献代码、文档或问题反馈。ProductFlow 当前定位为开源自托管项目，优先保证本地可运行、文档真实、数据和密钥边界清晰。

## 开始前

1. 阅读 `README.md`，确认项目定位和本地启动方式。
2. 阅读 `docs/PRD.md` 和 `docs/ARCHITECTURE.md`，理解当前功能边界。
3. 如果要改后端，参考 `.trellis/spec/backend/`。
4. 如果要改前端，参考 `.trellis/spec/frontend/`。
5. 不要提交 `.env`、`web/.env`、storage、缓存、构建产物、日志或 `.trellis/tasks/` / `.trellis/workspace/`。

## 本地开发

```bash
cp .env.example .env
cp .env.dev.example .env.dev
cp web/.env.example web/.env
docker compose up -d
just backend-install
just web-install
just backend-migrate
just backend-run
just backend-worker
just web-dev
```

默认 `mock` provider 不需要真实 API key。

## 常用检查

后端变更建议运行：

```bash
uv run --directory backend ruff check .
just backend-test
```

前端变更建议运行：

```bash
pnpm --dir web lint
pnpm --dir web test:run
just web-build
```

说明：当前根目录 `justfile` 只封装了 `web-build`，前端 lint / Vitest 仍通过 `web/package.json` 脚本直接执行。

文档或开源治理文件变更至少应确认引用的命令、路径和配置文件存在。

## 代码约定

- Python 目标版本为 3.12，Ruff 行宽 120，lint 规则见 `backend/pyproject.toml`。
- 后端保持 `presentation` / `application` / `domain` / `infrastructure` 分层。
- Provider 具体 SDK 调用应留在 `infrastructure/text` 或 `infrastructure/image`，不要从路由直接调用。
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

## Trellis 目录说明

仓库保留 `.trellis/spec/`、`.trellis/workflow.md` 和 `.trellis/scripts/` 作为开发规范和任务工具。`.trellis/tasks/` 和 `.trellis/workspace/` 属于本地任务/开发者记录，不应提交。
