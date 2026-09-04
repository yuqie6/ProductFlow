# 任务：有界且默认关闭的演化诊断轨迹（Self-Harness P2b）

状态：完成
类型：实现
认领者：主代理-agent-0905-0458
认领于：2026-09-05T05:41:22+08:00
业务组：Agent 能力
父账本：agent-self-harness.md
完成后可拆：P3 需评测 T-07 集合隔离合同；本任务不自动解锁 P4–P7

任务合同以本文件为准；认领与验收遵循 [Issue 协议](../README.md)。确认所有权后读取当前实现，不在认领前预研。

## 问题来源

父章程 P2b 要求可选、有界、默认关闭的生产演化轨迹，落 `STORAGE_ROOT`，不进入 PG 对话 journal。P1/P2 已分别在 `ab4eb8b7` / `abc5158d` 交付工件和归因；当前没有独立演化轨迹，P6 不能据 L1 桩世界宣称线上学习。

## 做成什么样

显式启用后，现有 Pi Turn 执行路径能保存带冻结 harness/Skill/provider/model 身份的结构化诊断轨迹，能定位模型调用、工具执行、错误与终态的先后关系。默认关闭不生成轨迹文件、不增加业务权限。采集故障不得改变 Turn 结果或阻塞 lease/journal。

本阶段提供诊断输入，不做自动归因、打分、提案或记忆更新。不宣称内容已适合直接提交模型，后续消费者仍须遵守脱敏与集合隔离合同。

## 前置与并行

- P2 已完成并提交；不依赖真实生产样本、人工标签或分数校准。
- 不改生产 Skill、行为指令、工具 schema、评测题目、grader、模型配置。不能与同 checkout 的 Agent live 采证并行。
- 只用临时目录、临时端口、fake provider 与包级测试库。图片采池会话占用的浏览器、共享 provider、storage 路径不受影响。不得开启共享 dev 采集或读取未授权生产内容。
- 认领确认：本会话主代理已核对 P2 已提交、无剩余源码 diff，图片组仍保留其记录与资源占用，无轨迹源码交集；本任务自行执行并自审。

## 修改边界

- `agent-service/src/`：轨迹唯一 owner、配置、现有 Pi/Turn/tool 观察接线与直接测试。沿现有实际回调确定最小接线，不重构调度、lease、journal、question/resume。
- 已核对实际 owner：新增 `src/evolution-traces.ts` / 直接测试，`config.ts` / 新配置测试，`runtime-manager.ts` 与 `turn-runtime.ts` 只接观察点，`pi-runtime.e2e.test.ts` 验证开/关的工具 Turn；沿用现有 Pi 事件，不加另一运行器。
- 配置范例、部署配置与当前架构文档：只记录新增 opt-in 配置和存储/隐私限制；确认实际 owner 后在本任务补齐路径。
- 部署 owner：根 `.env.example`、`docker-compose.yml`、`agent-service/Dockerfile`。Agent 非 root 用户使用独立 trace 卷挂载到其 `STORAGE_ROOT/agent-evolution-traces`，不扩大到整个商品素材卷；不改真实 `.env`。稳定文档为 ARCHITECTURE 中英文及 harness README。
- 新诊断产物只写 `STORAGE_ROOT` 下独立子目录；允许临时测试目录，不写 git，不写 PG，不复用业务 session/WAL 文件。
- `.gitignore` 排除任何位置的 `agent-evolution-traces/`，避免自定义仓库内 STORAGE_ROOT 时误入 Git。
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
- 实现：`src/evolution-traces.ts` 是结构投影、独立异步队列和保留策略的唯一 owner。现有 `TurnRuntime` 在 claim 后建立 attempt，观察实际 Pi model/tool 事件，在 journal 终态发布获确认后记 terminal，清理结束后记 footer。不改变模型输入、工具结果、lease 或 journal 协议。
- 限额：单条 4 KiB、单 attempt 64 KiB、待写 128 条（含在途）、最多 256 个自有文件和 16 MiB 预留容量。活动文件不淘汰；空间全被活动 attempt 占用时拒绝新轨迹。部分首次写入失败仍占容量。缺 header/footer、序号缺口、截断或写盘失败不能作为完整证据。
- 隐私：只保留哈希关联键、冻结身份、模型标识、封闭工具/操作名、计数、revision、usage 和终态；原文、结果正文、任意错误文本、路径、URL、媒体与推理不进入记录。参考 [OpenTelemetry GenAI 规范](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-spans.md) 的操作元数据与敏感内容分离，不引入遥测后端，也不宣称符合其完整 schema。
- 配置：默认 `AGENT_EVOLUTION_TRACES=0`。启用须有绝对 `STORAGE_ROOT`，每目录一个 writer。Compose 独立 trace 卷按 Agent UID 10001 初始化；不挂载商品素材卷，不修改共享 dev 的配置或运行进程。
- 2026-09-05：`just agent-service-test`：34 文件、233 passed / 2 skipped；`pnpm --dir agent-service build` 通过。定向 3 文件 21 passed 覆盖关闭不读内容/文件、字段排除、容量/队列、慢盘、部分写盘失败、并发保留与开/关/故障三模式真实 Pi fake-provider 工具 Turn。
- `bash scripts/with_dev_env.sh go test -C go ./internal/agent -run 'TestDurableAnswerCreatesNewAttemptAndInjectsPiToolResult|TestSIGKILL|TestModelInvocationHarnessIdentity' -count=1 -timeout 3m` 通过（9.063s），验证已覆盖的真实 Pi/Go 归因及恢复边界；不把正则表达式本身当作全套 crash gate 通过证据。
- `bash scripts/with_dev_env.sh docker compose config --quiet`、`just docs-check`、`git diff --check` 通过；自定义仓库内 storage 的轨迹路径被 `.gitignore` 排除。Docker 完整镜像重建未验收，此前 P1 的依赖下载阻塞仍未解除；Compose 静态验证不代替容器部署验收。
- 冻结工件 hash 保持 `13e8e19ae0ba1cadc732f5f4ba6d0ffba0da08d5a859671ccb67d72bf52dae21`；无生产 Skill、题目、grader 或模型配置 diff，未采集真实生产内容。
- 自审：主代理-agent-0905-0458；完整源码、未跟踪测试、部署与文档 diff 已检查，其他会话的图片池账本/issue/看板状态不纳入交付。自审补上首次部分写入的容量预留回归。结果满足 P2b 实现合同，随本任务提交；不宣称独立审核、生产部署、G1/G2 或线上学习验收通过。
