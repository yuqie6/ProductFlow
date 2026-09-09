# 后台队列选型探针

独立 Go module，固定 River 0.47.0 和 asynq 0.25.1。它不接入产品 Worker，也不改变 `go/go.mod`。需要 Go 1.26.5、可创建数据库的开发 PostgreSQL 连接，以及本机 `redis-server`。

```bash
bash scripts/with_dev_env.sh bash -lc 'go test -C scripts/queue-evaluation -v -race -count=1 -timeout=120s'
```

每个测试创建 `pf_queue_probe_<纳秒时间>` 数据库，只在新库迁移和造数据，退出时删除本次库。Redis 测试启动私有 Unix socket 实例，禁用 TCP，不访问 `REDIS_URL` 或已有 Redis 数据。缺依赖或数据库不可达直接失败，不以 skip 作为证据。

- `TestRiverGormAtomicityAndWorker` 使用项目同类 pgxpool → database/sql → GORM 配置，验证业务行与 River job 的外层事务回滚、嵌套 savepoint 回滚、共同提交，以及新客户端消费既有任务时能读到业务行。新客户端不等于真实进程崩溃恢复。
- `TestAsynqRetryDoesNotRepeatUnknownEffect` 使用实际 asynq 重试和合成业务执行器：第一次写入 unknown 后返回错误，第二次读取持久状态并退出；两次消费只有一次模拟副作用。该结果不能代替五类生产执行器的重入验收。

- `TestRiverSIGKILLUnknownDoesNotReplayProvider` 启动独立子 Worker，实际 HTTP 假供应商产生效果后杀死子进程，再由新 Worker 的 River rescue 重新领取并标记 unknown。测试配置 job timeout=100ms、rescue threshold=1s；未修改数据库时间，provider 只收到一次请求。这不是生产恢复时限或全部崩溃点验收。`TestRiverCrashChild` 只是该测试的子进程入口。

这是机制与适配验证，不是吞吐比较、完整迁移或发布验收。实际默认配置与源码可见行为才是本探针范围；River 免费核心不提供的 Pro 能力不计入方案收益。选择、迁移边界与最终验收由 [路线总纲](../../docs/ROADMAP.md#16-统一后台任务队列选型与切换) 管理。
