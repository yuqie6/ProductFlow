# 任务：按实际工具作用域覆盖L1真实观察

状态：完成
类型：实现
认领者：account_backend
认领于：2026-09-08T11:30:00+08:00
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：固定开发批次；前置矩阵通过前不得启动整批付费运行

遵循 [任务协议](../README.md)。

## 问题来源

r6固定c7204726、run 20260908T030817Z-618fbdd2在174/225停止，run-diagnosis-negative-edit-node的跨工具apply/propose仍为不可测。此前创建场景跨工具修复只覆盖intake overlay。生产工具按conversation作用域暴露，单个Skill名称不能证明模型不会调用其它已暴露工具。

## 结果与边界

有界追踪实际工具工厂→L1 client装配→Go宿主与world物化→调用记录/写后观察，消除按Skill留下的同类缺口。真实后端成功/拒绝/未知保持区别；禁止动作仍按原题判定，不因后端支持而放宽Skill要求。不改原题/world/评分阈值/生产Skill/原始r1-r6轨迹，不从无效旧run拼出完整基线。

root先持有设计与实现审核，范围agent-service/evals的L1装配/Go world/相关测试及go/internal/agent/eval_*观察宿主必要修复。baseline执行者仅清理r6独立资源和交回证据，不写这些源码；图片比较任务占imageeval与CLI，不重叠。共享生产Agent正在另会话修改，不能回退或夹带。PG测试另用pf_eval_scope_0908前缀，不启动共享服务或付费模型。

## 验收

必须在下一整批之前，按当前生产暴露工具和全部L1场景建立确定性覆盖证据；至少保留r6原调用回放、跨Skill图写入以及各作用域可达写操作的真实拒绝/成功和写后读取。正常行为失败不能被标成不可测；真正无观察或环境未知继续阻断消费。所有仍不可测项须显式列出，不以一个Skill成功或路由存在宣称全覆盖。受影响Node/Go门、完整diff审阅与docs-check，根审核后提交。

## 已核对的因果点与执行分配

root确认live-runner.ts:runTrial只在skill为graph-editing/product-intake时打开Go宿主，而工具工厂按作用域暴露，run-diagnosis调用apply/propose进入stub的requireGraphAuthority失败。go-world.ts同时存在graph/intake/full三种覆盖范围，full也并非完整工具覆盖。修复须消除Skill名字作为观察是否存在的依据，统一业务工具反馈来源；保持本地harness/lease会话模拟的既有职责。

不得直接把所有L1切到现有intake覆盖后宣告完成：StubWorld的first_write_409/write_409_count/read错误/payload等冻结注入、调用记录参数/时序/结果和写后context仍需保持。检查所有注入读写端及state/library观察消费者，避免真实适配绕过测试合同。只改变观察接线与真实world物化，不改变题目或评分要求；同一方法不能重复记两条成功调用或把失败记unknown。保留无Go权限时的明确不可测，不新增假成功reconcile。

account_backend现独占本任务Node eval装配/Go world/stub必要接线及其测试、Go eval宿主/种子必要文件；root持有合同/文档/Git/最终审核，当前imageeval复验已由root接管，执行者不得再修改imageeval。先交回有界实现边界与确定性覆盖计划，必要实现可自主继续，不等流程批准；新整批模型调用明确未授权。

只读矩阵分配：eval_baseline已完成r6交接并释放旧任务；仅从固定r6 checkout读取75个L1任务/world及生产工具工厂、注入字段，输出作用域/工具/注入覆盖清单到独立 `storage-dev/eval-scope-tool-observation-0908/coverage-inventory.json` 或说明文档。不得改主树源码/任务/评分或运行模型/DB；交回root和account_backend用于校验覆盖边界。account_backend仍为唯一实现写者，root负责集成审核。

## 已交付实现与root审核

L1使用统一full Go宿主，按实际product/global作用域覆盖业务工具；保留注入409、读错误、payload、调用参数/时间/真实结果。收口顺序为manager.close→最终观察→host.close，transcript保留持久化state与readback_errors。业务预期失败和真实读取异常分别处理。事实观察不由Expect.Writes筛选；不存在的request/draft明确为null，意外写入对象仍保留。root删除执行者追加的“未列Expect即禁止”评分规则，继续使用原题要求与既有forbidden/写入评分。

真实PG最终回归由root执行，23项Go-world和2项runner测试共25/25通过，日志`storage-dev/eval-scope-tool-observation-0908/root-pg-final.log`。覆盖跨Skill图写入、原r6 apply/propose精确参数的真实409拒绝、注入重试/读错误/payload、非预期实际写入和预期未写的真实状态。Node完整套317通过（PG门独立实际执行）、tsc和generated contract检查通过。root专属pf_eval_scope_root_0908数据库复核为空，见root-db-cleanup.json。最终75个L1输入逐一启动full宿主、读取当前scope合法数据并最终观察，基础设施/读回错误均为0，41项未执行业务动作的预期失败单独保留。matrix-results-r2.json与补discovery证据的r3、源码前后hash和清理报告保留。首次矩阵暴露空intake_json导致宿主Fatal，已修为既有写入mismatch并补真实PG回归；4个global缺商品场景通过discovery核对后保留真实404/400可观测拒绝，不添加假商品。该矩阵不执行模型或证明Agent任务通过率。

r6保留174/225原始试验、150原始pass/24fail与3次unknown，不生成summary或开发导出；旧批次身份停止后未变，原工具/模型失败不删除。本实现本身不产生Agent能力提升或新完整基线结论。

最终源码指纹见source-sha256-after-matrix-r3.txt，root逐项核对通过；新增未finalize真实PG回归1/1通过，非空畸形JSON仍报错。所有pf_eval_scope_root_0908及matrix_r2/r3独立库清理为空，无残留测试进程。
