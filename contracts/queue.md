# Durable 队列合同

PostgreSQL 业务表拥有业务终态、供应商 effect 和额度。River 0.47.0 的 `river_job` 拥有任务投递、延迟、基础设施重试及停止状态。Redis 仅用于认证限流。

```text
同一 GORM SQL 事务：业务受理 + River InsertTx
  -> COMMIT
  -> River typed Worker 领取
  -> 业务行锁内核对商家与执行身份
  -> 已持久化的成功 / 失败 / unknown / 取消
```

- `StageTaskForActor` / `StageTask` 要求调用方已有显式 GORM 事务；包含商品图片补偿包装的事务仍使用同一底层 sql.Tx，不接管 Commit/Rollback。
- `TaskArgs` 携带 actor、merchant_id、aggregate_id、execution_id。五类业务表持久化 `queue_execution_id`，显式业务重试旋转身份，自动恢复与候选续跑保持身份。旧 job 在领取行锁内被拒绝；原有 attempt/租约继续保护晚到写回。
- Busy/Later 使用 River snooze；基础设施错误最多尝试 10 次。已持久化业务终态结束 invocation。Provider 结果无法证明时保持 unknown，队列重投不授权再次调用供应商。Delivery 不使用 unknown。
- Cancelled/discarded 作业保留并阻止自动 restage。已 completed 的 invocation 可通过原生 JobRetryTx 修复未完成业务投影；不能把作业 completed 当业务成功。
- Graph/ImageSession 的全局容量和商家公平仍由业务 admission lock、实际占用及服务次序决定。River 队列分组隔离生成、交付、局部编辑和同步消费资源，不替代跨实例业务容量。
- `productflow-dispatcher` 仅运行业务恢复与额度待对账过期，不向 Redis 发布任务。Worker job timeout 为 30 分钟，rescue 阈值为 35 分钟；业务闲置阈值与执行租约仍须满足。连续生图默认闲置恢复为 90 分钟，不承诺在 River rescue 后立即再次生图。
- `productflow-migrate` 串行调用 River 原生逐版本迁移及业务 schema 事务。未处置的旧 pending/sent/dead 阻止切换；迁移不会自动重放 unknown 或抹去停止记录。

## 任务类型

River kind 固定为 `productflow_task`；下列 actor 选择现有业务执行器。

| actor | 业务行 | River queue |
|---|---|---|
| `run_workflow_graph_run` | `WorkflowGraphRun` | generation |
| `run_image_session_generation_task` | `ImageSessionGenerationTask` | generation |
| `run_delivery_rendition_job` | `DeliveryRenditionJob` | delivery |
| `run_local_image_edit_task` | `LocalImageEditTask` | local_edit |
| `run_agent_turn_sync` | `AgentTurnProjection` | agent |

原生队列回归与业务 handler 回归分别验证 River 生命周期和业务副作用。完整切换门由 [路线 §16](../docs/ROADMAP.md#16-统一后台任务队列选型与切换) 管理。
