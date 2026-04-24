# Security Policy

ProductFlow 是自托管项目。部署者负责保护自己的管理员密钥、模型 API key、数据库、Redis、文件存储和反向代理入口。

## 支持范围

当前安全修复优先覆盖默认分支上的最新代码。项目处于早期阶段，暂不维护多个长期支持版本。

## 报告安全问题

请不要在公开 issue 中贴出真实密钥、数据库 URL、Cookie、模型 API key、私有图片或生产日志。

如果你发现安全问题，请通过私有渠道联系维护者；如果仓库托管平台支持 private vulnerability reporting，请优先使用该功能。报告中建议包含：

- 影响范围和复现步骤。
- 受影响的 commit 或版本。
- 相关配置是否使用默认值。
- 最小化的日志或截图，且不要包含真实 secret。

## 部署者安全清单

- 修改 `ADMIN_ACCESS_KEY`、`SESSION_SECRET`、`POSTGRES_PASSWORD`，不要使用示例占位符。
- 不要提交 `.env`、`web/.env`、storage、日志、数据库 dump 或 `.trellis/tasks/`。
- 生产环境建议开启 HTTPS，并把 `SESSION_COOKIE_SECURE=true`。
- 只允许可信来源访问后台，正确配置 `BACKEND_CORS_ORIGINS`。
- Redis 和 PostgreSQL 不应暴露到公网。
- Provider API key 只放在私有环境变量或设置页中，不要写进文档、issue 或 PR。
- 上传目录和生成文件目录应定期备份，并按业务需要设置访问控制。

## 已知边界

当前版本是单管理员模型，不提供多用户权限、团队审计、对象级访问控制或公开注册防滥用能力。请不要把它直接暴露为公众多用户服务。
