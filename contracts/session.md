# Session 合同

- Cookie 名：`session`（Starlette `SessionMiddleware` 默认；`DELETE /api/auth/session` 显式 `delete_cookie("session")`）。
- SameSite：`lax`。
- Secure：环境变量 `SESSION_COOKIE_SECURE`。
- Secret：环境变量 `SESSION_SECRET`，仅 env，最短 16。
- 登录：`POST /api/auth/session`，body `{ "admin_key": "..." }`。`admin_access_required` 为 false 时不校验密钥，仍写入 `is_authenticated`。
- 状态：`GET /api/auth/session` 返回 `authenticated` 与 `access_required`。
- 登出：`DELETE /api/auth/session` 清空 session 并删除 cookie。
- Cutover：Go 发自己的签名 cookie，不复刻 Python itsdangerous 时序签名。允许要求重新登录。
- 请求相关：响应带 `x-request-id`；若请求已带该头则回显。
