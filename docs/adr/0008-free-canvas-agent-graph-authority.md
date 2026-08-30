# ADR 0008: 自由画布、图权威与 Agent 协作合同

## 状态

Accepted。当前在线图是 schema-v3 `workflow_graphs`。节点目录、端口、ChangeSet 操作和执行入口见 [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md) §6。工作台连续动作见 [`docs/USER_GUIDE.md`](../USER_GUIDE.md)；会话归属与 Goal 见 [`0009-agent-canvas-sandbox.md`](0009-agent-canvas-sandbox.md)。

## 背景

决策当时，schema-v2 把提示词、图片计划和运行归属同时放在 Prompt Artifact、节点配置 key 和 edge 里。为了维持三份关系一致，实现要求特定 lineage edge 必须存在，并把提示词节点和图片节点绑成隐藏计划组。

这种模型能表达确认后的固定生产计划，不能作为自由 DAG 的长期基础：用户不能先建不完整拓扑；删画布 edge 不一定删掉运行关系；Agent 通过完整 WorkflowDraft 提出另一份拓扑，形成双图边界。

## 决策

1. 正式工作流只有一份 canonical graph。页面选区、Agent transcript、Task、Draft 和本地 runtime 文件都不是图权威。
2. 权威对象只有 Node、Edge、Artifact。Node 持有配置和当前输出引用；Edge 持有类型、角色、顺序和依赖；Artifact 是一次运行的不可变结果。
3. 用户、Agent 和配方通过同一个 Graph Command 修改该图。写入形状是 `WorkflowChangeSet`。Agent 不拥有第二份正式拓扑，也不建立第二套执行器。
4. 编辑中的不完整 DAG 合法。必要输入只决定节点能否运行，不决定节点能否存在。任何 edge 都可以删除；删除后重新计算下游完整性。
5. 节点有效运行上下文完全由当前 graph revision 的 incoming edges 推导。Prompt Artifact、Visual System、页面选区和 Agent 会话不得隐式增加运行输入。
6. Node Catalog 是连接规则和可编辑配置的唯一 owner。前端与 Agent service 不复制另一份兼容矩阵。
7. 多节点改图先成为未应用的提案，画布预览后一次确认；单次可逆编辑可以直接应用并进入同一条撤销栈。
8. ChangeSet 必须携带 `base_graph_revision`。相交冲突返回结构化冲突，禁止 last-write-wins。
9. schema-v3 直接替换在线图合同。不保留 v2 执行器、plan key 拓扑或 Draft materialization 双读。

## 后果

- 图持久化校验与运行前完整性校验必须拆开。
- Agent 的完整 WorkflowDraft 拓扑不再是商品图的写入合同。
- 当前节点类型、端口、操作列表和 HTTP 入口以代码和 `ARCHITECTURE.md` 为准，不在本 ADR 同步成现状仪表盘。

## 排除方案

- 继续在 schema-v2 上加创建按钮，同时保留 plan-key 所属关系。
- 允许 Agent 直接写工作流表或节点 JSON。
- 让 Agent、配方和用户分别拥有不同图修改 API。
- 为保证工作流始终可运行而禁止删除必要 edge。
- 用坐标、最近节点或当前选择自动推断持久关系。
- 用 last-write-wins 处理 Agent 与用户并发修改。
- 保留 v2/v3 双执行器作为长期 fallback。

## 相关决策

- ADR 0001：业务权威仍在 PostgreSQL；商品拓扑由本 ADR 与 0009 接管。
- ADR 0003：历史 schema-v2 理由已归档。仍有效的 GenerationSpec / DeliverySpec / 一层分组写在 `CONTEXT.md`。
- ADR 0007：Pi 跑模型 loop；本 ADR 的 Graph Command 拥有图写入。
- ADR 0009：人是画布主控，创建写 live graph。
