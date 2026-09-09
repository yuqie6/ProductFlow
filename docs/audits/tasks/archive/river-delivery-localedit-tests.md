# 任务：交付与局部编辑的 River 队列契约回归

状态：完成
类型：实现
认领者：pg_queue
认领于：2026-09-09T15:25:00+08:00
完成后可拆：无

## 问题来源

主代理已接管统一队列实现，本单独立适配交付/局部编辑消费者测试。[总体合同](pg-queue-integration.md)不变。

## 做成什么样

原有业务受理原子性、重试、恢复、终态持久化、商家隔离等回归改为消费 River 队列，保留业务断言；删除仅验证旧信封内部生命周期的重复断言。增加旧执行身份不能领取用户新重试的回归。

## 前置与并行

- 候选 `/tmp/productflow-pg-queue-0909`；root 已固定 `StageTask/StageTaskForActor/RestageTaskIfIdle` 和 `TaskArgs.ExecutionID`、五表 `queue_execution_id`。队列写入同事务；业务显式重试换身份；旧任务在锁内 AssertExecution 被拒绝。
- root 负责生产代码、queue、schema及其它测试。执行者仅写 delivery/localedit 的测试文件，不改生产实现、不提交。
- 专用数据库 base `pf_river_0909`，包测试自动派生 delivery/localedit 后缀；这两个包由本单独占运行。使用共享 `.env.dev` 仅加载凭据后覆盖 DATABASE_URL 数据库名，禁止输出密钥或操作共享服务。

## 修改范围与所有权

`/tmp/productflow-pg-queue-0909/go/internal/delivery/*_test.go` 与 `go/internal/localedit/*_test.go`，仅这些路径。你不是唯一开发者，不覆盖 root 或其他任务的改动。

## 合同与验收

生产 AsyncDispatches/Stage/Requeue/Consume/RunDispatcherOnce 即将全部删除，不新增兼容层。测试用 river_job 原生字段/typed worker 或直接业务 executor 断言，生产缺陷报告 root。两包带实际 PG 的 focused/full tests，通过且未跳库；保留必要失败证据。不要为过测修改业务需求或删实际故障回归。

## 阻塞与交接

依赖当前候选可编译；如公共队列/迁移未就绪，反馈 root 并继续可独立改写部分。停止后报告完整 diff 与命令，主代理审阅。

## 证据

结果见下方最终审核；交付随本任务提交。

### 最终审核

Delivery/LocalEdit 全包、实际 PG 受理回滚并发、旧执行身份无法领取显式重试后的新轮通过。主代理审阅全部交付；并发回归重复十次通过，最终全量两包通过。 固定候选 ff23c2aa；详细现场结果由父任务持有。审核者 root，子代理自审后由主代理集成审阅，随本任务交付。
