# Backend audit checklist

本清单来自 2026-04-23 对 `backend/` 的只读审查，按“先稳定性、再安全边界、再可维护性”的顺序推进。

## P0 稳定性

- [x] 任务创建和入队要具备幂等语义：已有 active job 时不重复入队同一个 job id。
- [x] worker 执行前要校验 job 状态，重复消息不能让同一个 job 重跑。
- [x] 任务失败和重试语义要统一，不能一边声明 Dramatiq retry、一边在业务层吞掉所有异常。
- [x] 队列发送失败时不能静默留下不可执行的 `QUEUED` job。

## P0 输入和资源边界

- [x] 上传商品图/参考图/会话参考图时限制单文件大小、文件数量和 MIME 类型。
- [x] 上传图片要做真实图片解码校验，拒绝伪造 content-type 或不可解码文件。
- [x] 图片尺寸参数不能只用 `^\d+x\d+$`；预设按钮是前端内置快捷入口，实际生成入口必须统一限制正数和单边 `3840` 安全上限。
- [x] 商品价格、商品名等表单输入要把坏输入转成 400，而不是 500。

## P1 数据和存储安全

- [x] `LocalStorage.resolve()` 必须保证结果留在 storage root 下，防止 DB 脏数据导致任意文件读取。
- [x] 产品列表分页和 page/page_size 参数要下推/限制，避免全量加载。
- [x] session cookie 在生产环境应支持 secure 配置。
- [x] DB engine 应启用连接健康检查，例如 `pool_pre_ping=True`。

## P1 迁移和测试

- [x] 测试不能只依赖 `Base.metadata.create_all()`，要覆盖 Alembic `upgrade head`。
- [x] SQLite 迁移路径要么明确不支持，要么不能因为 ALTER constraint 失败。
- [x] 清理/解释重复 enum migration，避免迁移历史继续漂移。

## P2 API/架构清理

- [x] 旧 `/api/image-chat/generate` 已删除，连续生图只保留 `/api/image-sessions` 持久会话接口。
- [x] 连续生图已改为异步 job：HTTP 入口创建 `ImageSessionGenerationTask` 并返回 202，worker 后台生成，前端轮询任务状态。
- [x] 构建连续生图上下文时不要读取全部历史图片再丢弃，应只读取实际会传给 provider 的最近图片。
- [x] OpenAI 文本 provider 应使用结构化输出或更强 JSON 解析错误处理。
- [x] DB 层补业务唯一约束：active job、generated_asset_id、主图唯一性等。

## 剩余刻意暂缓项

- 继续完善异步生图任务的失败分类、用户可见重试入口和 provider 侧错误排查说明。
