# 任务：避免单个商家的批量任务占满生成队列

状态：认领
类型：实现
认领者：image_live_review
认领于：2026-09-09T01:51:50+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：新候选的固定争用验证

遵循[任务协议](README.md)。

## 问题来源

固定 e190e250 的正式争用实验中，A 一次提交 100 个短任务，B 每 5 秒提交一个、共 20 个；全局 3 个生成槽，mock 每次 2 秒。任务全部完成且 provider 各一次，但 B 等待 p95 为 74.629 秒，超过既定 10 秒目标。证据 `/tmp/productflow-saas-capacity-e190e250/storage-dev/saas-capacity-e190e250-0909/contention.json`；root 已将账本 Decimal 误判与实际等待超标分开。

## 做成什么样

一个商家的排队批量工作不能长期挡住其他商家的新任务；单商家独用时仍能使用全部可用槽。保留全局并发限制、商家内业务顺序、已运行任务不被抢占，以及既有额度/未知结果语义。以真实 claim/admission/dispatch 因果决定修改位置，不增加会员等级、收费优先级或独立调度服务。

## 初始范围与所有权

image_live_review 初始仅只读追踪 `go/internal/platform/generation`、`go/internal/worker`、`go/internal/imagesession`、`go/internal/graph` 的派发和 claim 调用链，以及冻结争用原始证据。交回真实阻塞机制、最小跨模块修改点、已有可复用调度逻辑、确定性测试方案。root 决定接口/调度规则并登记具体源码写域后再实现，不在调查阶段自行重构。调查产物独占 `storage-dev/generation-merchant-fairness-0909`，root 持本单/看板/Git。

## 前置与并行

容量 e190 专用资源已清理，旧源码和证据不可改。r8 Agent 在独立固定 f9 树中运行，图片实验使用专属端口/数据库；不改这些输入或共享运行配置。生产 Agent、workflow_requests.go、其隔离测试与其他作者的前端/文档修改不属本单。无模型调用；如需 PG 实验，先登记独立前缀。

## 验收

真实 admission 边界的单商家利用率、多商家公平性、全局并发不超限和任务不重复执行回归；共享槽位的 Graph/ImageChat 消费者均有相称证据。实现冻结后按同一 A100/B20 条件采证，失败照录，不改阈值或分母。root 审完整 diff、相关静态/测试、文档与独占资源后交付。初始调查不宣布已修复。

## root 实施裁定

调查证实当前全局 FIFO 一次可将 100 个 generation 信封预取入 broker，商家之间没有轮转；旧 sent_at 会在 retry 清空，原 accept-to-dispatch 不代表首次派发。采用现有 durable dispatcher 的有限准入，不另建队列或 scheduler。

image_live_review 获得 `go/internal/platform/queue/` 的派发实现及相关测试、`go/internal/platform/generation/` 的共享容量锁所有者及相关测试、`go/internal/graph/durability.go` 的锁调用调整独占写域；读取并验证 Graph/ImageSession 既有消费测试，未经根因确认不扩大生产写域。root 持文档和 Git。当前无其他任务写这些模块；r8 使用固定归档树，不受本候选影响。

规则：generation actor 仅 GraphRun/ImageSession，复用已有全局上限；同一容量锁下计入 SENT 与有效 pending lease，限制 broker 预取，保留 worker 的真实槽位检查。商家按 reserved 数、未服务优先、最久 service_at 轮转；服务历史复用 attempts>0 的 updated_at，retry 不清 attempts。两个未服务商家按最早到达优先，禁止 pending head 的 DESC 新客插队；商家内继续 available_at,id。当前仅支持同步 actor 合同，不把 max(running,reserved) 宣称为未来异步 reservation。非 generation actor 原行为保持，预算满不忙循环。

必须覆盖 max=1 / dispatcher limit=1、A 完成后 B 才到达的平局、多个首次等待商家、重试清 sent_at、多 dispatcher 竞争、混合 actor、单商家填槽及 SENT-before-enqueue。使用已有 GORM/事务入口，不写旧数据兼容分支。统计查询先 EXPLAIN，只有实际热点证据才提窄索引。允许独立测试数据库前缀 pf_fair_0909，按包串行；不使用共享业务库或其他代理测试库。确定性验证与 root 审核后才能冻结真实争用候选。

## Graph 内部续取边界

独立 PG 复现确认：同一 GraphRun 只有一个 SENT，但可持续补满三个 running node，B 的 ImageSession 两轮均未被派发。仅 envelope 预算不构成全生成公平。root 扩大必要写域至 `go/internal/graph/execute.go` 及对应 Graph 调度/容量回归；原 queue/generation/durability 写域继续有效，不扩大到 provider、额度、前端或新持久化调度器。

同一 GraphRun 的已运行节点不抢占；有其它商家到期 generation 等待时，当前消费轮已启动过节点后暂停续取，等待自己的在途节点结束；完全空闲时通过现有 ErrLater 释放信封，再参与 dispatcher 轮转。每次新消费轮至少允许一次合法节点准入，避免两个 GraphRun 同时礼让而零进展。当前轮许可为执行栈局部状态，不新增数据库字段。Graph 等待者查询复用现有 async_dispatches 模型与 queue 的 generation actor 常量，在现有容量锁内判断；同商家、未来available_at及普通actor不触发让行。真实槽位检查仍是最终并发上限。

必须覆盖 max=1 两个多节点Graph交替有进展、max=3 长Graph与新ImageSession共享、无竞争单Graph利用全部槽、完全空闲时释放SENT/lease与未知结果不重放。商家内顺序只指claim选择，不宣称并发enqueue后的强执行FIFO。真实混合复现通过后才冻结争用候选。

## 集成审核与候选冻结

主代理已审核 queue/generation/Graph 全部生产 diff。普通 actor 的等待比较覆盖 generation 填满整个 batch 的情况，limit=1/2 回归通过；当前并发 enqueue 不承诺执行 FIFO。统计查询在 100000 条已消费与 120 条 pending dispatch 上测得历史聚合约 23.955ms、pending heads 约 0.156ms；没有新增索引，结果只适用于该夹具。

原 Graph 直接 claim 单测不足以证明实际执行。新增真实商家、商品、schema-v3 graph、run 与 envelope 经 RunDispatcherOnce → queue.Consume → ExecuteRun 的集成回归；A 返回 ErrLater 后 SENT/lease 清除，两个各 8 节点工作流通过有界轮转全部 succeeded，dispatch 全部 consumed。删除使用空 snapshot 的伪集成测试。最终 4 项定向回归通过，完整 Go gate 由 root 在源码冻结后执行；日志 storage-dev/generation-merchant-fairness-0909/root-review/go-full.log。

max=3 Graph/ImageSession 目前只有 admission 边界单测，实际混合执行与 A100/B20 延迟仍待固定候选复验。允许必要的实现候选提交，本单保持认领，不能据此归档或宣称延迟达标。image_live_review 仅在自有 storage 目录准备一次性混合采证脚本；当前不启动栈、模型或负载，不再写生产源码/测试。完整 Go 检查结束后由 root 冻结提交和分配独立运行窗口。

完整 Go 命令已结束：除 auth 外全部包通过，其中 Graph 108.470s、ImageSession 55.511s、queue 16.150s、generation 1.203s。auth 原持久测试库在测试业务执行前被旧 memberships 数据迁移不变量拒绝；保留原库，在独立 pf_fair_root_gate_0909 新库重跑 auth 整包通过（17.759s），专用两个数据库已清理。新库日志 root-review/auth-fresh.log，原全量失败日志不覆盖。该组合证据覆盖本次后端改动；opt-in 真实模型/容量门未执行。

root 最终自审修复两个测试收集回调的并发 slice append，未改生产行为；全部 generation fairness 定向 race 回归通过（15.059s），日志 root-review/queue-race.log。
