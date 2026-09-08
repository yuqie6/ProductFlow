# 任务：覆盖冻结开发集合的持久化操作观察与评分

状态：完成
类型：实现
认领者：capacity_baseline
认领于：2026-09-08T21:06:16+08:00
业务组：Agent 自进化
父账本：agent-self-harness.md
完成后可拆：冻结新候选的完整开发批次

遵循[任务协议](../README.md)。

## 问题来源

r7在真实Go工具可调用的前置通过后，仍发现gradeEvalPersistedWrites仅处理apply的单节点rename/update、缺少discard，误判删除/断边/移动。初始化可读不证明实际操作可评分。原始18条保留于storage-dev/eval-development-0908-r7。

## 做成什么样

冻结L1 75题涉及的每种持久化写工具与操作组合，均能从真实数据库结果判断符合/不符合预期，并将观察不支持/读取失败与正常业务失败区分。不得通过修改题目、评分阈值或丢弃失败获得通过。

## 前置与并行

- 固定参考9f3d25e7与r7原题/world/预期；不修改旧批次。
- 独立PG前缀pf_eval_persisted_0908、产物storage-dev/eval-persisted-operation-0908；不运行模型或共享服务，不占用图片两臂端口/DB。
- 已交回r7并阻塞，capacity_baseline当前仅认领本实现任务。

## 修改范围与所有权

capacity_baseline独占go/internal/agent/eval_persisted_writes_test.go、eval_terminal_observation_test.go、eval_observation_fixture_test.go、eval_user_sim_host_test.go、eval_host_support_test.go、evalworld_test.go，以及agent-service/evals/live-runner.ts、live-runner-observation.test.ts、go-world.ts、go-world.test.ts。必要新回归文件限上述Go eval和Node eval目录，新增共享合同文件前交root核对。root持本单/看板/父文档/Git。

## 不要碰

生产Agent/Graph行为、workflow_requests.go及其商家隔离测试、任务/world/Expect数据、生产Skill/模型配置、图片池及旧证据。无兼容旧形状或自动降级观测。

## 合同

- 从当前冻结任务枚举全部expected工具及operation/path组合；支持清单用于前置覆盖，不能只初始化75个world后宣称全操作覆盖。
- discard核实际proposal终态与graph不被应用；apply核操作后的节点/边/分组/位置/配置和应保持的不变量，支持多操作而非只取operations[0]。复用已有模型与身份映射，不能仅凭调用参数或HTTP成功代替持久化。
- 正常未执行、业务拒绝、错误结果/未达到预期继续计业务失败；仅观察器不支持、真实读回失败等不可测导致unobservable。不能把所有finalObservation.errors一律转unknown。
- 额外变更的判断须来自具体任务/操作合同，不把Expect中未列举的一切动作泛化为禁止；不放宽已有明确禁止项。

## 怎么验收

固定集合涉及操作的真实Go正向写入与反向缺写/错写/额外越界变更，以及Node最终状态分类和Go/Node wire回归。对r7已暴露问题保留旧失败、修复后正确分流。全覆盖矩阵列每项实际执行及读取证据；无模型。相关Go包/Node静态与受影响测试通过，just docs-check与完整diff自审后交root。未覆盖项明确阻止下一付费批次。

## 阻塞与交接

前置已具备，无用户审批依赖；执行者不commit/push，交付后等待分配。

## 证据

首轮实现已交 root 审查，尚未验收，不宣称 Agent 能力提升。

- 首轮独立 PostgreSQL 聚焦回归通过，42.877s；Node 记录为 2 passed / 24 skipped，不能作为 Go/Node 实际集成通过的证据。
- 首轮矩阵枚举 75 题、188 条 expected 路径，但仅引用测试函数名，仍需对应实际执行的操作组合及确认后的读取结果。
- root 要求补验 create_group 对已有成员的归组副作用、边增删与其他边组重排同时出现、图标题及既有边端点/角色的不变量，避免合法操作被误判或额外变更被遗漏。
- 旧 library-observations fixture drift 待定位；不得覆盖旧批次证据或改变冻结题目来通过验证。上述缺口关闭前，不启动下一轮付费 Agent 基线。

补修已通过 Go/Node 实际 PG host 24 项，library fixture 17 项通过且生成内容与冻结快照一致，未重现旧 drift。追加矩阵审查发现 global world 初始化按 Skill 名称决定是否建图，导致全局诊断与跨 Skill 运行题缺少原题指定目标。测试专用补建图不能证明模型使用的 GoHost 正确。root 扩展 evalworld_test.go 写域，要求修正共用初始化、删除测试专用补建逻辑；media-library negative-off-topic-run 的原预期是成功分流并创建待确认运行，不能以业务拒绝记为正向覆盖。题目及 world JSON 保持冻结。

## 最终审核与交付

审核者：root。结果：实现验收通过，随本任务提交；不构成 Agent 模型能力提升或完整开发批次通过。

- 修复操作后的真实持久化判断，区分业务失败与观察/读回失败；图节点、边、分组和多操作、proposal 丢弃、素材库确认及全局运行请求均有相应读取证据。
- 共用 global seed 补齐原题指定图，删除仅供新测试补图和错误业务拒绝分支。当前观察 fixture 只增加该图的元数据；这是修复后的宿主输出合同，旧 r1–r7 运行材料及冻结 task/world/Expect 未改。
- 覆盖矩阵枚举 L1 75 题的 188 条持久化路径：97 条正向读回、64 条 pending/confirmed 读回、26 条预期 pending 读回、1 条 discard 终态读回；全部映射实际执行 case，无 support-only。无写入预期的题不因本矩阵被宣称已完成模型试验。
- 最终 Go 聚焦 PG 组通过（75.797s）；Node 真实 GoHost 26/26 通过（87.67s），包括两道此前漏初始化的全局题；library fixture 回归通过。root 核对最终日志、矩阵与完整任务 diff。未运行整个 Agent 包或真实模型。
- 证据根 `storage-dev/eval-persisted-operation-0908`，最终日志 `go-final-focused-pass.log` / `node-go-world-final.log`，矩阵 `coverage-matrix.json`。已无本任务测试进程。
- 交付范围包含 Go eval 四文件、Node GoHost 回归与当前 library observation fixture；生产 Agent/Graph、业务规则、原题/world/Expect 未改。新开发批次须固定本次修复后的代码与观察 fixture 身份，并等待容量计时窗口释放。
