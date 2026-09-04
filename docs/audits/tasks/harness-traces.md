# 任务：有界且默认关闭的演化诊断轨迹（Self-Harness P2b）

状态：开放
类型：实现
认领者：—
认领于：—
业务组：Agent 能力
父账本：agent-self-harness.md
完成后可拆：P3 需评测 T-07 集合隔离合同；本任务不自动解锁 P4–P7

任务合同以本文件为准；认领与验收遵循 [Issue 协议](README.md)。确认所有权后读取当前实现，不在认领前预研。

## 问题来源

父章程 P2b 要求可选、有界、默认关闭的生产演化轨迹，落 `STORAGE_ROOT`，不进入 PG 对话 journal。P1/P2 已分别在 `ab4eb8b7` / `abc5158d` 交付工件和归因；当前没有独立演化轨迹，P6 不能据 L1 桩世界宣称线上学习。

## 做成什么样

显式启用后，现有 Pi Turn 执行路径能保存带冻结 harness/Skill/provider/model 身份的结构化诊断轨迹，能定位模型调用、工具执行、错误与终态的先后关系。默认关闭不生成轨迹文件、不增加业务权限。采集故障不得改变 Turn 结果或阻塞 lease/journal。

本阶段提供诊断输入，不做自动归因、打分、提案或记忆更新。不宣称内容已适合直接提交模型，后续消费者仍须遵守脱敏与集合隔离合同。

## 前置与并行

- P2 已完成并提交；不依赖真实生产样本、人工标签或分数校准。
- 不改生产 Skill、行为指令、工具 schema、评测题目、grader、模型配置。不能与同 checkout 的 Agent live 采证并行。
- 只用临时目录、临时端口、fake provider 与包级测试库。图片采池会话占用的浏览器、共享 provider、storage 路径不受影响。不得开启共享 dev 采集或读取未授权生产内容。

## 修改边界

- `agent-service/src/`：轨迹唯一 owner、配置、现有 Pi/Turn/tool 观察接线与直接测试。沿现有实际回调确定最小接线，不重构调度、lease、journal、question/resume。
- 配置范例、部署配置与当前架构文档：只记录新增 opt-in 配置和存储/隐私限制；确认实际 owner 后在本任务补齐路径。
- 新诊断产物只写 `STORAGE_ROOT` 下独立子目录；允许临时测试目录，不写 git，不写 PG，不复用业务 session/WAL 文件。
- 父章程与任务索引由维护者同步。

## 合同

- 明确单条/单 Turn/总目录容量或保留上限，以及进程内待写队列上限。截断、丢弃和 I/O 错误必须可观察，不得伪装完整轨迹；关闭状态不得读取或创建诊断目录。
- 不记录 API key、授权头、带 token 的 URL、媒体 bytes、绝对路径或未筛选的任意业务对象。优先白名单化结构字段；原始输入、回答、工具大字段与模型内部推理不因开启诊断而自动落入新产物。
- 新输出关联实际冻结 `harness_hash`、基础 Skill hash 与有效 provider/model。跨 attempt 不混淆，结束/异常有可辨状态；崩溃留下的部分轨迹不得宣称完成。
- 不把诊断写入放入 PG 事务，不让磁盘慢写拖住心跳或模型请求。观察逻辑不能改变工具入参/结果或合法操作的先后顺序。
- 不接 Miner、playbook、候选修改、热切或模型可控配置；任何新开关和上限由人工配置持有，不属于可编辑壳面。

## 当前锚点

`src/pi-runtime.ts` 装配 Pi 并使用 `DEPLOYED_HARNESS`；`turn-runtime.ts` 拥有 `checkpointModelRequest` 和 Pi 事件处理；`runtime-manager.ts` 拥有进程调度与健康观察；`config.ts` 为环境配置入口。存储、工具事件和错误类型须认领后按实际读写者核对，不为凑结构另造一套运行器。

## 验收

- 默认关闭零文件/零内容采集；启用后真实 Pi SDK + fake provider 完成一次含工具调用的 Turn，身份与调用顺序可核对。
- 单条/Turn/目录/待写队列限额、部分记录、I/O 失败、并发 Turns、敏感字段排除的确定性测试；执行结果与关闭采集一致。
- `just agent-service-test`、`pnpm --dir agent-service build`、`just docs-check`；若接线影响跨进程协议则运行对应 Go Agent 回归。
- 审核落盘 fixture、冻结工件 hash 不变、无 Skill/题目/grader diff。有效线上采样不在本任务完成条件内。

## 证据

- 发布：2026-09-05，核对 P2 归档与当前看板，无重复轨迹实现任务；L6 生产采证任务因生产访问缺失阻塞，与本任务代码/资源不重叠。
- 待认领后记录实际设计、公开资料借鉴依据、测试结果与自审。
