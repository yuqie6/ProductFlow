# 已关闭 Issue

这里保存完成或取消的 issue，不再占用 [看板](../README.md)。文件名保持稳定，历史证据不覆写；回归或后续工作新建关联 issue。任务关闭不等于业务组验收通过。归档步骤见看板协议。

| Issue | 关闭结果 | 说明 |
|---|---|---|
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
