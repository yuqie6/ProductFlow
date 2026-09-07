# Session 合同

- Cookie 名为 `session`，由 Go `gorilla/sessions.CookieStore` 签名；`HttpOnly`、`SameSite=Lax`、路径 `/`、有效期 14 天。`SESSION_COOKIE_SECURE` 控制 Secure，`SESSION_SECRET` 仅来自环境变量。
- Cookie 引用 PostgreSQL `auth_sessions` 中的会话。每次请求验证会话未过期、未撤销、账号未停用；`users.is_operator` 和 `users.merchant_id` 从数据库读取。
- 空实例通过 `POST /api/auth/bootstrap`，提交 `admin_key`、邮箱、密码和商家名称创建部署管理员及开发商家。`ADMIN_ACCESS_KEY` 仅用于初始化；初始化后不提供共享密钥登录。
- 登录使用 `POST /api/auth/session`，body 为 `{ "email": "...", "password": "..." }`。公开注册使用 `POST /api/auth/registration-code` 与 `POST /api/auth/register`，通过 SMTP 验证邮箱后创建普通账号、自有商家与试用额度。
- `GET /api/auth/session` 返回 `authenticated`、`access_required`、`needs_bootstrap`、`registration_available`。已认证时返回 `user:{id,email,display_name,is_operator}` 与 `merchant:{id,name,status}|null`。匿名不能因旧 `admin_access_required=false` 被标记为已登录。
- 普通账号通过 `users.merchant_id` 直接拥有一个商家，普通账号之间归属唯一。Operator 为独立站点权限，可以没有自有商家。会话不返回成员数组、团队角色或商家选择。
- 浏览器普通业务请求只使用账号自己的商家；请求头中的商家 ID 不授予权限。跨商家根对象统一返回 404。管理员通过 `/api/ops/merchants/{merchant_id}/products` 显式管理目标商家商品，普通账号不能访问该入口；该入口不提供付费生成。
- `DELETE /api/auth/session` 撤销数据库会话并清除 Cookie。退出、停用和撤销后，旧 Cookie 不能继续获取业务数据。
- 浏览器写请求校验配置允许的 Origin/Referer；登录、初始化和验证码入口保持各自限流。响应带 `x-request-id`，请求已带该头时回显。
