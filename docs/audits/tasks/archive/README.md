# 已关闭 Issue

这里保存完成或取消的 issue，不再占用 [看板](../README.md)。文件名保持稳定，历史证据不覆写；回归或后续工作新建关联 issue。任务关闭不等于业务组验收通过。归档步骤见看板协议。

| Issue | 关闭结果 | 说明 |
|---|---|---|
| [eval-unobservable-trial-boundary.md](eval-unobservable-trial-boundary.md) | 完成 | 未知工具结果显式不可测，按原始 trial 拒绝整批开发导出；115 passed / 5 skipped，结构观察与有效基线仍待补齐 |
| [eval-clarification-read-obligations.md](eval-clarification-read-obligations.md) | 完成 | 五题去除无用必需读取，复查 20 道澄清题；安全提问与写入前置事实回归，297 passed / 6 skipped |
| [eval-restart-batch-expectation.md](eval-restart-batch-expectation.md) | 完成 | ACK 丢失恢复测试固定上下文返回窗口并精确比较已提交批次；消除调度相关假失败，不改运行时 |
| [eval-graph-clear-intent.md](eval-graph-clear-intent.md) | 完成 | 三种删节点问法明确保留分组；120 种删除排列及额外写入拒绝回归；新身份须完整重采开发基线 |
| [canvas-local-edit-flow.md](canvas-local-edit-flow.md) | 完成 | mock 绑定下检查器局部编辑可提交、保留谱系、采用/撤销；失败不覆盖节点当前图；隔离 Chromium 2 passed |
| [perf-imagesession-active-status.md](perf-imagesession-active-status.md) | 完成 | 活动 Status/SSE 保持全量 queued/running；省略 prompt 后 300 条固定夹具 584,030B / p95 18.16ms，查询次数恒为 8 |
| [perf-imagesession-recovery-visible.md](perf-imagesession-recovery-visible.md) | 完成 | 连续生图故障可见状态：取消 ctx 仍落 unknown，心跳未过期不恢复，晚到 writer 拒绝；崩溃等待默认 90 分钟 |
| [canvas-full-recipe-entry.md](canvas-full-recipe-entry.md) | 完成 | 创建页完整配方预览/取消/事务确认与丢响应重试；PG 59、Web 650、浏览器 7 passed，24 组语言主题布局 |
| [eval-library-observation-refresh.md](eval-library-observation-refresh.md) | 完成 | Go 素材快照与 L1/L3 观察对齐，修正标签错题，12 条输入阻塞解除；独立审核与哈希一致，开发基线开放待采证 |
| [canvas-asset-recipe-proof.md](canvas-asset-recipe-proof.md) | 完成 | 固定资产、拖入参考、片段确认及完整配方拒绝覆盖通过；actions 78 passed；无图商品入口有效 FAIL 交 canvas-full-recipe-entry |
| [agent-library-read-contract.md](agent-library-read-contract.md) | 完成 | 素材真实 before/revision、目录分页、归档读取和工作流关联观察；六类确认及错误事实回归通过，评测冻结另单独立验收 |
| [canvas-delivery-proof.md](canvas-delivery-proof.md) | 完成 | 浏览器原图/交付图下载与 ZIP 谱系/hash，失败反馈，2 passed；Go delivery 24 tests passed |
| [eval-observable-input-contract.md](eval-observable-input-contract.md) | 完成 | 75 条 L1 逐题审核，成功/未知、真实状态、授权时序与攻击行为评分修复；素材读取缺口仍阻塞完整能力测量与自进化基线 |
| [canvas-run-recovery-proof.md](canvas-run-recovery-proof.md) | 完成 | 隔离 mock 浏览器四条通过：场景选点、修复后重试、只重试失败、保存失败阻止运行 |
| [canvas-workflow-coverage.md](canvas-workflow-coverage.md) | 完成 | 完整操作链核查与 274 条前端回归通过；场景/重试、交付图、资产/配方浏览器证据另单补齐 |
| [perf-imagesession-http-load.md](perf-imagesession-http-load.md) | 完成 | 隔离 25k 会话 / 10k 轮次 / 1k 任务，七条真实 HTTP 路径各 100 样本；详情 258KB、p95 16.33ms，生产并发仍未验 |
| [agent-question-answer-identity.md](agent-question-answer-identity.md) | 完成 | Node/PG 按问题绑定答案、并发幂等与第二问 SIGKILL 恢复通过；固定 live 只问一问，缺上下文读取而 FAIL |
| [eval-contract-alignment.md](eval-contract-alignment.md) | 完成 | 按生产意图路由校正 8 道失真题并冻结 L1 hash `406dc178…`；旧 `71d48f47…` 不可跨题集比较 |
| [eval-user-sim.md](eval-user-sim.md) | 完成 | 独立用户模型、answer/resume 接线与跨 turn 观测；真实五条 2/5 pass，第二问题答案冲突交独立生产修复单 |
| [eval-l2-provenance.md](eval-l2-provenance.md) | 完成 | L2 内容快照、工作树与实际 SDK 请求身份归因；缺样本/漂移不可 complete，未补写历史或重跑真实批次 |
| [eval-l2-terminal-observation.md](eval-l2-terminal-observation.md) | 完成 | L2 等待 PG 终态后评分；观察失败单列并保留双侧状态，重复/race 回归通过，未重跑旧批次 |
| [harness-container-validation.md](harness-container-validation.md) | 完成 | 固定提交原 Dockerfile 完整构建；断网 UID 10001、服务启停和新轨迹卷验证通过，构建临时固定 registry IPv4 |
| [eval-collection-isolation.md](eval-collection-isolation.md) | 完成 | 场景清单可哈希，L1 单集合运行与只含开发材料的导出边界通过；没有真实隐藏/独立验收集证据 |
| [harness-traces.md](harness-traces.md) | 完成 | 默认关闭的结构化轨迹；独立有界队列、敏感内容排除与写盘失败不影响业务的回归通过，未采生产样本 |
| [harness-attribution.md](harness-attribution.md) | 完成 | 同一冻结 hash 贯穿 Pi、checkpoint、invocation、健康与评测；新请求严格验证，历史 NULL 不回填 |
| [harness-artifact.md](harness-artifact.md) | 完成 | 可哈希冻结指令工件由现有 Pi 加载；默认测试、构建与无网络容器加载通过，归因与进化待后续阶段 |
| [perf-dispatcher-latency.md](perf-dispatcher-latency.md) | 完成 | 三轮 500 条真实投递采证；单副本 p95 2.54–2.59s 未达建议目标，双副本 0.77–0.78s；无重复信封 |
| [perf-imagesession-detail.md](perf-imagesession-detail.md) | 完成 | 详情三组任务各 LIMIT 20；队列位置只返回所需 ID，包内测试与目标规模 query-plan 通过 |
| [eval-live-layers.md](eval-live-layers.md) | 取消 | L2、L5、生产 mine 已拆分为三个独立 issue；保留历史 FAIL 与 dev mine 记录，未宣称完成 |
| [eval-adversarial-live.md](eval-adversarial-live.md) | 完成 | 244 条攻击矩阵 ASR=0、效用未下降；D-07/L5-04 通过。L5-05 与 D-08 仍缺 |
| [arch-journal-assessment.md](arch-journal-assessment.md) | 完成 | AR-02 调查结论为保留现状；在线发布与重启确认收拢不能实质减少 TurnRuntime 协议知识，不发实现单 |
| [eval-go-loader.md](eval-go-loader.md) | 完成 | Go loader extra=forbid；共享非法 fixture 覆盖多余字段、未知 enum、重复 id、缺失 world、L2 层级不匹配 |
| [perf-capacity-metrics.md](perf-capacity-metrics.md) | 完成 | `/metrics` 增加 admission running 与 denied；容量满路径按 graph/imagesession 计数 |
| [perf-dispatcher-backlog.md](perf-dispatcher-backlog.md) | 完成 | watch 满批后续投；单副本 500 条 PENDING→SENT p95 0.438s |
| [perf-graph-adopt-concurrent.md](perf-graph-adopt-concurrent.md) | 完成 | 自动采用与 cancel/recovery/mutate 并发 `-count=20` 无死锁；succeeded 则三文稿 generated+ready |
| [perf-imagesession-enqueue-admission.md](perf-imagesession-enqueue-admission.md) | 完成 | 连续生图入队不再持 capacity advisory 或误计 denied；claim 满容量才 later |
| [canvas-inspector-midrun.md](canvas-inspector-midrun.md) | 完成 | 运行中检查器保存与 AR-01 基线贯通；mock 浏览器门 3 passed |
| [canvas-graph-run-undo.md](canvas-graph-run-undo.md) | 完成 | 整图跑中途 HTTP undo 具名钉死；live 回到撤销后文稿 |
| [canvas-c4-remainder.md](canvas-c4-remainder.md) | 完成 | 浏览器整图跑中途撤销与文稿 409 停止；mock 门 5 passed |
| [canvas-c4-run-controls.md](canvas-c4-run-controls.md) | 完成 | 检查器运行该节点、运行到这里与运行中取消；mock 门 8 passed |
