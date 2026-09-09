# 任务：连续生图的 River 契约回归

状态：完成
类型：实现
认领者：capacity_baseline
认领于：2026-09-09T15:40:00+08:00
完成后可拆：无

## 问题来源

[统一队列实现](pg-queue-integration.md)由 root 接管，连续生图旧信封测试需要适配已实现的 River 入口。

## 做成什么样

保留生成受理原子性、逐候选续跑、已证明失败重试、unknown 不重放、持久化失败及恢复隔离断言；队列观察使用 river_job。补旧任务不能领取显式 Retry 后新执行身份的实际 executor 回归。删除仅验证旧信封内部生命周期的断言，不删业务故障覆盖。

## 前置与并行

候选 `/tmp/productflow-pg-queue-0909`。root 已实现 StageTaskForActor 返回 RiverJob（ID int64、Args.ExecutionID）、五表 queue_execution_id、AssertExecution 在 claim 锁内验证身份、River 原生有限重试和 snooze、死信保留。queue 四项实际 PG 回归已通过。

运行使用原目录 `bash scripts/with_dev_env.sh python3 /tmp/pf-river-test-env.py go test -C /tmp/productflow-pg-queue-0909/go ./internal/imagesession ...`；专用 base pf_river_0909，包测试库仅本执行者使用。不得启动共享服务/真实模型。

## 修改范围与所有权

仅候选 `go/internal/imagesession/*_test.go`。root 独占全部生产代码、queue/schema、graph/agent 测试；另一执行者独占 delivery/localedit 测试。不是唯一开发者，不覆盖他人。无 Git 写权限，不提交/推送/重置。

## 合同与验收

旧 AsyncDispatches、Stage/Requeue/Consume/RunDispatcherOnce 等已删除，无兼容层。以 River 原生作业字段/typed Worker 或业务直接调用验证行为；业务缺陷交 root，不改生产。运行全 imagesession 测试，PG 不跳过，检查自身完整 diff，报告命令和结果。

## 阻塞与交接

编译或接口缺口及时发给 root，其余可独立部分继续。完成后等待主代理审阅，不扩展新任务。

## 证据

结果见下方最终审核；交付随本任务提交。

### 集成验收补充

已有完整包测试通过，主代理正在审阅。为满足父任务真实崩溃合同，本任务补 `river_crash_test.go`：使用原生 River Client + 实际 ImageSession Executor + 本地 HTTP 假供应商，在调用前、外部效果后、业务结果持久化后三个边界实际 SIGKILL 子进程，并由新 Worker/业务恢复器恢复，核对效果、素材、额度及 unknown。仅加/改这个测试文件；不得改生产或其它测试。可使用有标记的加速 River timeout/rescue 配置与公开业务 idle 配置，不能修改数据库时间冒充等待，也不能把直接 Work 调用当原生恢复。使用每场景独立 DB/storage，无付费请求。报告生产默认等待与加速测试的差异；无法覆盖的边界如实说明。

### 最终审核

ImageSession 全包含真实 SIGKILL 三边界在最终串行全量中通过 241.662s。停止重复包测试进程后无 schema 锁冲突；主代理审核真实供应商、素材、effect 与额度断言。生产与加速阈值差异见父任务。 固定候选 ff23c2aa；详细现场结果由父任务持有。审核者 root，子代理自审后由主代理集成审阅，随本任务交付。
