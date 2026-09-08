# 任务：创建确认评测使用真实 Go 执行反馈

状态：完成
类型：实现
认领者：eval_baseline
认领于：2026-09-08T06:17:42+08:00
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：无

任务遵循 [协议](../README.md)。

## 问题来源与结果

[开发基线](../eval-development-baseline.md) 第四批117/225后因finalize_product_intake_v1选择无法匹配预录Go观察停止。模型输入必须走真实Go语义得到可核对的成功或拒绝观察，不靠穷举参考答案或TypeScript仿造后端扩图。不改变题意/评分/Skill或旧结果。

## 前置与并行

旧run20260907T215536Z-e24d4e7f只读保留，宿主与测试库已清理。当前auth/product/quota/schema/Web由管理员工作面任务占用；本任务只使用独立pf_eval_intake_0908前缀DB，不改共享服务或provider，不调用付费模型。

## 范围与所有权

执行者eval_baseline使用Luna/max，排他agent-service/evals观察适配与相关测试、go/internal/agent/eval_*观察测试宿主；必要测试fixture仅由真实Go生成。不改产品runtime、Skill、task题集、grader评分或账户代码。先报告真实触发输入与最小方案，由主代理冻结接口再实现。主代理持有活文档/任务/Git，不push/reset/revert。

## 合同与验收

- 对照117份转录的真实失败调用与Go合同，定位选择匹配缺口；区分合法差异和真实拒绝，不能把全部调用变成功。
- 复用现有Go观察宿主，身份、输入参数、参考图、扩图节点/边/组与持久化状态一致；不要维护新的TS业务实现或穷举合法参数组合。
- 覆盖引发失败的实际输入、同义合法差异、缺参考/非法选择、重复确认；失败无部分写入。查询题集剩余工具观察路径，做不依赖付费模型的输入可测性检查，明确边界而非宣称穷尽自然语言行为。
- Go实际PG观察回归、Node focused tests及相关generated contracts检查；修改共享eval runtime时执行相应完整eval tests。不启动新225次运行，修复提交与主代理审核完成后由原基线任务重新冻结窗口。

## 阻塞与交接

- 原因：无。
- 解除条件：不适用。
- 跟进者：主代理。
- 交接：原基线只读证据已移交；本单独占观察适配，无生产资源占用。

## 证据

- 交付定位：随本任务提交。
- 审核者 / 结论：主代理审核六文件完整 diff、实际失败输入与宿主调用链，通过；源码 SHA-256 与执行者最终交付逐项一致。
- Issue 结果 / 剩余缺口：真实创建确认观察已交付。未调用模型；完整225次开发基线及 Agent 行为质量仍未验收。

## 已冻结的根因与修复

旧转录中的 detail.order=3（数组下标应为1）、scene.order=2/specifications.order=6、未知 recommended_set preset，应收到真实 Go validation。静态桩只匹配四个 canonical selection，将这些可判读错误和未预录合法选择统一变成 eval_unobservable。主代理批准复用现有 TestEvalUserSimHost intake 分支：L1 product-intake（含禁止 finalize 题）走 Go overlay，后续 context 读取真实状态，传递实际幂等 key。不得扩展静态枚举来掩盖缺口，不改题集/grader/Skill。无模型回归覆盖合法非canonical、旧错误入参、缺参考图、replay/conflict、失败无部分写入。观察合同验证完成前不启动下一次付费基线。

## 最终验证与边界

- Go `TestEvalIntakeObservationContract` 与 `TestEvalObservationFixtures` 真实PG通过；错order、未知preset、空/未知参考图与空key为400，同key不同payload为409。合法规范化选择扩图并可从后续context读回，失败与冲突不留部分写入。
- opt-in `evals/go-world.test.ts` 跨Node/Go宿主5项通过；完整evals为121通过、9项跳过，相关Go观察另行执行。generated contracts检查及Agent service build通过。
- 并行启动两个Go检查时曾遇SQLSTATE 40P01建库竞争，串行重跑通过；本次交付不包含数据库基础设施修复。
- intake宿主默认库前缀为eval_intake，当前采证显式覆写pf_eval_intake_0908；复用现有testdb校验与清理。交付时两前缀无库残留、无Go宿主或Vitest进程。
- 证据目录：`.debug/eval-intake-observation-20260908/`（Go、Node、generated contracts、build与清理日志）。未修改题集、grader、Skill、产品runtime或固定canonical fixture。
- 旧run剩余36题中6题属product-intake，其中5题禁止finalize；现已同样接真实宿主以观察违规调用。其余30题不涉及本次intake匹配缺口；没有宣称穷尽自然语言输入。
