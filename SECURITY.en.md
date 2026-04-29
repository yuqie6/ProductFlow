# Security Policy

[中文](SECURITY.md) | English

ProductFlow is a self-hosted project. Deployers are responsible for protecting their admin key, model API keys, database, Redis, file storage, and reverse-proxy entrypoints.

## Supported Scope

Security fixes currently prioritize the latest code on the default branch. The project is still early-stage and does not maintain multiple long-term support versions.

## Reporting a Security Issue

Do not post real secrets, database URLs, cookies, model API keys, private images, or production logs in public issues.

If you discover a security issue, contact the maintainers through a private channel. If the repository hosting platform supports private vulnerability reporting, prefer that feature. A useful report should include:

- Impact scope and reproduction steps.
- Affected commit or version.
- Whether relevant configuration uses default values.
- Minimal logs or screenshots, without real secrets.

## Deployer Security Checklist

- Change `ADMIN_ACCESS_KEY`, `SESSION_SECRET`, and `POSTGRES_PASSWORD`; do not use example placeholders.
- Do not commit `.env`, `web/.env`, storage, logs, database dumps, or `.trellis/tasks/` / `.trellis/workspace/`.
- Enable HTTPS in production and set `SESSION_COOKIE_SECURE=true`.
- Allow backend access only from trusted origins and configure `BACKEND_CORS_ORIGINS` correctly.
- Redis and PostgreSQL should not be exposed to the public internet.
- Provider API keys should live only in private environment variables or the settings page. Do not write them into docs, issues, or PRs.
- Upload and generated-file directories should be backed up regularly and protected with access control according to business needs.

## Known Boundaries

The current version uses a single-admin model. It does not provide multi-user permissions, team audit, object-level access control, or public-registration abuse prevention. Do not expose it directly as a public multi-user service.
