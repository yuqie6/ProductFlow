# 内部业务组

各组按可独立验收的结果组织工作。组内任务按本组序列推进，组外消费已经可用的版本化合同与产物。组名不构成永久代码领地；一个因果问题需要跨 Node、Go 和 Web 时，由同一任务覆盖必要调用链。

本目录维护六份组章程，一组一文档。[公共任务池](tasks/README.md) 保留唯一任务状态、认领和归档流程。组内没有具备前置的工作时停止发单，不以任务数量衡量进度。2026-09-07 根据用户明确的“可自托管多商家 SaaS + 自营站点”目标重新校准，当前未正式发布，也没有真实商户数据；产品决策及竞品依据见 [总纲](../ROADMAP.md)。

## 当前业务组

| 组 | 章程 | 当前结果与状态 | 组内任务序列 | 可消费的固定输入与边界 |
|---|---|---|---|---|
| 商家平台 | [merchant-platform.md](merchant-platform.md) | R1/MP-A/MP-B 已过；MP-C B0 骨架已交付；MP-D 未齐 | 冻结全链路覆盖与实施批次 → 身份和资源归属 → 业务/异步/Agent 隔离 → 商业额度与运营权限 → 双商家验收 | 完整隔离验收前不开放互不信任的第二商家；额度入口接线与 R5 仍开放 |
| Agent 质量 | [agent-eval-system.md](agent-eval-system.md) | 交付正确的商家 Agent 行为及可信的测量；图观察已交付，固定基线与候选复验仍阻塞 | 冻结题集 → 行为修复 → 独立复验 → 交付可复用基线；新商家行为在授权合同固定后纳入 | 当前产品合同与固定代码；题目修订和被测行为修订分任务串行，不等待自进化系统 |
| Agent 自进化 | [agent-self-harness.md](agent-self-harness.md) | 固定模型组合下自动发现、修改、验证和有界多轮改进，最终一次人工审批；尚未实现 | 固定实验输入与隔离执行 → 自动挖掘及候选验证 → 多轮运行与审批交付 | 可测开发包、固定评测与业务合同；当前有一次性启动阻塞，跨模型泛化与高级反馈系统不作为前置 |
| 图片质量 | [image-quality.md](image-quality.md) | 对成图内容与视觉改进负责；旧闸门未过，单图卖点候选待付费复验 | 有效参考/金标 → 冻结对照 → 事实与图种归因 → 保真/文字/生成链改进 → 复验 | 固定模型、评委与预算；不扩池凑数，不依赖 Agent 分数 |
| 工作流体验 | [canvas-test-system.md](canvas-test-system.md) | 承担从创建到采用、交付和复用的完整体验；已有配方与局部编辑不重复立项 | 完整成果投影 → 操作走查 → 采用/交付快照 → 品牌复用交互 → 整条任务验收 | 当前 Graph/资产/运行合同可用；新视图先验证，默认入口不预先裁定 |
| 平台可靠性 | [performance-governance.md](performance-governance.md) | 增加正式安装、恢复、升级与多商家资源职责；机制已有，发行证据未齐 | 发行差距与恢复清单 → 必要部署/执行修复 → 用量事实与容量 → 固定候选恢复/升级验证 | 固定业务合同与隔离资源；商业定价归商家平台，历史 Gate 不替代候选验收 |

表中序列用于安排完整结果的推进次序，不要求每一步发布任务。直接交办由主代理连续负责；需要委派、交接、并发协调或独立冻结采证时按 [任务协议](tasks/README.md) 发布。具体任务状态与占用只读 Issue 看板。

## 人员与容量安排

- 用户拥有产品范围、费用授权、经营安排和最终进化批准权。当前主会话以 CTO/协调者身份交付这次重划、任务合同和自审，不代替这些决定。
- 新增商家平台负责岗位；其执行负责人待实际任务认领。现有组保留原成果归属，后续负责人与执行者同样以当前认领为准；历史会话标识不视为仍在岗。
- 工作流体验承担商品交付负责人职能；图片质量承担内容与视觉技术负责人职能；平台可靠性增加发行运维职能。组边界按结果，不按前后端拆单。
- 有限执行容量优先覆盖商家底座、商品交付/图片质量、Agent 质量/自进化三条线；同文件和冻结运行资源串行。没有被实际指派的执行者保持待认领，不登记名义并行团队。
- 当前不新设增长组。试用协议、完整案例和成本记录由协调者组织；真实邀请与自营收费需要实际经营安排，现有开发数据不能成为获客业绩。

## 职责裁定

- 按本次交付结果指定唯一主组。修复普通 Agent 行为归 Agent 质量；实现自动进化机制归 Agent 自进化；图片质量、商家操作和运行基础按上表结果归属。跨层问题不在过程中逐层转单，由协调者一次确认完整必要写入范围。
- 一条交付链中必需且尚未实现的验证入口、夹具或适配由该交付组纳入组内前置，独立验收后固定使用。公共实现仍只有一个 owner；不为避免等待复制 runner、grader 或业务后端。
- 评测独立通过冻结版本、隐藏材料、受保护评分与候选写权限隔离实现。运行固定评测不需要其它组逐次接单。题目/评分合同确有错误时单独修订，既有候选分数失效并重建基线；不能在同一候选里改题刷分。
- 组外输入必须已可用，注明版本、使用方式和验收证据。输入不具备时，协调者将必要建设纳入交付链，或明确一次性启动阻塞；不能将等待中的组称为可独立开工。长期互相等待说明分组或任务范围需调整。
- 组内默认按本组序列串行。独立旁支满足不同写入范围、固定输入和资源条件时才并行；公共 Git、同一文件和共享 DB/provider 的互斥仍按看板执行。
- G-06/G-07 等全产品发布条件由协调者消费各项精确版本证据后汇总，不新增发布组，不把所有组的全部未完成目标变成每个局部修复的前置。
- 商家平台拥有身份、访问权和商业额度不变量；平台可靠性拥有持久调用事实、并发预留的执行正确性及资源调度。一次跨层额度实现指定唯一主任务，不能让两组各建一套账本。
- 图片质量定义事实、保真、文字/布局和视觉复用的质量合同；工作流体验拥有用户编辑、采用和交付流程。品牌版本或二维组合等跨层任务按主要交付结果指定一组全链负责，另一组提供固定验收输入，不同写者不得同时修改同一模型。

## 当前任务协调

| 现有任务 | 承接与次序 | 本次变化 |
|---|---|---|
| merchant-isolation-contract | 商家平台覆盖矩阵 | 2026-09-07 完成归档 |
| merchant-identity-skeleton | 商家平台 B0 | 2026-09-07 完成归档 |
| merchant-root-ownership | 商家平台 B1；根表 merchant_id | 2026-09-07 完成归档 |
| merchant-product-chain | 商家平台 B2；商品链隔离 | 2026-09-07 完成归档 |
| merchant-graph-recipe | 商家平台 B3；Graph/配方隔离 | 2026-09-07 完成归档 |
| merchant-image-session | 商家平台 B5；会话生图隔离 | 2026-09-07 完成归档 |
| merchant-library-binding | 商家平台 B4；图库绑定隔离 | 2026-09-07 完成归档 |
| merchant-delivery-localedit | 商家平台 B6；交付与局部编辑隔离 | 2026-09-07 完成归档 |
| merchant-agent-tools | 商家平台 B7；Agent 工具链隔离 | 2026-09-07 完成归档 |
| merchant-queue-frontend | 商家平台 B8；队列与前端边界 | 2026-09-07 完成归档 |
| merchant-ops-surface | 商家平台 B9；运营面最小集 | 2026-09-07 完成归档 |
| merchant-isolation-gate | 商家平台 B10；双商隔离门 MP-B | 2026-09-07 完成归档（自动化门通过；未开放第二商） |
| release-r6-readiness-gate | 平台可靠性 R6 汇总门 | 2026-09-07 完成归档；**R6 未通过** |
| release-r6-clean-candidate-gate | R6 干净冻结候选重跑 | 2026-09-07 完成归档；G-07 PASS；**R6 未通过** |
| release-r6-pin-and-formal-d4 | R6 候选 pin + 正式 D4 | 2026-09-07 完成归档；pin+D4 PASS；**R6 未通过** |
| release-r6-resource-budget | R6 资源预算↔部署规模 | 2026-09-07 完成归档；空闲/轻负载；非 SLA |
| release-r6-close-ruling | 总纲 R6 关闭裁定 | 2026-09-07 完成归档；**R6 通过** |
| workbench-remember-last-view | 工作流体验；记忆上次视图 | 2026-09-07 完成归档 |
| release-readiness-baseline | 发行差距 | 完成归档 |
| release-compose-proxy-overlay | 发行 B1 | 完成归档 |
| release-versioned-artifact | 发行 B2 | 2026-09-07 完成归档 |
| release-backup-restore | 发行 B3；备份恢复 | 2026-09-07 完成归档 |
| release-d3-restore-drill | 发行 B4；D3 恢复演练 | 2026-09-07 完成归档 |
| release-n-to-n1-upgrade | 发行 B5；N→N+1 | 2026-09-07 完成归档 |
| delivery-workbench-projection | 成果视图 | 完成归档；默认仍为流程 |
| delivery-adoption-snapshot | 采用快照 | 2026-09-07 完成归档 |
| brand-visual-reuse | 品牌视觉复用 | 2026-09-07 完成归档 |
| workbench-default-entry-walkthrough | 默认入口走查 | 2026-09-07 完成归档；建议暂不全局默认成果 |
| workbench-conditional-results-default | 有产出时默认成果 | 2026-09-07 完成归档 |
| compete-facts-layout-contract | 事实/排版合同 | 完成归档 |
| compete-facts-layer-gate | CF-B0 事实分层闸 | 2026-09-07 完成归档 |
| compete-facts-text-trace | CF-B1 图位文字追溯 | 2026-09-07 完成归档 |
| compete-facts-impact-preview | CF-B2 变更影响预览 | 2026-09-07 完成归档 |
| compete-facts-produce-route | CF-B3 双路线声明 | 2026-09-07 完成归档 |
| compete-facts-controlled-layout | CF-B4 受控二维排版 | 2026-09-07 完成归档 |
| compete-facts-brand-inherit | CF-B5 品牌/视觉继承 | 2026-09-07 完成归档 |
| image-quality-content-pilot | 图片质量真实对照 | 2026-09-07 完成归档；16/16；闸门 2/8；≠全面改善 |
| image-quality-selling-point-fidelity | 卖点保真与一理由对齐 | 2026-09-07 完成归档；确定性修补；无 live；≠R3 |
| compete-adoption-hard-gate | IQ-CF-08 采用硬闸 | 2026-09-07 完成归档；服务端强制 text/route_qualified；≠R3 |
| merchant-r1-close-ruling | 总纲 R1 关闭裁定 | 2026-09-07 完成归档；**R1 通过**（夹具双商；CreateMerchant 仍 409） |
| merchant-mp-c-quota-b0 | 商家平台 MP-C B0 额度账本 | 2026-09-07 完成归档；骨架+包测；入口未接线；≠R5 |
| image-quality-selling-point-live | 卖点保真 live k=1 | 2026-09-07 完成归档；闸门改善、保真平 4；≠R3 |
| merchant-mp-c-wire-b1 | 商家平台 MP-C B1 图会话额度接线 | 2026-09-07 完成归档；Generate Reserve/Settle/Release/MarkUnknown；≠R5 |
| delivery-r2-core-path-gate | R2 核心路径门 | 2026-09-07 完成归档；本门 PASS；总纲 R2 未通过 |
| image-quality-ocr-trace-b0 | 成片 OCR 追溯闸 B0 | 2026-09-07 完成归档；字形模板对照；≠R3 |
| merchant-mp-c-wire-b2-graph | 商家平台 MP-C B2 Graph 额度接线 | 2026-09-07 完成归档；image_generation Reserve/Settle；≠R5 |
| image-quality-ocr-adoption-wire | 采用路径默认 OCR | 2026-09-07 完成归档；CreateAdoption 对照；≠R3 |
| merchant-mp-c-wire-b3-agent | 商家平台 MP-C B3 Agent 额度接线 | 2026-09-07 完成归档；before_model_request；≠R5 |
| delivery-export-overlay-fix | 成果页导出叠层可达 | 2026-09-07 完成归档；UI 真实点击；≠R2 |
| image-quality-subject-extract-b0 | 主体提取 B0 | 2026-09-07 完成归档；角点色差分割；≠R3 |
| image-quality-subject-extract-apply | 主体提取接入生成路径 | 2026-09-07 完成归档；persist 前自动 Apply；≠R3 |
| merchant-mp-c-balance-http-b4 | 商家平台 MP-C B4 余额 HTTP | 2026-09-07 完成归档；商家/Op 只读+调账；≠R5 |
| eval-skills | Agent 质量 | 阻塞；保留候选与冻结 A/B |
| eval-labels / eval-state-live | Agent 质量证据 | 阻塞 |
| eval-production-mine | Agent 质量 | 阻塞（无生产商户） |
| eval-development-baseline | Agent 自进化启动输入 | 阻塞；见任务文件 |
| image-eval-pool | 图片质量池 | 阻塞 |

现有任务不因改组扩权或释放。原开发基线和图片采证待提交内容保持原归属，本次设计不代为验收。新任务仅发布为开放、未分配；未启动自动进化控制器实现或新的真实 provider 批次。

自进化组启动后对固定输入自行运行，不等待 Agent 质量组的日常修复、人工标签或 nightly 全部完成。若其特定验收必须使用尚不存在的材料，该次实验明确停止；可执行性与泛化/生产可用性分别给结论，不降低门槛消除依赖。

## 2026-09-05 重划依据

原四组将普通 Skill 修复与自进化研发合并，又把 Agent 与图片两条独立评测链合并，产生跨组串行依赖和无关输入等待。本次调整：

- 原 Agent 能力中的普通修复并入 Agent 质量，与测量校正按不同任务串行完成。
- 原 Self-Harness 留为 Agent 自进化，负责完整自动改进流程，正常路径只在最终版本发生一次人工审批。
- 图片质量从原评测拆出，保留原 IMG 合同和运行证据，不与 Agent 分数合并。
- 工作流体验与平台可靠性保留完整跨层交付范围；已有关闭任务、证据与历史 commit 不重写。
- 旧 P3–P7 与后加 S0–S6 不再作为双重未来计划；已实现 P1/P2/P2b 保留，自动挖掘、提案和验证仍是核心交付，Steer、记忆、无人工发布与元进化按实际收益另行立项。

以下旧归属只解释归档文件，不接受新任务：

| 历史组 | 原父账本 | 状态 |
|---|---|---|
| Agent 能力 | [agent-self-harness.md](agent-self-harness.md) | 普通修复归 Agent 质量，自进化归 Agent 自进化 |
| 评测 | [agent-eval-system.md](agent-eval-system.md) | Agent 质量与图片质量分别承接 |
| 性能 | [performance-governance.md](performance-governance.md) | 已并入平台可靠性 |

## 文档与外部 Issue

稳定事实按 [文档所有权](../README.md) 写回活文档；未实现方向由 [ROADMAP](../ROADMAP.md) 索引。历史架构来源与已关闭运行时所有权证据留在 [现有时间线](../history/agent-runtime-timeline.md)，不重新开组。GitHub 产品需求与本地执行任务的关系见 [issue tracker](../agents/issue-tracker.md)。

建设这些机制的编码 Agent 仍遵守任务认领、自动协调和代码审核规则；这些开发流程不等于未来进化系统每轮需要用户介入。系统最终进化批准由用户作出，协调者不能代签。
