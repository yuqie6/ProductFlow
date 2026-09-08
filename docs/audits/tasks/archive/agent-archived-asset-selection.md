# 任务：按管理选择与图片附件语义构造评测输入

状态：完成
类型：实现
认领者：account_backend
认领于：2026-09-09T02:59:42+08:00
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：固定开发诊断中的行为复验

遵循[任务协议](../README.md)。

## 可观察问题

r8 首批 restore-asset 三条轨迹均在模型前失败，错误“全局素材不存在或已归档”：选择已归档素材准备恢复时，PiRuntimeManager.loadInputImages 尝试读取二进制，Go ReadLibraryAssetContent 拒绝，Agent 无法到达恢复业务。证据 `storage-dev/eval-development-0909-r8/agent-evals/evidence/r8-pre-model-asset-load-failure.json`；原始运行 20260908T174928Z-ad09433d，不补抽或修改题目。

## 目标与不变量

当前商家选中归档素材后可以进行合法的查看元数据/恢复业务，不能被不必要的图片预加载提前中止。保持归档状态及已有恢复确认流程；未授权、跨商家、不存在素材仍正确拒绝，不能把读取错误一概吞掉或扩大二进制访问权限。普通未归档图的多模态输入保持可用。

## 范围与所有权

account_backend 初始只读追踪 `agent-service/src/pi-runtime.ts`、相关工具适配与 Go `internal/agent/tools_assets.go` 及实际素材读取/恢复业务。交真实因果和最小代码写域、正反验证；root 确定实现边界后执行，不先猜测采用放宽读取还是取消预加载。调查产物独占 `storage-dev/agent-archived-asset-selection-0909`，root 持文档/Git。不要改 eval task/world/Expect、grader、Skill 或前端。

## 并行与验证

capacity_baseline 独占 eval graders，image_live_review 独占 queue/generation/graph durability；外部作者的 workflow_requests.go 与隔离测试、文档/前端都不可碰。不运行付费模型或共享服务重启。需要 PG 测试时使用独立前缀 pf_archive_0909。确定性验证必须覆盖归档选择可到达合法恢复、未归档图片保留、跨商家/缺失/权限错误不被吞掉，并按最终跨层范围检查 wire 与真实 Go host。完整 diff 及相称回归经 root 审核后交付。

## root 因果裁定与实现范围

root 已回读调用点：生产 initialGlobalTaskTurnInput 使用空 asset_ids，页面管理选择仅在 page_context；评测 live-runner/user-sim 把 selected_asset_ids 无条件当附件，是 r8 模型前失败的直接原因。当前证据不支持生产恢复流程损坏，不修改 Pi runtime 或 Go 素材访问权限。

account_backend 获得 `agent-service/evals/live-runner.ts`、`agent-service/evals/user-sim.ts` 及对应已有输入/运行回归的独占写域。全局管理评测保留 page_context.selected_asset_ids、asset_ids 为空，商品工作流原图片输入继续保留；按现有 scope 判断，不按题目 ID 特判，不新增附件协议或通用抽象。固定 f9/raw225/task/world/Expect 保持不变。

以假模型的真实启动链验证归档管理选择不触发 binary preload 且能进入既有恢复确认路径；正向图片附件仍传给模型，显式归档/缺失/越权附件仍拒绝。使用现有真实 Go host/PG 权限与恢复回归验证边界，不以仅断言字段值代替关键运行证据。无模型付费调用。root 审核、文档和提交。

## 交付与验证

root 已审四文件完整 diff：两个 runner 按现有 scope 区分管理选择与图片附件，没有修改 runtime、权限、题目或生产页面。新增假 Responses provider 通过真实 PiRuntimeManager 启动链执行加载 Skill、读取归档元数据、提出恢复草案，最终 awaiting_confirmation；商品工作流真实出站请求仍包含图片。user-sim 输入回归保留 selected_asset_ids。

焦点 Vitest 23 项通过；Agent 包 38 文件/321 测试通过，2 文件/31 测试按原条件跳过；TypeScript 和生成契约检查通过。独立 PostgreSQL 下的 LibraryRead 事实/确认/冲突、MerchantAgentTools 跨商家权限、restore-asset 持久化操作回归均实际通过。现有显式非法附件拒绝路径未改。测试证据为执行终端，没有持久化日志或本任务 storage 目录，不虚构产物。

本任务假 provider、Go host、runner 与 pf_archive_0909 前缀数据库/连接均无残留，共享服务保留。root diff/文档检查通过，随本任务提交。修复的是测量输入合同，不宣称模型能力提升，也不回填 r8 首批失败。
