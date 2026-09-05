# 已关闭 Issue

这里保存完成或取消的 issue，不再占用 [看板](../README.md)。文件名保持稳定，历史证据不覆写；回归或后续工作新建关联 issue。任务关闭不等于业务组验收通过。归档步骤见看板协议。

| Issue | 关闭结果 | 说明 |
|---|---|---|
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
| [canvas-inspector-midrun.md](canvas-inspector-midrun.md) | 完成 | 运行中检查器保存与 AR-01 基线贯通；mock 浏览器门 3 passed |
| [canvas-graph-run-undo.md](canvas-graph-run-undo.md) | 完成 | 整图跑中途 HTTP undo 具名钉死；live 回到撤销后文稿 |
